package layoutgraph

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sync"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/invariant"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/typedpool"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

const (
	minCellSize           = 10.0
	turnPenaltyMultiplier = 0.125
	// Keep the exact value because center-port route scoring is output-sensitive.
	centerPortMultiplier = 0.04287499999999999
)

type Graph struct {
	// Unfortunate, but because we represent root as nil, we need to indicate root hierarchy as a field in graph
	IsRootHierarchy bool
	Nodes           []*Node
	Edges           []*Edge
	CellSize        float64

	// ovg
	crossingCost      float64
	turnCost          float64
	nonCenterPortCost float64
	costMu            sync.RWMutex

	// These are special entities within a graph:

	// -- Containers are nodes which have nodes enclosed inside them
	// ---- They can be nested
	// ---- The top level is considered a container of nil
	// ---- These are inferred
	Containers map[*Node][]*Node

	// -- Clusters are nodes which should stick together and can be reoriented and resized
	// ---- They can be oriented vertically or horizontally depending on where its connections are
	// ---- They can be resized to be all the same dimensions
	// ---- These are inferred
	Clusters map[*Node]*Cluster

	// -- Trees are nodes which are in a tree structure (i.e. any non-cyclic subgraph)
	// ---- They are oriented in one of 4 directions, depending on where its root node ends up
	// ---- These are inferred
	// ---- Trees are pruned from the graph, then after node placement, they are reattached and placed as trees
	Trees      map[*Node][]*Tree
	NodeToTree map[*Node]*Tree

	Hubs map[*Node][]*Node

	Sequences map[*Node]*Sequence

	//  -- Directions is used to lookup the direction the edges should point towards within each container
	// e.g. Bottom means the edges should primarily go from Top to Bottom
	Directions          map[*Node]geo.Orientation
	CommonUncleSiblings map[*Node]Nodes

	edgeLengthCache map[uint64]float64

	// snapshotSizeHint is the node count of the latest transaction snapshot
	// of this graph; it only sizes the next snapshot's storage.
	snapshotSizeHint int32
	// commitScratchFree holds validation scratch for transactions on this
	// graph (see commitScratch).
	commitScratchFree []*commitScratch
	// Released transactions' snapshot storage (see Transaction.Release).
	spareGraphStates []*GraphState
	spareOverlapSets []*existingOverlapSet
	// spareContainerMemos holds released transactions' container memos.
	spareContainerMemos []*containerMemo
	// prefilter is Transaction.PlacementRejected's scratch for transactions
	// on this graph (calls do not nest).
	prefilter *placementPrefilter
	// sharedInput marks a graph that several goroutines read at the same
	// time (see MarkSharedInput).
	sharedInput bool
}

// MarkSharedInput records that g will be read by several goroutines at the
// same time (the seeds' common input), so validation must not write the
// lookup hints it otherwise leaves on the objects it visits. Call it before
// the goroutines start; g must not be changed while they read it.
func (g *Graph) MarkSharedInput() {
	g.sharedInput = true
}

type descendantWalkEntry struct {
	node *Node
	emit bool
}

type descendantWalkScratch struct {
	// seen backs the ancestor walk's long chains and the descendant walk once
	// it has visited more than walkLinearSeen nodes; seenUsed records that it
	// holds entries. Shorter descendant walks search visited linearly, so a
	// map grown by one large walk is not cleared again by every small one.
	seen         map[*Node]struct{}
	seenUsed     bool
	visited      []*Node
	visitedInMap bool
	stack        []descendantWalkEntry
	// stackHigh is the largest stack length since the last release.
	stackHigh   int
	descendants []*Node
	// exceptions holds hasBadOverlapStateContext's descendant and ancestor list.
	exceptions []*Node
}

const walkLinearSeen = 64

// resetVisited empties the descendant walk's seen set.
func (scratch *descendantWalkScratch) resetVisited() {
	clear(scratch.visited)
	scratch.visited = scratch.visited[:0]
	if scratch.seenUsed {
		clear(scratch.seen)
		scratch.seenUsed = false
	}
	scratch.visitedInMap = false
}

func (scratch *descendantWalkScratch) markVisited(node *Node) {
	scratch.visited = append(scratch.visited, node)
	if scratch.visitedInMap {
		scratch.seen[node] = struct{}{}
		return
	}
	if len(scratch.visited) > walkLinearSeen {
		if scratch.seen == nil {
			scratch.seen = make(map[*Node]struct{}, 2*walkLinearSeen)
		}
		for _, visited := range scratch.visited {
			scratch.seen[visited] = struct{}{}
		}
		scratch.visitedInMap = true
		scratch.seenUsed = true
	}
}

func (scratch *descendantWalkScratch) wasVisited(node *Node) bool {
	if scratch.visitedInMap {
		_, ok := scratch.seen[node]
		return ok
	}
	return slices.Contains(scratch.visited, node)
}

var descendantWalkPool = typedpool.New(func() *descendantWalkScratch {
	return &descendantWalkScratch{seen: make(map[*Node]struct{})}
})

func NewGraph() *Graph {
	return &Graph{
		Nodes: make([]*Node, 0),
		Edges: make([]*Edge, 0),

		Containers:      make(map[*Node][]*Node),
		Clusters:        make(map[*Node]*Cluster),
		Trees:           make(map[*Node][]*Tree),
		Hubs:            make(map[*Node][]*Node),
		Sequences:       make(map[*Node]*Sequence),
		Directions:      make(map[*Node]geo.Orientation),
		edgeLengthCache: make(map[uint64]float64),
	}
}

// Direction reports the layout direction assigned to container.
func (g *Graph) Direction(container *Node) geo.Orientation {
	if direction, has := g.Directions[container]; has {
		return direction
	}
	return geo.NONE
}

func (g *Graph) isBadStateContext(node *Node, graphState *GraphState, ignoreContainerEscape bool, guard workStepper) (bool, error) {
	return g.isBadStateWithOrigins(node, graphState, ignoreContainerEscape, nil, guard)
}

// isBadStateWithOrigins is isBadStateContext with an optional cache of
// containerFixedOrigin answers that is valid while no geometry changes.
func (g *Graph) isBadStateWithOrigins(node *Node, graphState *GraphState, ignoreContainerEscape bool, origins *fixedOriginCache, guard workStepper) (bool, error) {
	steps := newBatchedSteps(guard)
	bad, skipOverlapCheck, err := g.structurallyBadCountedWithOrigins(node, graphState, ignoreContainerEscape, nil, false, origins, &steps)
	if flushErr := steps.flush(); flushErr != nil {
		return false, flushErr
	}
	if err != nil || bad {
		return bad, err
	}
	if skipOverlapCheck {
		return false, guard.Finish()
	}
	return g.hasBadOverlapStateContext(node, graphState, guard)
}

