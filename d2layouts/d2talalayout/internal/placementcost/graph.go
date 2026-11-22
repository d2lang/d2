package placementcost

import (
	"context"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/typedpool"
	"github.com/d2lang/d2/lib/geo"
)

const invalidEdgeLengthState uint64 = 0

// stateHash is FNV-1a (64-bit), the hash/fnv New64a function, computed
// inline: word feeds a value's eight little-endian bytes, exactly the bytes
// binary.LittleEndian.PutUint64 used to write into the hash.
type stateHash uint64

const (
	fnv64Offset stateHash = 14695981039346656037
	fnv64Prime  stateHash = 1099511628211
)

func (h stateHash) byte(b byte) stateHash {
	return (h ^ stateHash(b)) * fnv64Prime
}

func (h stateHash) word(v uint64) stateHash {
	h = (h ^ stateHash(byte(v))) * fnv64Prime
	h = (h ^ stateHash(byte(v>>8))) * fnv64Prime
	h = (h ^ stateHash(byte(v>>16))) * fnv64Prime
	h = (h ^ stateHash(byte(v>>24))) * fnv64Prime
	h = (h ^ stateHash(byte(v>>32))) * fnv64Prime
	h = (h ^ stateHash(byte(v>>40))) * fnv64Prime
	h = (h ^ stateHash(byte(v>>48))) * fnv64Prime
	return (h ^ stateHash(byte(v>>56))) * fnv64Prime
}

func placementEdgeLengthState(graph *layoutgraph.Graph, costs layoutgraph.RoutingCostState, options EdgeLengthOptions) uint64 {
	h := edgeLengthStateHeader(graph, costs, options)

	// Process nodes
	for _, n := range graph.Nodes {
		if n.TopLeft == nil {
			return invalidEdgeLengthState
		}
		h = edgeLengthStateNode(h, n)
	}

	// Process edge abductions
	for _, ea := range options.EdgeAbductions {
		h = h.word(uint64(ea.Edge.ID))
		if ea.OriginallyFrom != nil {
			h = h.word(uint64(ea.OriginallyFrom.ID))
		}
		if ea.OriginallyTo != nil {
			h = h.word(uint64(ea.OriginallyTo.ID))
		}
		if ea.CurrentTo != nil {
			h = h.word(uint64(ea.CurrentTo.ID))
		}
		if ea.CurrentFrom != nil {
			h = h.word(uint64(ea.CurrentFrom.ID))
		}
	}

	return uint64(h)
}

// edgeLengthStateHeader hashes the options and graph costs, the start of
// every EdgeLength cache key.
func edgeLengthStateHeader(graph *layoutgraph.Graph, costs layoutgraph.RoutingCostState, options EdgeLengthOptions) stateHash {
	h := fnv64Offset

	// Process booleans
	var flags byte
	if options.IncludeNodeSizes {
		flags |= 1 << 0
	}
	if options.EnforceMinimumGap {
		flags |= 1 << 1
	}
	if options.PenalizeDirection {
		flags |= 1 << 2
	}
	h = h.byte(flags)

	// Process graph costs
	h = h.word(math.Float64bits(graph.CellSize))
	h = h.word(math.Float64bits(costs.Turn))
	h = h.word(math.Float64bits(costs.NonCenterPort))
	h = h.word(math.Float64bits(costs.Crossing))
	return h
}

// edgeLengthStateNode hashes one node (TopLeft present) into a cache key.
func edgeLengthStateNode(h stateHash, n *layoutgraph.Node) stateHash {
	h = h.word(uint64(n.ID))
	h = h.word(math.Float64bits(n.TopLeft.X))
	h = h.word(math.Float64bits(n.TopLeft.Y))
	h = h.word(math.Float64bits(n.Width))
	h = h.word(math.Float64bits(n.Height))
	for _, e := range n.Edges {
		h = h.word(uint64(e.ID))
	}
	return h
}

