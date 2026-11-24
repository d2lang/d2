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
