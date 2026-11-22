package placementcost

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"runtime"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// EdgeLengthSession answers EdgeLength and GraphEdgeCrossings for one graph
// over and over while a caller moves nodes, evaluates, and moves them back
// (swap, equidistance and similar trial loops). It returns exactly what
// EdgeLength and GraphEdgeCrossings return, with the same graph-cache lookups
// and stores, but on a cache miss it re-evaluates only the node terms that can
// depend on geometry that changed since its base evaluation.
//
// Contract: between calls only node geometry (TopLeft, Width, Height), herd
// fence values and the graph's edge-length cache may change. Everything else
// EdgeLength reads must stay as it was when the session first evaluated: the
// node and edge lists, edge attributes, containers, clusters (members,
// abductions), sequences, nears, common uncle siblings, directions, cell size
// and routing costs. Each call re-checks the node and edge lists, the cell
// size, the routing costs and the cluster arrangements, and starts over when
// they differ. A session belongs to one goroutine.
//
// How a term is known to be unchanged: every node term is the sum of
// NodeEdgeLength, ColumnCrossingCost and the node's symmetry. What those read
// is, apart from obstacle scans, a set of nodes fixed by the topology (the
// node, its edges' endpoints and their abduction replacements, cluster vessels
// and the clusters' external nodes, tables adjacent to the far endpoint,
// nears, uncle siblings; for symmetry the node, its neighbours and their
// vessels). Obstacle scans read the children of containers; each scan is
// recorded with its rejection bounds, and a moved child only matters to it
// unless the bounds exclude the child both before and after the move (the
// scan then skips it at its first test either way). Symmetry results that a
// node's symmetry uses (its children's and its neighbours') are cached per
// node with the same rules and recorded as dependencies. A term none of whose
// inputs changed is computed by the same code from the same values, so its
// stored value is the value EdgeLength would compute. Recomputed terms use the
// same functions; all terms are summed in node order as EdgeLength sums them,
// and crossings are an exact integer count updated from the changed edges.
//
// Contexts without a Done channel observe every polling point, so for them
// every call runs the plain EdgeLength and GraphEdgeCrossings.
type EdgeLengthSession struct {
	graph   *layoutgraph.Graph
	options EdgeLengthOptions

	// unusable is set for graphs the session does not handle; every call then
	// runs the plain functions.
	unusable bool

	// Topology, valid while topoOK and topologyUnchanged.
	topoOK      bool
	nodes       []*layoutgraph.Node // graph.Nodes; term t is nodes[t], slot t
	edges       []*layoutgraph.Edge // graph.Edges
	tracked     []*layoutgraph.Node // slot -> node
	slots       map[*layoutgraph.Node]int32
	cellSize    float64
	costs       layoutgraph.RoutingCostState
	clusters    []sessionCluster
	numClusters int
	keyIDs      map[*layoutgraph.Node]int32 // container key (nil is the root) -> id
	nelReads    sessionCSR                  // term -> slots its NodeEdgeLength and ColumnCrossingCost read
	symReads    sessionCSR                  // slot -> slots its symmetry reads directly
	revNel      sessionCSR                  // slot -> terms reading it
	revSym      sessionCSR                  // slot -> slots whose symmetry reads it
	members     sessionCSR                  // slot -> container keys listing it as a child
	memberPos   sessionCSR                  // slot -> its index in the matching members entry's list
	keyLen      []int32                     // container key -> length of its children list
	slotEdges   sessionCSR                  // slot -> edges ending at it
	edgeLevel   []int                       // ContainerLevel of the edge's endpoints, -1 if they differ

	// Base evaluation, valid while baseOK.
	baseOK        bool
	geom          []sessionGeom // slot -> geometry at the base
	nel, ccc      []float64     // term -> NodeEdgeLength, ColumnCrossingCost
	term          []float64     // term -> its EdgeLength term
	baseTotal     float64       // EdgeLength at the base
	symVal        []float64     // symmetry unit (2*slot + checkNeighbors) -> value
	symStamp      []uint32      // == buildStamp when symVal is valid
	buildStamp    uint32
	scans         []sessionScan
	scanKeys      []int32
	keyScans      sessionCSR // container key -> scans over its children
	deps          []sessionDep
	fwdDeps       sessionCSR // symmetry unit -> units whose values it used
	revDeps       sessionCSR // symmetry unit -> units whose value used it
	edgeGroup     []int32    // edge -> crossing group, -1 if not counted
	edgePos       []int32    // edge -> position in its group
	groups        [][]int32  // crossing groups of edge indices, in graph.Edges order
	centers       []edgeCenters
	baseCross     []int64 // edge -> crossings with the other edges of its group at the base
	baseCrossings int64
	hashPrefix    []stateHash // cache-key state before node t (the flags and costs first)

	// The last updated crossing count and the changes it was computed for.
	memoOK      bool
	memoChanged []int32
	memoGeom    []sessionGeom
	memoCount   int64

	// Evaluation state.
	building  bool
	cur       int32 // owner of recorded reads: unit >= 0, NodeEdgeLength of term -cur-1
	stamp     uint32
	nelDirty  []uint32 // term -> stamp when its NodeEdgeLength must be recomputed
	symDirty  []uint32 // unit -> stamp when it may differ from the base
	symDirect []uint32 // unit -> stamp when one of its own reads changed
	ovVal     []float64
	ovStamp   []uint32 // unit -> stamp when ovVal holds this evaluation's value
	slotMark  []uint32 // slot -> stamp when its geometry changed
	edgeMark  []uint32 // edge -> stamp when an endpoint's geometry changed
	changed   []int32
	work      []int32
	chEdges   []int32
	curCenter []edgeCenters
	levels    map[int]int32
}

type sessionCluster struct {
	cluster     *layoutgraph.Cluster
	arrangement layoutgraph.ClusterArrangement
}