func edgeLengthWorkerCount(nodeCount, gomaxprocs int) int {
	if nodeCount <= 0 {
		return 0
	}
	// Preserve the inexpensive sequential path for small graphs.
	if nodeCount <= 10 {
		return 1
	}
	if gomaxprocs < 1 {
		gomaxprocs = 1
	}
	return min(nodeCount, gomaxprocs)
}

// EdgeLength evaluates the graph's placement cost and may populate its
// graph-owned edge-length cache.
func EdgeLength(ctx context.Context, graph *layoutgraph.Graph, options EdgeLengthOptions) (float64, error) {
	p := newScoringPoll(ctx)
	if err := p.entry(); err != nil {
		return 0, err
	}

	edgeLengthState := placementEdgeLengthState(graph, graph.RoutingCosts(), options)
	if edgeLengthState != invalidEdgeLengthState {
		if d, ok := graph.LookupEdgeLengthCost(edgeLengthState); ok {
			return d, nil
		}
	}

	totalSum, err := sumNodeEdgeLengths(ctx, &p, graph, options)
	if err != nil {
		return 0, err
	}

	crossings, err := GraphEdgeCrossings(ctx, graph)
	if err != nil {
		return 0, err
	}
	totalSum = addCrossingCost(totalSum, graph, crossings)
	if err := p.check(); err != nil {
		return 0, err
	}
	if edgeLengthState != invalidEdgeLengthState {
		graph.StoreEdgeLengthCost(edgeLengthState, totalSum)
	}

	return totalSum, nil
}

// addCrossingCost adds the crossing penalty to an EdgeLength sum. Like
// edgeLengthTermValue it is the single, non-inlined place where this sum is
// formed, so every evaluation path performs the same operations.
//
//go:noinline
func addCrossingCost(totalSum float64, graph *layoutgraph.Graph, crossings int64) float64 {
	totalSum += graph.CrossingCost() * float64(crossings)
	return totalSum
}

// edgeCenters holds the centers of an edge's endpoints, computed once per
// count by the same Center method the pair loop used to call per pair.
type edgeCenters struct {
	from, to geo.Point
}

func countEdgeCrossingsExcludingSharedNodes(p *scoringPoll, edges []*layoutgraph.Edge, centers []edgeCenters) (int64, error) {
	centers = centers[:len(edges)]
	var crossings int64
	for i := 0; i < len(edges)-1; i++ {
		if err := p.at(i); err != nil {
			return 0, err
		}
		edge1 := edges[i]
		segment1 := &centers[i]
		for j := i + 1; j < len(edges); j++ {
			if err := p.at(j - i - 1); err != nil {
				return 0, err
			}
			edge2 := edges[j]

			// Skip if edges share a common node
			if edge1.From == edge2.From || edge1.From == edge2.To ||
				edge1.To == edge2.From || edge1.To == edge2.To {
				continue
			}

			// Check if the segments between the endpoint centers cross
			segment2 := &centers[j]
			if layoutgraph.SegmentsCross(&segment1.from, &segment1.to, &segment2.from, &segment2.to) {
				crossings++
			}
		}
	}
	if err := p.check(); err != nil {
		return 0, err
	}
	return crossings, nil
}

type graphEdgeCrossingScratch struct {
	levelGroup map[int]int
	groups     [][]*layoutgraph.Edge
	active     int
	centers    []edgeCenters
}

const maxPooledGraphEdgeCrossingEntries = 4096

var graphEdgeCrossingScratchPool = typedpool.New(func() *graphEdgeCrossingScratch {
	return &graphEdgeCrossingScratch{levelGroup: make(map[int]int)}
})

