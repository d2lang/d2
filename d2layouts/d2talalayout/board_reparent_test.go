package d2talalayout

import (
	"slices"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

func TestMovedBoardPanels(t *testing.T) {
	type fixture struct {
		g           *d2graph.Graph
		panel, a, b *d2graph.Object
	}
	makePanel := func(parent string, gap float64) fixture {
		g := d2graph.NewGraph()
		scope := appendD2Object(g.Root, parent)
		panel := appendD2Object(scope, "telemetry")
		panel.Label.Value = "Telemetry"
		a := appendD2Object(panel, "ingest")
		b := appendD2Object(panel, "index")
		a.TopLeft.X, b.TopLeft.X = 15, 15+gap
		panel.Width, panel.Height = gap+100, 90
		port := func(x float64) *geo.Point { return geo.NewPoint(x, 20) }
		for i := 0; i < 2; i++ {
			g.Edges = append(g.Edges, &d2graph.Edge{Src: a, Dst: b, DstArrow: true, Index: i, Route: []*geo.Point{port(a.TopLeft.X + a.Width), port(b.TopLeft.X)}})
		}
		// An external edge can disappear or enter from a different endpoint.
		g.Edges = append(g.Edges, &d2graph.Edge{Src: scope, Dst: b, DstArrow: true})
		return fixture{g, panel, a, b}
	}
	t.Run("same content with different native spacing", func(t *testing.T) {
		old, cur := makePanel("platform", 100), makePanel("operations", 210)
		cur.g.Edges[2].Src, cur.g.Edges[2].Dst = cur.a, cur.g.Root.Children["operations"]
		slices.Reverse(cur.panel.ChildrenArray)
		cur.g.Edges[0], cur.g.Edges[1] = cur.g.Edges[1], cur.g.Edges[0]
		panels, wires := movedBoardPanels(old.g, cur.g)
		want := []boardAnchor{{old.panel, cur.panel}, {old.b, cur.b}, {old.a, cur.a}}
		if len(panels) != 1 || !slices.Equal(panels[cur.panel], want) || len(wires) != 2 ||
			wires[cur.g.Edges[0]] != old.g.Edges[1] || wires[cur.g.Edges[1]] != old.g.Edges[0] {
			t.Fatalf("incorrect subtree or parallel edge mapping: %v, %v", panels, wires)
		}
	})
	for _, tc := range []struct {
		name string
		edit func(old, cur fixture)
	}{
		{"ambiguous on old board", func(old, cur fixture) { appendD2Object(appendD2Object(old.g.Root, "other"), "telemetry") }},
		{"ambiguous on new board", func(old, cur fixture) { appendD2Object(appendD2Object(cur.g.Root, "other"), "telemetry") }},
		{"new panel title", func(old, cur fixture) { cur.panel.Label.Value = "Other service" }},
		{"different label dimensions", func(old, cur fixture) { cur.a.LabelDimensions.Width++ }},
		{"different label placement", func(old, cur fixture) { cur.a.LabelPosition = new("INSIDE_TOP_CENTER") }},
		{"different shape", func(old, cur fixture) { cur.a.Shape.Value = "square" }},
		{"resized leaf", func(old, cur fixture) { cur.a.Height++ }},
		{"extra descendant", func(old, cur fixture) { appendD2Object(cur.panel, "archive") }},
		{"changed endpoints", func(old, cur fixture) { cur.g.Edges[0].Src, cur.g.Edges[0].Dst = cur.b, cur.a }},
		{"reversed arrow", func(old, cur fixture) { cur.g.Edges[0].SrcArrow, cur.g.Edges[0].DstArrow = true, false }},
		{"lost parallel edge", func(old, cur fixture) { cur.g.Edges = cur.g.Edges[1:] }},
		{"additional edge", func(old, cur fixture) {
			cur.g.Edges = append(cur.g.Edges, &d2graph.Edge{Src: cur.b, Dst: cur.a, DstArrow: true})
		}},
		{"repeated parallel index", func(old, cur fixture) { cur.g.Edges[1].Index = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old, cur := makePanel("platform", 100), makePanel("operations", 210)
			tc.edit(old, cur)
			if p, w := movedBoardPanels(old.g, cur.g); len(p) != 0 || len(w) != 0 {
				t.Fatal("ambiguous or edited panel treated as an unchanged move")
			}
		})
	}
	t.Run("surviving scoped path", func(t *testing.T) {
		old, cur := makePanel("same", 100), makePanel("same", 210)
		if p, w := movedBoardPanels(old.g, cur.g); len(p) != 0 || len(w) != 0 {
			t.Fatal("panel with the same full path treated as a transfer")
		}
	})
}
