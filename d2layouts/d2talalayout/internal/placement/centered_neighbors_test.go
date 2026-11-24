package placement

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/placementcost"
	"github.com/d2lang/d2/lib/geo"
)

func TestBalanceSymmetryUsesExactPeerCenterline(t *testing.T) {
	for _, transpose := range []bool{false, true} {
		g := layoutgraph.NewGraph()
		add := func(id int64, x, y, w, h float64) *layoutgraph.Node {
			if transpose {
				x, y = y, x
				w, h = h, w
			}
			n := g.AddNode(layoutgraph.NewNode(id, w, h))
			n.TopLeft = geo.NewPoint(x, y)
			return n
		}
		hub := add(1, 300, 400, 80, 60)
		neighbors := layoutgraph.Nodes{add(2, 0, 220, 80, 60), add(3, 250, 200, 80, 100), add(4, 375, 220, 80, 60)}
		for _, n := range neighbors {
			g.Connect(hub, n)
		}
		if placementcost.AxisScore(neighbors) <= 1 {
			t.Fatal("fixture must retain the mixed-size placement bonus")
		}
		if err := BalanceSymmetry(t.Context(), g); err != nil {
			t.Fatal(err)
		}
		center := hub.Center()
		if (!transpose && center.X != 227) || (transpose && center.Y != 227) {
			t.Fatalf("transpose %t: hub center %v", transpose, center)
		}
	}
}

func TestEquidistanceDoesNotReportAlreadyCenteredNodeAsMoved(t *testing.T) {
	g := layoutgraph.NewGraph()
	for i := range 3 {
		n := g.AddNode(layoutgraph.NewNode(int64(i+1), 40, 40))
		n.TopLeft = geo.NewPoint(float64(120*i), 0)
		if i > 0 {
			g.Connect(g.Nodes[i-1], n)
		}
	}
	moved, err := Equidistance(t.Context(), g)
	if err != nil || moved {
		t.Fatalf("already centered: moved=%t, err=%v", moved, err)
	}
}