// fixedOriginCache answers node.Graph.containerFixedOrigin(node.container())
// for one pass of checks during which no node moves. A graph without any
// FixedTopLeft node has no fixed origin in any container.
type fixedOriginCache struct {
	graph    *Graph
	hasFixed bool
	ready    bool
	origins  map[*Node]*geo.Point
}

func (cache *fixedOriginCache) lookup(node *Node) *geo.Point {
	g := node.Graph
	if g != cache.graph || !cache.ready {
		cache.graph = g
		cache.hasFixed = false
		// containerFixedOrigin only answers from a node with FixedTopLeft. This
		// walk visits nodes in the same order up to the first such node, so a
		// nil node fails here exactly where that scan would fail.
		for _, n := range g.Nodes {
			if n.FixedTopLeft != nil {
				cache.hasFixed = true
				break
			}
		}
		// The map is only read when some node has FixedTopLeft.
		if cache.origins == nil {
			if cache.hasFixed {
				cache.origins = make(map[*Node]*geo.Point)
			}
		} else {
			clear(cache.origins)
		}
		cache.ready = true
	}
	if !cache.hasFixed {
		return nil
	}
	container := node.container()
	if origin, ok := cache.origins[container]; ok {
		return origin
	}
	origin := g.containerFixedOrigin(container)
	cache.origins[container] = origin
	return origin
}

// structurallyBadCounted checks the linear-time invariants of node that do
// not depend on any other non-descendant graph node (transactions check
// pairwise overlaps separately, against relevant candidates only), with its
// units recorded in steps. The check has no side effects, so the caller may
// charge the units after it returns (before acting on the result).
func (g *Graph) structurallyBadCounted(node *Node, graphState *GraphState, ignoreContainerEscape bool, fixedOrigin *geo.Point, fixedOriginCached bool, steps *batchedSteps) (bad, skipOverlapCheck bool, err error) {
	return g.structurallyBadCountedWithOrigins(node, graphState, ignoreContainerEscape, fixedOrigin, fixedOriginCached, nil, steps)
}

// structurallyBadCountedWithOrigins answers an uncached fixed-origin lookup
// from origins when it is non-nil.
func (g *Graph) structurallyBadCountedWithOrigins(node *Node, graphState *GraphState, ignoreContainerEscape bool, fixedOrigin *geo.Point, fixedOriginCached bool, origins *fixedOriginCache, steps *batchedSteps) (bad, skipOverlapCheck bool, err error) {
	if node == nil || node.TopLeft == nil {
		return true, false, invariant.New("transaction bad-state check received an incomplete node")
	}
	if err := steps.step(); err != nil {
		return false, false, err
	}
	// No need to check if cluster node is in bad state, the vessel covers it
	// Right now only happens when checking if containers bad state, since cluster nodes are still containers
	if node.Cluster != nil {
		return false, true, nil
	}
	if !ignoreContainerEscape {
		if c := node.container(); c != nil &&
			c.TopLeft != nil && node.TopLeft != nil &&
			!c.Surrounds(node, 0) {
			return true, false, nil
		}
		if node.isContainer {
			for _, child := range g.Containers[node] {
				if err := steps.step(); err != nil {
					return false, false, err
				}
				if child == nil || child.TopLeft == nil {
					return false, false, invariant.New("transaction container has an incomplete child")
				}
				if !node.Surrounds(child, 0) {
					return true, false, nil
				}
			}
		}
	}

	// Nothing should be past the fixed origin. Transactions cache the first
	// fixed origin for each container once per commit; standalone callers retain
	// the legacy lookup path.
	pastFixedOrigin := false
	if fixedOriginCached {
		pastFixedOrigin = fixedOrigin != nil &&
			(node.TopLeft.X < fixedOrigin.X || node.TopLeft.Y < fixedOrigin.Y)
	} else if origins != nil {
		// isPointPastFixedOrigin(x, y, true) with a cached origin.
		origin := origins.lookup(node)
		pastFixedOrigin = origin != nil &&
			(node.TopLeft.X < origin.X || node.TopLeft.Y < origin.Y)
	} else {
		pastFixedOrigin = node.isPointPastFixedOrigin(node.TopLeft.X, node.TopLeft.Y, true)
	}
	if pastFixedOrigin {
		return true, false, nil
	}

	if node.FixedTopLeft != nil && graphState != nil {
		originalPoint := geo.Point{}
		if original, exists := graphState.slots.lookup(node); exists && original.topLeft.pointer != nil {
			originalPoint = original.topLeft.value
		}
		if node.TopLeft.X != originalPoint.X || node.TopLeft.Y != originalPoint.Y {
			return true, false, nil
		}
	}
	return false, false, nil
}

func (g *Graph) hasBadOverlapStateContext(node *Node, graphState *GraphState, guard workStepper) (bool, error) {
	if node == nil || node.TopLeft == nil {
		return true, invariant.New("transaction overlap check received an incomplete node")
	}
	if node.Cluster != nil {
		return false, nil
	}
	var pairwiseExceptions *existingOverlapSet
	if graphState != nil {
		pairwiseExceptions = graphState.existingOverlaps
	}

	// The exception list is scratch: it is only read by the overlap scan below.
	// Build it in pooled storage with the same walks and unit charges as
	// allDescendantNodesGuarded followed by a guarded walk up the ancestors.
	scratch := descendantWalkPool.Get()
	defer releaseDescendantWalkScratch(scratch)
	exceptions := scratch.exceptions[:0]
	if node.isContainer || node.isClusterVessel || g.Sequences[node] != nil {
		var err error
		exceptions, err = g.appendDescendantNodesGuarded(exceptions, scratch, node, true, guard)
		if err != nil {
			scratch.exceptions = exceptions
			return false, err
		}
	}
	// doesOverlapWithDimensionsContext already skips node identity, so only
	// descendants and ancestors need to be materialized as exceptions.
	if node.Container != nil || node.Cluster != nil || node.Sequence != nil {
		var err error
		exceptions, err = g.appendAncestorsGuarded(exceptions, scratch, node, guard)
		if err != nil {
			scratch.exceptions = exceptions
			return false, err
		}
	}
	scratch.exceptions = exceptions
	return g.doesOverlapWithDimensionsContext(node, node.TopLeft, node.Width, node.Height, exceptions, pairwiseExceptions, guard)
}

// mirrorAxes performs a reflection of the graph (both horizontal and vertical according to x and y)
// When the graph has containers, the contents of those containers are recursively reflected iff it is reachable from other nodes
// If it is reachable, reflecting maintains edge length. Otherwise, reflecting could reverse edge directions undesirably

