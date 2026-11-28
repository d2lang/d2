package animation

import "testing"

func TestEaseInOut(t *testing.T) {
	for _, sample := range []struct{ time, want float64 }{
		{-1, 0}, {0, 0}, {.25, 1.0 / 16}, {.5, .5}, {.75, 15.0 / 16}, {1, 1}, {2, 1},
	} {
		if got := EaseInOut(sample.time); got != sample.want {
			t.Errorf("EaseInOut(%g) = %g, want %g", sample.time, got, sample.want)
		}
	}
}