type sessionScan struct {
	bounds           obstructionBounds
	owner            int32
	keyStart, keyEnd int32
	// reached is how many obstacles of the concatenated lists the scan
	// reached; -1 for a scan that always reaches them all.
	reached int32
}

type sessionDep struct{ from, to int32 }

// sessionGeom is the geometry of a node that scoring reads.
type sessionGeom struct {
	x, y, width, height float64
	hasTopLeft          bool
	herd                *layoutgraph.HerdAssignment
	herdVal             float64
	herdSide            geo.Orientation
}

const noSessionOwner = math.MinInt32

// NewEdgeLengthSession starts a session for graph with fixed options. Only
// options without explicit abductions that include node sizes are evaluated
// incrementally; other options always use the plain functions.
func NewEdgeLengthSession(graph *layoutgraph.Graph, options EdgeLengthOptions) *EdgeLengthSession {
	return &EdgeLengthSession{
		graph:    graph,
		options:  options,
		unusable: graph == nil || options.EdgeAbductions != nil || !options.IncludeNodeSizes,
	}
}

// Close releases the session's references to the graph.
func (s *EdgeLengthSession) Close() {
	*s = EdgeLengthSession{unusable: true}
}

// EdgeLength returns EdgeLength(ctx, graph, options) for the graph's current
// state.
func (s *EdgeLengthSession) EdgeLength(ctx context.Context) (float64, error) {
	return s.edgeLength(ctx, false)
}

// EdgeLengthBase is EdgeLength that also makes the current state the base
// that later calls compare against. Callers use it for the state they return
// to between trials.
func (s *EdgeLengthSession) EdgeLengthBase(ctx context.Context) (float64, error) {
	return s.edgeLength(ctx, true)
}

func (s *EdgeLengthSession) edgeLength(ctx context.Context, rebase bool) (float64, error) {
	graph := s.graph
	if s.unusable || ctx.Done() == nil || len(graph.Nodes) == 0 {
		return EdgeLength(ctx, graph, s.options)
	}
	p := newScoringPoll(ctx)
	if err := p.entry(); err != nil {
		return 0, err
	}
	costs := graph.RoutingCosts()
	if costs.Turn == 0 || costs.Crossing == 0 {
		// An evaluation that initializes the routing costs from the current
		// geometry: leave it to EdgeLength.
		return EdgeLength(ctx, graph, s.options)
	}
	// With a usable base, s.changed lists what differs from it, and the
	// cache key is resumed from the first changed node.
	diffed := s.baseOK && s.topologyUnchanged()
	if diffed {
		s.nextStamp()
		diffed = s.diffGeometry()
	}
	var state uint64
	if diffed {
		state = s.resumedState()
		if edgeLengthSessionVerify && state != placementEdgeLengthState(graph, costs, s.options) {
			panic("EdgeLengthSession: resumed cache key differs from placementEdgeLengthState")
		}
	} else {
		state = placementEdgeLengthState(graph, costs, s.options)
		if state == invalidEdgeLengthState {
			return EdgeLength(ctx, graph, s.options)
		}
	}
	if d, ok := graph.LookupEdgeLengthCost(state); ok {
		if rebase {
			// The result is the cached value whatever happens here.
			if _, handled, err := s.evaluate(ctx, &p, true, diffed); err != nil || !handled {
				s.baseOK = false
			}
		}
		return d, nil
	}
	total, handled, err := s.evaluate(ctx, &p, rebase, diffed)
	if err != nil {
		return 0, err
	}
	if !handled {
		return EdgeLength(ctx, graph, s.options)
	}
	if err := p.check(); err != nil {
		return 0, err
	}
	if edgeLengthSessionVerify {
		verifySessionEdgeLength(graph, s.options, total)
	}
	graph.StoreEdgeLengthCost(state, total)
	return total, nil
}

// GraphEdgeCrossings returns GraphEdgeCrossings(ctx, graph) for the graph's
// current state.
func (s *EdgeLengthSession) GraphEdgeCrossings(ctx context.Context) (int64, error) {
	if s.unusable || !s.baseOK || ctx.Done() == nil || !s.topologyUnchanged() {
		return GraphEdgeCrossings(ctx, s.graph)
	}
	p := newScoringPoll(ctx)
	if err := p.entry(); err != nil {
		return 0, err
	}
	s.nextStamp()
	if !s.diffGeometry() {
		return GraphEdgeCrossings(ctx, s.graph)
	}
	crossings, ok := s.memoCrossings()
	if !ok {
		crossings = s.updatedCrossings()
	}
	if err := p.check(); err != nil {
		return 0, err
	}
	if edgeLengthSessionVerify {
		verifySessionCrossings(s.graph, crossings)
	}
	return crossings, nil
}

// edgeLengthSessionVerify makes every incremental result also be computed by
// the plain functions, and panics if they differ. Tests set it.
var edgeLengthSessionVerify bool

func verifySessionEdgeLength(graph *layoutgraph.Graph, options EdgeLengthOptions, got float64) {
	ctx := context.Background()
	p := newScoringPoll(ctx)
	want, err := sumNodeEdgeLengths(ctx, &p, graph, options)
	if err != nil {
		panic(err)
	}
	crossings, err := GraphEdgeCrossings(ctx, graph)
	if err != nil {
		panic(err)
	}
	want = addCrossingCost(want, graph, crossings)
	if math.Float64bits(want) != math.Float64bits(got) {
		panic(fmt.Sprintf("EdgeLengthSession: EdgeLength %v (%#x), plain evaluation %v (%#x)", got, math.Float64bits(got), want, math.Float64bits(want)))
	}
}