// For example, we want to mirror the two outer nodes, but do not want to mirror the contents of the container
//
// . ┌─────────┐
// . │         │
// . │   ┌──┐  │
// . │   │  │  │
// . │   └─┬┘  │
// . │     │   │
// . │   ┌─▼┐  │
// . │   └──┘  │
// . │         │
// . └────▲────┘
// .      │
// .      │
// .  ┌───┴───┐
// .  │       │
// .  └───────┘
// Currently used by container child over container center
func (g *Graph) AddNodeUnchecked(node *Node) {
	g.Nodes = append(g.Nodes, node)
	node.Graph = g
}

func (g *Graph) AddNodeToContainer(container, node *Node) {
	g.Containers[container] = append(
		g.Containers[container],
		node,
	)
	node.Container = container
	if container != nil {
		container.isContainer = true
	}
}

func (g *Graph) AddNewNodeToContainer(container, node *Node) {
	g.AddNodeUnchecked(node)
	g.AddNodeToContainer(container, node)
}

func (g *Graph) AddNode(node *Node) *Node {
	if occupant, is := g.isOccupied(node.TopLeft); !is {
		g.AddNodeUnchecked(node)
		return node
	} else {
		return occupant
	}
}

func (g *Graph) RemoveNode(node *Node) {
	newNodes := make([]*Node, 0, len(g.Nodes)-1)

	for _, n := range g.Nodes {
		if n != node {
			newNodes = append(newNodes, n)
		}
	}

	g.Nodes = newNodes
}

func (g *Graph) AddEdge(edge *Edge) {
	if slices.Contains(g.Edges, edge) {
		return
	}
	g.Edges = append(g.Edges, edge)
}

func (g *Graph) ComputeCellSize() {
	minHeight := math.Inf(1)
	minWidth := math.Inf(1)
	maxHeight := math.Inf(-1)
	maxWidth := math.Inf(-1)

	for _, node := range g.Nodes {
		minWidth = math.Min(minWidth, node.Width)
		minHeight = math.Min(minHeight, node.Height)
		maxWidth = math.Max(maxWidth, node.Width)
		maxHeight = math.Max(maxHeight, node.Height)
	}

	minLength := math.Min(minWidth, minHeight)
	maxLength := math.Max(maxWidth, maxHeight)

	if maxLength < (3.0 * minLength) {
		g.CellSize = math.Ceil(maxLength)
	} else {
		g.CellSize = math.Ceil((3.0 * minLength) / 2.0)
	}
	g.CellSize = math.Max(g.CellSize, minCellSize)
}

func (g *Graph) Disconnect(edge *Edge) {
	edge.From.removeEdge(edge)
	edge.To.removeEdge(edge)

	newEdges := make([]*Edge, 0, len(g.Edges))
	for _, oldEdge := range g.Edges {
		if oldEdge != edge {
			newEdges = append(newEdges, oldEdge)
		}
	}

	g.Edges = newEdges
}

func (g *Graph) Connect(nodeA, nodeB *Node) *Edge {
	edge := NewEdge(nodeA, nodeB)
	g.Edges = append(g.Edges, edge)
	nodeA.addEdge(edge)
	if nodeA != nodeB {
		nodeB.addEdge(edge)
	}
	return edge
}

func (g *Graph) doesOverlapWithDimensionsContext(node *Node, p *geo.Point, newWidth, newHeight float64, exceptions []*Node, pairwiseExceptions *existingOverlapSet, guard workStepper) (bool, error) {
	if node == nil || p == nil {
		return true, invariant.New("overlap check received an incomplete node")
	}
	right := p.X + newWidth
	bottom := p.Y + newHeight
	// Unit charges are batched (see batchedSteps); every return below flushes
	// them first, so the guard sees the same units in the same order relative
	// to the errors this scan can report.
	steps := newBatchedSteps(guard)
	// Membership in a short exception list is a pointer comparison; longer lists
	// keep the set. Either way an excepted node is exactly a listed node.
	var exceptionSet map[*Node]struct{}
	var exceptionList []*Node
	if len(exceptions) > 0 {
		if len(exceptions) <= maxLinearOverlapExceptions {
			exceptionList = exceptions
			if err := steps.add(int64(len(exceptions))); err != nil {
				return false, err
			}
		} else {
			if len(exceptions) > maxPooledOverlapExceptions {
				// Large maps must not enter the pool after a partially populated failure.
				exceptionSet = make(map[*Node]struct{}, len(exceptions))
			} else {
				scratch := overlapExceptionPool.Get()
				defer releaseOverlapExceptions(scratch)
				if scratch.nodes == nil {
					scratch.nodes = make(map[*Node]struct{}, len(exceptions))
				}
				exceptionSet = scratch.nodes
			}
			for _, exception := range exceptions {
				if err := steps.step(); err != nil {
					return false, err
				}
				exceptionSet[exception] = struct{}{}
			}
		}
	}
	// Every candidate comparison uses the same moved-node exception set. Resolve
	// it once instead of repeating an outer-map lookup for every graph node.
	pairwiseNodeExceptions, hasPairwiseExceptions := pairwiseExceptions.nearExceptionsOf(node)

	for _, otherNode := range g.Nodes {
		if err := steps.step(); err != nil {
			return false, err
		}
		if otherNode == nil {
			return false, steps.flushWith(invariant.New("overlap check encountered a nil graph node"))
		}
		if otherNode == node {
			continue
		}
		if otherNode.TopLeft != nil {
			// This heuristic is only a trial filter; the transaction constructor uses
			// the proven graph-wide padding above as its construction bound.
			const maxSafeDelta = 500.0
			if p.X > otherNode.TopLeft.X+otherNode.Width+maxSafeDelta ||
				p.X+newWidth+maxSafeDelta < otherNode.TopLeft.X ||
				p.Y > otherNode.TopLeft.Y+otherNode.Height+maxSafeDelta ||
				p.Y+newHeight+maxSafeDelta < otherNode.TopLeft.Y {
				continue
			}
			// Distant boxes cannot overlap regardless of the exception sets. Test
			// geometry first so sparse layouts avoid two map probes per box.
			if hasPairwiseExceptions && pairwiseNodeExceptions.contains(otherNode) {
				continue
			}
			if exceptionList != nil {
				if slices.Contains(exceptionList, otherNode) {
					continue
				}
			} else if exceptionSet != nil {
				if _, excepted := exceptionSet[otherNode]; excepted {
					continue
				}
			}
			deltaValue, units, err := node.deltaToCounted(otherNode, p)
			if addErr := steps.add(units); addErr != nil {
				return false, addErr
			}
			if err != nil {
				return false, steps.flushWith(err)
			}
			delta := float64(deltaValue)

			// Standard overlap test
			if (p.X < (otherNode.TopLeft.X + otherNode.Width + delta)) && ((right + delta) > otherNode.TopLeft.X) &&
				(p.Y < (otherNode.TopLeft.Y + otherNode.Height + delta)) && ((bottom + delta) > otherNode.TopLeft.Y) {
				if err := steps.flush(); err != nil {
					return false, err
				}
				return true, nil
			}
		}
	}
	if err := steps.flush(); err != nil {
		return false, err
	}
	return false, guard.Finish()
}

