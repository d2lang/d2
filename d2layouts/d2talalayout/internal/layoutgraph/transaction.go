package layoutgraph

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/xsort"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/invariant"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/typedpool"
)

type GraphState struct {
	// slots is the transaction's deduplicated node set with each node's
	// geometry at capture time, in recording order.
	slots snapshotSlots
	// captureID changes with every capture into this state (states are
	// recycled), so derived data can tell whether it is still current.
	captureID uint64
	// originalGeometry[i] is the geometry of originalNodes[i] at capture time
	// (present is false for a nil entry). While g.Nodes still equals
	// originalNodes element for element it answers, by index, whether a
	// node's geometry changed (geometryChangedAt).
	originalGeometry []indexedGeometrySnapshot
	originalNodes    []*Node
	originalNodesRef []*Node
	hasFixedTopLeft  bool

	// clusterStates and treeOrientations hold one entry per cluster or tree
	// visited (a repeated one twice, with equal values, restored alike).
	clusterStates    []clusterStateSnapshot
	edgeGeometry     map[*Edge]edgeSnapshot
	treeOrientations []treeOrientationSnapshot

	// We don't want to be frozen due to some existing overlap, since many can be valid diagrams
	// We just care about not introducing new overlaps
	existingOverlaps  *existingOverlapSet
	captureTopology   bool
	captureEdgeRoutes bool
	// chargeOnly makes a topology update charge its capture without taking
	// it (ChargeTopologySnapshot); trackedNodes is then the traversal's node
	// set, the key set the capture would have had.
	chargeOnly   bool
	trackedNodes map[*Node]struct{}

	graph          graphSnapshot
	nodes          map[*Node]nodeSnapshot
	edges          map[*Edge]edgeSnapshot
	clusters       map[*Cluster]clusterSnapshot
	sequences      map[*Sequence]sequenceSnapshot
	trees          map[*Tree]treeSnapshot
	edgeAbductions map[*EdgeAbduction]EdgeAbduction
	herds          map[*HerdAssignment]herdSnapshot
	hierarchies    map[*Hierarchy]hierarchySnapshot
}

type clusterStateSnapshot struct {
	cluster     *Cluster
	arrangement ClusterArrangement
	desired     ClusterArrangement
	padding     float64
}

type treeOrientationSnapshot struct {
	tree        *Tree
	orientation geo.Orientation
}

// graphGeometrySnapshot keeps geometry in one value for speculative commit
// checks and preserves the original position pointer for rollback.
type graphGeometrySnapshot struct {
	topLeft pointerSnapshot[geo.Point]
	width   float64
	height  float64
}

type indexedGeometrySnapshot struct {
	geometry graphGeometrySnapshot
	present  bool
}

// nodeStack is pooled walk storage for snapshot capture.
type nodeStack struct {
	nodes []*Node
}

var recordStackPool = typedpool.New(func() *nodeStack { return new(nodeStack) })

// transactionEdgeSet is pooled storage for transactionSweepPadding's edge set.
type transactionEdgeSet struct {
	edges map[*Edge]struct{}
}

var transactionEdgeSetPool = typedpool.New(func() *transactionEdgeSet {
	return &transactionEdgeSet{edges: make(map[*Edge]struct{}, 64)}
})

// releaseRecordStack empties queue, the stack's storage after the walks, and
// pools it; storage beyond typedpool.MaxRetainedEntries is left to the
// collector (TinyGo's sync.Pool never frees what it holds).
func releaseRecordStack(stack *nodeStack, queue []*Node) {
	if cap(queue) > typedpool.MaxRetainedEntries {
		return
	}
	clear(queue[:cap(queue)])
	stack.nodes = queue[:0]
	recordStackPool.Put(stack)
}

// releaseTransactionEdgeSet empties the set and pools it, unless it grew
// beyond typedpool.MaxRetainedEntries (the set is only inserted into, so its
// length is the most it held).
func releaseTransactionEdgeSet(edgeSet *transactionEdgeSet) {
	if len(edgeSet.edges) > typedpool.MaxRetainedEntries {
		return
	}
	clear(edgeSet.edges)
	transactionEdgeSetPool.Put(edgeSet)
}

// changedFrom reports whether node's geometry differs from original, its
// recorded geometry.
func (original graphGeometrySnapshot) changedFrom(node *Node) bool {
	if node.TopLeft == nil {
		return original.topLeft.pointer != nil ||
			node.Width != original.width || node.Height != original.height
	}
	return original.topLeft.pointer == nil ||
		node.TopLeft.X != original.topLeft.value.X || node.TopLeft.Y != original.topLeft.value.Y ||
		node.Width != original.width || node.Height != original.height
}

type transactionPendingDescendant struct {
	node *Node
	emit bool
}

type Transaction struct {
	Ops     []func() error
	Graph   *Graph
	options TransactionOptions
	guard   *limits.WorkGuard

	PriorGraphState *GraphState
	spareGraphState *GraphState
	// placementCostSnapshot is optional stage-owned rollback data. Ordinary
	// speculative Rollback deliberately ignores it.
	placementCostSnapshot *PlacementCostSnapshot

	// indexOrder is g.Nodes as it was when the exception index was last
	// rebuilt (the legacy index from nodes to their g.Nodes positions was
	// reused, not rebuilt, by consecutive dirty-path commits).
	indexOrder      []*Node
	indexOrderReady bool
	// indexSlots holds the snapshot slot of each indexOrder entry (or
	// noIndexSlot past its end) for the capture it was computed from.
	indexSlots        []int32
	indexSlotsState   *GraphState
	indexSlotsCapture uint64
	// Geometry trial clones can share an immutable rollback point. Their first
	// accepted UpdateState must not recycle that shared state as scratch.
	priorGraphStateShared bool
	// sharedOverlaps marks a clone: its existing-overlap set belongs to the
	// transaction it was cloned from, so Release never recycles it.
	sharedOverlaps bool

	// commitScratch is the validation scratch of the Commit in progress,
	// borrowed from the graph (see commitScratch).
	*commitScratch

	// memo serves StableStructure transactions with AffectContainers.
	memo *containerMemo

	// runningOps counts the Commit calls of this transaction in progress (an
	// operation may commit its own transaction again), so Clear can leave the
	// operation list they iterate alone.
	runningOps int
}

// commitScratch is validation scratch that no state carries from one Commit
// to the next: every field is reset or regenerated before use. Commit
// borrows one from its graph's free list and returns it, so the many short
// transactions on one graph (one per transpose attempt, per bin-packing
// subgraph, ...) share storage instead of each allocating its own.
type commitScratch struct {
	// Geometry-only transactions usually move a small subset of the graph.
	dirtyNodeMarks []bool
	fixedOrigins   map[*Node]geo.Point

	// Final-state overlap validation uses one sweep-built candidate graph per
	// commit.
	overlapCandidates [][]int
	// candidateLists lists the indices whose candidate list is non-empty
	// (every other list in the backing array is empty), so a build resets,
	// sorts and visits only those.
	candidateLists      []int32
	overlapSweepNodes   []transactionSweepNode
	overlapActiveNodes  []transactionSweepNode
	overlapInvalidBoxes []bool
	overlapBoxes        []transactionBox
	// boxesReady reports that collectOverlapValidationNodes left every
	// g.Nodes entry's box, invalid-box flag and dirty count in the scratch
	// (fast path; see collectOverlapValidationNodes).
	boxesReady     bool
	readyDirty     int
	readyPositions int
	readyStructure bool
	readyInvalid   bool
	// marks serves the descendant walks as their seen set and the overlap
	// checks as their exception set.
	marks           slotMarks
	pass            containerPass
	descendantStack []transactionPendingDescendant
	descendantNodes []*Node

	// Existing-overlap regression check scratch (numbered like the set).
	overlapDirtyMarks      []uint32
	overlapDirtyGeneration uint32
	overlapDirtyList       []int32

	// repositionContainers' ordering and the ancestor walks.
	containerScratch []*Node
	// containerCheckOrder's keys.
	containerKeys   []*Node
	walkScratch     *descendantWalkScratch
	ancestorScratch []*Node
}

// ensureCommitScratch gives a transaction used outside Commit (tests drive
// the validation steps directly) scratch of its own.
func (t *Transaction) ensureCommitScratch() {
	if t.commitScratch == nil {
		t.commitScratch = &commitScratch{}
	}
}

// borrowCommitScratch takes scratch from the graph's free list; nested
// commits on the same graph (an operation that commits another transaction)
// take further entries.
func (g *Graph) borrowCommitScratch() *commitScratch {
	if count := len(g.commitScratchFree); count > 0 {
		scratch := g.commitScratchFree[count-1]
		g.commitScratchFree[count-1] = nil
		g.commitScratchFree = g.commitScratchFree[:count-1]
		return scratch
	}
	return &commitScratch{}
}

func (g *Graph) returnCommitScratch(scratch *commitScratch) {
	g.commitScratchFree = append(g.commitScratchFree, scratch)
}

type TransactionOptions struct {
	AffectContainers      bool
	IgnoreContainerEscape bool
	// AffectEdgeRoutes records route slice and point identity for operations
	// that move already-routed edges together with nodes.
	AffectEdgeRoutes bool
	// StableStructure is the caller's promise that from the transaction's
	// creation until its last Commit, its operations and the caller's code
	// between commits change only node geometry (TopLeft, the point it
	// refers to, Width, Height) and cluster arrangement and padding: the
	// graph's maps and node list, ownership fields, edges, labels, icons,
	// shapes, margins, loop offsets, fixed and desired sizes stay as they
	// were. With AffectContainers, Commit then reuses container wraps and
	// container checks whose geometry inputs are unchanged (containerMemo).
	StableStructure bool
}