func putGraphEdgeCrossingScratch(scratch *graphEdgeCrossingScratch) {
	if len(scratch.levelGroup) > maxPooledGraphEdgeCrossingEntries ||
		cap(scratch.groups) > maxPooledGraphEdgeCrossingEntries ||
		cap(scratch.centers) > maxPooledGraphEdgeCrossingEntries {
		return
	}
	remaining := maxPooledGraphEdgeCrossingEntries - cap(scratch.groups)
	for _, edges := range scratch.groups {
		if cap(edges) > remaining {
			return
		}
		remaining -= cap(edges)
	}

	for i := 0; i < scratch.active; i++ {
		clear(scratch.groups[i][:cap(scratch.groups[i])])
		scratch.groups[i] = scratch.groups[i][:0]
	}
	clear(scratch.levelGroup)
	scratch.active = 0
	graphEdgeCrossingScratchPool.Put(scratch)
}

// GraphEdgeCrossings counts crossings between edges that do not share a node.
func GraphEdgeCrossings(ctx context.Context, graph *layoutgraph.Graph) (int64, error) {
	p := newScoringPoll(ctx)
	if err := p.entry(); err != nil {
		return 0, err
	}
	if len(graph.Edges) == 0 {
		if err := p.check(); err != nil {
			return 0, err
		}
		return 0, nil
	}

	scratch := graphEdgeCrossingScratchPool.Get()
	defer putGraphEdgeCrossingScratch(scratch)

	for i, edge := range graph.Edges {
		if err := p.at(i); err != nil {
			return 0, err
		}
		if edge.From == nil || edge.To == nil || edge.From.TopLeft == nil || edge.To.TopLeft == nil {
			continue
		}
		fromLevel := edge.From.ContainerLevel()
		toLevel := edge.To.ContainerLevel()
		if fromLevel != toLevel {
			continue
		}

		groupIndex, exists := scratch.levelGroup[fromLevel]
		if !exists {
			groupIndex = scratch.active
			if groupIndex == len(scratch.groups) {
				scratch.groups = append(scratch.groups, nil)
			} else {
				scratch.groups[groupIndex] = scratch.groups[groupIndex][:0]
			}
			scratch.active++
			scratch.levelGroup[fromLevel] = groupIndex
		}
		scratch.groups[groupIndex] = append(scratch.groups[groupIndex], edge)
	}

	var totalCrossings int64
	for _, groupIndex := range scratch.levelGroup {
		edges := scratch.groups[groupIndex]
		if cap(scratch.centers) < len(edges) {
			scratch.centers = make([]edgeCenters, len(edges))
		}
		centers := scratch.centers[:len(edges)]
		for k, edge := range edges {
			centers[k] = edgeCenters{from: *edge.From.Center(), to: *edge.To.Center()}
		}
		crossings, err := countEdgeCrossingsExcludingSharedNodes(&p, edges, centers)
		if err != nil {
			return 0, err
		}
		totalCrossings += crossings
	}

	if err := p.check(); err != nil {
		return 0, err
	}
	return totalCrossings, nil
}

// ContainerAlignmentCost scores misalignment between peer containers.
func ContainerAlignmentCost(ctx context.Context, graph *layoutgraph.Graph) (float64, error) {
	p := newScoringPoll(ctx)
	if err := p.entry(); err != nil {
		return 0, err
	}
	l := 0.0
	for i := 0; i < len(graph.Nodes)-1; i++ {
		if err := p.at(i); err != nil {
			return 0, err
		}
		n1 := graph.Nodes[i]
		if !n1.IsContainer() {
			continue
		}
		for j := i + 1; j < len(graph.Nodes); j++ {
			if err := p.at(j - i - 1); err != nil {
				return 0, err
			}
			n2 := graph.Nodes[j]
			if !n2.IsContainer() {
				continue
			}
			if n1.EffectiveContainer() != n2.EffectiveContainer() {
				continue
			}
			if n1.Width == n2.Width && n1.Height == n2.Height {
				if n1.TopLeft.X != n2.TopLeft.X && n1.TopLeft.Y != n2.TopLeft.Y {
					l += graph.NonCenterPortCost()
				}
			}
		}
	}
	if err := p.check(); err != nil {
		return 0, err
	}
	return l, nil
}
