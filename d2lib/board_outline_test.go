package d2lib

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
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
	d, g := compileContinuity(t, src, d2talalayout.ContinueBoard)
	board := d.Steps[0]
	if len(board.TweenFrames) < 5 || !reflect.DeepEqual(board.TweenFrames[0].Shapes, d.Shapes) {
		t.Fatal("wrap must start on the old shapes")
	}
	old, end := shapeByID(t, d, "ledger"), shapeByID(t, board, "payments.ledger")
	if old.Pos == end.Pos {
		t.Fatal("fixture lost its moving member")
	}
	seenTravel, seenStroke, seenOverlap := false, false, false
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
				if child.Opacity != end.Opacity || child.Pos.X < p.Pos.X || child.Pos.Y < p.Pos.Y+p.LabelHeight ||
					child.Pos.X+child.Width > p.Pos.X+p.Width || child.Pos.Y+child.Height > p.Pos.Y+p.Height {
					t.Fatal("traced while a wrapped service still crossed the border or title")
				}
				seenOverlap = seenOverlap || child.Pos != end.Pos
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
	if !seenTravel || !seenStroke || !seenOverlap || shapeByID(t, board, "payments").OutlineProgress != nil {
		t.Fatal("missing slide, trace, or clean endpoint")
	}
	// Moving the old Ledger well outside Payments means the same early clock
	// would expose a border over the still-arriving service.
	ledgerBefore := gridEdgeShapeMap(d.Shapes)["ledger"]
	ledgerBefore.Pos.X += 4000
	out := planBoardOutline(d, board, g, g.Steps[0])
	ledgerBefore.Pos.X -= 4000
	if out == nil || out.start["payments"] < boardGatherEnd {
		t.Fatal("outline should wait for this longer approach")
	}

	static, _ := compileContinuity(t, strings.Replace(src, "800ms", "0ms", 1), d2talalayout.ContinueBoard)
	if len(static.Steps[0].TweenFrames) != 0 || !reflect.DeepEqual(board.Shapes, static.Steps[0].Shapes) || !reflect.DeepEqual(board.Connections, static.Steps[0].Connections) {
		t.Fatal("wrap changed its authored endpoint or ignored a zero duration")
	}
}

func TestBoardOutlineGatherClearsPaintAndTitles(t *testing.T) {
	panel := &d2graph.Object{ID: "panel"}
	leaf := &d2graph.Object{ID: "leaf", Parent: panel}
	sibling := &d2graph.Object{ID: "sibling"}
	members := map[*d2graph.Object]*d2graph.Object{leaf: nil, sibling: nil}
	frame := d2target.BaseShape()
	frame.ID, frame.Width, frame.Height, frame.StrokeWidth = "panel", 240, 200, 20
	frame.Label, frame.LabelHeight, frame.LabelPosition = "Operations", 20, "INSIDE_TOP_CENTER"
	a, b := d2target.BaseShape(), d2target.BaseShape()
	a.ID, a.Width, a.Height, a.Pos = leaf.AbsID(), 20, 20, d2target.Point{X: 110, Y: 100}
	b.ID, b.Width, b.Height, b.Pos = sibling.AbsID(), 20, 20, d2target.Point{X: 246, Y: 100}
	shapes := map[string]*d2target.Shape{frame.ID: frame, a.ID: a, b.ID: b}
	o := &boardOutline{positions: map[string]d2target.Point{a.ID: {X: 90, Y: 100}, b.ID: {X: 256, Y: 100}}}
	if o.canTraceDuringGather(panel, members, shapes) {
		t.Fatal("sibling overlaps the outside half of the painted border")
	}
	b.Pos = d2target.Point{X: 110, Y: -100}
	o.positions[b.ID] = d2target.Point{X: 110, Y: -120}
	if !o.canTraceDuringGather(panel, members, shapes) {
		t.Fatal("both bodies now clear the frame and title")
	}
	wire := d2target.BaseConnection()
	wire.Src, wire.Dst = b.ID, a.ID
	o.edges = &gridMotionEdges{pairs: map[string]*gridEdgePair{"link": {after: gridEdgeLayout{wire, b, a}}}}
	if o.canTraceDuringGather(panel, members, shapes) {
		t.Fatal("external connector crosses the panel title while its port moves")
	}
}

func TestBoardOutlinePenSlowsAtTurns(t *testing.T) {
	for _, r := range [][2]float64{{1, 100}, {200, 100}, {1000, 1}} {
		w, h := r[0], r[1]
		last := 0.
		for i := 1; i <= 1000; i++ {
			at := boardOutlineStroke(float64(i)/1000, w, h)
			if at < last || at > 1 {
				t.Fatalf("outline reversed or passed its seam at %v, frame %d", r, i)
			}
			last = at
		}
		if last != 1 {
			t.Fatal("outline did not close")
		}
	}
	// For w:h=2:1, both tips turn at one third and two thirds of their path.
	for _, corner := range []float64{1. / 3, 2. / 3} {
		lo, hi := 0., 1.
		for range 48 {
			mid := (lo + hi) / 2
			if boardOutlineStroke(mid, 200, 100) < corner {
				lo = mid
			} else {
				hi = mid
			}
		}
		tipSpeed := (boardOutlineStroke(hi+1e-5, 200, 100) - boardOutlineStroke(hi-1e-5, 200, 100)) / 2e-5
		freeSpeed := 12 * hi * math.Pow(1-hi, 2)
		if tipSpeed <= 0 || tipSpeed > freeSpeed*.75 {
			t.Fatalf("pen should round each turn at a reduced, nonzero speed: %f vs %f", tipSpeed, freeSpeed)
		}
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