func verifySessionCrossings(graph *layoutgraph.Graph, got int64) {
	want, err := GraphEdgeCrossings(context.Background(), graph)
	if err != nil {
		panic(err)
	}
	if want != got {
		panic(fmt.Sprintf("EdgeLengthSession: GraphEdgeCrossings %d, plain count %d", got, want))
	}
}

// evaluate computes EdgeLength for the current state (with the crossing
// cost). handled is false when the session cannot evaluate this graph.
// diffed reports that s.changed already holds the differences from a valid
// base.
func (s *EdgeLengthSession) evaluate(ctx context.Context, p *scoringPoll, rebase, diffed bool) (total float64, handled bool, err error) {
	if !diffed {
		if !s.topoOK || !s.topologyUnchanged() {
			s.topoOK, s.baseOK = false, false
			ok, err := s.buildTopology(p)
			if err != nil || !ok {
				return 0, ok, err
			}
			s.topoOK = true
		}
		if s.baseOK {
			s.nextStamp()
			// Otherwise a node gained or lost its TopLeft; start over.
			diffed = s.diffGeometry()
			s.baseOK = diffed
		}
	}
	if diffed {
		if len(s.changed) == 0 {
			return s.baseTotal, true, nil
		}
		if !rebase {
			total, err := s.trial(ctx, p)
			return total, err == nil, err
		}
	}
	total, err = s.build(ctx, p)
	if err != nil || s.unusable {
		s.baseOK = false
		return 0, !s.unusable, err
	}
	return total, true, nil
}

func (s *EdgeLengthSession) nextStamp() {
	s.stamp++
	if s.stamp == 0 {
		clear(s.nelDirty)
		clear(s.symDirty)
		clear(s.symDirect)
		clear(s.ovStamp)
		clear(s.slotMark)
		clear(s.edgeMark)
		s.stamp = 1
	}
}

// topologyUnchanged reports whether the cheap parts of the session's
// contract still hold.
func (s *EdgeLengthSession) topologyUnchanged() bool {
	g := s.graph
	if !s.topoOK || len(g.Nodes) != len(s.nodes) || len(g.Edges) != len(s.edges) || len(g.Clusters) != s.numClusters {
		return false
	}
	for i, n := range g.Nodes {
		if n != s.nodes[i] {
			return false
		}
	}
	for i, e := range g.Edges {
		if e != s.edges[i] {
			return false
		}
	}
	costs := g.RoutingCosts()
	if !sameFloatBits(g.CellSize, s.cellSize) || !sameFloatBits(costs.Turn, s.costs.Turn) ||
		!sameFloatBits(costs.Crossing, s.costs.Crossing) || !sameFloatBits(costs.NonCenterPort, s.costs.NonCenterPort) {
		return false
	}
	for _, c := range s.clusters {
		if c.cluster.Arrangement != c.arrangement {
			return false
		}
	}
	return true
}

func sameFloatBits(a, b float64) bool { return math.Float64bits(a) == math.Float64bits(b) }

func captureSessionGeom(n *layoutgraph.Node) sessionGeom {
	geom := sessionGeom{width: n.Width, height: n.Height, herd: n.HerdAssignment}
	if n.TopLeft != nil {
		geom.x, geom.y, geom.hasTopLeft = n.TopLeft.X, n.TopLeft.Y, true
	}
	if n.HerdAssignment != nil {
		geom.herdVal, geom.herdSide = n.HerdAssignment.Val, n.HerdAssignment.Orientation
	}
	return geom
}

func (geom *sessionGeom) matches(n *layoutgraph.Node) bool {
	if n.TopLeft == nil {
		if geom.hasTopLeft {
			return false
		}
	} else if !geom.hasTopLeft || !sameFloatBits(geom.x, n.TopLeft.X) || !sameFloatBits(geom.y, n.TopLeft.Y) {
		return false
	}
	if !sameFloatBits(geom.width, n.Width) || !sameFloatBits(geom.height, n.Height) || geom.herd != n.HerdAssignment {
		return false
	}
	return n.HerdAssignment == nil ||
		(sameFloatBits(geom.herdVal, n.HerdAssignment.Val) && geom.herdSide == n.HerdAssignment.Orientation)
}

// diffGeometry collects the slots whose geometry differs from the base into
// s.changed (marked with the current stamp). It returns false if a node
// gained or lost its TopLeft.
func (s *EdgeLengthSession) diffGeometry() bool {
	s.changed = s.changed[:0]
	ok := true
	for slot, n := range s.tracked {
		geom := &s.geom[slot]
		if geom.matches(n) {
			continue
		}
		if geom.hasTopLeft != (n.TopLeft != nil) {
			ok = false
		}
		s.changed = append(s.changed, int32(slot))
		s.slotMark[slot] = s.stamp
	}
	return ok
}

// sessionCSR is a compressed list of lists.
type sessionCSR struct {
	start []int32
	items []int32
}

func (c *sessionCSR) of(i int32) []int32 {
	return c.items[c.start[i]:c.start[i+1]]
}

// buildFromPairs fills c with, for each key in [0, keys), the values of the
// pairs with that key, in pair order.
func (c *sessionCSR) buildFromPairs(keys int, pairs []sessionDep) {
	c.start = slices.Grow(c.start[:0], keys+1)[:keys+1]
	clear(c.start)
	for _, pair := range pairs {
		c.start[pair.from+1]++
	}
	for i := 1; i <= keys; i++ {
		c.start[i] += c.start[i-1]
	}
	c.items = slices.Grow(c.items[:0], len(pairs))[:len(pairs)]
	next := slices.Clone(c.start[:keys])
	for _, pair := range pairs {
		c.items[next[pair.from]] = pair.to
		next[pair.from]++
	}
}

func (s *EdgeLengthSession) track(n *layoutgraph.Node) {
	if n == nil {
		return
	}
	if _, ok := s.slots[n]; ok {
		return
	}
	s.slots[n] = int32(len(s.tracked))
	s.tracked = append(s.tracked, n)
}

