package d2grid

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/lib/geo"
)

func TestRouteBoundsIncludeConnectionIcon(t *testing.T) {
	for _, tc := range []struct {
		artwork string
		bounds  [4]float64
	}{
		{"icon.shape: square", [4]float64{0, -5, 100, 5}},
		{"icon.shape: triangle; icon.style.animated: 2/s", [4]float64{-5, -5, 105, 5}},
	} {
		g, _, err := d2compiler.Compile("grid.d2", strings.NewReader("a -- b: {"+tc.artwork+"}"), nil)
		if err != nil {
			t.Fatal(err)
		}
		e := g.Edges[0]
		e.Route = geo.Route{{X: 0, Y: 0}, {X: 100, Y: 0}}
		left, top, right, bottom := routeBounds(e)
		if got := [4]float64{left, top, right, bottom}; got != tc.bounds {
			t.Errorf("%s: bounds %v, want %v", tc.artwork, got, tc.bounds)
		}
	}
}