type transactionWorkGuardKey struct{}

func contextWithTransactionWorkGuard(ctx context.Context, guard *limits.WorkGuard) context.Context {
	if ctx == nil || guard == nil {
		return ctx
	}
	return context.WithValue(ctx, transactionWorkGuardKey{}, guard)
}

func existingTransactionWorkGuard(ctx context.Context, location string) (*limits.WorkGuard, bool, error) {
	if ctx == nil {
		return nil, false, fmt.Errorf("TALA %s requires a context", location)
	}
	if guard, ok := ctx.Value(transactionWorkGuardKey{}).(*limits.WorkGuard); ok && guard != nil {
		if err := guard.Finish(); err != nil {
			return nil, false, err
		}
		return guard, true, nil
	}
	return nil, false, nil
}

func ensureTransactionWorkGuard(ctx context.Context, location string) (context.Context, *limits.WorkGuard, error) {
	guard, exists, err := existingTransactionWorkGuard(ctx, location)
	if err != nil {
		return nil, nil, err
	}
	if exists {
		return ctx, guard, nil
	}
	guard, err = limits.NewWorkGuard(ctx, location, maxTransactionWorkUnits)
	if err != nil {
		return nil, nil, err
	}
	return context.WithValue(ctx, transactionWorkGuardKey{}, guard), guard, nil
}

func (g *Graph) newRequestTransaction(ctx context.Context, options TransactionOptions) (*Transaction, error) {
	_, guard, err := ensureTransactionWorkGuard(ctx, "Transaction")
	if err != nil {
		return nil, err
	}
	return g.newTransactionWithOptionsContext(ctx, options, guard)
}

func (g *Graph) newRequestTransactionWithGuard(ctx context.Context, guard *limits.WorkGuard, options TransactionOptions) (*Transaction, error) {
	if guard == nil {
		return nil, fmt.Errorf("TALA transaction requires a shared work guard")
	}
	return g.newTransactionWithOptionsContext(ctx, options, guard)
}

func cloneSlice[V any](values []V) []V {
	cloned := make([]V, len(values))
	copy(cloned, values)
	return cloned
}

func (gs *GraphState) updateContext(g *Graph, guard *limits.WorkGuard) error {
	if g == nil {
		return fmt.Errorf("TALA transaction graph is nil")
	}
	if guard == nil {
		return fmt.Errorf("TALA transaction requires a work guard")
	}
	if len(g.Nodes) > maxEngineNodes {
		return fmt.Errorf("TALA transaction node count exceeds limit %d", maxEngineNodes)
	}
	if len(g.Edges) > maxEngineEdges {
		return fmt.Errorf("TALA transaction edge count exceeds limit %d", maxEngineEdges)
	}
	for _, collection := range []struct {
		name  string
		count int
	}{
		{name: "container", count: len(g.Containers)},
		{name: "cluster", count: len(g.Clusters)},
		{name: "sequence", count: len(g.Sequences)},
		{name: "tree", count: len(g.Trees)},
		{name: "node-tree", count: len(g.NodeToTree)},
		{name: "hub", count: len(g.Hubs)},
		{name: "direction", count: len(g.Directions)},
		{name: "sibling", count: len(g.CommonUncleSiblings)},
	} {
		if collection.count > maxEngineNodes+1 {
			return fmt.Errorf("TALA transaction %s map exceeds limit %d", collection.name, maxEngineNodes+1)
		}
	}
	// Cluster, sequence, and container views commonly repeat the same nodes.
	// Size the initial maps only from the graph's primary view and let Go grow
	// them if the deduped traversal discovers additional unique nodes. The
	// traversal itself, rather than an additive membership estimate, enforces
	// the supported unique-node limit.
	totalNodes := min(len(g.Nodes), maxEngineNodes)

	// The node set keeps its storage when a GraphState is refreshed. A new
	// state is sized from the previous snapshot of this graph (the traversal
	// usually reaches the same nodes), else from the primary view.
	expectedNodes := max(totalNodes, min(int(g.snapshotSizeHint), maxEngineNodes))
	if cap(gs.slots.entries) > 2*expectedNodes+64 {
		gs.slots = snapshotSlots{}
	}
	gs.slots.reset()
	gs.captureID++
	if gs.slots.entries == nil {
		gs.slots.entries = make([]snapshotEntry, 0, expectedNodes)
	}
	gs.hasFixedTopLeft = false

	clear(gs.clusterStates)
	gs.clusterStates = gs.clusterStates[:0]

	gs.originalNodesRef = g.Nodes
	if cap(gs.originalNodes) < len(g.Nodes) {
		gs.originalNodes = make([]*Node, len(g.Nodes))
	} else {
		gs.originalNodes = gs.originalNodes[:len(g.Nodes)]
	}
	copy(gs.originalNodes, g.Nodes)
	// Nothing below mutates geometry, so each entry equals the node's
	// recorded geometry.
	if cap(gs.originalGeometry) < len(g.Nodes) {
		gs.originalGeometry = make([]indexedGeometrySnapshot, len(g.Nodes))
	} else {
		gs.originalGeometry = gs.originalGeometry[:len(g.Nodes)]
	}
	for i, n := range g.Nodes {
		if n == nil {
			gs.originalGeometry[i] = indexedGeometrySnapshot{}
			continue
		}
		gs.originalGeometry[i] = indexedGeometrySnapshot{
			geometry: graphGeometrySnapshot{topLeft: snapshotPointer(n.TopLeft), width: n.Width, height: n.Height},
			present:  true,
		}
	}
	recordNode := func(n *Node) (bool, error) {
		if n == nil {
			return false, nil
		}
		if gs.slots.recordedDuringCapture(n) {
			return false, nil
		}
		if gs.slots.size() >= maxEngineNodes {
			return false, fmt.Errorf("TALA transaction unique node snapshot exceeds limit %d", maxEngineNodes)
		}
		if err := guard.Step(); err != nil {
			return false, err
		}
		gs.slots.add(n)
		gs.hasFixedTopLeft = gs.hasFixedTopLeft || n.FixedTopLeft != nil
		return true, nil
	}

	// One pooled stack serves every walk; each walk starts and ends with it
	// empty.
	stack := recordStackPool.Get()
	queue := stack.nodes[:0]
	defer func() { releaseRecordStack(stack, queue) }()
	recordDescendants := func(root *Node) error {
		queue = append(queue[:0], root)
		for len(queue) > 0 {
			node := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			recorded, err := recordNode(node)
			if err != nil {
				return err
			}
			if !recorded {
				continue
			}
			children := g.Containers[node]
			if len(children) != 0 {
				if err := guard.Add(int64(len(children))); err != nil {
					return err
				}
			}
			queue = append(queue, children...)
		}
		return nil
	}
	for _, n := range g.Nodes {
		if err := recordDescendants(n); err != nil {
			return err
		}
	}
	// Some speculative subgraphs keep descendants outside g.Nodes. Record map
	// roots and values as well so rollback does not depend on slice membership.
	for container, children := range g.Containers {
		if err := guard.Step(); err != nil {
			return err
		}
		if err := recordDescendants(container); err != nil {
			return err
		}
		for _, child := range children {
			if err := recordDescendants(child); err != nil {
				return err
			}
		}
	}

	// Store cluster policy and nodes.
	for _, c := range g.Clusters {
		if err := guard.Step(); err != nil {
			return err
		}
		if c == nil {
			continue
		}
		gs.clusterStates = append(gs.clusterStates, clusterStateSnapshot{
			cluster:     c,
			arrangement: c.Arrangement,
			desired:     c.DesiredArrangement,
			padding:     c.Padding,
		})
		if err := recordDescendants(c.Vessel); err != nil {
			return err
		}
		if err := recordDescendants(c.Container); err != nil {
			return err
		}
		for _, n := range c.Nodes {
			if err := recordDescendants(n); err != nil {
				return err
			}
		}
	}

	// Store sequence nodes that may be temporarily removed from g.Nodes.
	defer func() { g.snapshotSizeHint = int32(gs.slots.size()) }()
	for _, s := range g.Sequences {
		if err := guard.Step(); err != nil {
			return err
		}
		if s == nil {
			continue
		}
		if err := recordDescendants(s.Vessel); err != nil {
			return err
		}
		if err := recordDescendants(s.Container); err != nil {
			return err
		}
		for _, n := range s.Nodes {
			if err := recordDescendants(n); err != nil {
				return err
			}
		}
	}

	if gs.captureTopology && gs.chargeOnly {
		if err := gs.chargeRuntimeStateContext(g, guard); err != nil {
			return err
		}
	} else if gs.captureTopology {
		if err := gs.captureRuntimeStateContext(g, guard); err != nil {
			return err
		}
	} else {
		if err := gs.captureGeometryStateContext(g, guard); err != nil {
			return err
		}
	}
	return guard.Finish()
}

// PreservePriorGraphState pins the current rollback point so a later
// UpdateState cannot recycle it as scratch. Callers may retain the returned
// state for a larger atomic operation that spans multiple transaction commits.
func (t *Transaction) PreservePriorGraphState() *GraphState {
	if t == nil {
		return nil
	}
	t.priorGraphStateShared = true
	return t.PriorGraphState
}

