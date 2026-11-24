package d2lib

import (
	"context"
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
	if len(first.TweenFrames) != 6 || len(second.TweenFrames) != 6 || first.TweenDurationMS != 240 || !first.GridMotion || !second.GridMotion {
		t.Fatalf("grid changes did not share the configured clock")
	}
	mid := first.TweenFrames[3]
	if got := shapeByID(t, mid, "a").Fill; got != "#800080" {
		t.Fatalf("midpoint fill = %q", got)
	}
	if p := shapeByID(t, mid, "c").Opacity; p <= 0 || p >= 1 {
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
	if len(d.Steps[0].TweenFrames) != 5 || len(d.Steps[1].TweenFrames) != 5 {
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