// WouldOverlapWithWorkGuard reports whether placing node at point overlaps an
// unexcepted graph node while charging the complete comparison.
func (g *Graph) WouldOverlapWithWorkGuard(
	node *Node,
	point *geo.Point,
	exceptions []*Node,
	pairwiseExceptions map[*Node]map[*Node]struct{},
	guard *limits.WorkGuard,
) (bool, error) {
	if guard == nil {
		return false, fmt.Errorf("TALA overlap check requires a work guard")
	}
	pairwise := existingOverlapSetFromMap(pairwiseExceptions)
	if node == nil {
		return g.doesOverlapWithDimensionsContext(
			node,
			point,
			0,
			0,
			exceptions,
			pairwise,
			guard,
		)
	}
	return g.doesOverlapWithDimensionsContext(
		node,
		point,
		node.Width,
		node.Height,
		exceptions,
		pairwise,
		guard,
	)
}

func (g *Graph) isOccupied(p *geo.Point) (*Node, bool) {
	for _, n := range g.Nodes {
		if (n.TopLeft != nil) && p != nil && nonNilEquals(n.TopLeft, p) {
			return n, true
		}
	}
	return nil, false
}

func (node *Node) isPointPastFixedOrigin(x, y float64, includeSizes bool) bool {
	fixedOrigin := node.Graph.containerFixedOrigin(node.container())
	if fixedOrigin == nil {
		return false
	}
	if !includeSizes {
		fixedOrigin.X = math.Round(fixedOrigin.X / node.Graph.CellSize)
		fixedOrigin.Y = math.Round(fixedOrigin.Y / node.Graph.CellSize)
	}
	return x < fixedOrigin.X || y < fixedOrigin.Y
}

// if this graph is a subgraph, return the container of the least nested node
// normally this is the nil container
func (nodes Nodes) subgraphContainer() *Node {
	var root *Node
	minLevel := math.MaxInt
	for _, n := range nodes {
		c := n.container()
		if c == nil {
			return nil
		}
		level := c.containerLevel()
		if level < minLevel {
			minLevel = level
			root = c
		}
	}
	return root
}

func (g *Graph) bounds() (*geo.Point, *geo.Point) {
	tl, br := g.boundingBox(true)
	if tl == nil || br == nil {
		return nil, nil
	}
	// Placement consumes a pixel-aligned graph box even when edge points are
	// fractional.
	return geo.NewPoint(math.Round(tl.X), math.Round(tl.Y)), geo.NewPoint(math.Round(br.X), math.Round(br.Y))
}

func (g *Graph) unroundedBounds() (*geo.Point, *geo.Point) {
	return g.boundingBox(false)
}

func (g *Graph) boundingBox(roundNodeDimensions bool) (*geo.Point, *geo.Point) {
	var tl, br *geo.Point
	if roundNodeDimensions {
		tl, br = Nodes(g.Nodes).fixedBounds()
	} else {
		tl, br = Nodes(g.Nodes).unroundedFixedBounds()
	}
	if tl == nil || br == nil {
		return nil, nil
	}

	minX := tl.X
	minY := tl.Y

	maxX := br.X
	maxY := br.Y

	for _, edge := range g.Edges {
		edgeTL, edgeBR := edge.boundingBoxValues()
		if !math.IsInf(edgeTL.X, 0) {
			minX = math.Min(minX, edgeTL.X)
			minY = math.Min(minY, edgeTL.Y)
			maxX = math.Max(maxX, edgeBR.X)
			maxY = math.Max(maxY, edgeBR.Y)
		}

	}

	return geo.NewPoint(minX, minY), geo.NewPoint(maxX, maxY)
}

func (g *Graph) turnCostValue() float64 {
	g.costMu.RLock()
	if g.turnCost != 0.0 {
		cost := g.turnCost
		g.costMu.RUnlock()
		return cost
	}
	g.costMu.RUnlock()

	g.costMu.Lock()
	defer g.costMu.Unlock()
	if g.turnCost != 0.0 {
		return g.turnCost
	}
	cost := turnPenaltyMultiplier * float64(len(g.Edges)) * g.maxEdgeLength()
	g.turnCost = cost

	return cost
}

func (g *Graph) nonCenterPortCostValue() float64 {
	g.costMu.RLock()
	if g.nonCenterPortCost != 0.0 {
		cost := g.nonCenterPortCost
		g.costMu.RUnlock()
		return cost
	}
	g.costMu.RUnlock()

	g.costMu.Lock()
	defer g.costMu.Unlock()
	if g.nonCenterPortCost != 0.0 {
		return g.nonCenterPortCost
	}
	if len(g.Edges) == 0 {
		return 0
	}
	cost := centerPortMultiplier * float64(len(g.Edges)) * g.maxEdgeLength()

	// For certain diagrams, the `cost` above can be smaller than the distance between ports
	// and then edge routing doesn't prefer center ports as expected
	// ┌──────────────────────────────────────────────────────┐
	// │                                   port               │
	// |                                distance              |
	// │                                                      │
	// │                           │──────────────────│       │
	// └────────┬──────────────────┬──────────────────┬───────┘
	//          │                  │                  │
	//          │                  │                  │
	// the routine below estimates another cost based on the min node size
	minSize := math.Inf(1)
	for _, n := range g.Nodes {
		if n.isContainer {
			continue
		}
		minSize = math.Min(minSize, n.Height)
		minSize = math.Min(minSize, n.Width)
	}

	if !math.IsInf(minSize, 1) {
		cost = math.Max(cost, minSize/3.)
	}
	g.nonCenterPortCost = cost

	return cost
}

func (g *Graph) crossingCostValue() float64 {
	g.costMu.RLock()
	if g.crossingCost != 0.0 {
		cost := g.crossingCost
		g.costMu.RUnlock()
		return cost
	}
	g.costMu.RUnlock()

	g.costMu.Lock()
	defer g.costMu.Unlock()
	if g.crossingCost != 0.0 {
		return g.crossingCost
	}
	// c * (m * l_max)

	cost := CrossingCostWeight * float64(len(g.Edges)) * g.maxEdgeLength()
	g.crossingCost = cost

	return cost
}

func (g *Graph) resetTurnCost() {
	g.costMu.Lock()
	g.turnCost = 0
	g.costMu.Unlock()
}

func (g *Graph) maxEdgeLength() float64 {
	hasLength := false
	maxLength := float64(ConnectedNodeGap)
	for _, n := range g.Nodes {
		if n.TopLeft == nil {
			continue
		}
		for _, e := range n.Edges {
			adj := n.adjacent(e)
			if adj.TopLeft == nil {
				continue
			}
			hasLength = true
			distance := n.distanceTo(adj, true)
			maxLength = math.Max(maxLength, distance)
		}
	}
	if !hasLength {
		return 0.0
	}
	return maxLength
}