// CapturePlacementCosts attaches an optional stage-owned cost rollback point
// to this transaction. Ordinary Transaction.Rollback deliberately ignores it:
// speculative rejections retain memoized costs, while the enclosing stage
// decides whether to restore them.
func (t *Transaction) CapturePlacementCosts(location string) error {
	if t == nil || t.Graph == nil || t.guard == nil {
		return fmt.Errorf("TALA %s placement-cost snapshot requires an initialized transaction", location)
	}
	if t.placementCostSnapshot != nil {
		return nil
	}
	cacheEntries := t.Graph.EdgeLengthCacheEntries()
	if int64(cacheEntries) > maxEngineTopologyReferences {
		return fmt.Errorf("TALA %s edge-length cache entries exceed limit %d", location, maxEngineTopologyReferences)
	}
	for range cacheEntries {
		if err := t.guard.Step(); err != nil {
			return err
		}
	}
	costs := t.Graph.SnapshotPlacementCosts()
	if err := t.guard.Finish(); err != nil {
		return err
	}
	t.placementCostSnapshot = costs
	return nil
}

// RestorePlacementCosts restores a captured placement-cost rollback point and
// reports whether one was present.
func (t *Transaction) RestorePlacementCosts() bool {
	if t == nil || t.placementCostSnapshot == nil {
		return false
	}
	costs := t.placementCostSnapshot
	t.placementCostSnapshot = nil
	costs.Restore()
	return true
}

func transactionSweepPadding(g *Graph, guard *limits.WorkGuard) (float64, error) {
	padding := float64(TableNodeGap)
	maxMargin := 0.0
	maxLoopOffset := 0.0
	// The scan is pure, so its units are batched and flushed before every
	// return. The edge set only determines how many units the final per-edge
	// pass charges (one per distinct edge); the padding maximum does not
	// depend on duplicates, so it is taken while inserting.
	steps := batchedWork(guard)
	edgeSet := transactionEdgeSetPool.Get()
	defer releaseTransactionEdgeSet(edgeSet)
	edges := edgeSet.edges
	edgePadding := padding
	for _, node := range g.Nodes {
		steps.step()
		if node == nil {
			return 0, steps.flushWith(fmt.Errorf("TALA transaction contains a nil node"))
		}
		if node.TopLeft != nil {
			if !transactionNodeHasFiniteBox(node) {
				// Empty containers deliberately use NaN/+Inf sentinel geometry until
				// their final fit stage. The legacy all-pairs predicates never treated
				// those sentinels as overlaps, so omit them from the spatial index too.
				if node.isContainer && len(g.Containers[node]) == 0 {
					continue
				}
				return 0, steps.flushWith(invariant.Errorf(
					"transaction node %d has invalid geometry (x=%v y=%v width=%v height=%v)",
					node.ID, node.TopLeft.X, node.TopLeft.Y, node.Width, node.Height,
				))
			}
		}
		if !transactionFinite(node.margin.top) || !transactionFinite(node.margin.right) ||
			!transactionFinite(node.margin.bottom) || !transactionFinite(node.margin.left) {
			return 0, steps.flushWith(invariant.Errorf("transaction node %d has non-finite margin", node.ID))
		}
		maxMargin = max(maxMargin, node.margin.top, node.margin.right, node.margin.bottom, node.margin.left)
		for _, offset := range node.LoopOffsets {
			steps.step()
			if !transactionFinite(offset) {
				return 0, steps.flushWith(invariant.Errorf("transaction node %d has a non-finite loop offset", node.ID))
			}
			if offset > maxLoopOffset {
				maxLoopOffset = offset
			}
		}
		// Direct internal callers can temporarily expose an edge through only one
		// endpoint before graph.Edges is synchronized. Include those references so
		// the broad phase never drops a pair that deltaTo would inspect.
		for _, edge := range node.Edges {
			steps.step()
			if edge == nil {
				return 0, steps.flushWith(fmt.Errorf("TALA transaction node %d contains a nil edge", node.ID))
			}
			edges[edge] = struct{}{}
			edgePadding = max(edgePadding, float64(edge.MinWidth), float64(edge.MinHeight))
		}
	}
	for _, edge := range g.Edges {
		steps.step()
		if edge == nil {
			return 0, steps.flushWith(fmt.Errorf("TALA transaction contains a nil edge"))
		}
		edges[edge] = struct{}{}
		edgePadding = max(edgePadding, float64(edge.MinWidth), float64(edge.MinHeight))
	}
	if err := steps.add(int64(len(edges))); err != nil {
		return 0, err
	}
	if err := steps.flush(); err != nil {
		return 0, err
	}
	padding = edgePadding
	// deltaTo can combine one directional margin or loop offset from each
	// node in the pair. Twice the graph maximum is therefore a proven broad-
	// phase bound for every exact predicate evaluated below.
	padding = max(padding, 2*maxMargin, float64(NodeGap)+2*maxLoopOffset)
	if !transactionFinite(padding) {
		return 0, invariant.New("transaction overlap padding is non-finite")
	}
	return padding, guard.Finish()
}

func transactionFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func transactionNodeHasFiniteBox(node *Node) bool {
	if node == nil || node.TopLeft == nil {
		return false
	}
	// v-v is +0 for a finite v and NaN for an infinite or NaN one, so the sum
	// is zero exactly when all six values are finite.
	x, y, width, height := node.TopLeft.X, node.TopLeft.Y, node.Width, node.Height
	right, bottom := x+width, y+height
	return (x-x)+(y-y)+(width-width)+(height-height)+(right-right)+(bottom-bottom) == 0 &&
		width >= 0 && height >= 0
}

// transactionBox holds a node's edges with right = x+width and
// bottom = y+height, the sums the broad phases compare.
type transactionBox struct {
	left, top, right, bottom float64
}

type transactionSweepNode struct {
	node  *Node
	index int
}

// buildExistingOverlapSetInto builds the set in set's storage (emptied
// first).
func (g *Graph) buildExistingOverlapSetInto(set *existingOverlapSet, guard *limits.WorkGuard, referenceLimit int64) (*existingOverlapSet, error) {
	set.reset()
	padding, err := transactionSweepPadding(g, guard)
	if err != nil {
		return nil, err
	}
	placed := set.placed[:0]
	defer func() {
		clear(placed)
		set.placed = placed[:0]
	}()
	for index, node := range g.Nodes {
		if err := guard.Step(); err != nil {
			return nil, err
		}
		if node.TopLeft != nil && transactionNodeHasFiniteBox(node) {
			placed = append(placed, transactionSweepNode{node: node, index: index})
		}
	}
	if err := guard.Step(); err != nil {
		return nil, err
	}
	// The sweep meets each pair of placed entries once, so a pair can only
	// repeat when g.Nodes repeats a node; only then must insertions dedupe.
	// The overlap hint serves as a temporary stamp here (verified by the
	// entry it names, so stale values are harmless).
	dedupe := false
	for i := range placed {
		node := placed[i].node
		if stamp := node.overlapSlot; stamp >= 0 && int(stamp) < i && placed[stamp].node == node {
			dedupe = true
			break
		}
		node.overlapSlot = int32(i)
	}
	xsort.StableFunc(placed, func(left, right transactionSweepNode) int {
		if left.node.TopLeft.X != right.node.TopLeft.X {
			switch {
			case left.node.TopLeft.X < right.node.TopLeft.X:
				return -1
			case right.node.TopLeft.X < left.node.TopLeft.X:
				return 1
			default:
				return 0
			}
		}
		if order := cmp.Compare(left.node.ID, right.node.ID); order != 0 {
			return order
		}
		return cmp.Compare(left.index, right.index)
	})

	var retainedReferences int64
	addPair := func(exact bool, first, second *Node) error {
		if referenceLimit < 2 || retainedReferences > referenceLimit-2 {
			return fmt.Errorf("TALA transaction overlap references exceed limit %d", referenceLimit)
		}
		set.addPair(exact, first, second, dedupe)
		retainedReferences += 2
		return nil
	}

	active := set.active[:0]
	defer func() {
		clear(active[:cap(active)])
		set.active = active[:0]
	}()
	for _, current := range placed {
		if err := guard.Step(); err != nil {
			return nil, err
		}
		kept := active[:0]
		for _, other := range active {
			if err := guard.Step(); err != nil {
				return nil, err
			}
			// g.Nodes is a legacy public slice and malformed direct callers can
			// repeat the same pointer. The old all-pairs scan explicitly skipped
			// identity pairs, so the sweep must not retain a self-overlap either.
			if other == current.node {
				continue
			}
			if other.TopLeft.X+other.Width+padding <= current.node.TopLeft.X {
				continue
			}
			kept = append(kept, other)
			if other.TopLeft.Y >= current.node.TopLeft.Y+current.node.Height+padding ||
				current.node.TopLeft.Y >= other.TopLeft.Y+other.Height+padding {
				continue
			}
			if current.node.DoesOverlapExact(other) {
				if err := addPair(true, current.node, other); err != nil {
					return nil, err
				}
			}
			// Preserve the old ordered-pair semantics even if a malformed direct
			// caller has asymmetric edge references.
			if current.node.doesOverlap(other) || other.doesOverlap(current.node) {
				if err := addPair(false, current.node, other); err != nil {
					return nil, err
				}
			}
		}
		active = append(kept, current.node)
	}
	if err := guard.Finish(); err != nil {
		return nil, err
	}
	return set, nil
}

// newTransactionWithOptionsContext constructs a transaction using a request-scoped
// work guard so production stages can share one aggregate budget across all
// speculative candidates.
func (g *Graph) newTransactionWithOptionsContext(ctx context.Context, options TransactionOptions, guard *limits.WorkGuard) (*Transaction, error) {
	if guard == nil {
		var err error
		_, guard, err = ensureTransactionWorkGuard(ctx, "Transaction")
		if err != nil {
			return nil, err
		}
	} else if ctx == nil {
		return nil, fmt.Errorf("TALA transaction requires a context")
	}
	if err := guard.Finish(); err != nil {
		return nil, err
	}
	graphState := g.takeSpareGraphState()
	graphState.captureEdgeRoutes = options.AffectEdgeRoutes
	if err := graphState.updateContext(g, guard); err != nil {
		return nil, err
	}

	existingOverlaps, err := g.buildExistingOverlapSetInto(g.takeSpareOverlapSet(), guard, maxTransactionOverlapReferences)
	if err != nil {
		return nil, err
	}

	graphState.existingOverlaps = existingOverlaps
	txn := &Transaction{
		Ops:     make([]func() error, 0),
		Graph:   g,
		options: options,
		guard:   guard,

		PriorGraphState: graphState,
	}
	if options.StableStructure && options.AffectContainers {
		txn.memo = g.takeSpareContainerMemo()
	}

	return txn, guard.Finish()
}

