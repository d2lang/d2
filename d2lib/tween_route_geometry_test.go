package d2lib

import (
	"math"
	"reflect"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func tweenTestBox(x, y, w, h int) *d2target.Shape {
	return &d2target.Shape{Type: d2target.ShapeRectangle, Pos: d2target.NewPoint(x, y), Width: w, Height: h}
}

func TestTransportTweenRoute(t *testing.T) {
	held := tweenRouteShapePair{tweenTestBox(251, 57, 98, 78), tweenTestBox(44, 196, 164, 78)}
	current := tweenRouteShapePair{tweenTestBox(143, 112, 310, 78), held.dst}
	edge := d2target.Connection{Route: []*geo.Point{{X: 300, Y: 135}, {X: 300, Y: 235}, {X: 208, Y: 235}}}
	want := []*geo.Point{{X: 298, Y: 190}, {X: 298, Y: 235}, {X: 208, Y: 235}}
	got := transportTweenRoute(tweenRouteSample{edge: &edge, nodes: held}, tweenRouteSample{nodes: current})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("transported elbow %v, want %v", got, want)
	}
	if edge.Route[0].Y != 135 {
		t.Fatal("source template was modified")
	}
}

func TestTransportOrthogonalRouteKeepsDirection(t *testing.T) {
	held := tweenRouteShapePair{tweenTestBox(0, 0, 20, 20), tweenTestBox(100, 0, 20, 20)}
	edge := d2target.Connection{Route: []*geo.Point{{X: 20, Y: 10}, {X: 100, Y: 10}}}
	for _, dst := range []*d2target.Shape{tweenTestBox(100, 40, 20, 20), tweenTestBox(-100, 0, 20, 20)} {
		r := transportTweenRoute(tweenRouteSample{edge: &edge, nodes: held}, tweenRouteSample{nodes: tweenRouteShapePair{held.src, dst}})
		if r != nil {
			t.Fatalf("transport created a diagonal or inverted a terminal: %v", r)
		}
	}
}

func TestTransportNativeDirectDiagonal(t *testing.T) {
	held := tweenRouteShapePair{tweenTestBox(0, 0, 20, 20), tweenTestBox(100, 50, 20, 20)}
	edge := d2target.Connection{Route: []*geo.Point{{X: 20, Y: 15}, {X: 100, Y: 55}}}
	now := tweenRouteShapePair{held.src, tweenTestBox(90, 80, 20, 20)}
	r := transportTweenRoute(tweenRouteSample{edge: &edge, nodes: held}, tweenRouteSample{nodes: now})
	if len(r) != 2 || math.Hypot(r[0].X-20, r[0].Y-(10+80./9)) > 1e-6 || math.Hypot(r[1].X-90, r[1].Y-(90-80./9)) > 1e-6 {
		t.Fatalf("straight link failed to face its moving destination: %v", r)
	}
}

func TestTweenRouteRejectsFoldedPaths(t *testing.T) {
	for _, route := range [][]*geo.Point{
		{{X: 0, Y: 0}, {X: 30, Y: 0}, {X: 20, Y: 0}},
		{{X: 0, Y: 0}, {X: 30, Y: 0}, {X: 30, Y: 20}, {X: 10, Y: 20}, {X: 10, Y: -20}},
	} {
		if tweenRouteSimple(route) {
			t.Fatalf("accepted crossing or reversal: %v", route)
		}
	}
}

func TestTweenRouteShortRoundedElbowHasForwardPaintedTangents(t *testing.T) {
	nodes := tweenRouteShapePair{tweenTestBox(143, 110, 300, 78), tweenTestBox(44, 196, 164, 78)}
	nodes.src.StrokeWidth, nodes.dst.StrokeWidth = 2, 2
	c := d2target.BaseConnection()
	c.DstArrow = d2target.TriangleArrowhead
	c.Route = []*geo.Point{{X: 186, Y: 188}, {X: 186, Y: 191}, {X: 180, Y: 191}, {X: 180, Y: 196}}
	c.BorderRadius = tweenRouteCornerRadius(c, c.Route, nodes)
	src, dst := c.ArrowheadAdjustmentsForWidths(2, 2)
	var lines []d2target.ConnectionPathCommand
	for command := range c.PathCommands(src, dst) {
		lines = append(lines, command)
	}
	first, second := lines[0].End, lines[1].End
	p, last := lines[len(lines)-2].End, lines[len(lines)-1].End
	if second.Y <= first.Y || last.Y <= p.Y || c.BorderRadius <= 0 {
		t.Fatalf("inset and rounding reversed a terminal tangent: radius %g, commands %+v", c.BorderRadius, lines)
	}
}

func TestTweenRouteSwitchKeepsHeldCornerRadius(t *testing.T) {
	nodes := tweenRouteShapePair{tweenTestBox(0, 0, 40, 40), tweenTestBox(100, 100, 40, 40)}
	nodes.src.StrokeWidth, nodes.dst.StrokeWidth = 2, 2
	edge := d2target.BaseConnection()
	edge.BorderRadius = 10
	edge.DstArrow = d2target.TriangleArrowhead
	route := []*geo.Point{{X: 20, Y: 40}, {X: 20, Y: 81}, {X: 120, Y: 81}, {X: 120, Y: 100}}
	if radius := tweenRouteCornerRadius(edge, route, nodes); radius != edge.BorderRadius {
		t.Fatalf("a clear 19-unit terminal leg changed an authored corner radius from %v to %v", edge.BorderRadius, radius)
	}
}

func TestTransportStraightLaneWithMovingFace(t *testing.T) {
	from := tweenRouteShapePair{tweenTestBox(0, 0, 100, 30), tweenTestBox(0, 80, 100, 30)}
	now := tweenRouteShapePair{from.src, tweenTestBox(40, 80, 100, 30)}
	edge := d2target.Connection{Route: []*geo.Point{{X: 50, Y: 30}, {X: 50, Y: 80}}}
	r := transportTweenRoute(tweenRouteSample{edge: &edge, nodes: from}, tweenRouteSample{nodes: now})
	if !reflect.DeepEqual(r, edge.Route) {
		t.Fatalf("the stationary port's rail still fits both faces: %v", r)
	}
}
