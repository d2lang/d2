package routing

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// A split with one detached obstacle or decoration and a four-way rejoin.
// Each edge has its own destination but initially shares its hub port.
func TestRefineHierarchyFanPortsGuardsTheLocalStrip(t *testing.T) {
	for _, kind := range []string{"detached", "obstacle", "outside label", "unrelated route", "rounded foreign corner", "capacity", "edge label"} {
		t.Run(kind, func(t *testing.T) {
			g := layoutgraph.NewGraph()
			h := layoutgraph.NewHierarchy()
			add := func(id int64, x, y, w, height float64) *layoutgraph.Node {
				n := g.AddNode(layoutgraph.NewNode(id, w, height))
				n.TopLeft = geo.NewPoint(x, y)
				return n
			}
			hub := add(1, 0, 100, 40, 80)
			hub.Hierarchy = h
			if kind == "capacity" {
				hub.Height = 24
				hub.TopLeft.Y = 128
			}
			var arrivals []*layoutgraph.Edge
			for i, x := range []float64{85, 140, 200, 260} {
				n := add(int64(2+i), x-10, 0, 20, 20)
				n.Hierarchy = h
				e := g.Connect(n, hub)
				e.TargetArrowhead = layoutgraph.TriangleArrowhead
				e.Points = shortcutTestPoints(x, 20, x, 140, 40, 140)
				arrivals = append(arrivals, e)
			}
			key := add(7, 400, 200, 30, 20)
			switch kind {
			case "obstacle":
				key.TopLeft = geo.NewPoint(80, 105)
			case "outside label":
				key.TopLeft = geo.NewPoint(100, 75)
				key.Label = &layoutgraph.Label{Text: "Legend", Position: label.OutsideBottomCenter, Width: 80, Height: 32}
			case "unrelated route":
				remote := add(8, 130, 230, 20, 20)
				e := g.Connect(key, remote)
				e.Points = shortcutTestPoints(400, 210, 145, 210, 145, 100, 140, 100, 140, 230)
			case "rounded foreign corner":
				// Both straight shafts miss the strip's upper-right corner, but
				// the SVG rounds the foreign bend through the changed region.
				key.TopLeft = geo.NewPoint(222, 71)
				remote := add(8, 263, 140, 20, 20)
				e := g.Connect(key, remote)
				e.Points = shortcutTestPoints(235, 91, 235, 94, 263, 94, 263, 140)
			case "edge label":
				arrivals[0].Label = &layoutgraph.Label{Text: "keep", Position: label.UnlockedMiddle, Width: 36, Height: 20}
			}
			original := make([]*geo.Point, len(arrivals))
			for i, e := range arrivals {
				original[i] = e.Points[2]
			}
			if err := RefineHierarchyFanPorts(t.Context(), g); err != nil {
				t.Fatal(err)
			}
			y := make(map[float64]bool)
			for i, e := range arrivals {
				y[e.Points[2].Y] = true
				if e.Points[0].X != []float64{85, 140, 200, 260}[i] || e.Points[0].Y != 20 || original[i].Y != 140 {
					t.Fatal("unmoved endpoint or original route storage changed")
				}
			}
			want := 1
			if kind == "detached" {
				want = 4
			}
			if len(y) != want {
				t.Fatalf("%d distinct arrivals, want %d", len(y), want)
			}
		})
	}
}
