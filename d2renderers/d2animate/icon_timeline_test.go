package d2animate

import (
	"bytes"
	"math"
	"slices"
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
	writeLayerIconClocks(&css, "test", links, layerSlots(nav, frames, links))
	if !bytes.Contains(css.Bytes(), []byte("calc(var(--d2-click-time,0ms) + var(--d2-icon-delay,0s))")) {
		t.Fatal("plain board links must start draining immediately")
	}
}