// SplitOptions controls which relationships connect nodes while splitting a
// graph into subgraphs.
type SplitOptions struct {
	IncludeContainers bool
	IncludeNears      bool
	TraverseTrees     bool
}

// SplitSubgraphs partitions the graph into sets of mutually reachable nodes.
func (g *Graph) SplitSubgraphs(ctx context.Context, options SplitOptions) ([]*Graph, error) {
	return g.splitSubgraphsWithOwnership(ctx, options, nil, nil)
}

type nodeGraphOwnershipJournal map[*Node]*Graph

func (journal nodeGraphOwnershipJournal) restore() {
	for node, graph := range journal {
		node.Graph = graph
	}
}

// splitSubgraphsWithOwnership optionally returns every Node.Graph value changed
// by the split. Reachability includes the bounded Near closure, so a caller can
// restore even Near-only nodes that are not members of the source Graph.Nodes.
func (g *Graph) splitSubgraphsWithOwnership(
	ctx context.Context,
	options SplitOptions,
	ownership *nodeGraphOwnershipJournal,
	sharedGuard workStepper,
) ([]*Graph, error) {
	guard := sharedGuard
	if guard == nil {
		if err := validateEngineGraph(ctx, "SplitSubgraphs", g); err != nil {
			return nil, err
		}
		var err error
		guard, err = limits.NewWorkGuard(ctx, "SplitSubgraphs", maxEngineWorkUnits)
		if err != nil {
			return nil, err
		}
	} else if err := guard.Finish(); err != nil {
		return nil, err
	}
	originalGraphReferences := make(map[*Node]*Graph, len(g.Nodes))
	for _, node := range g.Nodes {
		if err := guard.Step(); err != nil {
			return nil, err
		}
		if node != nil {
			originalGraphReferences[node] = node.Graph
		}
	}
	complete := false
	defer func() {
		if complete {
			return
		}
		for node, graph := range originalGraphReferences {
			node.Graph = graph
		}
	}()
	graphs := make([]*Graph, 0)
	if len(g.Nodes) == 0 {
		complete = true
		return graphs, nil
	}

	added := map[*Node]struct{}{}
	nodeToSubgraph := make(map[*Node]*Graph, len(g.Nodes))
	addReachable := func(startingNode *Node, subgraph *Graph) error {
		reachableNodes, err := startingNode.allReachableNodesGuarded(
			options.IncludeContainers,
			options.IncludeNears,
			options.TraverseTrees,
			nil,
			guard,
		)
		if err != nil {
			return err
		}
		for _, node := range reachableNodes {
			if err := guard.Step(); err != nil {
				return err
			}
			if _, was := added[node]; was {
				continue
			}
			if _, captured := originalGraphReferences[node]; !captured {
				if len(originalGraphReferences) >= maxEngineNodes {
					return fmt.Errorf("TALA SplitSubgraphs unique node ownership exceeds limit %d", maxEngineNodes)
				}
				originalGraphReferences[node] = node.Graph
			}
			subgraph.AddNodeUnchecked(node)
			added[node] = struct{}{}
			nodeToSubgraph[node] = subgraph
		}
		return nil
	}

	fixedNodes := make([]*Node, 0)
	for _, node := range g.Nodes {
		if err := guard.Step(); err != nil {
			return nil, err
		}
		if node.FixedTopLeft != nil {
			fixedNodes = append(fixedNodes, node)
		}
	}
	if len(fixedNodes) > 0 {
		// create first subgraph with all fixed nodes
		// it will be placed at 0,0 in CombineSubgraphs so fixed locations are accurate
		subgraph := NewGraph()
		subgraph.CopyEntitiesFrom(g)

		for _, startingNode := range fixedNodes {
			if err := addReachable(startingNode, subgraph); err != nil {
				return nil, err
			}
		}

		graphs = append(graphs, subgraph)
	}

	for _, startingNode := range g.Nodes {
		if err := guard.Step(); err != nil {
			return nil, err
		}
		if _, was := added[startingNode]; was {
			continue
		}

		subgraph := NewGraph()
		subgraph.CopyEntitiesFrom(g)
		if err := addReachable(startingNode, subgraph); err != nil {
			return nil, err
		}

		graphs = append(graphs, subgraph)
	}

	// Each original edge is considered once. This preserves the original graph's
	// edge order without the quadratic duplicate scan in AddEdge. In the unusual
	// case where a traversal deliberately omits one endpoint into another
	// subgraph, retain the edge in both subgraphs as the old endpoint scan did.
	seenEdges := make(map[*Graph]map[*Edge]struct{}, len(graphs))
	appendEdge := func(graph *Graph, edge *Edge) {
		graphEdges := seenEdges[graph]
		if graphEdges == nil {
			graphEdges = make(map[*Edge]struct{})
			seenEdges[graph] = graphEdges
		}
		if _, seen := graphEdges[edge]; seen {
			return
		}
		graphEdges[edge] = struct{}{}
		graph.Edges = append(graph.Edges, edge)
	}
	for _, edge := range g.Edges {
		if err := guard.Step(); err != nil {
			return nil, err
		}
		fromGraph := nodeToSubgraph[edge.From]
		toGraph := nodeToSubgraph[edge.To]
		if fromGraph != nil {
			appendEdge(fromGraph, edge)
		}
		if toGraph != nil && toGraph != fromGraph {
			appendEdge(toGraph, edge)
		}
	}
	if err := guard.Finish(); err != nil {
		return nil, err
	}

	if ownership != nil {
		*ownership = originalGraphReferences
	}
	complete = true
	return graphs, nil
}

func (g *Graph) area() float64 {
	if len(g.Nodes) == 0 {
		return 0
	}
	topLeft, bottomRight := g.unroundedBounds()
	return math.Abs(topLeft.X-bottomRight.X) * math.Abs(topLeft.Y-bottomRight.Y)
}

// 2D bin packing with rectangles
// Algorithm:
// 1. keep a list of candidate points, which are 3 non-upper-left corners of every placed subgraph. Initially, just (0,0)
// 2. calculate how much empty space remains after potential placement
// 3. the min empty space candidate point gets the subgraph placed there
// Start from largest to smallest subgraphs
func (g *Graph) containerRDFSOrder(root *Node) []*Node {
	var order []*Node

	if root != nil && !root.isContainer {
		return order
	}

	for _, child := range slices.Backward(g.Containers[root]) {

		if child.isContainer {
			order = append(order, g.containerRDFSOrder(child)...)
			order = append(order, child)
			continue
		}

		if child.isClusterVessel {
			cluster := g.Clusters[child]
			for _, cNode := range slices.Backward(cluster.Nodes) {

				if cNode.isContainer {
					order = append(order, g.containerRDFSOrder(cNode)...)
					order = append(order, cNode)
				}
			}
			continue
		}
	}

	return order
}

