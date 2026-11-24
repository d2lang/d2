package d2lib

import (
	"context"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2target"
	d2log "github.com/d2lang/d2/lib/log"
)

func TestGridStepTimingAndComposition(t *testing.T) {
	const input = `vars: {d2-config: {animate-interval: 350ms; animate-duration: 240ms}}
grid-columns: 2
a: Alpha {style.fill: red}
b: Beta
steps: {
  arrive: {a.style.fill: blue; c: Gamma}
  retire: {b: null}
  hold: {}
}
layers: {separate: {grid-columns: 1; a; b; steps: {expand: {c}}}}
`
	compile := func(opts *CompileOptions) *d2target.Diagram {
		t.Helper()
		d, _, err := Compile(d2log.WithDefault(context.Background()), input, opts, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := d2target.ValidateRenderTarget(d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	calls := 0
	opts := dagreCompileOptions(&calls)
	d := compile(opts)
	first, second := d.Steps[0], d.Steps[1]
	if len(first.TweenFrames) < 6 || len(second.TweenFrames) < 6 || first.TweenDurationMS != 240 || !first.GridMotion || !second.GridMotion {
		t.Fatalf("grid changes did not share the configured clock")
	}
	midIndex := slices.Index(first.TweenFrameEndsMS, 120)
	if midIndex < 0 {
		t.Fatal("lost the original paint tween midpoint at 120ms")
	}
	mid := first.TweenFrames[midIndex+1]
	if got := shapeByID(t, mid, "a").Fill; got != "#800080" {
		t.Fatalf("midpoint fill = %q", got)
	}
	lateIndex := slices.Index(first.TweenFrameEndsMS, 160)
	if lateIndex < 0 {
		t.Fatal("lost the original paint sample at 160ms")
	}
	if p := shapeByID(t, first.TweenFrames[lateIndex+1], "c").Opacity; p <= 0 || p >= 1 {
		t.Fatalf("incoming opacity = %v", p)
	}
	if got := shapeByID(t, second.TweenFrames[0], "c").Pos; got != shapeByID(t, first, "c").Pos {
		t.Fatalf("second step started c at %v", got)
	}
	if len(d.Steps[2].TweenFrames) != 0 || len(d.Layers[0].TweenFrames) != 0 || !d.Layers[0].Steps[0].GridMotion {
		t.Fatal("grid motion should follow step ancestry and skip a hold")
	}

	disabled := false
	opts = dagreCompileOptions(&calls)
	opts.AnimateTweens = &disabled
	static := compile(opts)
	if len(static.Steps[0].TweenFrames) != 0 || static.Steps[0].GridMotion {
		t.Fatal("static compile animated the grid")
	}
	opts = dagreCompileOptions(&calls)
	zero := int64(0)
	opts.AnimateDuration = &zero
	if len(compile(opts).Steps[0].TweenFrames) != 0 {
		t.Fatal("explicit zero did not override source duration")
	}
}

func TestGridPopUsesAuthoredSlotDuringConcurrentResize(t *testing.T) {
	board, _ := compileGridMotionTest(t, `vars: {d2-config: {animate-interval: 1200ms; animate-duration: 900ms}}
grid-rows: 1
grid-columns: 3
grid-gap: 10
a: A {width: 80; height: 46}
b: B {width: 80; height: 46}
steps: {expand: {a.width: 380; c: C {width: 80; height: 46}}}`)
	next := board.Steps[0]
	target := shapeByID(t, next, "c")
	centerX := 2*target.Pos.X + target.Width
	grew := false
	for i, frame := range next.TweenFrames {
		for _, cell := range frame.Shapes {
			if cell.ID != target.ID {
				continue
			}
			// Width and x are independently rounded to whole pixels.
			delta := 2*cell.Pos.X + cell.Width - centerX
			if delta < -1 || delta > 1 {
				t.Fatalf("frame %d: incoming cell left its slot by %.1fpx", i, float64(delta)/2)
			}
			grew = grew || cell.Width < target.Width
		}
	}
	if !grew {
		t.Fatal("expected an intermediate arrival")
	}
}

func TestGridPopScalesFractionalStrokeWidth(t *testing.T) {
	board, graph := compileGridMotionTest(t, `vars: {n: 1; d2-config: {animate-interval: 200ms; animate-duration: 160ms}}
grid-columns: 1
a: {style.stroke-width: ${n}}
steps: {grow: {vars: {n: 2}; b: {style.stroke-width: ${n}}}}
`)
	step := board.Steps[0]
	index := slices.Index(step.TweenFrameEndsMS, 80)
	if index < 0 {
		t.Fatal("missing midpoint in grid step")
	}
	sample := step.TweenFrames[index+1]
	if got := shapeByID(t, sample, "a").RenderedStrokeWidth(); got != 1.5 {
		t.Fatalf("existing cell has width %v, want 1.5", got)
	}
	motion := newGridMotion(board, step, graph, graph.Steps[0])
	start, end, _ := motion.popWindows(160)
	scale, _ := gridPop(gridPhase(80, float64(start), float64(end)))
	if got := shapeByID(t, sample, "b").RenderedStrokeWidth(); got != 1.5*scale {
		t.Errorf("entering cell has width %v, want %v", got, 1.5*scale)
	}
}

func TestGridStepCanStartAndFinishEmpty(t *testing.T) {
	const input = `vars: {d2-config: {animate-interval: 300ms; animate-duration: 200ms}}
grid-columns: 2
steps: {
  attach: { worker: Worker }
  drain: { worker: null }
}`
	calls := 0
	d, _, err := Compile(d2log.WithDefault(context.Background()), input, dagreCompileOptions(&calls), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.IsFolderOnly || d.Steps[1].IsFolderOnly {
		t.Fatal("empty grid endpoint skipped in animated export")
	}
	if len(d.Steps[0].TweenFrames) < 8 || len(d.Steps[1].TweenFrames) < 8 {
		t.Fatal("missing animation to/from empty")
	}
	for _, step := range d.Steps {
		translucent := false
		for _, frame := range step.TweenFrames {
			for _, s := range frame.Shapes {
				translucent = translucent || (s.ID == "worker" && s.Opacity > 0 && s.Opacity < 1)
			}
		}
		if !translucent {
			t.Fatalf("step %s has no partially visible worker", step.Name)
		}
	}
	if len(d.Steps[1].Shapes) != 0 {
		t.Fatal("final grid should be empty")
	}
}

func TestGridSampleTimes(t *testing.T) {
	for _, duration := range []int{1, 2, 13, 161, 1050, 4830, d2target.MaxTweenDurationMS} {
		for _, paint := range []int{0, d2target.TweenFrameCount(duration)} {
			for _, m := range []gridMotion{{moves: true}, {hasEnter: true}, {hasExit: true}, {hasEnter: true, hasExit: true}, {moves: true, hasEnter: true, hasExit: true}} {
				bounds := m.sampleBoundaries(duration, paint)
				times := bounds[:len(bounds)-1]
				if len(times) == 0 || times[0] != 0 || len(times) > d2target.MaxTweenFrames {
					t.Fatalf("invalid sample schedule for duration %d: %v", duration, times)
				}
				enterStart, enterEnd, exitEnd := m.popWindows(duration)
				if !m.moves && paint == 0 && bounds[len(bounds)-1] != max(enterEnd, exitEnd) {
					t.Fatal("pure membership change should hand off to the endpoint at the end of its gesture")
				}
				for i, start := range times {
					end := bounds[i+1]
					if end <= start || end > duration {
						t.Fatalf("invalid sample boundary %d to %d of %d", start, end, duration)
					}
					if (start < exitEnd || (start >= enterStart && start < enterEnd)) && end-start > 17 {
						t.Fatalf("membership change paused for %dms at %d of %d", end-start, start, duration)
					}
				}
			}
		}
	}
}

func TestGridStepsShareDocumentBudget(t *testing.T) {
	// This small budget exercises the same fallback as a long export without
	// building thousands of frames. A separate paint-only layer already owns
	// three frames. The two grid transitions share the remaining seven.
	const input = `grid-rows: 1
a; b
layers: {colors: {x; steps: {paint: {x.style.fill: red}}}}
steps: {add: {c}; remove: {c: null}}`
	board, g := compileGridMotionTest(t, input)
	paint := board.Layers[0].Steps[0]
	paint.TweenFrames = []*d2target.Diagram{d2target.NewDiagram(), d2target.NewDiagram(), d2target.NewDiagram()}
	paint.TweenDurationMS = 240
	// This grid transition's existing paint frames must be replaced, not
	// counted in addition to the new ones.
	add := board.Steps[0]
	for range 6 {
		frame := *add
		frame.TweenFrames = nil
		add.TweenFrames = append(add.TweenFrames, &frame)
	}
	add.TweenDurationMS = 240
	const limit = 15 // Five authored boards, three paint frames, seven grid frames.
	if err := sampleBoardTransitions(context.Background(), board, g, 240, limit, false); err != nil {
		t.Fatal(err)
	}
	total := 0
	for i, step := range board.Steps {
		if n := len(step.TweenFrames); n < 2 || n > 5 {
			t.Fatalf("unbalanced grid samples: %d", n)
		}
		// The add includes a 240 ms paint tween. A pure removal returns
		// control to the authored endpoint as soon as the 160 ms pop ends.
		wantEnd := []int{240, 160}[i]
		if end := step.TweenFrameEndsMS[len(step.TweenFrames)-1]; end != wantEnd || step.TweenDurationMS != 240 || !step.GridMotion {
			t.Fatalf("%s: last sample ends at %d ms, duration %d ms, grid motion %t", step.Name, end, step.TweenDurationMS, step.GridMotion)
		}
		total += len(step.TweenFrames)
	}
	if total != 7 || len(paint.TweenFrames) != 3 {
		t.Fatalf("got %d grid and %d paint frames", total, len(paint.TweenFrames))
	}
	if err := d2target.ValidateRenderTarget(board); err != nil {
		t.Fatal(err)
	}
}

func TestGridSampleBudgetIncludesGeometry(t *testing.T) {
	plans := []boardTransitionPlan{
		{bounds: []int{0, 10, 20, 30, 40}, cost: 3},
		{bounds: []int{0, 10, 20, 30, 40}, cost: 6},
	}
	if err := fitBoardSamples(plans, 100, 18); err != nil {
		t.Fatal(err)
	}
	for _, p := range plans {
		if !slices.Equal(p.bounds, []int{0, 20, 40}) {
			t.Fatalf("got sample boundaries %v", p.bounds)
		}
	}
	// Even the two starting samples need nine geometry elements.
	if err := fitBoardSamples(plans, 100, 8); err == nil {
		t.Fatal("accepted a schedule whose first samples exceed the geometry limit")
	}
}
