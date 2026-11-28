package d2lib

import (
	"net/url"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestTweenRouteClear(t *testing.T) {
	shape := func(id string, x, y, w, h int) d2target.Shape {
		return d2target.Shape{ID: id, Type: d2target.ShapeRectangle, Pos: d2target.Point{X: x, Y: y}, Width: w, Height: h, Opacity: 1, StrokeWidth: 2}
	}
	a, b := shape("group.a", 0, 20, 40, 40), shape("b", 140, 20, 40, 40)
	pair := tweenRouteShapePair{src: &a, dst: &b}
	p := geo.NewPoint
	direct := []*geo.Point{p(40, 40), p(140, 40)}
	over := []*geo.Point{p(20, 20), p(20, 0), p(160, 0), p(160, 20)}
	edge := d2target.Connection{ID: "edge", Src: a.ID, Dst: b.ID, Route: over, StrokeWidth: 2, Opacity: 1, SrcArrow: d2target.NoArrowhead, DstArrow: d2target.NoArrowhead}
	frame := func(others ...d2target.Shape) *d2target.Diagram {
		return &d2target.Diagram{Shapes: append([]d2target.Shape{a, b}, others...), Connections: []d2target.Connection{edge}}
	}
	check := func(t *testing.T, f *d2target.Diagram, e d2target.Connection, route []*geo.Point, want bool) {
		t.Helper()
		if got := tweenRouteClear(f, &e, route, pair); got != want {
			t.Errorf("clear = %t, want %t", got, want)
		}
	}
	t.Run("moving third object", func(t *testing.T) {
		block := shape("moving", 75, 30, 20, 20)
		check(t, frame(block), edge, direct, false)
		block.Pos.Y = 80
		check(t, frame(block), edge, direct, true)
	})
	t.Run("invisible or clipped", func(t *testing.T) {
		block := shape("moving", 75, 30, 20, 20)
		block.Opacity = 0
		check(t, frame(block), edge, direct, true)
		block.Opacity = 1
		block.MotionClip = &d2target.MotionClip{Point: d2target.Point{X: 75, Y: 48}, Width: 20, Height: 2}
		check(t, frame(block), edge, direct, true)
	})
	t.Run("stack behind obstacle", func(t *testing.T) {
		stack := shape("stack", 75, 46, 20, 18)
		check(t, frame(stack), edge, direct, true)
		stack.Multiple = true
		check(t, frame(stack), edge, direct, false)
	})
	t.Run("ancestor title", func(t *testing.T) {
		group := shape("group", -20, -20, 220, 140)
		check(t, frame(group), edge, direct, true)
		group.Label = "Group"
		group.LabelWidth, group.LabelHeight = 20, 12
		group.MotionLabelTopLeft = &d2target.Point{X: 80, Y: 34}
		check(t, frame(group), edge, direct, false)
	})
	t.Run("unavoidable exit overlap", func(t *testing.T) {
		card := shape("key", 34, 33, 16, 14)
		e := edge
		e.Route = direct
		away := []*geo.Point{p(40, 40), p(60, 40), p(60, 55), p(130, 55), p(130, 40), p(140, 40)}
		check(t, frame(card), e, away, true)
		more := []*geo.Point{p(40, 40), p(46, 40), p(46, 45), p(70, 45), p(70, 40), p(140, 40)}
		check(t, frame(card), e, more, false)
	})
	t.Run("returns through its own endpoint", func(t *testing.T) {
		bad := []*geo.Point{p(20, 20), p(20, 8), p(8, 8), p(8, 40), p(140, 40)}
		check(t, frame(), edge, bad, false)
	})
	t.Run("another edge label", func(t *testing.T) {
		f := frame()
		e := edge
		e.ID = "label"
		e.Label = "transfer"
		e.LabelWidth, e.LabelHeight = 30, 12
		e.MotionLabel = &d2target.MotionLabel{X: 75, Y: 34}
		f.Connections = append(f.Connections, e)
		check(t, f, edge, direct, false)
	})
	t.Run("icon moves onto another wire", func(t *testing.T) {
		e := edge
		e.Icon, e.IconPosition = &url.URL{Path: "icon.svg"}, "INSIDE_MIDDLE_CENTER"
		f := frame()
		wire := edge
		wire.ID = "neighbor"
		wire.Route = []*geo.Point{p(40, 27), p(140, 27)}
		f.Connections = append(f.Connections, wire)
		check(t, f, e, direct, false)
		f.Connections[1].Opacity = 0
		check(t, f, e, direct, true)
		f.Connections[1].Opacity = 1
		f.Connections[1].IsCurve = true // Missing both control points.
		check(t, f, e, direct, false)
	})
	t.Run("moving icon", func(t *testing.T) {
		e := edge
		e.Icon = &url.URL{Path: "icon.svg"}
		e.IconPosition = "INSIDE_MIDDLE_CENTER"
		f := frame()
		candidate := e
		candidate.Route = direct
		icon := candidate.GetIconPosition()
		obstacle := shape("icon obstacle", int(icon.X)+8, int(icon.Y)+1, 6, 5)
		f.Shapes = append(f.Shapes, obstacle)
		check(t, f, e, direct, false)
	})
}