// Release hands the transaction's snapshot storage back to its graph for the
// next transaction created there. It is for a caller that owns the
// transaction outright: neither the transaction nor its PriorGraphState (nor
// anything taken from them) may be used afterwards. A rollback point that is
// shared (CloneGeometryContext, PreservePriorGraphState) is left alone.
func (t *Transaction) Release() {
	if t == nil || t.Graph == nil || t.priorGraphStateShared {
		return
	}
	g := t.Graph
	if state := t.PriorGraphState; state != nil && !state.captureTopology {
		if set := state.existingOverlaps; set != nil && !t.sharedOverlaps {
			g.recycleOverlapSet(set)
		}
		g.recycleGraphState(state)
	}
	if spare := t.spareGraphState; spare != nil && !spare.captureTopology {
		g.recycleGraphState(spare)
	}
	t.PriorGraphState = nil
	t.spareGraphState = nil
	clear(t.Ops)
	t.Ops = nil
	if t.memo != nil {
		g.recycleContainerMemo(t.memo)
		t.memo = nil
	}
}

const maxSpareTransactionStates = 4

// takeSpareGraphState returns a released geometry state for reuse, or a new
// one. updateContext rewrites every field a geometry capture reads.
func (g *Graph) takeSpareGraphState() *GraphState {
	if g == nil {
		// updateContext reports the missing graph.
		return &GraphState{}
	}
	if count := len(g.spareGraphStates); count > 0 {
		state := g.spareGraphStates[count-1]
		g.spareGraphStates[count-1] = nil
		g.spareGraphStates = g.spareGraphStates[:count-1]
		return state
	}
	return &GraphState{}
}

func (g *Graph) recycleGraphState(state *GraphState) {
	if len(g.spareGraphStates) >= maxSpareTransactionStates {
		return
	}
	state.existingOverlaps = nil
	state.captureEdgeRoutes = false
	state.edgeGeometry = nil
	g.spareGraphStates = append(g.spareGraphStates, state)
}

func (g *Graph) takeSpareContainerMemo() *containerMemo {
	if count := len(g.spareContainerMemos); count > 0 {
		memo := g.spareContainerMemos[count-1]
		g.spareContainerMemos[count-1] = nil
		g.spareContainerMemos = g.spareContainerMemos[:count-1]
		return memo
	}
	return &containerMemo{}
}

func (g *Graph) recycleContainerMemo(memo *containerMemo) {
	memo.reset()
	if len(g.spareContainerMemos) < maxSpareTransactionStates {
		g.spareContainerMemos = append(g.spareContainerMemos, memo)
	}
}

func (g *Graph) takeSpareOverlapSet() *existingOverlapSet {
	if g == nil {
		return &existingOverlapSet{}
	}
	if count := len(g.spareOverlapSets); count > 0 {
		set := g.spareOverlapSets[count-1]
		g.spareOverlapSets[count-1] = nil
		g.spareOverlapSets = g.spareOverlapSets[:count-1]
		return set
	}
	return &existingOverlapSet{}
}

func (g *Graph) recycleOverlapSet(set *existingOverlapSet) {
	if len(g.spareOverlapSets) >= maxSpareTransactionStates {
		return
	}
	g.spareOverlapSets = append(g.spareOverlapSets, set)
}

func (t *Transaction) UpdateState() error {
	// Existing-overlap exceptions intentionally remain anchored to transaction
	// construction. Reusing a transaction updates the rollback point, not which
	// preexisting collisions are allowed. Snapshot refresh remains cancellable
	// and budgeted.
	previous := t.PriorGraphState
	updated := t.spareGraphState
	if updated == nil {
		// A released geometry state; updateContext rewrites every field the
		// capture reads (as for a new transaction's state).
		updated = t.Graph.takeSpareGraphState()
	}
	updated.captureTopology = previous.captureTopology
	updated.captureEdgeRoutes = previous.captureEdgeRoutes
	if err := updated.updateContext(t.Graph, t.guard); err != nil {
		// updateContext only mutates the spare snapshot. The prior rollback point
		// remains exact, so a failed refresh can reject the accepted candidate.
		t.Rollback()
		return err
	}
	updated.existingOverlaps = previous.existingOverlaps
	t.PriorGraphState = updated
	if t.priorGraphStateShared {
		t.spareGraphState = nil
		t.priorGraphStateShared = false
	} else {
		t.spareGraphState = previous
	}
	return nil
}

// containerCheckOrder returns g.Containers' keys in the order the stepped
// container check visits them: the nil key first, then by entity ID, then by
// geometry (copies of one node share its ID). The check charges a unit per
// key and stops at the first bad container; in map order the units it
// charged, and so whether a commit hit MaxTransactionWorkUnits, varied from
// run to run, and so did the error when one container's check fails with an
// error and another is bad.
func (t *Transaction) containerCheckOrder() []*Node {
	t.ensureCommitScratch()
	keys := t.containerKeys[:0]
	for container := range t.Graph.Containers {
		keys = append(keys, container)
	}
	xsort.Func(keys, compareNodeKeys)
	t.containerKeys = keys
	return keys
}

// compareNodeKeys orders map keys deterministically: nil first, then by
// entity ID, then by geometry.
func compareNodeKeys(a, b *Node) int {
	if a == b {
		return 0
	}
	if a == nil || b == nil {
		if a == nil {
			return -1
		}
		return 1
	}
	if c := cmp.Compare(a.ID, b.ID); c != 0 {
		return c
	}
	if (a.TopLeft == nil) != (b.TopLeft == nil) {
		if a.TopLeft == nil {
			return -1
		}
		return 1
	}
	if a.TopLeft != nil {
		if c := cmp.Compare(a.TopLeft.X, b.TopLeft.X); c != 0 {
			return c
		}
		if c := cmp.Compare(a.TopLeft.Y, b.TopLeft.Y); c != 0 {
			return c
		}
	}
	if c := cmp.Compare(a.Width, b.Width); c != 0 {
		return c
	}
	return cmp.Compare(a.Height, b.Height)
}

func (t *Transaction) AddOp(fn func() error) {
	t.Ops = append(t.Ops, fn)
}

func (t *Transaction) repositionContainers(poll *contextPoll) error {
	steps := batchedWork(t.guard)
	smallestToLargest := t.containerScratch[:0]
	defer func() {
		clear(smallestToLargest)
		t.containerScratch = smallestToLargest[:0]
	}()
	for container := range t.Graph.Containers {
		if err := steps.step(); err != nil {
			return err
		}
		if container == nil || container.TopLeft == nil {
			continue
		}
		smallestToLargest = append(smallestToLargest, container)
	}
	// Charge the collection units before any container is resized.
	if err := steps.flush(); err != nil {
		return err
	}
	// slices.SortFunc runs the same pattern-defeating quicksort as sort.Slice
	// (both are generated from one template and consult the order only as
	// "less"), so this cmp, negative exactly when the former less holds,
	// gives the same order without sort.Slice's reflection allocation.
	// xsort.Func is slices.SortFunc in native builds; WebAssembly builds
	// share one sort.Slice sorter instead (see lib/xsort).
	xsort.Func(smallestToLargest, func(a, b *Node) int {
		less := a.area() < b.area()
		if a.area() == b.area() {
			less = a.ID < b.ID
		}
		if less {
			return -1
		}
		return 1
	})

	for _, container := range smallestToLargest {
		if err := poll.err(); err != nil {
			return err
		}
		if t.memo != nil {
			t.memo.wrap(container)
		} else {
			container.wrapChildren()
		}
		if err := t.guard.Step(); err != nil {
			return err
		}
	}
	return t.guard.Finish()
}

func (gs *GraphState) hasOriginalNodeOrder(g *Graph) bool {
	if gs == nil || g == nil || len(gs.originalNodes) != len(g.Nodes) {
		return false
	}
	for i, node := range g.Nodes {
		if gs.originalNodes[i] != node {
			return false
		}
	}
	return true
}

// geometryChangedAt reports whether node, which is originalNodes[index] while
// hasOriginalNodeOrder holds, has changed its geometry since the capture
// (true for a nil node or a nil entry).
func (gs *GraphState) geometryChangedAt(index int, node *Node) bool {
	if node == nil {
		return true
	}
	original := gs.originalGeometry[index]
	if !original.present {
		return true
	}
	return original.geometry.changedFrom(node)
}

func transactionBoxesMayInteract(first, second *Node) bool {
	if first == nil || second == nil || first == second || first.TopLeft == nil || second.TopLeft == nil {
		return false
	}
	// This is the same symmetric broad phase used by
	// doesOverlapWithDimensionsContext. NaN values deliberately survive the
	// comparisons and are sent to the normal bad-state path.
	const maxSafeDelta = 500.0
	return !(first.TopLeft.X > second.TopLeft.X+second.Width+maxSafeDelta ||
		first.TopLeft.X+first.Width+maxSafeDelta < second.TopLeft.X ||
		first.TopLeft.Y > second.TopLeft.Y+second.Height+maxSafeDelta ||
		first.TopLeft.Y+first.Height+maxSafeDelta < second.TopLeft.Y)
}