func sortedNodesByID(nodes []*layoutgraph.Node) []*layoutgraph.Node {
	slices.SortFunc(nodes, func(a, b *layoutgraph.Node) int {
		if a == nil || b == nil {
			switch {
			case a == b:
				return 0
			case a == nil:
				return -1
			default:
				return 1
			}
		}
		return cmp.Compare(a.IDValue(), b.IDValue())
	})
	return nodes
}

// buildTopology collects the nodes the session tracks and what each term
// reads apart from obstacle scans. It returns false for graphs the session
// does not handle.
func (s *EdgeLengthSession) buildTopology(p *scoringPoll) (bool, error) {
	g := s.graph
	s.nodes = append(s.nodes[:0], g.Nodes...)
	s.edges = append(s.edges[:0], g.Edges...)
	s.cellSize = g.CellSize
	s.costs = g.RoutingCosts()
	if s.slots == nil {
		s.slots = make(map[*layoutgraph.Node]int32, 2*len(g.Nodes))
	} else {
		clear(s.slots)
	}
	s.tracked = s.tracked[:0]
	for _, n := range g.Nodes {
		if n == nil || n.Graph != g {
			s.unusable = true
			return false, nil
		}
		if _, dup := s.slots[n]; dup {
			s.unusable = true
			return false, nil
		}
		s.track(n)
	}

	// Container keys in a fixed order (by ID; the root first).
	keys := make([]*layoutgraph.Node, 0, len(g.Containers))
	for key := range g.Containers {
		keys = append(keys, key)
	}
	sortedNodesByID(keys)
	if s.keyIDs == nil {
		s.keyIDs = make(map[*layoutgraph.Node]int32, len(keys))
	} else {
		clear(s.keyIDs)
	}
	for i, key := range keys {
		s.keyIDs[key] = int32(i)
		s.track(key)
		for _, child := range g.Containers[key] {
			s.track(child)
		}
	}

	// Every cluster's and sequence's nodes, in a fixed order.
	vessels := make([]*layoutgraph.Node, 0, len(g.Clusters)+len(g.Sequences))
	for vessel := range g.Clusters {
		vessels = append(vessels, vessel)
	}
	for _, vessel := range sortedNodesByID(vessels) {
		s.track(vessel)
		if c := g.Clusters[vessel]; c != nil {
			s.track(c.Vessel)
			for _, member := range c.Nodes {
				s.track(member)
			}
			for _, ea := range c.EdgeAbductions {
				s.track(ea.CurrentFrom)
				s.track(ea.CurrentTo)
				s.track(ea.OriginallyFrom)
				s.track(ea.OriginallyTo)
			}
		}
	}
	vessels = vessels[:0]
	for vessel := range g.Sequences {
		vessels = append(vessels, vessel)
	}
	for _, vessel := range sortedNodesByID(vessels) {
		s.track(vessel)
		if sequence := g.Sequences[vessel]; sequence != nil {
			s.track(sequence.Vessel)
			for _, member := range sequence.Nodes {
				s.track(member)
			}
		}
	}

	// Close the tracked set over everything a term can reach.
	var nears []*layoutgraph.Node
	for i := 0; i < len(s.tracked); i++ {
		n := s.tracked[i]
		for _, e := range n.Edges {
			s.track(n.Adjacent(e))
			s.track(e.From)
			s.track(e.To)
		}
		s.track(n.Container)
		if n.Cluster != nil {
			s.track(n.Cluster.Vessel)
			for _, member := range n.Cluster.Nodes {
				s.track(member)
			}
			for _, ea := range n.Cluster.EdgeAbductions {
				s.track(ea.CurrentFrom)
				s.track(ea.CurrentTo)
				s.track(ea.OriginallyFrom)
				s.track(ea.OriginallyTo)
			}
		}
		if n.Sequence != nil {
			s.track(n.Sequence.Vessel)
			for _, member := range n.Sequence.Nodes {
				s.track(member)
			}
		}
		for _, child := range g.Containers[n] {
			s.track(child)
		}
		nears = nears[:0]
		for near := range n.Nears {
			nears = append(nears, near)
		}
		for _, near := range sortedNodesByID(nears) {
			s.track(near)
		}
		for _, sibling := range g.CommonUncleSiblings[n] {
			s.track(sibling)
		}
	}
	tracked := len(s.tracked)

	// Cluster arrangements are read by the terms and checked on every call.
	s.clusters = s.clusters[:0]
	vessels = vessels[:0]
	for vessel := range g.Clusters {
		vessels = append(vessels, vessel)
	}
	for _, vessel := range sortedNodesByID(vessels) {
		if c := g.Clusters[vessel]; c != nil {
			s.clusters = append(s.clusters, sessionCluster{cluster: c, arrangement: c.Arrangement})
		}
	}
	s.numClusters = len(g.Clusters)

	// What each term's NodeEdgeLength and ColumnCrossingCost read.
	mark := make([]int32, tracked)
	for i := range mark {
		mark[i] = -1
	}
	var pairs []sessionDep
	untracked := false
	for t, n := range s.nodes {
		owner := int32(t)
		add := func(read *layoutgraph.Node) {
			if read == nil {
				return
			}
			slot, ok := s.slots[read]
			if !ok {
				untracked = true
				return
			}
			if mark[slot] != owner {
				mark[slot] = owner
				pairs = append(pairs, sessionDep{from: owner, to: slot})
			}
		}
		// prepareNodeEdgeLength expects cleared scratch.
		scratch := scratchPool.Get()
		if _, err := prepareNodeEdgeLength(p, n, s.options, scratch); err != nil {
			putEdgeScratch(scratch)
			return false, err
		}
		add(n)
		for i, e := range n.Edges {
			add(n.Adjacent(e))
			add(e.From)
			add(e.To)
			for _, r := range [2]*layoutgraph.Node{scratch.nRepl[i], scratch.aRepl[i]} {
				add(r)
				if r.Cluster != nil {
					add(r.Cluster.Vessel)
					for _, ea := range r.Cluster.EdgeAbductions {
						add(ea.CurrentFrom)
						add(ea.CurrentTo)
					}
				}
			}
			// The column-crossing check reads the tables the far endpoint
			// connects to through table columns, for column edges only.
			if e.IsBetweenTableColumns() {
				far := scratch.aRepl[i]
				for _, e2 := range far.Edges {
					if e2.IsBetweenTableColumns() {
						add(far.Adjacent(e2))
					}
				}
			}
		}
		for near := range n.Nears {
			add(near)
		}
		for _, sibling := range g.CommonUncleSiblings[n] {
			add(sibling)
		}
		putEdgeScratch(scratch)
		if untracked {
			// A read outside the closure; cannot happen, but never guess.
			s.unusable = true
			return false, nil
		}
	}
	s.nelReads.buildFromPairs(len(s.nodes), pairs)
	for i := range pairs {
		pairs[i].from, pairs[i].to = pairs[i].to, pairs[i].from
	}
	s.revNel.buildFromPairs(tracked, pairs)

	// What each node's own symmetry reads (abductions are nil here).
	pairs = pairs[:0]
	for i := range mark {
		mark[i] = -1
	}
	for slot, n := range s.tracked {
		owner := int32(slot)
		add := func(read *layoutgraph.Node) {
			if read == nil {
				return
			}
			readSlot, ok := s.slots[read]
			if !ok {
				untracked = true
				return
			}
			if mark[readSlot] != owner {
				mark[readSlot] = owner
				pairs = append(pairs, sessionDep{from: owner, to: readSlot})
			}
		}
		add(n)
		for _, e := range n.Edges {
			adjacent := n.Adjacent(e)
			add(adjacent)
			if adjacent.Cluster.IsActive() {
				add(adjacent.Cluster.Vessel)
			} else if adjacent.Sequence.IsActive() {
				add(adjacent.Sequence.Vessel)
			}
		}
	}
	if untracked {
		s.unusable = true
		return false, nil
	}
	s.symReads.buildFromPairs(tracked, pairs)
	for i := range pairs {
		pairs[i].from, pairs[i].to = pairs[i].to, pairs[i].from
	}
	s.revSym.buildFromPairs(tracked, pairs)

	// Which container lists each node is in, and where.
	pairs = pairs[:0]
	positions := make([]sessionDep, 0, len(s.tracked))
	s.keyLen = slices.Grow(s.keyLen[:0], len(keys))[:len(keys)]
	for _, key := range keys {
		id := s.keyIDs[key]
		s.keyLen[id] = int32(len(g.Containers[key]))
		for index, child := range g.Containers[key] {
			if child != nil {
				pairs = append(pairs, sessionDep{from: s.slots[child], to: id})
				positions = append(positions, sessionDep{from: s.slots[child], to: int32(index)})
			}
		}
	}
	s.members.buildFromPairs(tracked, pairs)
	s.memberPos.buildFromPairs(tracked, positions)

	// Edges for the crossing count: endpoint slots and container levels.
	pairs = pairs[:0]
	s.edgeLevel = slices.Grow(s.edgeLevel[:0], len(s.edges))[:len(s.edges)]
	for i, e := range s.edges {
		s.edgeLevel[i] = -1
		if e == nil {
			// GraphEdgeCrossings fails on it; keep its behaviour.
			s.unusable = true
			return false, nil
		}
		if e.From == nil || e.To == nil {
			continue
		}
		from, okFrom := s.slots[e.From]
		to, okTo := s.slots[e.To]
		if !okFrom || !okTo {
			s.unusable = true
			return false, nil
		}
		pairs = append(pairs, sessionDep{from: from, to: int32(i)})
		if to != from {
			pairs = append(pairs, sessionDep{from: to, to: int32(i)})
		}
		if fromLevel := e.From.ContainerLevel(); fromLevel == e.To.ContainerLevel() {
			s.edgeLevel[i] = fromLevel
		}
	}
	s.slotEdges.buildFromPairs(tracked, pairs)

	units := 2 * tracked
	s.geom = slices.Grow(s.geom[:0], tracked)[:tracked]
	s.nel = slices.Grow(s.nel[:0], len(s.nodes))[:len(s.nodes)]
	s.ccc = slices.Grow(s.ccc[:0], len(s.nodes))[:len(s.nodes)]
	s.term = slices.Grow(s.term[:0], len(s.nodes))[:len(s.nodes)]
	s.nelDirty = make([]uint32, len(s.nodes))
	s.symVal = make([]float64, units)
	s.symStamp = make([]uint32, units)
	s.symDirty = make([]uint32, units)
	s.symDirect = make([]uint32, units)
	s.ovVal = make([]float64, units)
	s.ovStamp = make([]uint32, units)
	s.slotMark = make([]uint32, tracked)
	s.edgeMark = make([]uint32, len(s.edges))
	s.baseCross = make([]int64, len(s.edges))
	s.edgeGroup = slices.Grow(s.edgeGroup[:0], len(s.edges))[:len(s.edges)]
	s.edgePos = slices.Grow(s.edgePos[:0], len(s.edges))[:len(s.edges)]
	s.centers = slices.Grow(s.centers[:0], len(s.edges))[:len(s.edges)]
	s.curCenter = slices.Grow(s.curCenter[:0], len(s.edges))[:len(s.edges)]
	s.buildStamp, s.stamp = 0, 0
	return true, nil
}

