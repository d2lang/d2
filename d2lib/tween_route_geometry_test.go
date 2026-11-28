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

func TestTweenRouteCandidateTransportsHeldElbow(t *testing.T) {
	held := tweenRouteShapePair{tweenTestBox(251, 57, 98, 78), tweenTestBox(44, 196, 164, 78)}
	current := tweenRouteShapePair{tweenTestBox(143, 112, 310, 78), held.dst}
	edge := d2target.Connection{Route: []*geo.Point{{X: 300, Y: 135}, {X: 300, Y: 235}, {X: 208, Y: 235}}}
	want := []*geo.Point{{X: 298, Y: 190}, {X: 298, Y: 235}, {X: 208, Y: 235}}
	for _, at := range []float64{0, .25, .5, .75, 1} {
		got := tweenRouteCandidate(edge, edge, held, held, current, at)
		if len(got) != len(want) {
			t.Fatalf("t=%v: route %v", at, got)
		}
		for i := range want {
			if math.Hypot(got[i].X-want[i].X, got[i].Y-want[i].Y) > 1e-6 {
				t.Errorf("t=%v: elbow %d is %v, want %v", at, i, got[i], want[i])
			}
		}
	}
	if edge.Route[0].Y != 135 {
		t.Fatal("input route was modified")
	}
}

func TestTweenRouteCandidateChangesFacesAndBends(t *testing.T) {
	pair := tweenRouteShapePair{tweenTestBox(0, 0, 40, 40), tweenTestBox(140, 140, 40, 40)}
	pair.src.BorderRadius = 6
	start := d2target.Connection{Route: []*geo.Point{{X: 40, Y: 20}, {X: 160, Y: 20}, {X: 160, Y: 140}}}
	end := d2target.Connection{Route: []*geo.Point{{X: 20, Y: 40}, {X: 20, Y: 80}, {X: 120, Y: 80}, {X: 120, Y: 160}, {X: 140, Y: 160}}}
	var last []*geo.Point
	for i := 0; i <= 80; i++ {
		at := float64(i) / 80
		got := tweenRouteCandidate(start, end, pair, pair, pair, at)
		if len(got) < 2 {
			t.Fatalf("no route at %g", at)
		}
		if i == 0 && !reflect.DeepEqual(got, start.Route) || i == 80 && !reflect.DeepEqual(got, end.Route) {
			t.Fatalf("changed held geometry at %g: %v", at, got)
		}
		if i == 40 {
			p := got[0]
			// The port travels around the painted arc, across neither the
			// square bounding corner nor the page interior.
			if p.X < 34 || p.Y < 34 || math.Abs(math.Hypot(p.X-34, p.Y-34)-6) > 1e-6 {
				t.Fatalf("port left rounded corner: %v", p)
			}
		}
		if last != nil {
			oldLength, newLength := geo.Route(last).Length(), geo.Route(got).Length()
			for j := 0; j <= 24; j++ {
				u := float64(j) / 24
				p, _ := geo.Route(last).GetPointAtDistance(u * oldLength)
				q, _ := geo.Route(got).GetPointAtDistance(u * newLength)
				if math.Hypot(p.X-q.X, p.Y-q.Y) > 6 {
					t.Fatalf("route jumped at %g, distance %g: %v -> %v", at, u, p, q)
				}
			}
		}
		last = got
	}
}

func TestTweenRouteCandidateDirectPortsAndOverlap(t *testing.T) {
	before := tweenRouteShapePair{tweenTestBox(0, 0, 20, 20), tweenTestBox(100, 0, 20, 20)}
	after := tweenRouteShapePair{tweenTestBox(100, 0, 20, 20), tweenTestBox(0, 0, 20, 20)}
	start := d2target.Connection{Route: []*geo.Point{{X: 20, Y: 10}, {X: 100, Y: 10}}}
	end := d2target.Connection{Route: []*geo.Point{{X: 100, Y: 10}, {X: 20, Y: 10}}}
	for _, tc := range []struct{ x, wantStart, wantEnd int }{{10, 30, 90}, {90, 90, 30}} {
		now := tweenRouteShapePair{tweenTestBox(tc.x, 0, 20, 20), tweenTestBox(100-tc.x, 0, 20, 20)}
		got := tweenRouteCandidate(start, end, before, after, now, float64(tc.x)/100)
		if len(got) != 2 || got[0].X != float64(tc.wantStart) || got[1].X != float64(tc.wantEnd) {
			t.Fatalf("x=%d: %v", tc.x, got)
		}
	}
	overlap := tweenRouteShapePair{tweenTestBox(48, 0, 20, 20), tweenTestBox(52, 0, 20, 20)}
	if got := tweenRouteCandidate(start, end, before, after, overlap, .48); got != nil {
		t.Fatalf("overlapping direct ports produced %v", got)
	}
}