func (t *Transaction) collectFixedOrigins(poll *contextPoll) error {
	if t.fixedOrigins == nil {
		t.fixedOrigins = make(map[*Node]geo.Point)
	} else {
		clear(t.fixedOrigins)
	}
	if !t.PriorGraphState.hasFixedTopLeft {
		return t.guard.Finish()
	}
	steps := batchedWork(t.guard)
	for _, node := range t.Graph.Nodes {
		if err := steps.step(); err != nil {
			return err
		}
		if err := poll.err(); err != nil {
			return steps.flushWith(err)
		}
		if node == nil || node.TopLeft == nil || node.FixedTopLeft == nil {
			continue
		}
		container := node.container()
		if _, found := t.fixedOrigins[container]; found {
			continue
		}
		t.fixedOrigins[container] = geo.Point{
			X: node.TopLeft.X - node.FixedTopLeft.X,
			Y: node.TopLeft.Y - node.FixedTopLeft.Y,
		}
	}
	if err := steps.flush(); err != nil {
		return err
	}
	return t.guard.Finish()
}

// collectOverlapValidationNodes records geometry changes for the legacy
// non-exact-to-exact regression check. Ordered overlap validation itself uses a
// complete post-state sweep, so it no longer needs a changed-node closure.
func (t *Transaction) collectOverlapValidationNodes(poll *contextPoll) (useDirtyValidation bool, err error) {
	t.ensureCommitScratch()
	t.boxesReady = false
	if !t.PriorGraphState.hasOriginalNodeOrder(t.Graph) {
		return false, nil
	}
	if cap(t.dirtyNodeMarks) < len(t.Graph.Nodes) {
		t.dirtyNodeMarks = make([]bool, len(t.Graph.Nodes))
	} else {
		t.dirtyNodeMarks = t.dirtyNodeMarks[:len(t.Graph.Nodes)]
		clear(t.dirtyNodeMarks)
	}

	// g.Nodes equals originalNodes element for element here, so the snapshot
	// is read by index rather than through the node set.
	// One unit per node, added up where the loop stops.
	state := t.PriorGraphState
	steps := batchedWork(t.guard)
	marks := t.dirtyNodeMarks
	if poll.done != nil {
		// With a Done channel the n strided polls are accounted at once, and
		// the same pass reads every node's box for the overlap phase
		// (buildPostStateOverlapCandidates) and the facts the structural loop
		// needs: nothing moves until those run.
		return t.collectOverlapValidationNodesFast(poll, &steps)
	}
	for index, node := range t.Graph.Nodes {
		if err := poll.err(); err != nil {
			if addErr := steps.add(int64(index + 1)); addErr != nil {
				return false, addErr
			}
			return false, steps.flushWith(err)
		}
		if state.geometryChangedAt(index, node) {
			marks[index] = true
		}
	}
	if err := steps.add(int64(len(t.Graph.Nodes))); err != nil {
		return false, err
	}
	if err := steps.flush(); err != nil {
		return false, err
	}
	return true, t.guard.Finish()
}

// collectOverlapValidationNodesFast is the dirty-node loop for a context
// with a Done channel. Besides the dirty marks it records each g.Nodes
// entry's box sums and finite flag (as buildPostStateOverlapCandidates'
// first loop computes them), the dirty count, the number of positioned
// nodes, and whether any node needs the structural loop's full check under
// onlyFixedTopLeft (a positioned non-cluster node with FixedTopLeft) or is
// nil. Units and polls are those of the per-node loop.
func (t *Transaction) collectOverlapValidationNodesFast(poll *contextPoll, steps *batchedSteps) (bool, error) {
	nodes := t.Graph.Nodes
	nodeCount := len(nodes)
	state := t.PriorGraphState
	marks := t.dirtyNodeMarks
	if cap(t.overlapInvalidBoxes) < nodeCount {
		t.overlapInvalidBoxes = make([]bool, nodeCount)
	} else {
		t.overlapInvalidBoxes = t.overlapInvalidBoxes[:nodeCount]
	}
	if cap(t.overlapBoxes) < nodeCount {
		t.overlapBoxes = make([]transactionBox, nodeCount)
	}
	boxes := t.overlapBoxes[:nodeCount]
	invalid := t.overlapInvalidBoxes
	dirty, positioned := 0, 0
	structure, hasInvalid := false, false
	for index, node := range nodes {
		invalid[index] = false
		if node == nil {
			marks[index] = true
			dirty++
			structure = true
			continue
		}
		topLeft := node.TopLeft
		if topLeft == nil {
			if state.geometryChangedAt(index, node) {
				marks[index] = true
				dirty++
			}
			continue
		}
		x, y, width, height := topLeft.X, topLeft.Y, node.Width, node.Height
		original := state.originalGeometry[index]
		if !original.present || original.geometry.topLeft.pointer == nil ||
			x != original.geometry.topLeft.value.X || y != original.geometry.topLeft.value.Y ||
			width != original.geometry.width || height != original.geometry.height {
			marks[index] = true
			dirty++
		}
		positioned++
		right, bottom := x+width, y+height
		boxes[index] = transactionBox{left: x, top: y, right: right, bottom: bottom}
		// transactionNodeHasFiniteBox(node)
		if !((x-x)+(y-y)+(width-width)+(height-height)+(right-right)+(bottom-bottom) == 0 &&
			width >= 0 && height >= 0) {
			invalid[index] = true
			hasInvalid = true
		}
		if node.Cluster == nil && node.FixedTopLeft != nil {
			structure = true
		}
	}
	if err := steps.add(int64(nodeCount)); err != nil {
		return false, err
	}
	if err := steps.flush(); err != nil {
		return false, err
	}
	if err := poll.skip(int64(nodeCount)); err != nil {
		return false, err
	}
	t.boxesReady = true
	t.readyDirty = dirty
	t.readyPositions = positioned
	t.readyStructure = structure
	t.readyInvalid = hasInvalid
	return true, t.guard.Finish()
}

