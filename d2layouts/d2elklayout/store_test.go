package d2elklayout

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

// TestFormattingMatchesSprintf checks the strconv formatting that replaced
// fmt.Sprintf in the request builder.
func TestFormattingMatchesSprintf(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	floats := []float64{0, math.Copysign(0, -1), 1, -1, 0.5, 1e-7, 1e21, 123456789, 1e6, 999999, math.NaN(),
		math.Inf(1), math.Inf(-1), math.MaxFloat64, 5e-324}
	for i := 0; i < 20000; i++ {
		floats = append(floats, math.Float64frombits(rng.Uint64()), float64(rng.Intn(2000)), rng.NormFloat64()*1e4)
	}
	for i := 0; i+3 < len(floats); i++ {
		p := shapePadding{top: floats[i], left: floats[i+1], bottom: floats[i+2], right: floats[i+3]}
		want := fmt.Sprintf("[top=%g,left=%g,bottom=%g,right=%g]", p.top, p.left, p.bottom, p.right)
		if got := p.String(); got != want {
			t.Fatalf("padding %q, want %q", got, want)
		}
	}
	// The last is 1<<40 where int has 64 bits (written so that the test also
	// compiles where it has 32, as for TinyGo's wasm).
	ints := []int{0, 1, -1, 7, 100, -100, math.MaxInt, math.MinInt, math.MaxInt>>23 + 1}
	for i := 0; i < 2000; i++ {
		ints = append(ints, rng.Intn(1<<20)-1<<19)
	}
	for i := 0; i+1 < len(ints); i++ {
		if got, want := nodeSizeMinimum(ints[i], ints[i+1]), fmt.Sprintf("(%d, %d)", ints[i], ints[i+1]); got != want {
			t.Fatalf("minimum %q, want %q", got, want)
		}
	}
	for _, column := range []string{"", "id", "a.b", "%s", "é"} {
		if got, want := portID("x.y", column, ".src"), fmt.Sprintf("%s.%s.src", "x.y", column); got != want {
			t.Fatalf("port %q, want %q", got, want)
		}
		if got, want := portID("t", column, ".dst")+"."+fmt.Sprint(12), fmt.Sprintf("%s.%d", fmt.Sprintf("%s.%s.dst", "t", column), 12); got != want {
			t.Fatalf("root port %q, want %q", got, want)
		}
	}
}