// build evaluates every term at the current state, recording what each
// reads, and makes this state the base.
func (s *EdgeLengthSession) build(ctx context.Context, p *scoringPoll) (float64, error) {
	s.baseOK = false
	s.buildStamp++
	if s.buildStamp == 0 {
		clear(s.symStamp)
		s.buildStamp = 1
	}
	for slot, n := range s.tracked {
		s.geom[slot] = captureSessionGeom(n)
	}
	s.memoOK = false
	s.hashPrefix = slices.Grow(s.hashPrefix[:0], len(s.nodes)+1)[:len(s.nodes)+1]
	s.hashPrefix[0] = edgeLengthStateHeader(s.graph, s.graph.RoutingCosts(), s.options)
	for t, n := range s.nodes {
		s.hashPrefix[t+1] = edgeLengthStateNode(s.hashPrefix[t], n)
	}
	s.scans = s.scans[:0]
	s.scanKeys = s.scanKeys[:0]
	s.deps = s.deps[:0]

	s.building = true
	total, err := s.evaluateTerms(ctx, p, true)
	s.building = false
	if err != nil || s.unusable {
		return 0, err
	}
	crossings, err := s.buildCrossings(p)
	if err != nil {
		return 0, err
	}
	total = addCrossingCost(total, s.graph, crossings)

	// Index the recordings.
	scanPairs := make([]sessionDep, 0, len(s.scanKeys))
	for i := range s.scans {
		scan := &s.scans[i]
		for _, key := range s.scanKeys[scan.keyStart:scan.keyEnd] {
			scanPairs = append(scanPairs, sessionDep{from: key, to: int32(i)})
		}
	}
	s.keyScans.buildFromPairs(len(s.keyIDs), scanPairs)
	s.fwdDeps.buildFromPairs(len(s.symVal), s.deps)
	for i := range s.deps {
		s.deps[i].from, s.deps[i].to = s.deps[i].to, s.deps[i].from
	}
	s.revDeps.buildFromPairs(len(s.symVal), s.deps)
	s.baseTotal = total
	s.baseOK = true
	return total, nil
}