// buildPostStateOverlapCandidates builds the exact 500-unit broad phase used
// by doesOverlapWithDimensionsContext. Sparse geometry transactions retain only
// pairs incident to a changed box; full geometry scans retain every pair.
// Candidate lists are sorted by g.Nodes index so directional deltaTo and
// first-rejection order remain unchanged.
func (t *Transaction) buildPostStateOverlapCandidates(poll *contextPoll, dirtyOnly bool) error {
	// Every unit below is charged in batches; each return flushes first. The
	// function only fills transaction scratch, so deferring the charges within
	// it cannot change what the guard or the caller observes.
	steps := batchedWork(t.guard)
	nodeCount := len(t.Graph.Nodes)
	if nodeCount > maxEngineNodes {
		return fmt.Errorf("TALA transaction node count exceeds limit %d", maxEngineNodes)
	}
	// Only the lists the previous build filled can be non-empty.
	if cap(t.overlapCandidates) < nodeCount {
		t.overlapCandidates = make([][]int, nodeCount)
		t.candidateLists = t.candidateLists[:0]
	} else {
		full := t.overlapCandidates[:cap(t.overlapCandidates)]
		for _, index := range t.candidateLists {
			full[index] = full[index][:0]
		}
		t.candidateLists = t.candidateLists[:0]
		t.overlapCandidates = t.overlapCandidates[:nodeCount]
	}
	ready := t.boxesReady && dirtyOnly && len(t.overlapBoxes) >= nodeCount && len(t.overlapInvalidBoxes) == nodeCount
	t.boxesReady = false
	if !ready {
		if cap(t.overlapInvalidBoxes) < nodeCount {
			t.overlapInvalidBoxes = make([]bool, nodeCount)
		} else {
			t.overlapInvalidBoxes = t.overlapInvalidBoxes[:nodeCount]
			clear(t.overlapInvalidBoxes)
		}
	}
	rebuildNodeIndex := !dirtyOnly || !t.indexOrderReady
	if rebuildNodeIndex {
		t.indexOrder = append(t.indexOrder[:0], t.Graph.Nodes...)
		t.indexSlotsState = nil
	}
	t.overlapSweepNodes = t.overlapSweepNodes[:0]
	if cap(t.overlapBoxes) < nodeCount {
		t.overlapBoxes = make([]transactionBox, nodeCount)
	}
	boxes := t.overlapBoxes[:nodeCount]
	hasInvalidBox := false
	dirtyCount := 0
	if ready {
		// collectOverlapValidationNodes read every box, flag and the dirty
		// count with nothing moved since; the first loop's units and strided
		// polls are accounted at once.
		hasInvalidBox = t.readyInvalid
		dirtyCount = t.readyDirty
		if err := poll.skip(int64(nodeCount)); err != nil {
			if addErr := steps.add(int64(nodeCount)); addErr != nil {
				return addErr
			}
			return steps.flushWith(err)
		}
	}
	for index, node := range t.Graph.Nodes {
		if ready {
			break
		}
		// One unit per node, added where the loop stops.
		if err := poll.err(); err != nil {
			if addErr := steps.add(int64(index + 1)); addErr != nil {
				return addErr
			}
			return steps.flushWith(err)
		}
		if node == nil {
			if addErr := steps.add(int64(index + 1)); addErr != nil {
				return addErr
			}
			return steps.flushWith(invariant.New("transaction overlap check encountered a nil graph node"))
		}
		if dirtyOnly && t.dirtyNodeMarks[index] {
			dirtyCount++
		}
		if node.TopLeft == nil {
			continue
		}
		boxes[index] = transactionBox{
			left: node.TopLeft.X, top: node.TopLeft.Y,
			right: node.TopLeft.X + node.Width, bottom: node.TopLeft.Y + node.Height,
		}
		if !transactionNodeHasFiniteBox(node) {
			t.overlapInvalidBoxes[index] = true
			hasInvalidBox = true
		}
	}
	if err := steps.add(int64(nodeCount)); err != nil {
		return err
	}
	t.indexOrderReady = dirtyOnly
	// Exceptions are marked per node; candidate j is excepted when the node
	// the index places at j is marked (the current node unless the index is
	// stale). Positions the index does not cover are never excepted.
	// Slots only change with the index order or the captured snapshot.
	state := t.PriorGraphState
	if t.indexSlotsState != state || t.indexSlotsCapture != state.captureID || len(t.indexSlots) != nodeCount {
		slots := &state.slots
		if cap(t.indexSlots) < nodeCount {
			t.indexSlots = make([]int32, nodeCount)
		} else {
			t.indexSlots = t.indexSlots[:nodeCount]
		}
		for index := range t.indexSlots {
			t.indexSlots[index] = noIndexSlot
			if index < len(t.indexOrder) {
				t.indexSlots[index] = int32(slots.slotOf(t.indexOrder[index]))
			}
		}
		t.indexSlotsState = state
		t.indexSlotsCapture = state.captureID
	}

	retainedReferences := int64(0)
	addPair := func(first, second int) error {
		if retainedReferences > maxTransactionOverlapReferences-2 {
			return fmt.Errorf("TALA transaction overlap references exceed limit %d", maxTransactionOverlapReferences)
		}
		if len(t.overlapCandidates[first]) == 0 {
			t.candidateLists = append(t.candidateLists, int32(first))
		}
		t.overlapCandidates[first] = append(t.overlapCandidates[first], second)
		if len(t.overlapCandidates[second]) == 0 {
			t.candidateLists = append(t.candidateLists, int32(second))
		}
		t.overlapCandidates[second] = append(t.overlapCandidates[second], first)
		retainedReferences += 2
		return nil
	}

	// A sparse geometry trial is cheapest as changed-nodes x graph. When many
	// boxes move, the sweep avoids scanning pairs that are far apart.
	useDirtyScan := dirtyOnly && dirtyCount*4 < nodeCount
	if useDirtyScan {
		// Every (first, second) step is charged: nodeCount units per changed
		// first node, batched after its row (or up to the failing pair). The
		// broad phase reads the same sums (x+width, y+height) from boxes.
		for first, firstNode := range t.Graph.Nodes {
			if !t.dirtyNodeMarks[first] || firstNode.TopLeft == nil {
				continue
			}
			firstBox := boxes[first]
			for second, secondNode := range t.Graph.Nodes {
				if second == first || firstNode == secondNode || secondNode.TopLeft == nil {
					continue
				}
				if t.dirtyNodeMarks[second] && second < first {
					continue
				}
				// transactionBoxesMayInteract(firstNode, secondNode)
				secondBox := boxes[second]
				const maxSafeDelta = 500.0
				if firstBox.left > secondBox.right+maxSafeDelta ||
					firstBox.right+maxSafeDelta < secondBox.left ||
					firstBox.top > secondBox.bottom+maxSafeDelta ||
					firstBox.bottom+maxSafeDelta < secondBox.top {
					continue
				}
				if err := addPair(first, second); err != nil {
					if addErr := steps.add(int64(second + 1)); addErr != nil {
						return addErr
					}
					return steps.flushWith(err)
				}
			}
			if err := steps.add(int64(nodeCount)); err != nil {
				return err
			}
		}
	} else {
		// The positioned nodes with finite boxes, in g.Nodes order.
		for index, node := range t.Graph.Nodes {
			if node.TopLeft != nil && !t.overlapInvalidBoxes[index] {
				t.overlapSweepNodes = append(t.overlapSweepNodes, transactionSweepNode{node: node, index: index})
			}
		}
		xsort.StableFunc(t.overlapSweepNodes, func(left, right transactionSweepNode) int {
			if left.node.TopLeft.X != right.node.TopLeft.X {
				switch {
				case left.node.TopLeft.X < right.node.TopLeft.X:
					return -1
				case right.node.TopLeft.X < left.node.TopLeft.X:
					return 1
				default:
					return 0
				}
			}
			if order := cmp.Compare(left.node.ID, right.node.ID); order != 0 {
				return order
			}
			return cmp.Compare(left.index, right.index)
		})
		t.overlapActiveNodes = t.overlapActiveNodes[:0]
		for _, current := range t.overlapSweepNodes {
			if err := steps.step(); err != nil {
				return err
			}
			kept := t.overlapActiveNodes[:0]
			for _, other := range t.overlapActiveNodes {
				if err := steps.step(); err != nil {
					return err
				}
				// Equality remains active because the legacy broad phase only rejects
				// a pair when there is a strict gap greater than 500.
				if other.node.TopLeft.X+other.node.Width+500 < current.node.TopLeft.X {
					continue
				}
				kept = append(kept, other)
				if other.node == current.node || !transactionBoxesMayInteract(other.node, current.node) {
					continue
				}
				if dirtyOnly {
					if !t.dirtyNodeMarks[other.index] && !t.dirtyNodeMarks[current.index] {
						continue
					}
				}
				if err := addPair(other.index, current.index); err != nil {
					return steps.flushWith(err)
				}
			}
			t.overlapActiveNodes = append(kept, current)
		}

		// Non-finite or negative boxes deliberately follow legacy comparisons,
		// whose NaN behavior cannot be represented by a numeric sweep ordering.
		if hasInvalidBox {
			for first := 0; first < nodeCount; first++ {
				for second := first + 1; second < nodeCount; second++ {
					if !t.overlapInvalidBoxes[first] && !t.overlapInvalidBoxes[second] {
						continue
					}
					if dirtyOnly {
						if !t.dirtyNodeMarks[first] && !t.dirtyNodeMarks[second] {
							continue
						}
					}
					if err := steps.step(); err != nil {
						return err
					}
					firstNode, secondNode := t.Graph.Nodes[first], t.Graph.Nodes[second]
					if firstNode == secondNode || firstNode.TopLeft == nil || secondNode.TopLeft == nil ||
						!transactionBoxesMayInteract(firstNode, secondNode) {
						continue
					}
					if err := addPair(first, second); err != nil {
						return steps.flushWith(err)
					}
				}
			}
		}
	}

	// Visit the filled lists in g.Nodes order (Commit's overlap loop reads
	// candidateLists in this order).
	xsort.Ordered(t.candidateLists)
	for _, index := range t.candidateLists {
		if candidates := t.overlapCandidates[index]; len(candidates) > 1 {
			sort.Ints(candidates)
		}
	}
	if err := steps.add(int64(len(t.overlapCandidates))); err != nil {
		return err
	}
	if err := steps.flush(); err != nil {
		return err
	}
	return t.guard.Finish()
}

// noIndexSlot marks a candidate position the exception index does not cover.
const noIndexSlot = int32(-2)

// isOverlapException reports whether the candidate at g.Nodes position index
// is marked with generation. node is the node being checked: the descendant
// walk marks it too (the marks are also its seen set), but the legacy
// exception index never held it, so a stale index entry naming node itself is
// not an exception.
func (t *Transaction) isOverlapException(index int, node *Node, generation uint32) bool {
	slot := t.indexSlots[index]
	if slot == noIndexSlot {
		return false
	}
	if t.indexOrder[index] == node {
		return false
	}
	return t.marks.hasSlot(slot, t.indexOrder[index], generation)
}

// markDescendantOverlapExceptions performs the descendant walk of
// allDescendantNodesGuarded (with cluster nodes) and marks every descendant
// as an exception. Its units are the walk's (one per push and pop) plus one
// per descendant marked, all charged before Finish as the stepped walk did;
// the walk's seen set is the same marks, so node itself is marked too, which
// no check can observe (a node is never its own candidate).
func (t *Transaction) markDescendantOverlapExceptions(node *Node, generation uint32) error {
	// walkDescendants refills t.descendantNodes; read its length afterwards
	// in a separate statement (the order inside one expression is unspecified).
	units := t.walkDescendants(node, generation)
	units += int64(len(t.descendantNodes))
	if units != 0 {
		if err := t.guard.Add(units); err != nil {
			return err
		}
	}
	return t.guard.Finish()
}

