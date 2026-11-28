package d2talalayout

import (
	"reflect"
	"testing"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func TestResizeBoardRetainsClearWiresAndReroutesCoveredWire(t *testing.T) {
	scene := func() *d2graph.Graph {
		g, crossing := newD2TransactionGraph(false)
		block := appendD2Object(g.Root, "expanding")
		block.Box = geo.NewBox(geo.NewPoint(170, 0), 50, 25)
		// A second wire already uses a traced cylinder port and a source-only
		// arrow. Its label and lane should survive the unrelated reroute.
		a, b := appendD2Object(g.Root, "cache"), appendD2Object(g.Root, "log")
		a.Shape.Value = d2target.ShapeCylinder
		a.Box, b.Box = geo.NewBox(geo.NewPoint(0, 180), 100, 60), geo.NewBox(geo.NewPoint(350, 180), 100, 60)
		e := &d2graph.Edge{Src: a, Dst: b, SrcArrow: true, Route: []*geo.Point{geo.NewPoint(100, 210), geo.NewPoint(350, 210)}}
		e.Label.Value = "writes"
		e.LabelDimensions = d2target.TextDimensions{Width: 45, Height: 13}
		e.LabelPosition = new(label.UnlockedTop.String())
		e.LabelPercentage = new(0.3)
		g.Edges = append(g.Edges, e)
		crossing.DstArrow = true
		return g
	}
	old, current := scene(), scene()
	original := snapshotD2Graph(t, old)
	plan := map[*d2graph.Object]*geo.Box{}
	for _, o := range current.Objects {
		plan[o] = o.Box.Copy()
	}
	block := plan[current.Root.Children["expanding"]]
	block.Height = 100
	current.Edges[1].LabelPercentage = new(0.75)
	if !routeResizeBoard(t.Context(), old, current, plan) {
		t.Fatal("could not place routes on an expanded obstacle")
	}
	original.assertUnchanged(t, old)
	route := current.Edges[0].Route
	for i := 1; i < len(route); i++ {
		if block.Intersects(geo.Segment{Start: route[i-1], End: route[i]}, -0.01) {
			t.Fatalf("rerouted %v through the expanded obstacle", route)
		}
	}
	got, want := current.Edges[1], old.Edges[1]
	if !reflect.DeepEqual(got.Route, want.Route) || *got.LabelPosition != *want.LabelPosition || *got.LabelPercentage != *want.LabelPercentage {
		t.Errorf("untouched reverse arrow changed route or label: %v, %v, %v", got.Route, *got.LabelPosition, *got.LabelPercentage)
	}
}

func TestResizeBoardPreservesCaptionAlongDogleg(t *testing.T) {
	old := &layoutgraph.Edge{Points: []*geo.Point{{X: 10, Y: 0}, {X: 10, Y: 180}}}
	edge := &layoutgraph.Edge{Points: []*geo.Point{{X: 10, Y: 0}, {X: 10, Y: 60}, {X: 80, Y: 60}, {X: 80, Y: 180}}}
	for _, position := range []label.Position{label.OutsideBottomLeft, label.OutsideTopCenter, label.InsideMiddleRight, label.UnlockedMiddle} {
		old.Label = &layoutgraph.Label{Text: "reply", Width: 44, Height: 15, Position: position}
		old.LabelPercentage = 0.55
		before := old.LabelTopLeft(old.Label.Position, old.Label.Width, old.Label.Height)
		copied := *old.Label
		edge.Label = &copied
		if !resizeBoardLabel(old, edge) {
			t.Fatalf("did not keep %s", position.String())
		}
		after := edge.LabelTopLeft(edge.Label.Position, edge.Label.Width, edge.Label.Height)
		if after.Y != before.Y || (after.X != before.X && after.X != before.X+70) {
			t.Errorf("%s caption moved from %v to %v", position.String(), before, after)
		}
		edge.Label.FixPosition()
		if resizeBoardLabel(old, edge) {
			t.Fatal("moved an authored label anchor")
		}
	}
}

func TestResizeBoardRejectsOverflowWithoutMovingObjects(t *testing.T) {
	for _, origin := range []geo.Point{{X: limits.MaxGraphSize - 99}, {Y: limits.MaxGraphSize - 59}} {
		old, _ := newD2TransactionGraph(false)
		current, edge := newD2TransactionGraph(false)
		beforeOld, beforeCurrent := snapshotD2Graph(t, old), snapshotD2Graph(t, current)
		plan := map[*d2graph.Object]*geo.Box{edge.Src: edge.Src.Box.Copy(), edge.Dst: geo.NewBox(&origin, 100, 60)}
		plan[edge.Src].TopLeft.X++
		if routeResizeBoard(t.Context(), old, current, plan) {
			t.Fatalf("accepted a 100 by 60 box at %v", origin)
		}
		beforeOld.assertUnchanged(t, old)
		beforeCurrent.assertUnchanged(t, current)
	}
}

func TestResizeBoardRejectsOverflowAfterPlacingCornerKey(t *testing.T) {
	scene := func() *d2graph.Graph {
		g, _ := newD2TransactionGraph(false)
		key := appendD2Object(g.Root, "key")
		key.NearKey = d2ast.MakeKeyPath([]string{"bottom-right"})
		key.Box = geo.NewBox(geo.NewPoint(600, 160), 100, 40)
		return g
	}
	old, current := scene(), scene()
	before := snapshotD2Graph(t, current)
	p := resizePlacement{boxes: make(map[*d2graph.Object]*geo.Box)}
	for _, o := range current.Objects {
		p.boxes[o] = o.Box.Copy()
	}
	right := p.boxes[current.Edges[0].Dst]
	right.TopLeft.X = limits.MaxGraphSize - right.Width
	if p.route(t.Context(), old, current, true) {
		t.Fatal("accepted a decoration extending outside the final diagram range")
	}
	before.assertUnchanged(t, current)
}
