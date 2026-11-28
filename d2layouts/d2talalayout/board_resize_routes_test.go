package d2talalayout

import (
	"reflect"
	"testing"

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

func TestResizeBetweenSwitchesAuthoredLanesOnce(t *testing.T) {
	start, in := newD2TransactionGraph(false)
	end, out := newD2TransactionGraph(false)
	in.Route = []*geo.Point{{X: 60, Y: 20}, {X: 60, Y: 0}, {X: 300, Y: 0}, {X: 300, Y: 20}}
	out.Route = []*geo.Point{{X: 160, Y: 80}, {X: 160, Y: 120}, {X: 400, Y: 120}, {X: 400, Y: 80}}
	for _, o := range end.Objects {
		o.TopLeft.X += 100
	}
	beforeStart, beforeEnd := snapshotD2Graph(t, start), snapshotD2Graph(t, end)
	plans := make([]map[*d2graph.Object]*geo.Box, 10)
	end.TweenFrames = append(end.TweenFrames, start)
	for i := 1; i < 10; i++ {
		g, e := newD2TransactionGraph(false)
		end.TweenFrames = append(end.TweenFrames, g)
		if i == 4 {
			e.Style.Opacity = &d2graph.Scalar{Value: "0"}
		}
		plans[i] = make(map[*d2graph.Object]*geo.Box)
		for _, o := range g.Objects {
			plans[i][o] = o.Box.Copy()
			plans[i][o].TopLeft.X += float64(10 * i)
		}
	}
	if !routeResizeSequence(t.Context(), start, end, plans) {
		t.Fatal("could not place lanes across the moving track")
	}
	handedOff := false
	for i := 1; i < 10; i++ {
		actual := end.TweenFrames[i].Edges[0].Route
		source := translatedBoardRoute(in.Route, float64(10*i), 0)
		destination := translatedBoardRoute(out.Route, float64(10*i-100), 0)
		switch {
		case reflect.DeepEqual(actual, source):
			if handedOff {
				t.Fatalf("sample %d returned to the old lane", i)
			}
		case reflect.DeepEqual(actual, destination):
			if !handedOff && i != 4 && i != 5 {
				t.Fatalf("visible change to destination at sample %d", i)
			}
			handedOff = true
		default:
			t.Fatalf("sample %d left both clear lanes", i)
		}
	}
	if !handedOff {
		t.Fatal("did not reach the destination lane")
	}
	beforeStart.assertUnchanged(t, start)
	beforeEnd.assertUnchanged(t, end)
}

func TestResizeBoardCarriesCorridorWithBothEndpoints(t *testing.T) {
	old, in := newD2TransactionGraph(false)
	in.Route = []*geo.Point{{X: 110, Y: 50}, {X: 150, Y: 50}, {X: 150, Y: 110}, {X: 210, Y: 110}, {X: 210, Y: 50}, {X: 250, Y: 50}}
	current, e := newD2TransactionGraph(false)
	boxes := map[*d2graph.Object]*geo.Box{e.Src: e.Src.Box.Copy(), e.Dst: e.Dst.Box.Copy()}
	boxes[e.Src].TopLeft.X += 50
	boxes[e.Dst].TopLeft.X += 150
	if !routeResizeBoard(t.Context(), old, current, boxes) {
		t.Fatal("could not carry an unobstructed moving wire")
	}
	want := translatedBoardRoute(in.Route, 50, 0)
	want[len(want)-1].X += 100
	if !reflect.DeepEqual(e.Route, want) {
		t.Fatalf("did not carry the shared interior corridor with its endpoint boxes")
	}
}

func TestResizeWireDropsElbowCoincidentAfterExport(t *testing.T) {
	route := []*geo.Point{{X: 10, Y: 60}, {X: 50.0003, Y: 60}, {X: 50.0007, Y: 60}, {X: 50.0007, Y: 90}}
	want := []*geo.Point{route[0], route[1], route[3]}
	if got := distinctResizePoints(route); !reflect.DeepEqual(got, want) {
		t.Fatalf("a subpixel segment collapsed in the render target: %v", got)
	}
}

func TestResizeBoardKeepsTracedPortWhenStraightLaneSeparates(t *testing.T) {
	scene := func() (*d2graph.Graph, *d2graph.Edge) {
		g, e := newD2TransactionGraph(false)
		e.Src.Shape.Value = d2target.ShapeCylinder
		e.Dst.TopLeft = geo.NewPoint(10, 220)
		e.Route = []*geo.Point{{X: 60, Y: 80}, {X: 60, Y: 220}}
		return g, e
	}
	old, _ := scene()
	current, e := scene()
	boxes := map[*d2graph.Object]*geo.Box{e.Src: e.Src.Box.Copy(), e.Dst: e.Dst.Box.Copy()}
	boxes[e.Dst].TopLeft.X += 10
	if !routeResizeBoard(t.Context(), old, current, boxes) {
		t.Fatal("could not carry cylinder port")
	}
	if len(e.Route) != 4 || *e.Route[0] != (geo.Point{X: 60, Y: 80}) || *e.Route[3] != (geo.Point{X: 70, Y: 220}) ||
		e.Route[1].X != 60 || e.Route[2].X != 70 || e.Route[1].Y != e.Route[2].Y {
		t.Fatalf("traced ports lost their vertical approaches: %v", e.Route)
	}
}

func TestResizeBypassesLocalObstructionWithoutMovingDistantLanes(t *testing.T) {
	lane := []*geo.Point{{X: 20, Y: 0}, {X: 20, Y: 320}, {X: 180, Y: 320}, {X: 180, Y: 10}}
	block := geo.NewBox(geo.NewPoint(4, 80), 52, 48)
	detour := bypassResizeBox(lane, *block)
	if len(detour) < len(lane) || !reflect.DeepEqual(detour[len(detour)-3:], lane[1:]) || !detour[0].Equals(lane[0]) || detour[1].X != lane[0].X {
		t.Fatalf("discarded a clear port or bend: %v", geo.Points(detour))
	}
	for i := 1; i < len(detour); i++ {
		a, b := detour[i-1], detour[i]
		if a.X != b.X && a.Y != b.Y || block.Intersects(geo.Segment{Start: a, End: b}, -0.01) {
			t.Fatalf("unusable local detour: %v", geo.Points(detour))
		}
	}
}

func TestResizeMeasuresDegeneratePortsWithoutChangingThem(t *testing.T) {
	g, e := newD2TransactionGraph(false)
	e.Route = append([]*geo.Point{e.Route[0]}, e.Route...)
	e.Label.Value = "unknown placement"
	e.LabelPosition = new("UNRECOGNIZED")
	before := snapshotD2Graph(t, g)
	poses := resizeWirePoses(g)
	if poses == nil || resizeWireJump(poses[e.AbsID()], poses[e.AbsID()]) != 0 {
		t.Fatal("expected a finite measurement of a repeated port")
	}
	before.assertUnchanged(t, g)
	e.Route = []*geo.Point{{X: 10, Y: 10}, {X: 10, Y: 10}}
	if resizeWirePoses(g) != nil {
		t.Fatal("all-coincident route has no measurable approach")
	}
}