// evaluateTerms sums the terms in node order. When building, every term is
// computed and stored; otherwise only dirty terms are recomputed.
func (s *EdgeLengthSession) evaluateTerms(ctx context.Context, p *scoringPoll, building bool) (totalSum float64, err error) {
	// Where EdgeLength evaluated nodes on helpers or sanitized its inline
	// evaluation, a panic was reported only as an invariant failure.
	if edgeLengthWorkerCount(len(s.nodes), runtime.GOMAXPROCS(0)) > 1 {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.baseOK = false
				s.building = false
				panic(edgeLengthWorkerPanic)
			}
		}()
	}
	symmetryCost := s.graph.CellSize
	p.sess = s
	p.symmetry = getSymmetryWorkspace()
	defer func() {
		putSymmetryWorkspace(p.symmetry)
		p.symmetry = nil
		p.sess = nil
		s.building = false
	}()
	for t, n := range s.nodes {
		nelDirty := building || s.nelDirty[t] == s.stamp
		if !nelDirty && s.symDirty[2*t+1] != s.stamp {
			totalSum += s.term[t]
			continue
		}
		nl, columnCrossingCost := s.nel[t], s.ccc[t]
		if nelDirty {
			s.cur = -int32(t) - 1
			nl, err = nodeEdgeLength(p, n, s.options)
			if err != nil {
				return 0, err
			}
			columnCrossingCost, err = ColumnCrossingCost(ctx, n, nil)
			if err != nil {
				return 0, err
			}
		}
		s.cur = noSessionOwner
		symmetry, err := s.symmetry(p, n, nil, true)
		if err != nil {
			return 0, err
		}
		if !nelDirty && sameFloatBits(symmetry, s.symVal[2*t+1]) {
			// Same parts as at the base, so the same term.
			totalSum += s.term[t]
			continue
		}
		value := edgeLengthTermValue(nl, columnCrossingCost, symmetry, symmetryCost, len(n.Edges))
		if building {
			s.nel[t], s.ccc[t], s.term[t] = nl, columnCrossingCost, value
		}
		if err := p.check(); err != nil {
			return 0, err
		}
		totalSum += value
	}
	return totalSum, nil
}

// symmetry is memoNodeSymmetry inside a session evaluation.
func (s *EdgeLengthSession) symmetry(p *scoringPoll, node *layoutgraph.Node, abductions []*layoutgraph.EdgeAbduction, checkNeighbors bool) (float64, error) {
	slot, ok := s.slots[node]
	if !ok || abductions != nil || node.Graph != s.graph {
		if s.building {
			// Its reads would not be tracked.
			s.unusable = true
		}
		return pollNodeSymmetry(p, node, abductions, checkNeighbors)
	}
	unit := 2 * slot
	if checkNeighbors {
		unit++
	}
	if s.building {
		if s.cur >= 0 {
			s.deps = append(s.deps, sessionDep{from: s.cur, to: unit})
		}
		if s.symStamp[unit] == s.buildStamp {
			return s.symVal[unit], nil
		}
		owner := s.cur
		s.cur = unit
		value, err := pollNodeSymmetry(p, node, nil, checkNeighbors)
		s.cur = owner
		if err != nil {
			return 0, err
		}
		s.symVal[unit], s.symStamp[unit] = value, s.buildStamp
		return value, nil
	}
	if s.ovStamp[unit] == s.stamp {
		return s.ovVal[unit], nil
	}
	if s.symStamp[unit] == s.buildStamp {
		if s.symDirty[unit] != s.stamp {
			return s.symVal[unit], nil
		}
		if s.symDirect[unit] != s.stamp {
			// Only units it used may differ. If they all have their base
			// values, it reads the same values and computes its base value.
			unchanged, err := s.depsUnchanged(p, unit)
			if err != nil {
				return 0, err
			}
			if unchanged {
				s.ovVal[unit], s.ovStamp[unit] = s.symVal[unit], s.stamp
				return s.symVal[unit], nil
			}
		}
	}
	value, err := pollNodeSymmetry(p, node, nil, checkNeighbors)
	if err != nil {
		return 0, err
	}
	s.ovVal[unit], s.ovStamp[unit] = value, s.stamp
	return value, nil
}