func (g *Graph) containerRDFSOrderContext(root *Node, guard workStepper) ([]*Node, error) {
	var order []*Node
	if root != nil && !root.isContainer {
		return order, nil
	}

	for _, child := range slices.Backward(g.Containers[root]) {
		if err := guard.Step(); err != nil {
			return nil, err
		}

		if child.isContainer {
			descendants, err := g.containerRDFSOrderContext(child, guard)
			if err != nil {
				return nil, err
			}
			order = append(order, descendants...)
			order = append(order, child)
			continue
		}
		if child.isClusterVessel {
			cluster := g.Clusters[child]
			for _, clusterNode := range slices.Backward(cluster.Nodes) {
				if err := guard.Step(); err != nil {
					return nil, err
				}

				if clusterNode.isContainer {
					descendants, err := g.containerRDFSOrderContext(clusterNode, guard)
					if err != nil {
						return nil, err
					}
					order = append(order, descendants...)
					order = append(order, clusterNode)
				}
			}
		}
	}
	return order, nil
}

func (g *Graph) syncNested() {
	for _, n := range g.Nodes {
		if n.isContainer {
			n.positionContainerChildren(true)
		}
		if n.isClusterVessel {
			c := g.Clusters[n]
			c.SyncGeometry()
			// Since cluster nodes can themselves be containers, we can't merely move all children nodes, we must
			// also care to give container children padding
			for _, cn := range c.Nodes {
				if cn.isContainer {
					padding := g.containerPadding(cn, true)
					for _, child := range g.Containers[cn] {
						child.moveNodeWithChildren(padding.left, padding.top)
					}
				}
			}
		}
		if seq, is := g.Sequences[n]; is {
			seq.SyncGeometry()
		}
	}
}

// includeClusterNodes should be included when the purpose is to move all nodes
// But if the purpose is to find nodes to perform operations on, the cluster nodes should not be included (vessels will be included)
// Does not include current node
func (g *Graph) allDescendantNodes(node *Node, includeClusterNodes bool) []*Node {
	guard := unboundedWork
	descendants, err := g.allDescendantNodesGuarded(node, includeClusterNodes, guard)
	if err != nil {
		panic(err)
	}
	return descendants
}

func (g *Graph) allDescendantNodesGuarded(node *Node, includeClusterNodes bool, guard workStepper) ([]*Node, error) {
	scratch := descendantWalkPool.Get()
	defer releaseDescendantWalkScratch(scratch)
	descendants, err := g.appendDescendantNodesGuarded(scratch.descendants[:0], scratch, node, includeClusterNodes, guard)
	scratch.descendants = descendants
	if err != nil {
		return nil, err
	}
	return append([]*Node(nil), descendants...), nil
}

// retainable reports whether the scratch is small enough to pool (see
// typedpool.MaxRetainedEntries). visited records every node a walk marks and
// the ancestor walk's set holds nodes it also appends to its result, so the
// capacities bound what seen has held.
func (scratch *descendantWalkScratch) retainable() bool {
	const limit = typedpool.MaxRetainedEntries
	return len(scratch.seen) <= limit && cap(scratch.visited) <= limit && cap(scratch.stack) <= limit &&
		cap(scratch.descendants) <= limit && cap(scratch.exceptions) <= limit
}

// releaseDescendantWalkScratch drops every graph pointer held by pooled walk
// storage and returns it to the pool. Only the storage used since the last
// release can hold pointers, so only that is cleared. Oversized storage is
// left to the collector.
func releaseDescendantWalkScratch(scratch *descendantWalkScratch) {
	if !scratch.retainable() {
		return
	}
	scratch.resetVisited()
	clear(scratch.stack[:scratch.stackHigh])
	scratch.stackHigh = 0
	// The whole capacity: callers store the lists they appended to back into
	// the scratch only when the walk returns, so after a panic the appended
	// entries lie beyond the stored length.
	clear(scratch.descendants[:cap(scratch.descendants)])
	clear(scratch.exceptions[:cap(scratch.exceptions)])
	scratch.stack = scratch.stack[:0]
	scratch.descendants = scratch.descendants[:0]
	scratch.exceptions = scratch.exceptions[:0]
	descendantWalkPool.Put(scratch)
}

// forEachDescendant applies fn to allDescendantNodes(node,
// includeClusterNodes) in order, without copying the list out of the pooled
// walk storage.
func (g *Graph) forEachDescendant(node *Node, includeClusterNodes bool, fn func(*Node)) {
	scratch := descendantWalkPool.Get()
	defer releaseDescendantWalkScratch(scratch)
	descendants, err := g.appendDescendantNodesGuarded(scratch.descendants[:0], scratch, node, includeClusterNodes, unboundedWork)
	scratch.descendants = descendants
	if err != nil {
		panic(err)
	}
	for _, descendant := range descendants {
		fn(descendant)
	}
}

// appendDescendantNodesGuarded appends allDescendantNodesGuarded's result to
// dst using scratch's walk state, with identical unit charges and Finish.
func (g *Graph) appendDescendantNodesGuarded(dst []*Node, scratch *descendantWalkScratch, node *Node, includeClusterNodes bool, guard workStepper) ([]*Node, error) {
	scratch.resetVisited()
	if len(scratch.stack) > scratch.stackHigh {
		scratch.stackHigh = len(scratch.stack)
	}
	scratch.stack = scratch.stack[:0]
	// The walk has no other failure than the guard and observes nothing else
	// through it, so its units are charged in one batch before Finish.
	steps := newBatchedSteps(guard)
	if node != nil {
		scratch.markVisited(node)
	}
	pushChildren := func(parent *Node) error {
		// Push in reverse traversal order so the iterative walk retains the old
		// container-before-cluster-before-sequence preorder.
		if sequence := g.Sequences[parent]; sequence != nil {
			for _, v := range slices.Backward(sequence.Nodes) {
				if err := steps.step(); err != nil {
					return err
				}
				scratch.stack = append(scratch.stack, descendantWalkEntry{node: v, emit: includeClusterNodes})
			}
		}
		if parent != nil && parent.isClusterVessel {
			if cluster := g.Clusters[parent]; cluster != nil {
				for _, v := range slices.Backward(cluster.Nodes) {
					if err := steps.step(); err != nil {
						return err
					}
					scratch.stack = append(scratch.stack, descendantWalkEntry{node: v, emit: includeClusterNodes})
				}
			}
		}
		if parent == nil || parent.isContainer {
			children := g.Containers[parent]
			for _, child := range slices.Backward(children) {
				if err := steps.step(); err != nil {
					return err
				}
				scratch.stack = append(scratch.stack, descendantWalkEntry{node: child, emit: true})
			}
		}
		if len(scratch.stack) > scratch.stackHigh {
			scratch.stackHigh = len(scratch.stack)
		}
		return nil
	}
	if err := pushChildren(node); err != nil {
		return dst, err
	}

	for len(scratch.stack) > 0 {
		if err := steps.step(); err != nil {
			return dst, err
		}
		last := len(scratch.stack) - 1
		current := scratch.stack[last]
		scratch.stack = scratch.stack[:last]
		if current.node == nil {
			continue
		}
		if scratch.wasVisited(current.node) {
			continue
		}
		scratch.markVisited(current.node)
		if current.emit {
			dst = append(dst, current.node)
		}
		if err := pushChildren(current.node); err != nil {
			return dst, err
		}
	}
	if err := steps.flush(); err != nil {
		return dst, err
	}
	if err := guard.Finish(); err != nil {
		return dst, err
	}
	return dst, nil
}

