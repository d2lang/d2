package d2animate

import (
	"bytes"
	"math"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func iconTestBoard(rate float64) *d2target.Diagram {
	b := d2target.NewDiagram()
	for _, id := range []string{"a", "b"} {
		s := d2target.BaseShape()
		s.ID, s.Type, s.Width, s.Height = id, d2target.ShapeRectangle, 40, 40
		b.Shapes = append(b.Shapes, *s)
	}
	e := d2target.BaseConnection()
	e.ID, e.Src, e.Dst = "(a -> b)[0]", "a", "b"
	e.DstArrow = d2target.TriangleArrowhead
	e.Route = []*geo.Point{geo.NewPoint(40, 20), geo.NewPoint(410, 20)}
	e.IconShape = d2target.ShapeCircle
	e.IconAnimation = &d2target.ConnectionIconAnimation{Rate: &rate}
	b.Connections = []d2target.Connection{*e}
	return b
}

func TestIconCadenceChangeAndDrain(t *testing.T) {
	root, faster, stopped := iconTestBoard(2), iconTestBoard(4), iconTestBoard(0)
	root.Steps = []*d2target.Diagram{faster, stopped}
	timeline, err := NewIconTimeline(root, 650)
	if err != nil {
		t.Fatal(err)
	}
	xs := func(b *d2target.Diagram, at int) []float64 {
		out := []float64{}
		for _, p := range timeline.Sample(b, b, at).Connections[0].IconPositions {
			out = append(out, math.Round(p.X*100)/100)
		}
		slices.Sort(out)
		return out
	}
	if end, start := xs(root, 650), xs(faster, 0); !slices.Equal(end, start) {
		t.Fatalf("existing copies jumped on rate change: %v -> %v", end, start)
	}
	// 150ms into the old 500ms period, so 70% of the new 250ms period remains.
	old, new := xs(faster, 174), xs(faster, 175)
	if len(new) <= len(old) || new[0] >= old[0] {
		t.Fatalf("missing launch on continuous phase: %v -> %v", old, new)
	}
	if end, start := xs(faster, 650), xs(stopped, 0); !slices.Equal(end, start) {
		t.Fatalf("stop dropped live copies: %v -> %v", end, start)
	}
	if got := xs(stopped, 2200); len(got) != 0 {
		t.Fatalf("new copies after stop: %v", got)
	}
	if root.Connections[0].IconPositions != nil {
		t.Fatal("sampling changed the authored connection")
	}
	frames, err := Timeline(root, 650)
	if err != nil {
		t.Fatal(err)
	}
	var duration int
	for _, frame := range frames {
		duration += frame.DurationMS
	}
	if duration != 1950 || len(frames) != 3 || frames[1].Picture().Connections[0].IconSchedule == nil {
		t.Fatalf("unexpected animation: %d frames, %dms", len(frames), duration)
	}
}

func TestIconTimelineIndependentBranches(t *testing.T) {
	root, other, step := iconTestBoard(2), iconTestBoard(0), iconTestBoard(0)
	root.Layers = []*d2target.Diagram{other}
	root.Steps = []*d2target.Diagram{step}
	tl, err := NewIconTimeline(root, 500)
	if err != nil {
		t.Fatal(err)
	}
	if tl.Active(other) || tl.Active(step) {
		t.Fatal("unrelated board leaked in-flight stream")
	}
}

func TestIconPathFollowsRoundedRouteAndArrow(t *testing.T) {
	b := iconTestBoard(2)
	e := b.Connections[0]
	e.BorderRadius = 30
	e.Route = []*geo.Point{geo.NewPoint(0, 0), geo.NewPoint(100, 0), geo.NewPoint(100, 100)}
	forward := e.IconMotionPath(2, 2)
	e.SrcArrow, e.DstArrow = e.DstArrow, e.SrcArrow
	e.Route[0], e.Route[2] = e.Route[2], e.Route[0]
	reverse := e.IconMotionPath(2, 2)
	if forward.Length() >= 200 || len(forward.Points) < 4 {
		t.Fatalf("unrounded path %+v", forward)
	}
	for _, fraction := range []float64{0, .25, .5, .75, 1} {
		a, b := forward.PointAt(forward.Length()*fraction), reverse.PointAt(reverse.Length()*fraction)
		if math.Hypot(a.X-b.X, a.Y-b.Y) > .3 {
			t.Errorf("arrow direction changed travel at %g: %v vs %v", fraction, a, b)
		}
	}
	rate := 1e6
	if n := math.Ceil(forward.Length() / d2target.IconMotionSpeed / forward.LaunchPeriod(&d2target.ConnectionIconAnimation{Rate: &rate})); n > d2target.MaxMovingIconsPerConnection {
		t.Errorf("dense stream has %g copies", n)
	}
}

func TestLongIconHoldUsesBoundedArtwork(t *testing.T) {
	root, step := iconTestBoard(10), iconTestBoard(10)
	root.Steps = []*d2target.Diagram{step}
	frames, err := Timeline(root, 20_000_000)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range frames {
		data, err := d2svg.Render(f.Picture(), &d2svg.RenderOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if count := bytes.Count(data, []byte("--d2-icon-origin:")); count == 0 || count > 2*d2target.MaxMovingIconsPerConnection {
			t.Fatalf("long hold drew %d copies", count)
		}
	}
}

func TestIconTourClockAndLayerLinks(t *testing.T) {
	root, detail, stop := iconTestBoard(2), iconTestBoard(4), iconTestBoard(0)
	detail.Name, stop.Name = "detail", "stop"
	root.Layers = []*d2target.Diagram{detail}
	detail.Steps = []*d2target.Diagram{stop}
	root.Shapes[0].Link, detail.Shapes[1].Link = "root.layers.detail", "root"

	frames, err := SVGTimeline(root, nil, 450, 650)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 {
		t.Fatalf("got %d tour frames, want 3", len(frames))
	}
	hash, err := root.HashID(nil)
	if err != nil {
		t.Fatal(err)
	}
	opts := d2svg.RenderOpts{MasterID: hash}
	var svgs [][]byte
	for i, board := range []*d2target.Diagram{root, detail, stop} {
		f := frames[i]
		if f.Diagram != board || f.DurationMS != 450 || f.Picture() == board {
			t.Fatalf("frame %d lost its authored board or step duration", i)
		}
		s := f.Picture().Connections[0].IconSchedule
		// Zoom into detail, run its stop step, then zoom back to the root.
		// The interactive detail sequence retains its original 900ms clock.
		if s == nil || s.FilmMS != 1750 || s.StartMS != []int{0, 650, 1100}[i] || s.DurationMS != []int{650, 450, 650}[i] || s.FixedLayer != (i == 0) {
			t.Fatalf("frame %d schedule: %+v", i, s)
		}
		if i == 2 && (!math.IsInf(s.Period, 1) || len(s.Carried) == 0) {
			t.Fatalf("the stop step should drain the in-flight copies: %+v", s)
		}
		b, err := d2svg.Render(f.Picture(), &opts)
		if err != nil {
			t.Fatal(err)
		}
		svgs = append(svgs, b)
	}
	out, err := WrapTimeline(root, nil, frames, svgs, opts, 650)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`href="#d2-layer-link-`, `--d2-icon-private-clock:d2-icon-layer 900ms`, `animation:d2-icon-free var(--d2-icon-free-duration)`, `--d2-icon-film-ms:1750ms`, `class="d2-still-icon"`} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("animated linked tour is missing %q", want)
		}
	}
}