// depsUnchanged reports whether every symmetry unit that unit used at the
// base has its base value (bit for bit) in the current state, evaluating the
// ones that may differ.
func (s *EdgeLengthSession) depsUnchanged(p *scoringPoll, unit int32) (bool, error) {
	for _, dep := range s.fwdDeps.of(unit) {
		if s.symDirty[dep] != s.stamp {
			continue
		}
		value, err := s.symmetry(p, s.tracked[dep/2], nil, dep%2 == 1)
		if err != nil {
			return false, err
		}
		if !sameFloatBits(value, s.symVal[dep]) {
			return false, nil
		}
	}
	return true, nil
}

// recordScan records an obstacle scan of the NodeEdgeLength being built: its
// bounds, the containers whose children it scanned in order, and how many of
// those obstacles it reached before it stopped.
func (s *EdgeLengthSession) recordScan(bounds obstructionBounds, keys []*layoutgraph.Node, reached int) {
	if !s.building {
		return
	}
	if s.cur >= 0 || s.cur == noSessionOwner {
		s.unusable = true
		return
	}
	start := int32(len(s.scanKeys))
	for _, key := range keys {
		if id, ok := s.keyIDs[key]; ok {
			s.scanKeys = append(s.scanKeys, id)
		}
	}
	if int32(len(s.scanKeys)) > start {
		s.scans = append(s.scans, sessionScan{bounds: bounds, owner: s.cur, keyStart: start, keyEnd: int32(len(s.scanKeys)), reached: int32(reached)})
	}
}

// recordSymmetryScan records the sibling scan obstructed(center, a, b, ...)
// performs for the symmetry unit being built.
func (s *EdgeLengthSession) recordSymmetryScan(center, a, b *layoutgraph.Node) {
	if !s.building {
		return
	}
	if s.cur < 0 {
		s.unusable = true
		return
	}
	bounds := scoringNodeBounds(center).including(scoringNodeBounds(a)).including(scoringNodeBounds(b))
	start := int32(len(s.scanKeys))
	if id, ok := s.keyIDs[center.Container]; ok {
		s.scanKeys = append(s.scanKeys, id)
	}
	if a.Container != center.Container {
		if id, ok := s.keyIDs[a.Container]; ok {
			s.scanKeys = append(s.scanKeys, id)
		}
	}
	if int32(len(s.scanKeys)) > start {
		s.scans = append(s.scans, sessionScan{bounds: bounds, owner: s.cur, keyStart: start, keyEnd: int32(len(s.scanKeys)), reached: -1})
	}
}

// scanPosition is the position, in scan's concatenated container lists, of
// the child at index position of key's list.
func (s *EdgeLengthSession) scanPosition(scan *sessionScan, key, position int32) int32 {
	offset := int32(0)
	for _, scanned := range s.scanKeys[scan.keyStart:scan.keyEnd] {
		if scanned == key {
			break
		}
		offset += s.keyLen[scanned]
	}
	return offset + position
}

// markDirect marks a unit whose own reads changed (and so may differ).
func (s *EdgeLengthSession) markDirect(unit int32) {
	s.symDirect[unit] = s.stamp
	s.markUnit(unit)
}

func (s *EdgeLengthSession) markUnit(unit int32) {
	if s.symDirty[unit] != s.stamp {
		s.symDirty[unit] = s.stamp
		s.work = append(s.work, unit)
	}
}

func (s *EdgeLengthSession) ownerDirty(owner int32) bool {
	if owner < 0 {
		return s.nelDirty[-owner-1] == s.stamp
	}
	return s.symDirect[owner] == s.stamp
}

// trial evaluates the current state against the base without changing it.
// s.changed holds the slots whose geometry changed.
func (s *EdgeLengthSession) trial(ctx context.Context, p *scoringPoll) (float64, error) {
	s.work = s.work[:0]
	for _, slot := range s.changed {
		for _, t := range s.revNel.of(slot) {
			s.nelDirty[t] = s.stamp
		}
		for _, owner := range s.revSym.of(slot) {
			s.markDirect(2 * owner)
			s.markDirect(2*owner + 1)
		}
		old := &s.geom[slot]
		node := s.tracked[slot]
		positions := s.memberPos.of(slot)
		for m, key := range s.members.of(slot) {
			for _, scanIndex := range s.keyScans.of(key) {
				scan := &s.scans[scanIndex]
				if s.ownerDirty(scan.owner) {
					continue
				}
				if scan.reached >= 0 && s.scanPosition(scan, key, positions[m]) >= scan.reached {
					// The scan stopped before reaching this node; while
					// everything it reached behaves as before it stops there
					// again (any change to those makes the owner dirty).
					continue
				}
				if scan.bounds.excludesGeometry(old.hasTopLeft, old.x, old.y, old.width, old.height) && scan.bounds.excludes(node) {
					continue
				}
				if scan.owner < 0 {
					s.nelDirty[-scan.owner-1] = s.stamp
				} else {
					s.markDirect(scan.owner)
				}
			}
		}
	}
	for i := 0; i < len(s.work); i++ {
		for _, dependent := range s.revDeps.of(s.work[i]) {
			s.markUnit(dependent)
		}
	}
	total, err := s.evaluateTerms(ctx, p, false)
	if err != nil {
		return 0, err
	}
	return addCrossingCost(total, s.graph, s.updatedCrossings()), nil
}