// hasBadPostStateOverlap records its units in steps (the caller's batch). It
// flushes them before every call that charges the guard directly and before
// returning, so the guard observes the legacy sequence of charges.
func (t *Transaction) hasBadPostStateOverlap(nodeIndex int, steps *batchedSteps) (bool, error) {
	node := t.Graph.Nodes[nodeIndex]
	if node == nil || node.TopLeft == nil {
		return node == nil, steps.flush()
	}
	if node.Cluster != nil {
		return false, steps.flush()
	}

	generation := t.beginMarks()
	if node.isContainer || node.isClusterVessel || t.Graph.Sequences[node] != nil {
		if err := steps.flush(); err != nil {
			return false, err
		}
		if err := t.markDescendantOverlapExceptions(node, generation); err != nil {
			return false, err
		}
	}
	if node.Container != nil || node.Cluster != nil || node.Sequence != nil {
		if err := steps.flush(); err != nil {
			return false, err
		}
		if t.walkScratch == nil {
			t.walkScratch = &descendantWalkScratch{seen: make(map[*Node]struct{})}
		}
		ancestors, err := t.Graph.appendAncestorsGuarded(t.ancestorScratch[:0], t.walkScratch, node, t.guard)
		t.walkScratch.resetVisited()
		t.ancestorScratch = ancestors
		if err != nil {
			clear(ancestors)
			return false, err
		}
		slots := &t.PriorGraphState.slots
		for _, ancestor := range ancestors {
			if err := steps.step(); err != nil {
				return false, err
			}
			t.marks.mark(slots, ancestor, generation)
		}
		clear(ancestors)
	}

	pairwiseExceptions, hasPairwiseExceptions := t.PriorGraphState.existingOverlaps.nearExceptionsOf(node)
	right := node.TopLeft.X + node.Width
	bottom := node.TopLeft.Y + node.Height
	// buildPostStateOverlapCandidates left every positioned node's sums
	// (x+width, y+height) in overlapBoxes; nothing has moved since.
	boxes := t.overlapBoxes
	nodeBox := boxes[nodeIndex]
	// One unit per inspected candidate, added up where the loop stops.
	candidates := t.overlapCandidates[nodeIndex]
	for position, otherIndex := range candidates {
		other := t.Graph.Nodes[otherIndex]
		if other == nil {
			if err := steps.add(int64(position + 1)); err != nil {
				return false, err
			}
			return false, steps.flushWith(invariant.New("transaction overlap check encountered a nil graph node"))
		}
		if t.isOverlapException(otherIndex, node, generation) {
			continue
		}
		if hasPairwiseExceptions && pairwiseExceptions.contains(other) {
			continue
		}
		if other.TopLeft == nil || other == node {
			continue
		}
		// transactionBoxesMayInteract(node, other)
		otherBox := boxes[otherIndex]
		const maxSafeDelta = 500.0
		if nodeBox.left > otherBox.right+maxSafeDelta ||
			nodeBox.right+maxSafeDelta < otherBox.left ||
			nodeBox.top > otherBox.bottom+maxSafeDelta ||
			nodeBox.bottom+maxSafeDelta < otherBox.top {
			continue
		}
		deltaValue, units, err := node.deltaToCounted(other, node.TopLeft)
		if addErr := steps.add(units); addErr != nil {
			return false, addErr
		}
		if err != nil {
			if addErr := steps.add(int64(position + 1)); addErr != nil {
				return false, addErr
			}
			return false, steps.flushWith(err)
		}
		delta := float64(deltaValue)
		if node.TopLeft.X < otherBox.right+delta && right+delta > otherBox.left &&
			node.TopLeft.Y < otherBox.bottom+delta && bottom+delta > otherBox.top {
			if err := steps.add(int64(position + 1)); err != nil {
				return false, err
			}
			if err := steps.flush(); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	if err := steps.add(int64(len(candidates))); err != nil {
		return false, err
	}
	if err := steps.flush(); err != nil {
		return false, err
	}
	return false, t.guard.Finish()
}

func (t *Transaction) Commit(ctx context.Context) (err error) {
	// A rejected trial is atomic: callers never observe a half-applied graph.
	// Keep Rollback idempotent because existing call sites also invoke it after
	// an error and after successful candidate evaluation.
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Rollback()
			panic(recovered)
		}
		if err != nil {
			t.Rollback()
		}
	}()

	if ctx == nil {
		return fmt.Errorf("TALA transaction commit requires a context")
	}
	if t.guard == nil {
		return fmt.Errorf("TALA transaction commit requires a work guard")
	}
	// The deferred Rollback above uses no scratch, so returning it first
	// (deferred calls run last-in first-out) is safe.
	previousScratch := t.commitScratch
	borrowed := t.Graph.borrowCommitScratch()
	t.commitScratch = borrowed
	// An operation that clears (and re-arms) its own transaction must not
	// disturb the operation list iterated below: the range keeps running the
	// operations it started with, as it always did (see Clear).
	t.runningOps++
	defer func() {
		t.runningOps--
		t.commitScratch = previousScratch
		t.Graph.returnCommitScratch(borrowed)
	}()
	if err := t.guard.Finish(); err != nil {
		return err
	}
	// The same polls as before, answered from the cached Done channel. Polls
	// around caller operations always look (a context without a Done channel
	// is asked Err every time); per-node polls are strided.
	poll := newContextPoll(ctx)
	if err := poll.check(); err != nil {
		return err
	}
	for _, op := range t.Ops {
		if err := t.guard.Step(); err != nil {
			return err
		}
		if err := poll.check(); err != nil {
			return err
		}
		if err := op(); err != nil {
			return err
		}
		if err := poll.check(); err != nil {
			return err
		}
		// Syncing clusters will reposition cluster nodes, which may shrink containers
		// But repositioning containers can also expand clusters, since cluster nodes can be containers
		// TODO by this logic I think we need to run reposition containers again after syncCluster, but can't find a scenario where it matter
		if t.options.AffectContainers {
			if err := t.repositionContainers(&poll); err != nil {
				return err
			}
			// No node moves while the containers are checked, so fixed-origin
			// lookups and per-node scan data can be shared by all checks of
			// this pass.
			var origins fixedOriginCache
			t.beginContainerPass()
			if t.memo != nil && poll.done != nil && !t.PriorGraphState.hasFixedTopLeft {
				handled, memoErr := t.checkContainersMemo(&poll, &origins)
				if memoErr != nil {
					return memoErr
				}
				if handled {
					t.Graph.SyncClusters()
					t.Graph.SyncSequences()
					if err := poll.check(); err != nil {
						return err
					}
					continue
				}
				// Some container is bad (or a check fails): the legacy loop
				// below runs from the start and charges as it goes.
				t.beginContainerPass()
				origins = fixedOriginCache{}
			}
			for _, container := range t.containerCheckOrder() {
				if err := t.guard.Step(); err != nil {
					return err
				}
				if err := poll.err(); err != nil {
					return err
				}
				if container == nil || container.TopLeft == nil {
					continue
				}
				bad, badErr := t.containerBad(container, &origins)
				if badErr != nil {
					return badErr
				}
				if bad {
					return ErrInvalidCandidate
				}
			}
		}
		t.Graph.SyncClusters()
		t.Graph.SyncSequences()
		if err := poll.check(); err != nil {
			return err
		}
	}

	useDirtyValidation, err := t.collectOverlapValidationNodes(&poll)
	if err != nil {
		return err
	}
	if err := t.collectFixedOrigins(&poll); err != nil {
		return err
	}
	// Containment and fixed-position invariants remain graph-wide. Pairwise
	// overlap is validated afterward from one stable post-state sweep. The
	// checks are pure, so their units are charged in one batch that is flushed
	// before any return.
	steps := batchedWork(t.guard)
	// Without containment checks and without fixed origins, the structural
	// check of a node charges its one unit and can only fail through
	// FixedTopLeft (a cluster member is skipped after that unit).
	onlyFixedTopLeft := t.options.IgnoreContainerEscape && len(t.fixedOrigins) == 0
	// The loop's own unit per node and the skipped checks' units are counted
	// locally and added to the batch where the loop stops.
	var loopUnits int64
	nodesToCheck := t.Graph.Nodes
	if onlyFixedTopLeft && t.boxesReady && !t.readyStructure && poll.done != nil {
		// collectOverlapValidationNodes found no nil node and no positioned
		// node outside a cluster with FixedTopLeft, so every node takes the
		// loop's one-unit (unpositioned) or two-unit (skipped) path.
		loopUnits = int64(len(nodesToCheck)) + int64(t.readyPositions)
		nodesToCheck = nil
		if err := poll.skip(int64(len(t.Graph.Nodes))); err != nil {
			if addErr := steps.add(loopUnits); addErr != nil {
				return addErr
			}
			return steps.flushWith(err)
		}
	}
	for _, n := range nodesToCheck {
		loopUnits++
		if err := poll.err(); err != nil {
			if addErr := steps.add(loopUnits); addErr != nil {
				return addErr
			}
			return steps.flushWith(err)
		}
		if n == nil {
			if addErr := steps.add(loopUnits); addErr != nil {
				return addErr
			}
			return steps.flushWith(invariant.New("transaction bad-state check received a nil node"))
		}
		if n.TopLeft == nil {
			continue
		}
		if onlyFixedTopLeft && (n.Cluster != nil || n.FixedTopLeft == nil) {
			loopUnits++
			continue
		}
		var fixedOriginPointer *geo.Point
		if len(t.fixedOrigins) != 0 {
			fixedOrigin, hasFixedOrigin := t.fixedOrigins[n.container()]
			if hasFixedOrigin {
				fixedOriginPointer = &fixedOrigin
			}
		}
		bad, _, badErr := t.Graph.structurallyBadCounted(
			n,
			t.PriorGraphState,
			t.options.IgnoreContainerEscape,
			fixedOriginPointer,
			true,
			&steps,
		)
		if badErr != nil {
			if addErr := steps.add(loopUnits); addErr != nil {
				return addErr
			}
			return steps.flushWith(badErr)
		}
		if bad {
			if addErr := steps.add(loopUnits); addErr != nil {
				return addErr
			}
			return steps.flushWith(ErrInvalidCandidate)
		}
	}
	if err := steps.add(loopUnits); err != nil {
		return err
	}
	if err := steps.flush(); err != nil {
		return err
	}
	if err := t.buildPostStateOverlapCandidates(&poll, useDirtyValidation); err != nil {
		return err
	}
	// candidateLists holds exactly the indices with candidates, ascending.
	for _, listIndex := range t.candidateLists {
		nodeIndex := int(listIndex)
		n := t.Graph.Nodes[nodeIndex]
		if err := steps.step(); err != nil {
			return err
		}
		if err := poll.err(); err != nil {
			return steps.flushWith(err)
		}
		if n.TopLeft == nil {
			continue
		}
		// hasBadPostStateOverlap flushes steps before it returns.
		bad, badErr := t.hasBadPostStateOverlap(nodeIndex, &steps)
		if badErr != nil {
			return badErr
		}
		if bad {
			return ErrInvalidCandidate
		}
	}
	if err := steps.flush(); err != nil {
		return err
	}

	// If a non-exact overlap became an exact overlap, that's bad
	if err := t.checkExistingOverlapRegressions(&poll, &steps, useDirtyValidation); err != nil {
		return err
	}
	if err := steps.flush(); err != nil {
		return err
	}

	if err := poll.check(); err != nil {
		return err
	}
	return t.guard.Finish()
}

