package d2lib

import (
	"fmt"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2target"
)

func TestBoardGlide(t *testing.T) {
	if boardGlide(0) != 0 || boardGlide(1) != 1 {
		t.Fatal("glide must meet both endpoints exactly")
	}
	previous := 0.
	for i := 1; i <= 1000; i++ {
		d := boardGlide(float64(i) / 1000)
		if d < previous || d > 1 {
			t.Fatalf("backtracked or overshot at %d: %g -> %g", i, previous, d)
		}
		previous = d
	}
	if boardGlide(.01) > .001 || boardGlide(.5) < .7 || 1-boardGlide(.9) > .002 {
		t.Fatal("expected an eased departure and a longer deceleration")
	}
}

func TestStructuralStepSampling(t *testing.T) {
	for _, tc := range []struct{ ms, count int }{{0, 0}, {1, 1}, {800, 48}, {4500, 270}, {6000, d2target.MaxTweenFrames}} {
		src := fmt.Sprintf("vars: {d2-config: {animate-interval: 1600ms; animate-duration: %dms}}\nqueue -> store\nsteps: {delivery: {store -> ship}}", tc.ms)
		d, _ := compileContinuity(t, src, d2talalayout.ContinueBoard)
		step := d.Steps[0]
		if len(step.TweenFrames) != tc.count || step.TweenDurationMS != tc.ms {
			t.Fatalf("%dms: got %d samples covering %dms", tc.ms, len(step.TweenFrames), step.TweenDurationMS)
		}
		if len(step.TweenFrameEndsMS) != 0 {
			t.Fatal("uniform structural motion should retain its fractional frame boundaries")
		}
	}
}
