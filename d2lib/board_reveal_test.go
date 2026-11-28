package d2lib

import (
	"bytes"
	"math"
	"reflect"
	"slices"
	"sort"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	d2log "github.com/d2lang/d2/lib/log"
)

func TestDagreStableNodeArrivalAndRemoval(t *testing.T) {
	const src = `vars: {d2-config: {animate-interval: 800ms; animate-duration: 400ms}}
build: Build {width: 160; height: 80}
steps: {
  audit: {audit: Audit {shape: hexagon; width: 160; height: 80}}
  remove: {audit: null}
  prepend: {lint -> build}
  undo: {lint: null}
  clear: {build: null}
  hold: {}
  restore: {report: Report}
}`
	var calls int
	d, _, err := Compile(d2log.WithDefault(t.Context()), src, dagreCompileOptions(&calls), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		step                *d2target.Diagram
		early, late, fading int
	}{
		{d.Steps[0], 100, 300, 50},
		{d.Steps[1], 300, 100, 350},
	} {
		at := func(ms int) d2target.Shape {
			t.Helper()
			i := sort.Search(len(tc.step.TweenFrames), func(i int) bool { return tc.step.TweenFrameEndMS(i) >= ms })
			if i+1 >= len(tc.step.TweenFrames) || tc.step.TweenFrameEndMS(i) != ms {
				t.Fatalf("%s missing sample at %d ms", tc.step.Name, ms)
			}
			f := tc.step.TweenFrames[i+1]
			if !reflect.DeepEqual(shapeByID(t, f, "build"), shapeByID(t, d, "build")) {
				t.Fatal("the persistent build stage changed during the transition")
			}
			return shapeByID(t, f, "audit")
		}
		// The 160px body grows from 32% size. A quarter of its clock
		// covers 1/16 of the remaining distance, three quarters 15/16.
		if early, late := at(tc.early).Width, at(tc.late).Width; early != 58 || late != 153 {
			t.Fatalf("%s quarter widths = %d, %d; want 58, 153", tc.step.Name, early, late)
		}
		if alpha := at(tc.fading).Opacity; math.Abs(alpha-.23842) > .0001 {
			t.Fatalf("%s opacity at %d ms = %v", tc.step.Name, tc.fading, alpha)
		}
	}
	if shapeByID(t, d, "build").Pos == shapeByID(t, d.Steps[2], "build").Pos {
		t.Fatal("fixture did not reposition the build stage")
	}
	for _, step := range d.Steps[2:4] {
		if len(step.TweenFrames) != 0 {
			t.Fatalf("%s animated a node on a different layout", step.Name)
		}
	}
	clear, hold, restore := d.Steps[4], d.Steps[5], d.Steps[6]
	if clear.IsFolderOnly || len(clear.TweenFrames) == 0 || len(clear.Shapes) != 0 ||
		hold.IsFolderOnly || len(hold.TweenFrames) != 0 ||
		len(restore.TweenFrames) == 0 || len(restore.TweenFrames[0].Shapes) != 0 {
		t.Fatal("clearing, holding, and rebuilding must preserve the displayed empty canvas")
	}
}