func TestTweenRouteCandidateRejectsUnsupportedGeometry(t *testing.T) {
	pair := tweenRouteShapePair{tweenTestBox(0, 0, 20, 20), tweenTestBox(100, 0, 20, 20)}
	edge := d2target.Connection{Route: []*geo.Point{{X: 20, Y: 10}, {X: 100, Y: 10}}}
	for _, c := range []d2target.Connection{
		{}, {IsCurve: true, Route: edge.Route}, {Route: []*geo.Point{nil, edge.Route[1]}},
		{Route: []*geo.Point{{X: math.NaN(), Y: 10}, edge.Route[1]}},
		{Route: []*geo.Point{edge.Route[0], edge.Route[0]}},
		{Route: []*geo.Point{{X: 10, Y: 10}, edge.Route[1]}},
	} {
		if got := tweenRouteCandidate(c, edge, pair, pair, pair, .5); got != nil {
			t.Fatalf("unexpected route %v", got)
		}
	}
	for _, kind := range []string{d2target.ShapeOval, d2target.ShapeCircle, d2target.ShapeDiamond, d2target.ShapeHexagon} {
		pair.src.Type = kind
		mid := tweenBoundaryPort(pair.src, pair.src, pair.src, geo.NewPoint(20, 10), geo.NewPoint(10, 20), .5)
		if mid == nil || mid.X <= 10 || mid.Y <= 10 || mid.X >= 20 || mid.Y >= 20 {
			t.Errorf("%s port did not follow its outline: %v", kind, mid)
		}
	}
}

func TestTweenRoutePortAlternatesKeepPagePortsAndElbows(t *testing.T) {
	now := tweenRouteShapePair{tweenTestBox(0, 0, 80, 60), tweenTestBox(120, 120, 100, 60)}
	now.src.BorderRadius, now.dst.BorderRadius = 16, 12
	r := []*geo.Point{{X: 40, Y: 60}, {X: 40, Y: 135}, {X: 120, Y: 135}}
	seen := make(map[[2]float64]bool)
	sawPair := false
	for candidate := range tweenRoutePortAlternates(r, now) {
		a, elbow, b := candidate[0], candidate[1], candidate[2]
		if a.Y != 60 || a.X < 16 || a.X > 64 || b.X != 120 || b.Y < 132 || b.Y > 168 ||
			elbow.X != a.X || elbow.Y != b.Y {
			t.Fatalf("port or elbow left its face: %v", candidate)
		}
		coords := [2]float64{a.X, b.Y}
		if seen[coords] {
			t.Fatalf("duplicate choice %v", coords)
		}
		seen[coords] = true
		both := a.X != r[0].X && b.Y != r[2].Y
		if sawPair && !both {
			t.Fatal("single port move came after a paired move")
		}
		sawPair = sawPair || both
	}
	if !sawPair || !seen[[2]float64{40, 165}] || !seen[[2]float64{20, 135}] {
		t.Fatalf("missing single or paired alternatives: %v", seen)
	}
	if r[0].X != 40 || r[1].Y != 135 || r[2].Y != 135 {
		t.Fatal("altered input route")
	}
}

func TestTweenRoutePortAlternatesDoNotInvertSharedLeg(t *testing.T) {
	now := tweenRouteShapePair{tweenTestBox(0, 0, 100, 40), tweenTestBox(25, 100, 100, 40)}
	r := []*geo.Point{{X: 60, Y: 40}, {X: 60, Y: 70}, {X: 70, Y: 70}, {X: 70, Y: 100}}
	count := 0
	for c := range tweenRoutePortAlternates(r, now) {
		if c[1].X >= c[2].X || c[0].X != c[1].X || c[2].X != c[3].X || c[1].Y != c[2].Y {
			t.Fatalf("shared rail inverted: %v", c)
		}
		count++
	}
	if count == 0 {
		t.Fatal("no legal port options")
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

// A narrow vertical gap opens into an L as its destination port rounds the
// top-right corner. The arrow must approach both faces from outside, even
// during the first samples where the path is barely longer than its marker.
func TestTweenRouteOpeningElbowKeepsTerminalApproach(t *testing.T) {
	leaf := tweenTestBox(44, 196, 164, 78)
	a := tweenRouteShapePair{tweenTestBox(143, 112, 310, 78), leaf}
	b := tweenRouteShapePair{tweenTestBox(251, 57, 98, 78), leaf}
	before := d2target.Connection{Route: []*geo.Point{{X: 175, Y: 190}, {X: 175, Y: 196}}}
	after := d2target.Connection{Route: []*geo.Point{{X: 300, Y: 135}, {X: 300, Y: 235}, {X: 208, Y: 235}}}
	for i := 1; i < 22; i++ {
		tm := float64(i) / 22
		lerp := func(old, next int) int { return int(math.Round(float64(old)*(1-tm) + float64(next)*tm)) }
		now := tweenRouteShapePair{tweenTestBox(lerp(143, 251), lerp(112, 57), lerp(310, 98), 78), leaf}
		for _, r := range [][]*geo.Point{
			tweenRouteCandidate(before, after, a, b, now, tm),
			tweenRouteCandidate(after, before, b, a, now, 1-tm),
		} {
			if len(r) < 2 {
				t.Fatalf("sample %d collapsed", i)
			}
			for j := 1; j < len(r); j++ {
				if r[j].X > r[j-1].X+1e-6 || r[j].Y+1e-6 < r[j-1].Y {
					t.Fatalf("sample %d: path reverses travel inside the narrowing gap: %v", i, r)
				}
			}
			port, lead := r[len(r)-1], r[len(r)-2]
			if port.Y == 196 && lead.Y >= port.Y || port.X == 208 && lead.X <= port.X {
				t.Fatalf("sample %d: terminal arm heads into leaf: %v→%v", i, lead, port)
			}
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