// appendAncestorsGuarded appends node's ancestors to dst, nearest first,
// following Container, else Cluster.Vessel, else Sequence.Vessel (node
// itself excluded). It charges one unit per node of the chain, node
// included, fails on a cycle or a chain of more than maxEngineNodes nodes,
// and ends with guard.Finish. Short chains are checked for repeats by
// scanning the chain itself.
func (g *Graph) appendAncestorsGuarded(dst []*Node, scratch *descendantWalkScratch, node *Node, guard workStepper) ([]*Node, error) {
	const linearChain = 32
	start := len(dst)
	steps := newBatchedSteps(guard)
	useSet := false
	visited := 0
	for current := node; current != nil; {
		if err := steps.step(); err != nil {
			return dst, err
		}
		// The chain so far is node followed by dst[start:].
		var repeated bool
		if useSet {
			_, repeated = scratch.seen[current]
		} else {
			repeated = visited > 0 && (current == node || slices.Contains(dst[start:], current))
		}
		if repeated {
			return dst, steps.flushWith(invariant.New("cycle in transaction ancestry"))
		}
		visited++
		if !useSet && visited > linearChain {
			if scratch.seen == nil {
				scratch.seen = make(map[*Node]struct{})
			}
			clear(scratch.seen)
			scratch.seenUsed = true
			scratch.seen[node] = struct{}{}
			for _, ancestor := range dst[start:] {
				scratch.seen[ancestor] = struct{}{}
			}
			useSet = true
		}
		if useSet {
			scratch.seen[current] = struct{}{}
		}
		if visited > maxEngineNodes {
			return dst, steps.flushWith(invariant.New("transaction ancestry exceeds node limit"))
		}
		if current != node {
			// don't include node itself
			dst = append(dst, current)
		}
		if current.Container != nil {
			current = current.Container
		} else if current.Cluster != nil {
			current = current.Cluster.Vessel
		} else if current.Sequence != nil {
			current = current.Sequence.Vessel
		} else {
			break
		}
	}
	if err := steps.flush(); err != nil {
		return dst, err
	}
	return dst, guard.Finish()
}

func (g *Graph) isTreeSentinel(n *Node) bool {
	_, is := g.Trees[n]
	return is
}

func (g *Graph) isSequence(n *Node) bool {
	_, is := g.Sequences[n]
	return is
}

func (g *Graph) CopyEntitiesFrom(other *Graph) {
	g.Containers = other.Containers
	g.Clusters = other.Clusters
	g.Trees = other.Trees
	g.NodeToTree = other.NodeToTree
	g.Sequences = other.Sequences
	g.Hubs = other.Hubs
	g.Directions = other.Directions
	g.CommonUncleSiblings = other.CommonUncleSiblings
	// TODO maybe not
	g.IsRootHierarchy = other.IsRootHierarchy
}

func (nodes Nodes) fixedNodes() []*Node {
	var fixed []*Node
	for _, n := range nodes {
		if n.FixedTopLeft != nil {
			fixed = append(fixed, n)
		}
	}
	return fixed
}

func (g *Graph) fixedNodes() []*Node {
	return Nodes(g.Nodes).fixedNodes()
}

func (nodes Nodes) hasFixedNode() bool {
	for _, n := range nodes {
		if n.FixedTopLeft != nil {
			return true
		}
	}
	return false
}

func (g *Graph) hasFixedNode() bool {
	return Nodes(g.Nodes).hasFixedNode()
}

// If container A has child B at (80,30) with a FixedTopLeft of (70,20) (ignoring padding)
// then the FixedOrigin point of the nodes [B,C] should be at (10,10), despite their bounding box TL being at (80,30).
// This way when A positions itself around its children, it has the space for B's FixedTopLeft to be correct.
// | Note: This assumes the relative positions between fixed nodes are correct:
// |  if C also has a FixedTopLeft, then it must be (70,50), otherwise B and C's positions are inconsistent.
//
// .  ┌(10,10)───────────────┐
// .  │ A                    │
// .  │          ┌(80,30)┐   │
// .  │          │   B   ├─┐ │
// .  │          └───────┘ │ │
// .  │          ┌(80,60)┐ │ │
// .  │          │   C   ├─┘ │
// .  │          └───────┘   │
// .  └──────────────────────┘
func (nodes Nodes) fixedOrigin() *geo.Point {
	container := nodes.subgraphContainer()
	for _, child := range nodes {
		if child.container() != container {
			continue
		}
		// the origin is the same for all fixed nodes in the container (see note above)
		if p := child.fixedOrigin(); p != nil {
			return p
		}
	}
	return nil
}

func (g *Graph) containerFixedOrigin(container *Node) *geo.Point {
	for _, n := range g.Nodes {
		if n.container() != container {
			continue
		}
		if p := n.fixedOrigin(); p != nil {
			return p
		}
	}
	return nil
}

func (node *Node) fixedOrigin() *geo.Point {
	if node.TopLeft != nil && node.FixedTopLeft != nil {
		return geo.NewPoint(
			node.TopLeft.X-node.FixedTopLeft.X,
			node.TopLeft.Y-node.FixedTopLeft.Y,
		)
	}
	return nil
}