func TestTALABoardRevealAcrossSteps(t *testing.T) {
	const src = `vars: {d2-config: {animate-interval: 900ms; animate-duration: 600ms}}
direction: right
gateway -> queue
steps: {
  worker: {queue -> worker}
  logger: {logger -> worker; logger.style.opacity: 0.45}
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
		anchor := end.Route[0]
		if end.Src == id {
			anchor = end.Route[len(end.Route)-1]
		}
		familiar := shapeByID(t, prev, "gateway")
		lastLength := 0.
		for _, sample := range next.TweenFrames[1:] {
			if !reflect.DeepEqual(familiar, shapeByID(t, sample, "gateway")) {
				t.Fatalf("%s: familiar node changed", id)
			}
			ei := slices.IndexFunc(sample.Connections, func(e d2target.Connection) bool { return e.ID == end.ID })
			if ei < 0 {
				continue
			}
			e := sample.Connections[ei]
			origin := e.Route[0]
			if end.Src == id {
				origin = e.Route[len(e.Route)-1]
			}
			if *origin != *anchor || e.SrcArrow != end.SrcArrow || e.DstArrow != end.DstArrow {
				t.Fatalf("%s: changed anchor or arrow direction", id)
			}
			length := geo.Route(e.Route).Length()
			if length < lastLength {
				t.Fatalf("%s: trace retracted", id)
			}
			lastLength = length
		}
		requireArrivalOverlap(t, next, end, id)
		prev = next
	}
	if err := d2target.ValidateRenderTarget(board); err != nil {
		t.Fatal(err)
	}
}

// Check the native samples a viewer sees, including when the new node is at
// the source end of the authored route. Measure distance along the route so a
// bend that passes near the port cannot trigger the pop halfway through.
func requireArrivalOverlap(t *testing.T, step *d2target.Diagram, wire d2target.Connection, id string) {
	t.Helper()
	final := shapeByID(t, step, id)
	length := geo.Route(wire.Route).Length()
	inFlight, overlap, docked := false, false, false
	for i, f := range step.TweenFrames[1:] {
		node := shapeByID(t, f, id)
		gap := length
		for _, e := range f.Connections {
			if e.ID == wire.ID {
				gap -= geo.Route(e.Route).Length()
				break
			}
		}
		ms := step.TweenFrameEndMS(i)
		opacity := node.Opacity / final.Opacity
		if gap > 8 && opacity > .02 {
			t.Fatalf("%s at %dms: visible with %.2fpx of route left", id, ms, gap)
		}
		if gap <= .5 && opacity < .1 {
			t.Fatalf("%s at %dms: tip is within half a pixel of the port, but node is only %.0f%% opaque", id, ms, 100*opacity)
		}
		inFlight = inFlight || gap > 8 && gap < length && opacity == 0
		overlap = overlap || gap > .01 && opacity >= .1 && node.Width > final.Width/3
		if gap <= 1e-8 && !docked {
			if opacity < .5 || node.Width <= final.Width*2/5 {
				t.Fatalf("%s at %dms: arrow reached port with node %.0f%% opaque and %d/%dpx wide", id, ms, 100*opacity, node.Width, final.Width)
			}
			docked = true
		}
	}
	if !inFlight || !overlap || !docked {
		t.Fatalf("%s: saw approach=%t, visible opening during approach=%t, contact=%t", id, inFlight, overlap, docked)
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
	requireArrivalOverlap(t, step, continuityWire(t, step, "intake.worker", "audit.log"), "audit.log")
	requireArrivalOverlap(t, step, continuityWire(t, step, "audit.log", "audit.reports"), "audit.reports")
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

func TestBoardRevealShorterThanArrowhead(t *testing.T) {
	prev, g := compileContinuity(t, "direction: right\nqueue\nsteps: {send: {queue -> ship}}", d2talalayout.ContinueBoard)
	next := prev.Steps[0]
	wire := &next.Connections[0]
	ship := gridEdgeShapeMap(next.Shapes)["ship"]
	// Put Shipping just four pixels beyond Queue, leaving too little shaft
	// to paint an intermediate arrow without its marker covering the origin.
	origin, port := wire.Route[0], wire.Route[len(wire.Route)-1]
	x := origin.X + 4
	ship.Pos.X += int(math.Round(x - port.X))
	wire.Route = []*geo.Point{origin, geo.NewPoint(x, origin.Y)}
	m := planBoardReveal(prev, next, g.Steps[0])
	if m == nil {
		t.Fatal("missing short-route reveal")
	}
	for i := 0; i <= 48; i++ {
		frame := *next
		frame.Shapes = slices.Clone(next.Shapes)
		m.apply(&frame, float64(i)/48)
		node := shapeByID(t, &frame, "ship")
		if len(frame.Connections) == 0 {
			if node.Opacity > 0 {
				t.Fatalf("sample %d: node became visible before its incoming wire", i)
			}
			continue
		}
		if node.Opacity < ship.Opacity/2 || node.Width >= ship.Width {
			t.Fatalf("sample %d: first wire arrived with node opacity %.2f and width %d/%d", i, node.Opacity, node.Width, ship.Width)
		}
		return
	}
	t.Fatal("short wire never appeared")
}

func TestBoardArrivalMaintainsGrowthSpeedAtContact(t *testing.T) {
	// A long wire spends little time in the final six pixels. Its node
	// still needs to keep opening smoothly as the tip docks.
	b := boardBirth{start: .48, contact: .5, end: 1, rebound: .04}
	scale := func(now float64) float64 {
		value, _ := b.pop(b.progress(now))
		return value
	}
	const dt = .000001
	at := scale(b.contact)
	in := (at - scale(b.contact-dt)) / dt
	out := (scale(b.contact+dt) - at) / dt
	if in <= 0 || out <= 0 || math.Abs(in-out) > .01*max(in, out) {
		t.Fatalf("node growth rate changed at contact: %.4f -> %.4f", in, out)
	}
}

func TestBoardRevealEasesToThePortThenRebounds(t *testing.T) {
	const src = `vars: {d2-config: {animate-interval: 1400ms; animate-duration: 800ms}}
direction: right
order: Order service
queue: Dispatch queue
order -> queue
steps: {ship: {ship: Shipping; queue -> ship}}`
	prev, graph := compileContinuity(t, src, d2talalayout.ContinueBoard)
	next, g := prev.Steps[0], graph.Steps[0]
	m := planBoardReveal(prev, next, g)
	if m == nil || m.nodes["ship"].rebound <= 0 {
		t.Fatal("isolated leaf should have room for a small rebound")
	}
	apply := func(t float64) *d2target.Diagram {
		frame := *next
		frame.Shapes = slices.Clone(next.Shapes)
		m.apply(&frame, t)
		return &frame
	}
	wire := continuityWire(t, next, "queue", "ship")
	trace := m.wires[wire.ID]
	half := continuityWire(t, apply((trace.start+trace.end)/(2*m.end)), "queue", "ship")
	start, port := wire.Route[0], wire.Route[1]
	if len(wire.Route) != 2 || *half.Route[0] != *start {
		t.Fatal("expected a straight route anchored at the dispatch queue")
	}
	tip := half.Route[1]
	fraction := math.Hypot(tip.X-start.X, tip.Y-start.Y) / math.Hypot(port.X-start.X, port.Y-start.Y)
	if math.Abs(fraction-.6875) > 1e-6 {
		t.Fatalf("at half the trace time, tip has traveled %.3f of the route", fraction)
	}
	body := shapeByID(t, next, "ship")
	peak := body
	for _, f := range next.TweenFrames[1:] {
		if s := shapeByID(t, f, "ship"); s.Width > peak.Width {
			peak = s
		}
	}
	if float64(body.Pos.X) != port.X || peak.Pos.X != body.Pos.X || math.Abs(float64(peak.Pos.Y)+float64(peak.Height)/2-port.Y) > 1 {
		t.Fatal("the overshoot moved the port away from the arriving wire")
	}
	if peak.Width <= body.Width || peak.Width > body.Width+5 || peak.Height > body.Height+5 {
		t.Fatalf("rebound changed %dx%d to %dx%d", body.Width, body.Height, peak.Width, peak.Height)
	}
	end := apply(1)
	if !reflect.DeepEqual(end.Shapes, next.Shapes) || !reflect.DeepEqual(end.Connections, next.Connections) {
		t.Fatal("revealed diagram differs from its authored endpoint")
	}
	// Put a neighboring body, then a passing wire, just outside the final
	// right face. They both need the settling motion to stay within the box.
	n := g.Root.Children["ship"]
	for _, obstacle := range []string{"body", "wire"} {
		packed := *next
		packed.Shapes = slices.Clone(next.Shapes)
		packed.Connections = slices.Clone(next.Connections)
		shapes := gridEdgeShapeMap(packed.Shapes)
		x, y := body.Pos.X+body.Width+2, body.Pos.Y
		if obstacle == "body" {
			shapes["order"].Pos = d2target.Point{X: x, Y: y}
		} else {
			packed.Connections[0].Route = []*geo.Point{geo.NewPoint(float64(x), float64(y-20)), geo.NewPoint(float64(x), float64(y+body.Height+20))}
		}
		b := m.nodes["ship"]
		b.rebound = m.popClearance(n, &packed, g, shapes, &packed.Connections[1], b)
		if b.rebound != 0 {
			t.Errorf("nearby %s was too close to allow a rebound", obstacle)
		}
		last := 0.
		for i := 0; i <= 100; i++ {
			scale, _ := b.pop(float64(i) / 100)
			if scale < last || scale > 1 || i == 100 && scale != 1 {
				t.Fatalf("tight %s: scale %.5f at %d%%", obstacle, scale, i)
			}
			last = scale
		}
	}
}

func TestBoardTraceRoundsOffSpeedAtSharpTurns(t *testing.T) {
	points := []*geo.Point{geo.NewPoint(777, 410), geo.NewPoint(777, 490), geo.NewPoint(497, 490), geo.NewPoint(497, 570)}
	w := boardTrace{points: points, lengths: []float64{0, 80, 360, 440}}
	// Bisection gives the instant the pen reaches the bend's location.
	lo, hi := 0., 1.
	for range 40 {
		mid := (lo + hi) / 2
		if boardStroke(mid) < 80./440 {
			lo = mid
		} else {
			hi = mid
		}
	}
	const dt = .0001
	speed := (w.travel(lo+dt) - w.travel(lo-dt)) / (boardStroke(lo+dt) - boardStroke(lo-dt))
	if speed < .50 || speed > .55 {
		t.Fatalf("speed at the first turn was %.1f%% of the straight stroke", 100*speed)
	}
	last := 0.
	for i := 0; i <= 1000; i++ {
		d := w.travel(float64(i) / 1000)
		if d < last || d > 1 || i == 0 && d != 0 || i == 1000 && d != 1 {
			t.Fatalf("unexpected travel %.8f at sample %d", d, i)
		}
		last = d
	}
	// A straight span that happens to have an extra waypoint retains its speed.
	w.points = []*geo.Point{geo.NewPoint(0, 0), geo.NewPoint(80, 0), geo.NewPoint(440, 0)}
	w.lengths = []float64{0, 80, 440}
	if w.travel(lo+dt) != boardStroke(lo+dt) {
		t.Fatal("collinear waypoints slowed down the tip")
	}
}

func TestBoardRevealLeavesRoomForArrowheads(t *testing.T) {
	const config = "vars: {d2-config: {animate-interval: 1900ms; animate-duration: 1500ms}}\ndirection: right\n"
	for _, tc := range []struct{ name, source string }{
		{"two arrows", "queue\nsteps: {add: {queue <-> worker}}"},
		{"thick endpoints", "queue.style.stroke-width: 15\nsteps: {add: {queue -> worker; worker.style.stroke-width: 15}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			board, _ := compileContinuity(t, config+tc.source, d2talalayout.ContinueBoard)
			step := board.Steps[0]
			full := geo.Route(step.Connections[0].Route).Length()
			visible := false
			for _, frame := range step.TweenFrames {
				for _, e := range frame.Connections {
					if geo.Route(e.Route).Length() >= full {
						continue
					}
					visible = true
					src, dst := e.ArrowheadAdjustmentsForWidths(shapeByID(t, frame, e.Src).RenderedStrokeWidth(), shapeByID(t, frame, e.Dst).RenderedStrokeWidth())
					cmd := slices.Collect(e.PathCommands(src, dst))
					separation := cmd[len(cmd)-1].End.X - cmd[0].End.X
					if separation <= 0 {
						t.Fatalf("rightward trace points left by %.2f px", -separation)
					}
					if e.SrcArrow != d2target.NoArrowhead {
						width, _ := e.DstArrow.Dimensions(e.RenderedStrokeWidth())
						// Each triangle extends inward from its marker reference point.
						inward := width - 1.5*e.RenderedStrokeWidth()
						if separation < 2*inward {
							t.Fatalf("marker origins are %.2f apart; triangles occupy %.2f", separation, 2*inward)
						}
					}
				}
			}
			if !visible {
				t.Fatal("expected a partially drawn connection")
			}
		})
	}
}
