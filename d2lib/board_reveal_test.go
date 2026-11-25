package d2lib

import (
	"bytes"
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestTALABoardRevealAcrossSteps(t *testing.T) {
	const src = `vars: {d2-config: {animate-interval: 900ms; animate-duration: 600ms}}
direction: right
gateway -> queue
steps: {
  worker: {queue -> worker}
  logger: {logger -> worker}
}`
	board, _ := compileContinuity(t, src, d2talalayout.ContinueBoard)
	prev := board
	for i, id := range []string{"worker", "logger"} {
		next := board.Steps[i]
		if len(next.TweenFrames) < 5 || next.TweenDurationMS != 600 {
			t.Fatalf("%s: missing structural samples", id)
		}
		first := next.TweenFrames[0]
		if !reflect.DeepEqual(first.Shapes, prev.Shapes) || !reflect.DeepEqual(first.Connections, prev.Connections) {
			t.Errorf("%s: first sample is not the preceding board", id)
		}
		end := next.Connections[len(next.Connections)-1]
		start, tip := 0, len(end.Route)-1
		if end.Src == id {
			start, tip = tip, start
		}
		familiar := shapeByID(t, prev, "gateway")
		final := shapeByID(t, next, id)
		lastLength := 0.
		traced, popped := false, false
		for _, sample := range next.TweenFrames[1:] {
			if !reflect.DeepEqual(familiar, shapeByID(t, sample, "gateway")) {
				t.Fatalf("%s: familiar node changed", id)
			}
			n := shapeByID(t, sample, id)
			ei := slices.IndexFunc(sample.Connections, func(e d2target.Connection) bool { return e.ID == end.ID })
			if ei < 0 {
				continue
			}
			e := sample.Connections[ei]
			origin, front := e.Route[0], e.Route[len(e.Route)-1]
			if tip == 0 {
				origin, front = front, origin
			}
			if *origin != *end.Route[start] || e.SrcArrow != end.SrcArrow || e.DstArrow != end.DstArrow {
				t.Fatalf("%s: changed anchor or arrow direction", id)
			}
			length := 0.
			for j := 1; j < len(e.Route); j++ {
				length += math.Hypot(e.Route[j].X-e.Route[j-1].X, e.Route[j].Y-e.Route[j-1].Y)
			}
			if length < lastLength {
				t.Fatalf("%s: trace retracted", id)
			}
			lastLength = length
			traced = traced || *front != *end.Route[tip] && n.Opacity == 0
			popped = popped || n.Opacity > 0 && n.Width < final.Width && *front == *end.Route[tip]
		}
		if !traced || !popped {
			t.Fatalf("%s: trace before a docked pop = %t/%t", id, traced, popped)
		}
		prev = next
	}
	if err := d2target.ValidateRenderTarget(board); err != nil {
		t.Fatal(err)
	}
}

func TestTALABoardRevealRequiresDisplayedPredecessor(t *testing.T) {
	const src = `vars: {d2-config: {animate-interval: 900ms; animate-duration: 600ms}}
gateway -> queue
layers: {other: {a -> b}}
steps: {worker: {queue -> worker}}
`
	d, _ := compileContinuity(t, src, d2talalayout.ContinueBoard)
	if len(d.Steps[0].TweenFrames) != 0 {
		t.Fatal("transition played from a board that is no longer on screen")
	}
}

func TestBoardTraceUsesArcLengthThroughBends(t *testing.T) {
	a, b, c := geo.NewPoint(2, 4), geo.NewPoint(12, 4), geo.NewPoint(12, 34)
	for _, tt := range []struct {
		reverse  bool
		points   []*geo.Point
		lengths  []float64
		fraction float64
		expected []geo.Point
	}{
		{false, []*geo.Point{a, b, c}, []float64{0, 10, 40}, .5, []geo.Point{*a, *b, {X: 12, Y: 14}}},
		{false, []*geo.Point{a, b, c}, []float64{0, 10, 40}, .25, []geo.Point{*a, *b}},
		{true, []*geo.Point{c, b, a}, []float64{0, 30, 40}, .875, []geo.Point{{X: 7, Y: 4}, *b, *c}},
	} {
		w := boardTrace{backwards: tt.reverse, points: tt.points, lengths: tt.lengths}
		route := w.prefix(tt.fraction)
		if len(route) != len(tt.expected) {
			t.Fatalf("wrong point count: %v", route)
		}
		for i, p := range route {
			if *p != tt.expected[i] {
				t.Errorf("%d: got %v, want %v", i, p, tt.expected[i])
			}
		}
	}
}

func TestBoardRevealConnectedPanelAndEdgeOnly(t *testing.T) {
	const src = `vars: {d2-config: {animate-interval: 1200ms; animate-duration: 1000ms}}
direction: right
intake: Intake {gateway: Gateway; queue: Order queue; worker: Order worker; gateway -> queue -> worker}
fulfillment: Fulfillment {pack: Packing; ship: Shipping; pack -> ship}
intake.worker -> fulfillment.pack
steps: {
	audit: {
		audit: Audit {log: Audit log; reports: Reports; log -> reports}
		intake.worker -> audit.log
	}
	transfer: {
		intake.worker: null; fulfillment.worker: Order worker
		intake.queue -> fulfillment.worker
		fulfillment.worker -> fulfillment.pack
		fulfillment.worker -> audit.log
	}
	dispatch: {fulfillment.ship -> audit.log}
}`
	d, _ := compileContinuity(t, src, d2talalayout.ContinueBoard)
	step := d.Steps[0]
	if len(step.TweenFrames) == 0 {
		t.Fatal("panel reveal was not planned")
	}
	early, late := false, false
	for _, frame := range step.TweenFrames[1:] {
		a, b := shapeByID(t, frame, "audit.log"), shapeByID(t, frame, "audit.reports")
		for _, e := range frame.Connections {
			switch {
			case e.Src == "intake.worker" && e.Dst == "audit.log" && a.Opacity == 0:
				early = true
			case e.Src == "audit.log" && e.Dst == "audit.reports" && b.Opacity == 0:
				if !sameSlot(&a, gridEdgeShapeMap(step.Shapes)["audit.log"]) {
					t.Fatal("outgoing wire left before its source finished entering")
				}
				late = true
			}
		}
	}
	if !early || !late {
		t.Fatal("the two wires did not reveal in succession")
	}
	if err := d2target.ValidateRenderTarget(d); err != nil {
		t.Fatal(err)
	}

	// A new cross-container link can grow between two surviving services.
	previous, dispatch := d.Steps[1], d.Steps[2]
	if len(dispatch.TweenFrames) < 5 {
		t.Fatal("wire-only edit wasn't sampled")
	}
	mid := dispatch.TweenFrames[len(dispatch.TweenFrames)/2]
	if !reflect.DeepEqual(mid.Shapes, dispatch.Shapes) || len(mid.Connections) != len(dispatch.Connections) {
		t.Fatal("wire-only sample changed node geometry or lost a connection")
	}
	newWire := continuityWire(t, dispatch, "fulfillment.ship", "audit.log")
	partial := continuityWire(t, mid, "fulfillment.ship", "audit.log")
	if *partial.Route[0] != *newWire.Route[0] || reflect.DeepEqual(partial.Route, newWire.Route) {
		t.Fatal("new connection should be partially drawn from Shipping")
	}
	for _, e := range previous.Connections {
		i := slices.IndexFunc(mid.Connections, func(c d2target.Connection) bool { return c.ID == e.ID })
		if i < 0 || !reflect.DeepEqual(mid.Connections[i].Route, e.Route) {
			t.Fatalf("changed an existing route %s", e.ID)
		}
	}
}

func TestBoardRevealAttachesBadgesAfterPop(t *testing.T) {
	const source = `vars: {d2-config: {animate-interval: 1600ms; animate-duration: 800ms}}
a: Start
steps: {more: {a -> b; b.tooltip: "New object tip"; b.link: "https://example.com/receipt"}}`
	d, _ := compileContinuity(t, source, d2talalayout.ContinueBoard)
	step := d.Steps[0]
	final := shapeByID(t, step, "b")
	if len(step.TweenFrames) < 3 || final.Tooltip == "" || final.Link == "" {
		t.Fatal("expected a linked node with an animated entrance")
	}
	early, growing := false, false
	for _, frame := range step.TweenFrames[1:] {
		s := shapeByID(t, frame, "b")
		if sameSlot(&s, &final) && s.Opacity == final.Opacity {
			continue
		}
		early = early || s.Opacity == 0
		growing = growing || s.Opacity > 0
		doc, err := d2svg.Render(frame, nil)
		if err != nil {
			t.Fatal(err)
		}
		if s.Tooltip != "" || s.Link != "" || s.PrettyLink != "" || bytes.Contains(doc, []byte(`class="appendix-icon"`)) {
			t.Fatal("floating badge on an entering node")
		}
	}
	if !early || !growing {
		t.Fatal("missing hidden or growing sample")
	}
	doc, err := d2svg.Render(step, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(doc, []byte(`class="appendix-icon"`)) != 2 {
		t.Fatal("settled node lost its tooltip or link badge")
	}
}