// checkExistingOverlapRegressions rejects the commit when a pair that
// overlapped with padding but not exactly at transaction creation now
// overlaps exactly. The legacy loop iterates the near pairs as a map of maps
// (key by key, in map order) and charges one unit per key and one per ordered
// pair it inspects (with dirty validation, the pairs with a changed
// endpoint), polling with each unit, and stops at the first regression.
//
// Without a regression the totals do not depend on the order, so they are
// counted from the dirty nodes' pairs and charged at once, with the polls
// accounted the same way. A regression (or a context without a Done channel,
// whose Err calls are observable) runs the legacy loop itself over the map
// form; its map order is then as arbitrary as before.
func (t *Transaction) checkExistingOverlapRegressions(poll *contextPoll, steps *batchedSteps, useDirtyValidation bool) error {
	t.ensureCommitScratch()
	set := t.PriorGraphState.existingOverlaps
	if set == nil || set.nearKeys == 0 {
		return nil
	}
	if poll.done == nil || steps.direct == nil {
		return t.checkExistingOverlapRegressionsLegacy(poll, steps, useDirtyValidation)
	}
	units := int64(set.nearKeys)
	regression := false
	if useDirtyValidation {
		marks, generation := t.markOverlapDirtyNodes(set)
		for _, i := range t.overlapDirtyList {
			if len(set.near[i]) == 0 {
				continue
			}
			exact := set.mark(true, int(i))
			first := set.nodes[i]
			for _, j := range set.near[i] {
				// The ordered pair (i, j); and (j, i) when j has not changed,
				// since a changed j counts it among its own pairs.
				units++
				if marks[j] != generation {
					units++
				}
				if !set.marked(int(j), exact) && first.DoesOverlapExact(set.nodes[j]) {
					regression = true
				}
			}
		}
	} else {
		units += int64(set.nearRefs)
		for i, neighbors := range set.near {
			if len(neighbors) == 0 {
				continue
			}
			exact := set.mark(true, i)
			first := set.nodes[i]
			for _, j := range neighbors {
				if !set.marked(int(j), exact) && first.DoesOverlapExact(set.nodes[j]) {
					regression = true
				}
			}
		}
	}
	if regression {
		return t.checkExistingOverlapRegressionsLegacy(poll, steps, useDirtyValidation)
	}
	if err := steps.add(units); err != nil {
		return err
	}
	if err := poll.skip(units); err != nil {
		return steps.flushWith(err)
	}
	return nil
}

// markOverlapDirtyNodes stamps the set's numbers of the changed g.Nodes
// entries (collectOverlapValidationNodes marked them) with a new generation
// and lists them, in g.Nodes order, in t.overlapDirtyList.
func (t *Transaction) markOverlapDirtyNodes(set *existingOverlapSet) ([]uint32, uint32) {
	if cap(t.overlapDirtyMarks) < len(set.nodes) {
		t.overlapDirtyMarks = make([]uint32, len(set.nodes))
		t.overlapDirtyGeneration = 0
	}
	marks := t.overlapDirtyMarks[:len(set.nodes)]
	t.overlapDirtyGeneration++
	if t.overlapDirtyGeneration == 0 {
		clear(marks)
		t.overlapDirtyGeneration = 1
	}
	generation := t.overlapDirtyGeneration
	dirty := t.overlapDirtyList[:0]
	for index, node := range t.Graph.Nodes {
		if !t.dirtyNodeMarks[index] {
			continue
		}
		i := set.indexOf(node)
		if i < 0 || marks[i] == generation {
			continue
		}
		marks[i] = generation
		dirty = append(dirty, int32(i))
	}
	t.overlapDirtyList = dirty
	return marks, generation
}

// checkExistingOverlapRegressionsLegacy is the stepped check: one unit and
// one poll per node with a near pair and per ordered pair it inspects (with
// dirty validation, the pairs with a changed endpoint), stopping at the first
// regression. It walks the set in its own order: nodes in first-insertion
// order, each node's neighbors in insertion order. The original walked the
// pairs as a map of maps, so where it stopped, the units it charged, and,
// when MaxTransactionWorkUnits binds, whether a commit failed varied from run
// to run; the units of a check without a regression are the same in any
// order.
func (t *Transaction) checkExistingOverlapRegressionsLegacy(poll *contextPoll, steps *batchedSteps, useDirtyValidation bool) error {
	set := t.PriorGraphState.existingOverlaps
	if set == nil {
		return nil
	}
	var marks []uint32
	var generation uint32
	if useDirtyValidation {
		marks, generation = t.markOverlapDirtyNodes(set)
	}
	for i, neighbors := range set.near {
		if len(neighbors) == 0 {
			continue
		}
		if err := steps.step(); err != nil {
			return err
		}
		if err := poll.err(); err != nil {
			return steps.flushWith(err)
		}
		exact := set.mark(true, i)
		first := set.nodes[i]
		firstChanged := useDirtyValidation && marks[i] == generation
		for _, j := range neighbors {
			if useDirtyValidation && !firstChanged && marks[j] != generation {
				continue
			}
			if err := steps.step(); err != nil {
				return err
			}
			if err := poll.err(); err != nil {
				return steps.flushWith(err)
			}
			if !set.marked(int(j), exact) && first.DoesOverlapExact(set.nodes[j]) {
				return steps.flushWith(ErrInvalidCandidate)
			}
		}
	}
	return nil
}

// Clear only needs to be called if transaction will be reused. Outside
// Commit the operation list keeps its storage; nothing else holds it (clones
// copy it). While a Commit of this transaction runs its operations (an
// operation clearing and re-arming its own transaction), Clear starts a new
// list as it always did, so the running Commit still runs every operation it
// started with and none added after the Clear.
func (t *Transaction) Clear() {
	if t.runningOps > 0 {
		t.Ops = make([]func() error, 0)
		return
	}
	clear(t.Ops)
	t.Ops = t.Ops[:0]
}

func (t *Transaction) Rollback() {
	state := t.PriorGraphState
	if state.captureTopology {
		state.graph.restore(t.Graph)

		for cluster, snapshot := range state.clusters {
			*cluster = snapshot.value
			cluster.Nodes = snapshot.nodes.restore()
			cluster.EdgeAbductions = snapshot.edgeAbductions.restore()
		}
		for sequence, snapshot := range state.sequences {
			*sequence = snapshot.value
			sequence.Nodes = snapshot.nodes.restore()
			sequence.EdgeAbductions = snapshot.edgeAbductions.restore()
		}
		for tree, snapshot := range state.trees {
			*tree = snapshot.value
			tree.Children = snapshot.children.restore()
		}
		for abduction, snapshot := range state.edgeAbductions {
			*abduction = snapshot
		}
		for herd, snapshot := range state.herds {
			*herd = snapshot.value
			herd.oppositeSidePaired = restoreMap(snapshot.value.oppositeSidePaired, snapshot.oppositeSidePaired)
			herd.sameSidePaired = restoreMap(snapshot.value.sameSidePaired, snapshot.sameSidePaired)
		}
		for hierarchy, snapshot := range state.hierarchies {
			*hierarchy = snapshot.value
			hierarchy.level = restoreMap(snapshot.value.level, snapshot.level)
		}
		for edge, snapshot := range state.edges {
			snapshot.restore(edge)
		}
		for node, snapshot := range state.nodes {
			snapshot.restore(node)
		}
		return
	}

	// Every restore writes only its own node (nodes sharing a TopLeft pointer
	// recorded the same value for it), so the order is immaterial.
	for i := range state.slots.entries {
		entry := &state.slots.entries[i]
		node := entry.node
		node.TopLeft = entry.geometry.topLeft.restore()
		node.Width = entry.geometry.width
		node.Height = entry.geometry.height
	}
	for _, snapshot := range state.clusterStates {
		snapshot.cluster.Arrangement = snapshot.arrangement
		snapshot.cluster.DesiredArrangement = snapshot.desired
		snapshot.cluster.Padding = snapshot.padding
	}
	for edge, snapshot := range state.edgeGeometry {
		snapshot.restore(edge)
	}
	for _, snapshot := range state.treeOrientations {
		snapshot.tree.Orientation = snapshot.orientation
	}
	copy(state.originalNodesRef, state.originalNodes)
	t.Graph.Nodes = state.originalNodesRef
}

// CloneGeometryContext creates a nested speculative transaction that may only
// move geometry. GraphState snapshots are immutable for such trials, so sharing
// the rollback point avoids cloning every node map and overlap-exception set.
// UpdateState detaches on the first accepted candidate before recycling state.
func (t *Transaction) CloneGeometryContext() (*Transaction, error) {
	if t == nil || t.PriorGraphState == nil || t.guard == nil {
		return nil, fmt.Errorf("TALA geometry transaction clone requires an initialized transaction")
	}
	if t.PriorGraphState.captureTopology {
		return nil, fmt.Errorf("TALA topology transaction requires an independent clone")
	}
	if err := t.guard.Finish(); err != nil {
		return nil, err
	}
	// The source and clone now have equal read-only ownership of this rollback
	// point. Force either side's next UpdateState to allocate its own scratch
	// state instead of recycling and mutating the shared snapshot.
	t.priorGraphStateShared = true
	return &Transaction{
		Ops:                   cloneSlice(t.Ops),
		Graph:                 t.Graph,
		options:               t.options,
		guard:                 t.guard,
		PriorGraphState:       t.PriorGraphState,
		priorGraphStateShared: true,
		sharedOverlaps:        true,
	}, nil
}
