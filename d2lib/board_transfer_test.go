package d2lib

import (
	"image"
	"math"
	"reflect"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestBoardTransferKeepsTeamTogether(t *testing.T) {
	d, g := compileContinuity(t, nestedContinuityExample, d2talalayout.ContinueBoard)
	prev, next := d.Steps[0], d.Steps[1]
	m := newBoardTransfer(prev, next, g.Steps[0], g.Steps[1])
	if m == nil {
		t.Fatal("intact Telemetry transfer was not recognized")
	}
	before, after := diagramGeometryOf(prev), diagramGeometryOf(next)
	oldPanel := continuityRect(t, prev, "platform.telemetry")
	oldWire := continuityWire(t, prev, "platform.telemetry.ingest", "platform.telemetry.index").Route
	for _, progress := range []float64{0, .10, .16, .25, .48, .5, .75, .86, 1} {
		f := m.sample(next, progress)
		if err := d2target.ValidateRenderTarget(f); err != nil {
			t.Fatal(err)
		}
		panel := "reliability.telemetry"
		if progress < .14 {
			panel = "platform.telemetry"
		}
		shift := continuityRect(t, f, panel).Min.Sub(oldPanel.Min)
		if progress == .5 {
			landed := continuityRect(t, next, "reliability.telemetry").Min.Y - oldPanel.Min.Y
			fraction := float64(shift.Y) / float64(landed)
			if fraction < .70 || fraction > .90 {
				t.Fatalf("team should have a long braking approach, halfway progress %v", fraction)
			}
		}
		for _, name := range []string{".ingest", ".index"} {
			if continuityRect(t, f, panel+name) != continuityRect(t, prev, "platform.telemetry"+name).Add(shift) {
				t.Fatalf("team broke up at %.2f", progress)
			}
		}
		wire := continuityWire(t, f, panel+".ingest", panel+".index").Route
		for i, p := range wire {
			if math.Abs(p.X-oldWire[i].X-float64(shift.X)) > 1e-6 || math.Abs(p.Y-oldWire[i].Y-float64(shift.Y)) > 1e-6 {
				t.Fatalf("internal wire detached at %.2f", progress)
			}
		}
		if progress == .5 {
			for _, e := range f.Connections {
				if e.Src == "platform.api" && e.Dst == panel+".ingest" {
					t.Fatal("external connector still pointing at the moving service")
				}
			}
		}
	}
	if !reflect.DeepEqual(diagramGeometryOf(prev), before) || !reflect.DeepEqual(diagramGeometryOf(next), after) {
		t.Fatal("sampling mutated an authored endpoint")
	}
	// All the box dimensions are integer pixels. Start reconnecting on the
	// first settled sample, without exposing the final route during travel.
	firstLanded, firstWire := 0, 0
	for tick := 20; tick <= 100; tick++ {
		f := m.sample(next, float64(tick)/100)
		shapes := gridEdgeShapeMap(f.Shapes)
		landed := true
		for _, target := range next.Shapes {
			s := shapes[target.ID]
			if s.Pos != target.Pos || s.Width != target.Width || s.Height != target.Height {
				landed = false
				break
			}
		}
		if landed && firstLanded == 0 {
			firstLanded = tick
		}
		for _, e := range f.Connections {
			if e.Src == "platform.api" && e.Dst == "reliability.telemetry.ingest" {
				if !landed {
					t.Fatalf("wire reattached before the geometry arrived at tick %d", tick)
				}
				if firstWire == 0 {
					firstWire = tick
				}
			}
		}
	}
	if firstLanded == 0 || firstWire == 0 || firstWire-firstLanded > 1 {
		t.Fatalf("idle gap after landing: last body arrived at tick %d, wire entered at %d", firstLanded, firstWire)
	}
}

func TestTransferCrossesBetweenSamples(t *testing.T) {
	rect := func(x, y int) *d2target.Shape {
		return &d2target.Shape{Pos: d2target.Point{X: x, Y: y}, Width: 2, Height: 2}
	}
	if !motionBoxesCross(rect(0, 0), rect(1000, 0), rect(17, 0), rect(17, 0)) {
		t.Fatal("missed narrow obstacle between sample instants")
	}
	if motionBoxesCross(rect(0, 0), rect(1000, 0), rect(17, 2), rect(17, 2)) {
		t.Fatal("touching boundaries should not obstruct the transfer")
	}
}

func TestBoardTransferFadesCrossedStationaryWire(t *testing.T) {
	const source = `depart: {service; guard}
receive: {station}
north -> south
steps: {move: {depart.service: null; receive.service}}`
	prev, g := compileContinuity(t, source, d2talalayout.ContinueBoard)
	next := prev.Steps[0]
	// Pin the two snapshots of the same compiled transfer to a corridor.
	// The stationary wire is clear at both endpoints of the animation.
	rects := map[string]image.Rectangle{
		"depart": image.Rect(0, 0, 80, 150), "depart.guard": image.Rect(20, 20, 40, 40),
		"depart.service": image.Rect(20, 80, 40, 100), "receive.service": image.Rect(200, 80, 220, 100),
		"receive": image.Rect(160, 0, 240, 150), "receive.station": image.Rect(200, 20, 220, 40),
		"north": image.Rect(100, -60, 120, -40), "south": image.Rect(100, 180, 120, 200),
	}
	for _, board := range []*d2target.Diagram{prev, next} {
		for i, s := range board.Shapes {
			r := rects[s.ID]
			n := d2target.BaseShape()
			n.ID, n.Type, n.Pos, n.Width, n.Height = s.ID, d2target.ShapeRectangle, d2target.Point{X: r.Min.X, Y: r.Min.Y}, r.Dx(), r.Dy()
			board.Shapes[i] = *n
		}
		board.Connections[0].Route = []*geo.Point{geo.NewPoint(110, -40), geo.NewPoint(110, 180)}
	}
	next.LayoutContinuity = true
	p := newBoardTransfer(prev, next, g, g.Steps[0])
	if p == nil {
		t.Fatal("the service can travel without colliding with either endpoint")
	}
	if len(p.sample(next, .5).Connections) != 0 || len(p.sample(next, .9).Connections) != 1 {
		t.Fatal("stationary wire should fade during the crossing and reappear at rest")
	}
	// Move the entire perpendicular wire outside the corridor.
	for _, board := range []*d2target.Diagram{prev, next} {
		for i := range board.Shapes {
			if board.Shapes[i].ID == "north" || board.Shapes[i].ID == "south" {
				board.Shapes[i].Pos.X += 200
			}
		}
		for _, pt := range board.Connections[0].Route {
			pt.X += 200
		}
	}
	p = newBoardTransfer(prev, next, g, g.Steps[0])
	if p == nil || len(p.sample(next, .5).Connections) != 1 {
		t.Fatal("an unrelated clear wire should remain in place")
	}
}

func TestTransferWireAndDiagonalSweep(t *testing.T) {
	old := d2target.BaseShape()
	old.Pos, old.Width, old.Height = d2target.Point{X: 248, Y: 142}, 142, 66
	next := *old
	next.Pos = d2target.Point{X: 601, Y: 0}
	for _, tc := range []struct {
		name    string
		route   []*geo.Point
		curve   bool
		crosses bool
	}{
		{"left of worker", []*geo.Point{geo.NewPoint(112, 33), geo.NewPoint(253, 33)}, false, false},
		{"below worker", []*geo.Point{geo.NewPoint(733, 175), geo.NewPoint(894, 175)}, false, false},
		{"between the two poses", []*geo.Point{geo.NewPoint(510, 85), geo.NewPoint(510, 120)}, false, true},
		{"slanted wire outside sweep", []*geo.Point{geo.NewPoint(250, 72), geo.NewPoint(420, 5)}, false, false},
		{"cubic outside sweep", []*geo.Point{geo.NewPoint(733, 175), geo.NewPoint(760, 190), geo.NewPoint(820, 182), geo.NewPoint(894, 175)}, true, false},
		{"cubic inside sweep", []*geo.Point{geo.NewPoint(510, 85), geo.NewPoint(510, 95), geo.NewPoint(510, 110), geo.NewPoint(510, 120)}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := &d2target.Connection{Opacity: 1, Route: tc.route, IsCurve: tc.curve}
			if got := transferWireCrosses(wire, 0, 0, old, &next); got != tc.crosses {
				t.Fatalf("crosses = %v, want %v", got, tc.crosses)
			}
		})
	}
}

