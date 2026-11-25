package d2dagrelayout

import (
	"math"
	"runtime"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

// TestPointAlongNaNs checks the NaNs pointAlong returns for NaN coordinates.
// A NaN operand passes its NaN on, so with one NaN in a coordinate the result
// carries it whatever order the compiled instructions take their operands
// in. When both points' coordinates are NaN, the original's arm64 code (the
// inlined Vector methods: q.X-p.X with q.X first, then the scaled difference
// first in the addition) gives q's NaN; pointAlong must too (arm64 only: on
// amd64 the original's addition took p's coordinate first, which pointAlong
// does for X but not for Y).
func TestPointAlongNaNs(t *testing.T) {
	t.Parallel()
	nan := func(payload uint64) float64 { return math.Float64frombits(0x7ff8000000000000 | payload) }
	same := func(a, b float64) bool { return math.Float64bits(a) == math.Float64bits(b) }
	for _, tt := range []float64{.2, .5, .8} {
		p := &geo.Point{X: nan(1), Y: nan(2)}
		q := &geo.Point{X: 10, Y: 20}
		if r := pointAlong(p, q, tt); !same(r.X, p.X) || !same(r.Y, p.Y) {
			t.Errorf("NaN p, t %v: %x %x", tt, math.Float64bits(r.X), math.Float64bits(r.Y))
		}
		p, q = &geo.Point{X: 10, Y: 20}, &geo.Point{X: nan(3), Y: nan(4)}
		if r := pointAlong(p, q, tt); !same(r.X, q.X) || !same(r.Y, q.Y) {
			t.Errorf("NaN q, t %v: %x %x", tt, math.Float64bits(r.X), math.Float64bits(r.Y))
		}
		if runtime.GOARCH != "arm64" {
			continue
		}
		p, q = &geo.Point{X: nan(5), Y: nan(6)}, &geo.Point{X: nan(7), Y: nan(8)}
		if r := pointAlong(p, q, tt); !same(r.X, q.X) || !same(r.Y, q.Y) {
			t.Errorf("NaN p and q, t %v: %x %x, the original gives q's NaNs %x %x", tt,
				math.Float64bits(r.X), math.Float64bits(r.Y), math.Float64bits(q.X), math.Float64bits(q.Y))
		}
	}
}
