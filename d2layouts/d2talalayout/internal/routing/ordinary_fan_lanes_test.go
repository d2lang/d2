package routing

import (
	"slices"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func TestOrdinaryFanLanes(t *testing.T) {
	for _, scenario := range []string{"fan", "parallel", "rotated", "bidirectional", "blocked", "unselected", "root hierarchy", "nested hierarchy"} {
		t.Run(scenario, func(t *testing.T) {
			g := layoutgraph.NewGraph()
			add := func(x, y, w, h float64) *layoutgraph.Node {
				n := g.AddNode(layoutgraph.NewNode(int64(len(g.Nodes)+1), w, h))
				n.TopLeft = geo.NewPoint(x, y)
				return n
			}
			target := add(0, 180, 160, 50)
			var routes [][]*geo.Point
			var src *layoutgraph.Node
			for i := range 3 {
				x := 200. + float64(i)*60
				if scenario != "parallel" || i == 0 {
					src = add(x-30, 0, 60, 50)
				} else {
					x = 200
				}
				e := g.Connect(src, target)
				e.TargetArrowhead = layoutgraph.TriangleArrowhead
				if scenario == "bidirectional" {
					e.SourceArrowhead = layoutgraph.TriangleArrowhead
				}
				e.Points = shortcutTestPoints(x, 50, x, 115, 80, 115, 80, 180)
				routes = append(routes, slices.Clone(e.Points))
			}
			switch scenario {
			case "root hierarchy":
				g.IsRootHierarchy = true
			case "nested hierarchy":
				target.ForceHierarchy = true
			}
			if scenario == "blocked" {
				add(0, 105, 350, 20)
			}
			if scenario == "parallel" {
				target.Label = &layoutgraph.Label{Text: "outside", Position: label.OutsideTopCenter, Width: 12, Height: 12}
			}
			if scenario == "rotated" {
				for _, n := range g.Nodes {
					n.TopLeft.X, n.TopLeft.Y = n.TopLeft.Y, n.TopLeft.X
					n.Width, n.Height = n.Height, n.Width
				}
				for _, e := range g.Edges {
					for _, p := range e.Points {
						p.X, p.Y = p.Y, p.X
					}
				}
			}
			selected := g.Edges
			if scenario == "unselected" {
				selected = g.Edges[:1]
			}
			if err := RefineOrdinaryFanLanes(t.Context(), g, selected); err != nil {
				t.Fatal(err)
			}
			moved := scenario == "fan" || scenario == "parallel" || scenario == "rotated"
			for i, e := range g.Edges {
				if !moved {
					if !slices.Equal(e.Points, routes[i]) {
						t.Fatalf("route %d changed", i)
					}
					continue
				}
				if len(e.Points) != 4 {
					t.Fatalf("unexpected bend count in %v", e.Points)
				}
				if scenario != "parallel" && *e.Points[0] != *routes[i][0] {
					t.Fatalf("changed distinct source port on edge %d", i)
				}
				for _, prior := range g.Edges[:i] {
					for a := 1; a < len(e.Points); a++ {
						for b := 1; b < len(prior.Points); b++ {
							if shortcutSegmentsMeet(e.Points[a-1], e.Points[a], prior.Points[b-1], prior.Points[b]) {
								t.Fatalf("routes still touch: %v, %v", e.Points, prior.Points)
							}
						}
					}
				}
				if scenario == "parallel" {
					box := geo.NewBox(target.LabelTopLeft(target.Label.Position, 12, 12), 12, 12)
					if segmentIntersectsBox(e.Points[2], e.Points[3], box) {
						t.Fatalf("arrival crosses caption: %v", e.Points)
					}
				}
			}
		})
	}
}
