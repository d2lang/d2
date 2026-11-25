package placementcost

import (
	"context"
	"math"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// newWideSymmetryGraph has a hub with many neighbors (mirrored pairs,
// parallel edges and a container whose children have protruding edges), so
// symmetry frames exceed the linear-scan threshold and recurse.
func newWideSymmetryGraph() *layoutgraph.Graph {
	g := layoutgraph.NewGraph()
	hub := layoutgraph.NewNode(1, 40, 40)
	hub.TopLeft = geo.NewPoint(1000, 1000)
	g.AddNewNodeToContainer(nil, hub)
	id := layoutgraph.EntityID(2)
	var previous *layoutgraph.Node
	for i := 0; i < 20; i++ {
		for _, side := range []float64{-1, 1} {
			n := layoutgraph.NewNode(id, 30, 30)
			id++
			offset := float64(100 + 40*(i%5))
			if i%2 == 0 {
				n.TopLeft = geo.NewPoint(1005+side*offset, 1005+float64(i/5)*50)
			} else {
				n.TopLeft = geo.NewPoint(1005+float64(i/5)*50, 1005+side*offset)
			}
			g.AddNewNodeToContainer(nil, n)
			g.Connect(hub, n)
			if i%7 == 0 {
				g.Connect(n, hub)
			}
			if previous != nil && i%3 == 0 {
				g.Connect(previous, n)
			}
			previous = n
		}
	}
	container := layoutgraph.NewNode(id, 300, 300)
	id++
	container.TopLeft = geo.NewPoint(1500, 1500)
	container.SetContainer(true)
	g.AddNewNodeToContainer(nil, container)
	for i := 0; i < 3; i++ {
		child := layoutgraph.NewNode(id, 30, 30)
		id++
		child.TopLeft = geo.NewPoint(1550+float64(i)*60, 1550)
		g.AddNewNodeToContainer(container, child)
		g.Connect(child, hub)
		g.Connect(child, g.Nodes[2+i])
	}
	g.Connect(container, hub)
	g.ComputeCellSize()
	return g
}

func TestSymmetryFrameMembershipRepresentationsAgree(t *testing.T) {
	defer func(old int) { symmetrySetThreshold = old }(symmetrySetThreshold)
	g := newWideSymmetryGraph()
	var abductions []*layoutgraph.EdgeAbduction
	for _, e := range g.Edges {
		if e.From.Container != nil {
			abductions = append(abductions, &layoutgraph.EdgeAbduction{
				Edge: e, OriginallyFrom: e.From, CurrentFrom: e.From.Container, CurrentTo: e.To,
			})
		}
	}
	type symmetryFunc func(context.Context, *layoutgraph.Node, []*layoutgraph.EdgeAbduction, bool) (float64, error)
	type symmetrySumFunc func(context.Context, layoutgraph.Nodes, []*layoutgraph.EdgeAbduction) (float64, error)
	evaluateWith := func(one symmetryFunc, sum symmetrySumFunc) []uint64 {
		var out []uint64
		for _, abd := range [][]*layoutgraph.EdgeAbduction{nil, abductions} {
			for _, node := range g.Nodes {
				for _, checkNeighbors := range []bool{true, false} {
					value, err := one(context.Background(), node, abd, checkNeighbors)
					if err != nil {
						t.Fatal(err)
					}
					out = append(out, math.Float64bits(value))
				}
			}
			total, err := sum(context.Background(), layoutgraph.Nodes(g.Nodes), abd)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, math.Float64bits(total))
		}
		return out
	}
	evaluate := func() []uint64 { return evaluateWith(nodeSymmetry, NodesSymmetry) }
	want := evaluateWith(refNodeSymmetry, refNodesSymmetry)
	symmetrySetThreshold = 1 << 30
	if got := evaluate(); len(got) != len(want) {
		t.Fatalf("got %d values, want %d", len(got), len(want))
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("linear frames: value %d = %v, original %v", i, math.Float64frombits(got[i]), math.Float64frombits(want[i]))
			}
		}
	}
	nonZero := false
	for _, bits := range want {
		nonZero = nonZero || bits != 0
	}
	if !nonZero {
		t.Fatal("fixture produced no symmetry")
	}
	for _, threshold := range []int{0, 1, 4, 32} {
		symmetrySetThreshold = threshold
		got := evaluate()
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("threshold %d: value %d = %v, want %v", threshold, i, math.Float64frombits(got[i]), math.Float64frombits(want[i]))
			}
		}
	}
}
