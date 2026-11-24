package routing

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func TestHierarchyFanPortsRetainSharedPortsAndAvoidUnrelatedObstacles(t *testing.T) {
	for _, extra := range []bool{false, true} {
		name := "isolated fan"
		if extra {
			name = "additional obstacle"
		}
		t.Run(name, func(t *testing.T) {
			g := layoutgraph.NewGraph()
			hierarchy := layoutgraph.NewHierarchy()
			hub := g.AddNode(layoutgraph.NewNode(1, 40, 40))
			hub.TopLeft, hub.Hierarchy = geo.NewPoint(0, 0), hierarchy
			// The farthest leaf shares the lower port with the nearest. An
			// edge leaving the middle port crosses the line to the far leaf.
			var edges []*layoutgraph.Edge
			for i, y := range []float64{10, 30, 30} {
				x := []float64{120, 75, 160}[i]
				leaf := g.AddNode(layoutgraph.NewNode(int64(i+2), 20, 20))
				leaf.TopLeft, leaf.Hierarchy = geo.NewPoint(x-10, 90), hierarchy
				edge := g.Connect(leaf, hub)
				edge.Points = []*geo.Point{geo.NewPoint(x, 90), geo.NewPoint(x, y), geo.NewPoint(40, y)}
				edges = append(edges, edge)
			}
			if extra {
				g.AddNode(layoutgraph.NewNode(5, 10, 10)).TopLeft = geo.NewPoint(125, 5)
			}
			guard, err := newRouteWorkGuard(t.Context(), "fan", maxRouteStageWorkUnits)
			if err != nil {
				t.Fatal(err)
			}
			changed, err := orderHierarchyFanPorts(hub, geo.Right, edges, guard)
			if err != nil || changed == extra {
				t.Fatalf("changed=%v err=%v", changed, err)
			}
			for i, edge := range hub.Edges {
				want := []float64{30, 30, 10}[i]
				if extra {
					want = []float64{10, 30, 30}[i]
				}
				p, bend, end := pointsAt(edge, hub)
				if p.Y != want || bend.Y != want || end.X != []float64{120, 75, 160}[i] || end.Y != 90 {
					t.Fatalf("edge %d: unexpected points %v", i, edge.Points)
				}
			}
		})
	}
}
