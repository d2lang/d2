package placementcost

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// referenceEdgeLengthState is the original hash/fnv implementation.
func referenceEdgeLengthState(graph *layoutgraph.Graph, costs layoutgraph.RoutingCostState, options EdgeLengthOptions) uint64 {
	h := fnv.New64a()
	var b [8]byte
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
	h.Write([]byte{flags})
	for _, v := range []float64{graph.CellSize, costs.Turn, costs.NonCenterPort, costs.Crossing} {
		binary.LittleEndian.PutUint64(b[:], math.Float64bits(v))
		h.Write(b[:])
	}
	for _, n := range graph.Nodes {
		if n.TopLeft == nil {
			return invalidEdgeLengthState
		}
		for _, v := range []uint64{uint64(n.ID), math.Float64bits(n.TopLeft.X), math.Float64bits(n.TopLeft.Y), math.Float64bits(n.Width), math.Float64bits(n.Height)} {
			binary.LittleEndian.PutUint64(b[:], v)
			h.Write(b[:])
		}
		for _, e := range n.Edges {
			binary.LittleEndian.PutUint64(b[:], uint64(e.ID))
			h.Write(b[:])
		}
	}
	for _, ea := range options.EdgeAbductions {
		binary.LittleEndian.PutUint64(b[:], uint64(ea.Edge.ID))
		h.Write(b[:])
		for _, node := range []*layoutgraph.Node{ea.OriginallyFrom, ea.OriginallyTo, ea.CurrentTo, ea.CurrentFrom} {
			if node != nil {
				binary.LittleEndian.PutUint64(b[:], uint64(node.ID))
				h.Write(b[:])
			}
		}
	}
	return h.Sum64()
}

func TestPlacementEdgeLengthStateMatchesHashFNV(t *testing.T) {
	g := newLargeEdgeLengthGraph()
	g.Nodes[3].TopLeft = geo.NewPoint(-0.0, math.Inf(1))
	g.Nodes[4].Width = math.NaN()
	g.Nodes[5].ID = -7
	abductions := []*layoutgraph.EdgeAbduction{
		{Edge: g.Edges[0], OriginallyFrom: g.Nodes[1], CurrentTo: g.Nodes[2]},
		{Edge: g.Edges[1], OriginallyTo: g.Nodes[3], CurrentFrom: g.Nodes[4], CurrentTo: g.Nodes[5], OriginallyFrom: g.Nodes[6]},
	}
	for _, costs := range []layoutgraph.RoutingCostState{{}, {Turn: 1.5, NonCenterPort: 0.25, Crossing: 7}} {
		for flags := 0; flags < 8; flags++ {
			for _, abd := range [][]*layoutgraph.EdgeAbduction{nil, abductions} {
				options := EdgeLengthOptions{EdgeAbductions: abd, IncludeNodeSizes: flags&1 != 0, EnforceMinimumGap: flags&2 != 0, PenalizeDirection: flags&4 != 0}
				if got, want := placementEdgeLengthState(g, costs, options), referenceEdgeLengthState(g, costs, options); got != want {
					t.Fatalf("costs %+v flags %d: state %#x, want %#x", costs, flags, got, want)
				}
			}
		}
	}
	g.Nodes[7].TopLeft = nil
	if got := placementEdgeLengthState(g, layoutgraph.RoutingCostState{}, EdgeLengthOptions{}); got != invalidEdgeLengthState {
		t.Fatalf("unpositioned node: state %#x, want invalid", got)
	}
}