// return the amount of padding the container should have on each side. (more padding if there are icons)
// Note: we need the graph reference since container may be the nil container
// considerChildren should be false if the output is used together with the nodes bounding box, becuase it already accounts for icons
func (g *Graph) containerPadding(container *Node, considerChildren bool) Spacing {
	padding := float64(ContainerPadding)
	spacing := Spacing{top: padding, bottom: padding, left: padding, right: padding}
	if container == nil {
		return spacing
	}
	if container.shapeType == circleType {
		// Circles just need padding so the corner of child doesn't touch corner of circle
		// Optically, there will be a lot of padding on the sides even given a small value
		padding /= 4.
	}

	if container.Icon != nil && container.shapeType != imageType && !(container.Icon.PositionFixed() && container.Icon.Position.IsOutside()) {
		// Reserve an interior position only if the icon can use one.
		padding = float64(MaxIconSize + 2*label.PADDING)
	}

	if container.Label != nil && !container.Label.Position.IsOutside() {
		// First ensure total container width/height can accommodate label
		labelWidth := container.Label.Width + 2*label.PADDING
		labelHeight := container.Label.Height + 2*label.PADDING

		switch container.Label.Position {
		case label.InsideTopLeft, label.InsideTopCenter, label.InsideTopRight:
			spacing.top = math.Max(spacing.top, labelHeight)
		case label.InsideBottomLeft, label.InsideBottomCenter, label.InsideBottomRight:
			spacing.bottom = math.Max(spacing.bottom, labelHeight)
		case label.InsideMiddleLeft:
			spacing.left = math.Max(spacing.left, labelWidth)
		case label.InsideMiddleRight:
			spacing.right = math.Max(spacing.right, labelWidth)
		}

		// Ensure total width/height can accommodate label
		minContainerWidth := labelWidth + spacing.left + spacing.right
		if container.Width < minContainerWidth {
			extraPadding := math.Ceil((minContainerWidth - container.Width) / 2)
			spacing.left = math.Max(spacing.left, extraPadding)
			spacing.right = math.Max(spacing.right, extraPadding)
		}

		minContainerHeight := labelHeight + spacing.top + spacing.bottom
		if container.Height < minContainerHeight {
			extraPadding := math.Ceil((minContainerHeight - container.Height) / 2)
			spacing.top = math.Max(spacing.top, extraPadding)
			spacing.bottom = math.Max(spacing.bottom, extraPadding)
		}
	}

	if considerChildren {
		var hasChildWithIcon bool
		var childrenMargin Spacing
		for _, child := range g.Containers[container] {
			if !hasChildWithIcon && child.Icon != nil && child.shapeType != imageType && !child.Icon.PositionFixed() {
				hasChildWithIcon = true
			}
			childrenMargin.left = math.Max(childrenMargin.left, child.margin.left)
			childrenMargin.right = math.Max(childrenMargin.right, child.margin.right)
			childrenMargin.top = math.Max(childrenMargin.top, child.margin.top)
			childrenMargin.bottom = math.Max(childrenMargin.bottom, child.margin.bottom)
		}
		spacing.left = math.Max(spacing.left, container.padding.left+childrenMargin.left)
		spacing.right = math.Max(spacing.right, container.padding.right+childrenMargin.right)
		spacing.top = math.Max(spacing.top, container.padding.top+childrenMargin.top)
		spacing.bottom = math.Max(spacing.bottom, container.padding.bottom+childrenMargin.bottom)

		if hasChildWithIcon {
			// we don't know if the icon will be placed at an inside position, but leave enough padding so it has that option
			// Note: we don't know what the exact icon size will be until the final layout size so we use max
			padding = float64(MaxIconSize + 2*label.PADDING)
		}
	}

	if container.shapeType == circleType {
		spacing.top /= 4.
		spacing.left /= 4.
		spacing.bottom /= 4.
		spacing.right /= 4.
	}

	spacing.left = math.Max(spacing.left, math.Max(container.padding.left, padding))
	spacing.right = math.Max(spacing.right, math.Max(container.padding.right, padding))
	spacing.top = math.Max(spacing.top, math.Max(container.padding.top, padding))
	spacing.bottom = math.Max(spacing.bottom, math.Max(container.padding.bottom, padding))

	// The center belongs to the authored title. With a single plain child
	// the fitted frame and title position are known before placing the child.
	if children := g.Containers[container]; len(children) == 1 && container.IsRectangular() &&
		container.Icon == nil && container.Label != nil && container.Label.PositionFixed() &&
		container.Label.Position == label.InsideMiddleCenter {
		child := children[0]
		if child.IsRectangular() && !child.isContainer && child.FixedTopLeft == nil && child.Icon == nil &&
			!child.Is3D && !child.IsMultiple && (child.Label == nil || !child.Label.Position.IsOutside()) {
			spacing.reserveCenteredTitle(container, child, len(g.Containers[container.container()]) > 1)
		}
	}

	if children := g.Containers[container]; len(children) == 1 && (container.DesiredWidth != nil || container.DesiredHeight != nil) {
		spacing.fitAuthoredGutters(container, children[0])
	}

	return spacing
}

// reserveCenteredTitle adds just enough top padding to clear either the
// content-sized frame's midpoint or the midpoint of a larger authored frame.
func (s *Spacing) reserveCenteredTitle(container, child *Node, hasSiblings bool) {
	title := container.Label
	width, height := container.fittedDimensions(child.Width, child.Height, *s)
	titleBox := geo.NewBox(geo.NewPoint((width-title.Width)/2, (height-title.Height)/2), title.Width, title.Height)
	childBox := geo.NewBox(geo.NewPoint(s.left, s.top), child.Width, child.Height)
	if titleBox.Overlaps(*childBox) {
		belowTitle := math.Ceil(titleBox.TopLeft.Y + title.Height + label.PADDING)
		// With neighboring frames, move the child within the current box
		// when the remaining band can hold it with a visible border gap.
		if hasSiblings && height-belowTitle-child.Height >= label.PADDING {
			s.top, s.bottom = belowTitle, height-belowTitle-child.Height
			return
		}
		s.top = math.Max(child.Height+s.bottom+title.Height+2*label.PADDING, belowTitle)
	}
}

// fitAuthoredGutters lets an isolated plain tile fit inside a requested frame
// when the usual 60 px margin would be the only reason to grow it.
func (s *Spacing) fitAuthoredGutters(container, child *Node) {
	if !container.IsRectangular() || container.AspectRatio1() || container.Icon != nil ||
		len(container.Edges) > 0 || len(container.Nears) > 0 || (container.Label != nil && !container.Label.Position.IsOutside()) {
		return
	}
	if !child.IsRectangular() || child.isContainer || child.FixedTopLeft != nil || child.Icon != nil ||
		child.IsMultiple || child.Is3D || len(child.Edges) > 0 || len(child.Nears) > 0 {
		return
	}
	if l := child.Label; l != nil && (l.Position.IsOutside() || l.Position.IsBorder() || l.Width > child.Width || l.Height > child.Height) {
		return
	}
	if container.DesiredWidth != nil {
		gap := max(NodeGap, math.Floor((*container.DesiredWidth-child.Width)/2))
		s.left, s.right = min(s.left, gap), min(s.right, gap)
	}
	if container.DesiredHeight != nil {
		gap := max(NodeGap, math.Floor((*container.DesiredHeight-child.Height)/2))
		s.top, s.bottom = min(s.top, gap), min(s.bottom, gap)
	}
}