func TestTransferMovingWireAndResizingObstacle(t *testing.T) {
	// The two-pixel line travels a thousand pixels over the transition.
	// At 0.011 the growing obstacle reaches its height near the passing line.
	old := d2target.BaseShape()
	old.Pos, old.Width, old.Height = d2target.Point{X: 11, Y: 0}, 1, 1
	next := *old
	next.Height = 2000
	wire := &d2target.Connection{
		Opacity: 1, Route: []*geo.Point{geo.NewPoint(0, 20), geo.NewPoint(0, 22)},
	}
	if !transferWireCrosses(wire, 1000, 0, old, &next) {
		t.Fatal("missed a crossing between the first two 50 Hz samples")
	}
	// Moving both the wire and the obstacle at the same speed leaves them
	// horizontally separated even as the obstacle grows.
	next.Pos.X += 1000
	if transferWireCrosses(wire, 1000, 0, old, &next) {
		t.Fatal("parallel translation introduced a crossing")
	}
}

func TestBoardTransferKeepsClearWiresThroughout(t *testing.T) {
	const source = `vars: {d2-config: {animate-interval: 1700ms; animate-duration: 850ms}}
direction: right
checkout: Checkout
queue: Order queue
checkout -> queue
steps: {
  accept: {worker: Order worker; queue -> worker}
  group: {
    queue: null; worker: null
    intake: Order intake {queue: Order queue; worker: Order worker; queue -> worker}
    checkout -> intake.queue
  }
  fulfill: {
    fulfillment: Fulfillment {pack: Pack order; ship: Ship parcel; pack -> ship}
    intake.worker -> fulfillment.pack
  }
  handoff: {
    intake.worker: null
    fulfillment.worker: Order worker
    intake.queue -> fulfillment.worker -> fulfillment.pack
  }
}`
	board, _ := compileContinuity(t, source, d2talalayout.ContinueBoard)
	before, handoff := board.Steps[2], board.Steps[3]
	if len(handoff.TweenFrames) < 3 {
		t.Fatal("expected animated handoff")
	}
	for _, pair := range [][2]string{{"checkout", "intake.queue"}, {"fulfillment.pack", "fulfillment.ship"}} {
		want := continuityWire(t, before, pair[0], pair[1])
		for i, frame := range handoff.TweenFrames {
			got := continuityWire(t, frame, pair[0], pair[1])
			if got.Opacity != want.Opacity || !reflect.DeepEqual(got.Route, want.Route) {
				t.Fatalf("frame %d changed unobstructed wire %s", i, want.ID)
			}
		}
	}
}
