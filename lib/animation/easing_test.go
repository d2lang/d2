package animation

import (
	"math"
	"testing"
)

func TestEaseInOut(t *testing.T) {
	if got := EaseInOut(.5); got != .5 {
		t.Fatalf("midpoint = %.17g, want exactly 0.5", got)
	}
	for _, sample := range []struct{ time, want float64 }{
		{0, 0}, {.25, .129161931}, {.5, .5}, {.75, .870838069}, {1, 1},
	} {
		got := EaseInOut(sample.time)
		if math.Abs(got-sample.want) > 1e-8 {
			t.Errorf("EaseInOut(%g) = %g, want %g", sample.time, got, sample.want)
		}
	}
}