// buildCrossings counts crossings as GraphEdgeCrossings does and keeps what
// updatedCrossings needs.
func (s *EdgeLengthSession) buildCrossings(p *scoringPoll) (int64, error) {
	if s.levels == nil {
		s.levels = make(map[int]int32)
	} else {
		clear(s.levels)
	}
	for i := range s.groups {
		s.groups[i] = s.groups[i][:0]
	}
	groups := 0
	for i, edge := range s.edges {
		if err := p.at(i); err != nil {
			return 0, err
		}
		s.edgeGroup[i] = -1
		if s.edgeLevel[i] < 0 || edge.From.TopLeft == nil || edge.To.TopLeft == nil {
			continue
		}
		group, ok := s.levels[s.edgeLevel[i]]
		if !ok {
			group = int32(groups)
			groups++
			s.levels[s.edgeLevel[i]] = group
			if int(group) == len(s.groups) {
				s.groups = append(s.groups, nil)
			}
		}
		s.edgeGroup[i] = group
		s.edgePos[i] = int32(len(s.groups[group]))
		s.groups[group] = append(s.groups[group], int32(i))
		s.centers[i] = edgeCenters{from: *edge.From.Center(), to: *edge.To.Center()}
	}
	s.groups = s.groups[:groups]
	clear(s.baseCross)
	var crossings int64
	for _, group := range s.groups {
		for a := 0; a < len(group)-1; a++ {
			if err := p.at(a); err != nil {
				return 0, err
			}
			first := group[a]
			edge1 := s.edges[first]
			for b := a + 1; b < len(group); b++ {
				second := group[b]
				edge2 := s.edges[second]
				if edge1.From == edge2.From || edge1.From == edge2.To ||
					edge1.To == edge2.From || edge1.To == edge2.To {
					continue
				}
				if layoutgraph.SegmentsCross(&s.centers[first].from, &s.centers[first].to, &s.centers[second].from, &s.centers[second].to) {
					crossings++
					s.baseCross[first]++
					s.baseCross[second]++
				}
			}
		}
	}
	s.baseCrossings = crossings
	return crossings, nil
}

// updatedCrossings is the crossing count of the current state, from the base
// count and the pairs of counted edges at least one of which has a changed
// endpoint (s.changed, s.slotMark). Node TopLeft presence is unchanged, so
// the same edges are counted in the same groups. With C the changed edges,
// the pairs touching C crossed sum(baseCross[c]) - (pairs inside C that
// crossed) times at the base; their current crossings are counted directly.
// Every pair is tested with its edges in graph order, as GraphEdgeCrossings
// tests it.
func (s *EdgeLengthSession) updatedCrossings() int64 {
	s.chEdges = s.chEdges[:0]
	for _, slot := range s.changed {
		for _, edge := range s.slotEdges.of(slot) {
			if s.edgeGroup[edge] < 0 || s.edgeMark[edge] == s.stamp {
				continue
			}
			s.edgeMark[edge] = s.stamp
			s.chEdges = append(s.chEdges, edge)
			e := s.edges[edge]
			s.curCenter[edge] = edgeCenters{from: *e.From.Center(), to: *e.To.Center()}
		}
	}
	crossings := s.baseCrossings
	for _, changed := range s.chEdges {
		crossings -= s.baseCross[changed]
		group := s.groups[s.edgeGroup[changed]]
		position := s.edgePos[changed]
		for otherPosition, other := range group {
			otherChanged := s.edgeMark[other] == s.stamp
			if other == changed || (otherChanged && int32(otherPosition) < position) {
				continue
			}
			first, second := changed, other
			if int32(otherPosition) < position {
				first, second = other, changed
			}
			edge1, edge2 := s.edges[first], s.edges[second]
			if edge1.From == edge2.From || edge1.From == edge2.To ||
				edge1.To == edge2.From || edge1.To == edge2.To {
				continue
			}
			if otherChanged && layoutgraph.SegmentsCross(&s.centers[first].from, &s.centers[first].to, &s.centers[second].from, &s.centers[second].to) {
				// Counted in both edges' baseCross.
				crossings++
			}
			now1, now2 := &s.centers[first], &s.centers[second]
			if s.edgeMark[first] == s.stamp {
				now1 = &s.curCenter[first]
			}
			if s.edgeMark[second] == s.stamp {
				now2 = &s.curCenter[second]
			}
			if layoutgraph.SegmentsCross(&now1.from, &now1.to, &now2.from, &now2.to) {
				crossings++
			}
		}
	}
	s.memoOK = true
	s.memoChanged = append(s.memoChanged[:0], s.changed...)
	s.memoGeom = s.memoGeom[:0]
	for _, slot := range s.changed {
		s.memoGeom = append(s.memoGeom, captureSessionGeom(s.tracked[slot]))
	}
	s.memoCount = crossings
	return crossings
}

// memoCrossings returns the last updated count if it was computed for the
// current state: the same nodes differ from the base, with the same geometry.
func (s *EdgeLengthSession) memoCrossings() (int64, bool) {
	if !s.memoOK || len(s.memoChanged) != len(s.changed) {
		return 0, false
	}
	for i, slot := range s.changed {
		if s.memoChanged[i] != slot || !s.memoGeom[i].matches(s.tracked[slot]) {
			return 0, false
		}
	}
	return s.memoCount, true
}

// resumedState is placementEdgeLengthState for the current state: the nodes
// before the first changed one hash as at the base.
func (s *EdgeLengthSession) resumedState() uint64 {
	first := len(s.nodes)
	if len(s.changed) > 0 && int(s.changed[0]) < first {
		first = int(s.changed[0])
	}
	h := s.hashPrefix[first]
	for _, n := range s.nodes[first:] {
		h = edgeLengthStateNode(h, n)
	}
	return uint64(h)
}
