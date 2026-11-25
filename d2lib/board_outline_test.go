package d2lib

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2target"
)

func TestBoardOutlineWrapsSurvivingNodes(t *testing.T) {
	const src = `vars: {d2-config: {animate-interval: 1600ms; animate-duration: 800ms}}
direction: right
intake: Intake
check: Verify order
ledger: Ledger
intake -> check -> ledger
steps: {boundary: {
  check: null
  ledger: null
  payments: Payments {check: Verify order; ledger: Ledger; check -> ledger}
  intake -> payments.check
}}`
	d, _ := compileContinuity(t, src, d2talalayout.ContinueBoard)
	board := d.Steps[0]
	if len(board.TweenFrames) < 5 || !reflect.DeepEqual(board.TweenFrames[0].Shapes, d.Shapes) {
		t.Fatal("wrap must start on the old shapes")
	}
	old, end := shapeByID(t, d, "ledger"), shapeByID(t, board, "payments.ledger")
	if old.Pos == end.Pos {
		t.Fatal("fixture lost its moving member")
	}
	seenTravel, seenStroke := false, false
	last := -1.
	for _, f := range board.TweenFrames[1:] {
		p, child := shapeByID(t, f, "payments"), shapeByID(t, f, "payments.ledger")
		if child.Pos != end.Pos && child.Pos != old.Pos {
			seenTravel = true
		}
		if p.OutlineProgress != nil {
			pt := *p.OutlineProgress
			if pt < last {
				t.Fatal("outline reversed")
			}
			last = pt
			if pt > 0 && pt < 1 {
				seenStroke = true
				if child.Pos != end.Pos || child.Opacity != end.Opacity {
					t.Fatal("traced around a moving or fading member")
				}
			}
		}
		if len(f.Connections) != 2 {
			t.Fatal("lost a surviving wire during the wrap")
		}
		for _, edge := range f.Connections {
			a, b := shapeByID(t, f, edge.Src), shapeByID(t, f, edge.Dst)
			if !(onRectEdge(a, edge.Route[0].X, edge.Route[0].Y) && onRectEdge(b, edge.Route[len(edge.Route)-1].X, edge.Route[len(edge.Route)-1].Y)) {
				t.Fatal("detached wire during wrap")
			}
		}
	}
	if !seenTravel || !seenStroke || shapeByID(t, board, "payments").OutlineProgress != nil {
		t.Fatal("missing slide, trace, or clean endpoint")
	}
	static, _ := compileContinuity(t, strings.Replace(src, "800ms", "0ms", 1), d2talalayout.ContinueBoard)
	if len(static.Steps[0].TweenFrames) != 0 || !reflect.DeepEqual(board.Shapes, static.Steps[0].Shapes) || !reflect.DeepEqual(board.Connections, static.Steps[0].Connections) {
		t.Fatal("wrap changed its authored endpoint or ignored a zero duration")
	}
}

func onRectEdge(s d2target.Shape, x, y float64) bool {
	l, t := float64(s.Pos.X), float64(s.Pos.Y)
	r, b := l+float64(s.Width), t+float64(s.Height)
	return x >= l-1 && x <= r+1 && y >= t-1 && y <= b+1 && (x <= l+1 || x >= r-1 || y <= t+1 || y >= b-1)
}

func TestBoardOutlineGuardsAndNesting(t *testing.T) {
	d, g := compileContinuity(t, `root: Root
steps: {section: {group: {nested: {one: Entry}}}}`, d2talalayout.ContinueBoard)
	step := d.Steps[0]
	p := planBoardOutline(d, step, g, g.Steps[0])
	if p == nil || len(p.start) != 2 || p.start["group.nested"] <= p.start["group"] || !p.needsReveal {
		t.Fatal("nested containers should enter in sequence alongside their new leaf")
	}
	frame := *step
	frame.Shapes = slices.Clone(step.Shapes)
	p.apply(&frame, .08)
	if shapeByID(t, &frame, "group").Opacity <= 0 || shapeByID(t, &frame, "group.nested").Opacity != 0 {
		t.Fatal("inner frame should follow outer frame")
	}
	step.Shapes = slices.Clone(step.Shapes)
	for i := range step.Shapes {
		if step.Shapes[i].ID == "group" {
			step.Shapes[i].BorderRadius = 12
		}
	}
	if planBoardOutline(d, step, g, g.Steps[0]) != nil {
		t.Fatal("rounded outline requires its native renderer")
	}
}