func TestPlainLinkToStoppedStream(t *testing.T) {
	root, stop := iconTestBoard(2), iconTestBoard(0)
	stop.Name = "stop"
	root.Steps = []*d2target.Diagram{stop}
	root.Shapes[0].Link = "root.steps.stop"
	frames, err := SVGTimeline(root, nil, 450, 650)
	if err != nil {
		t.Fatal(err)
	}
	s := frames[1].Picture().Connections[0].IconSchedule
	if s == nil || !s.FixedLayer || !math.IsInf(s.Period, 1) || len(s.Carried) == 0 {
		t.Fatalf("pinned stop must let in-flight copies finish: %+v", s)
	}
	// A registered CSS number may round a transit in seconds. The terminal
	// visibility gate instead compares normalized progress against exactly 1.
	data, err := d2svg.Render(frames[1].Picture(), &d2svg.RenderOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(data, []byte("--d2-icon-alive:max(0,sign(1 - var(--d2-icon-free-phase)))")); n != len(s.Carried) {
		t.Fatalf("only %d of %d carried copies disappear at the endpoint", n, len(s.Carried))
	}
	nav := NewLayerNavigation(root, nil)
	links := layerLinks(nav, frames, 650)
	var css bytes.Buffer
	writeLayerIconClocks(&css, "test", links, layerSlots(nav, frames, links), "")
	if !bytes.Contains(css.Bytes(), []byte("calc(var(--d2-click-time,0ms) + var(--d2-icon-delay,0s))")) {
		t.Fatal("plain board links must start draining immediately")
	}
}

func TestIconRateFitsCompleteSVGLoop(t *testing.T) {
	root := iconTestBoard(.25)
	if _, err := SVGTimeline(root, nil, 1000, 0); err != nil {
		t.Fatalf("standalone SVG runs continuously: %v", err)
	}
	step := iconTestBoard(.25)
	root.Steps = []*d2target.Diagram{step}
	if _, err := SVGTimeline(root, nil, 1000, 0); err == nil || !strings.Contains(err.Error(), "at least 4s (got 2s)") {
		t.Fatalf("expected the two-second film to reject a four-second period: %v", err)
	}
	if _, err := SVGTimeline(root, nil, 2000, 0); err != nil {
		t.Fatalf("two short steps together make one full departure period: %v", err)
	}
	root.Steps = nil
	root.Layers = []*d2target.Diagram{step}
	step.Name = "detail"
	root.Shapes[0].Link, step.Shapes[1].Link = "root.layers.detail", "root"
	if err := ValidateSVGIconLoop(root, nil, 1000, 2000); err != nil {
		t.Fatalf("linked-board camera holds extend the actual loop to four seconds: %v", err)
	}
}

func TestIconLoopChecksOnlyVisibleNumericRates(t *testing.T) {
	board := iconTestBoard(.25)
	c := &board.Connections[0]
	c.Opacity = 0
	if err := d2target.ValidateIconLoopDuration(board, 100); err != nil {
		t.Fatal(err)
	}
	c.Opacity = 1
	for _, stroke := range []string{"transparent", "#ff000000"} {
		c.Stroke = stroke
		if err := d2target.ValidateIconLoopDuration(board, 100); err != nil {
			t.Fatalf("invisible circle on %q: %v", stroke, err)
		}
	}
	c.IconShape, c.Icon = "", &url.URL{Path: "envelope.svg"}
	if err := d2target.ValidateIconLoopDuration(board, 100); err == nil {
		t.Fatal("an image remains visible on a transparent wire")
	}
	c.Icon, c.IconShape, c.Stroke = nil, d2target.ShapeCircle, "#000"
	c.IconAnimation.Rate = nil
	if err := d2target.ValidateIconLoopDuration(board, 100); err != nil {
		t.Fatalf("a single repeating icon has no authored departure rate: %v", err)
	}
	c.IconAnimation.Rate = new(0.)
	if err := d2target.ValidateIconLoopDuration(board, 100); err != nil {
		t.Fatalf("a stopped stream has no departures: %v", err)
	}
	c.IconAnimation.Rate = new(1000.)
	c.Route = []*geo.Point{geo.NewPoint(0, 0), geo.NewPoint(230402, 0)}
	if err := d2target.ValidateIconLoopDuration(board, 1000); err == nil {
		t.Fatal("a crowded long route must use its effective departure period")
	}
}

func TestIconRateToTimedLoopAndBack(t *testing.T) {
	stream, timed, resumed, stop := iconTestBoard(5), iconTestBoard(0), iconTestBoard(2), iconTestBoard(0)
	timed.Connections[0].IconAnimation = &d2target.ConnectionIconAnimation{DurationMS: 800}
	boards := []*d2target.Diagram{stream, timed, resumed, stop}
	// The first two boards take 300ms to cross at default speed. A layout
	// change lengthens the route on the last two boards to a 500ms trip.
	for i, b := range boards {
		wire := &b.Connections[0]
		length := []float64{54, 54, 90, 90}[i]
		insets := wire.EndpointInsets(b.Shapes[0].RenderedStrokeWidth(), b.Shapes[1].RenderedStrokeWidth())
		wire.Route[1].X = wire.Route[0].X + length + insets[0] + insets[1]
	}
	stream.Steps = boards[1:]
	timeline, err := NewIconTimeline(stream, 400)
	if err != nil {
		t.Fatal(err)
	}
	at := func(b *d2target.Diagram, elapsed int, want ...float64) {
		t.Helper()
		wire := b.Connections[0].IconMotionPath(2, 2)
		points := timeline.Sample(b, b, elapsed).Connections[0].IconPositions
		var got []float64
		for _, p := range points {
			got = append(got, (p.X-wire.Points[0].X)/wire.Length())
		}
		slices.Sort(got)
		if len(got) != len(want) {
			t.Fatalf("at %dms got %v, want %v", elapsed, got, want)
		}
		for i := range want {
			if math.Abs(got[i]-want[i]) > 1e-8 {
				t.Fatalf("at %dms got %v, want %v", elapsed, got, want)
			}
		}
	}
	// The old stream's last icon finishes at 100ms, then the single 800ms
	// loop begins. Rerouting in the next board keeps its original clock.
	at(timed, 0, 2./3)
	at(timed, 99, .299/.3)
	at(timed, 100, 0)
	at(resumed, 0, .3/.8)
	at(resumed, 350, .0375/.5, .65/.8)

	// Both kinds of trip finish on the stopped board.
	at(stop, 0, .0875/.5, .7/.8)
	at(stop, 150, .2375/.5)
	at(stop, 413)
	s := timeline.ScenePicture(stop, Frame{Diagram: stop, DurationMS: 400}).Connections[0].IconSchedule
	if len(s.Carried) != 2 || s.Carried[0].Transit != .8 || s.Carried[1].Transit != .5 {
		t.Fatalf("in-flight trips lost their launch timing: %+v", s)
	}
}

func TestTimedLoopStartsOnIdleWire(t *testing.T) {
	stream, timed := iconTestBoard(.125), iconTestBoard(0)
	timed.Connections[0].IconAnimation = &d2target.ConnectionIconAnimation{DurationMS: 200}
	stream.Steps = []*d2target.Diagram{timed}
	timeline, err := NewIconTimeline(stream, 3000)
	if err != nil {
		t.Fatal(err)
	}
	s := timeline.ScenePicture(timed, Frame{Diagram: timed, DurationMS: 3000}).Connections[0].IconSchedule
	if len(s.Carried) != 0 || s.First != 0 || s.Period != .2 {
		t.Fatalf("duration-only board should immediately start looping: %+v", s)
	}
}

func TestTimedSingleIconCompletesBeforeRestarting(t *testing.T) {
	root, faster, stillFast := iconTestBoard(1), iconTestBoard(1), iconTestBoard(1)
	for _, b := range []*d2target.Diagram{root, faster, stillFast} {
		b.Connections[0].IconAnimation.Rate = nil
		b.Connections[0].IconAnimation.DurationMS = 200
	}
	root.Connections[0].IconAnimation.DurationMS = 800
	root.Steps = []*d2target.Diagram{faster, stillFast}
	timeline, err := NewIconTimeline(root, 350)
	if err != nil {
		t.Fatal(err)
	}
	// The 800ms trip spans both 350ms boundaries. It reaches its destination
	// 100ms into the third board, where the 200ms loop begins.
	wire := stillFast.Connections[0].IconMotionPath(2, 2)
	for elapsed, want := range map[int]float64{0: .875, 99: .799 / .8, 100: 0, 150: .25, 299: .995, 300: 0} {
		points := timeline.Sample(stillFast, stillFast, elapsed).Connections[0].IconPositions
		if len(points) != 1 || math.Abs(points[0].X-wire.PointAt(want*wire.Length()).X) > 1e-7 {
			t.Errorf("at %dms got %v, want one icon at %.3f", elapsed, points, want)
		}
	}
}

func TestTimedStreamHandoffKeepsCarriedIconsAtCapacity(t *testing.T) {
	root, burst := iconTestBoard(0), iconTestBoard(1000)
	root.Connections[0].IconAnimation = &d2target.ConnectionIconAnimation{DurationMS: 1800}
	// At default speed the new wire takes 64ms to cross, so 1000/s
	// fills all 64 slots. The timed icon still has 800ms left.
	wire := &burst.Connections[0]
	insets := wire.EndpointInsets(burst.Shapes[0].RenderedStrokeWidth(), burst.Shapes[1].RenderedStrokeWidth())
	wire.Route[1].X = wire.Route[0].X + .064*d2target.IconMotionSpeed + insets[0] + insets[1]
	root.Steps = []*d2target.Diagram{burst}
	timeline, err := NewIconTimeline(root, 1000)
	if err != nil {
		t.Fatal(err)
	}
	s := timeline.ScenePicture(burst, Frame{Diagram: burst, DurationMS: 1000}).Connections[0].IconSchedule
	if len(s.Carried) != 1 || math.Abs(s.First-.737) > 1e-10 || math.Abs(s.Period-.001) > 1e-10 {
		t.Fatalf("new departures weren't admitted at the earliest safe time: %+v", s)
	}
	for ms := range 1001 {
		at := float64(ms) / 1000
		// These are the tracks the fixed SVG and native renderers receive.
		visible := 0
		for _, trip := range s.Carried {
			if at+1e-10 < (1-trip.Progress)*trip.Transit {
				visible++
			}
		}
		for launch := 0; launch < 64; launch++ {
			if at+1e-10 >= s.First+float64(launch)*s.Period {
				visible++
			}
		}
		sampled := len(timeline.Sample(burst, burst, ms).Connections[0].IconPositions)
		if visible != sampled || visible > d2target.MaxMovingIconsPerConnection {
			t.Fatalf("at %dms scheduled %d copies, sample has %d", ms, visible, sampled)
		}
	}
}

func TestSlowIconRateKeepsDeparturePhase(t *testing.T) {
	root, step := iconTestBoard(0.000000001), iconTestBoard(0.000000001)
	root.Steps = []*d2target.Diagram{step}
	timeline, err := NewIconTimeline(root, 10)
	if err != nil {
		t.Fatal(err)
	}
	s := timeline.ScenePicture(step, Frame{Diagram: step, DurationMS: 10}).Connections[0].IconSchedule
	if len(s.Carried) != 1 || math.Abs(s.First-(s.Period-.01)) > 1e-6 {
		t.Fatalf("10ms after the initial launch, the next departure should be almost a full period away: %+v", s)
	}
}

func TestFullIconStreamWaitsForSlotAfterStop(t *testing.T) {
	root, stop, resume := iconTestBoard(100), iconTestBoard(0), iconTestBoard(100)
	for _, b := range []*d2target.Diagram{root, stop, resume} {
		wire := &b.Connections[0]
		insets := wire.EndpointInsets(b.Shapes[0].RenderedStrokeWidth(), b.Shapes[1].RenderedStrokeWidth())
		wire.Route[1].X = wire.Route[0].X + .64*d2target.IconMotionSpeed + insets[0] + insets[1]
	}
	root.Steps = []*d2target.Diagram{stop, resume}
	timeline, err := NewIconTimeline(root, 1)
	if err != nil {
		t.Fatal(err)
	}
	s := timeline.ScenePicture(resume, Frame{Diagram: resume, DurationMS: 1}).Connections[0].IconSchedule
	if len(s.Carried) != d2target.MaxMovingIconsPerConnection || math.Abs(s.First-.008) > 1e-10 {
		t.Fatalf("the full wire should wait for its first arrival in 8ms: carried=%d, first=%g", len(s.Carried), s.First)
	}
}
