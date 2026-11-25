package d2graph

import (
	"math/rand"
	"sort"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

// TestSortPointsMatchesSortSlice checks that sortPoints leaves
// every list in the order sort.Slice leaves it, ties included (compared by
// identity), for lists of 0 to 14 points with few distinct coordinates.
func TestSortPointsMatchesSortSlice(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	for c := 0; c < 200000; c++ {
		n := rng.Intn(15)
		pts := make([]*geo.Point, n)
		for i := range pts {
			pts[i] = &geo.Point{X: float64(rng.Intn(4)), Y: float64(rng.Intn(4))}
		}
		a := append([]*geo.Point(nil), pts...)
		b := append([]*geo.Point(nil), pts...)
		dir := rng.Intn(4)
		lessOf := func(s []*geo.Point) func(i, j int) bool {
			switch dir {
			case 0:
				return func(i, j int) bool { return s[i].Y < s[j].Y }
			case 1:
				return func(i, j int) bool { return s[i].Y > s[j].Y }
			case 2:
				return func(i, j int) bool { return s[i].X < s[j].X }
			}
			return func(i, j int) bool { return s[i].X > s[j].X }
		}
		sort.Slice(a, lessOf(a))
		sortPoints(b, lessOf(b))
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("case %d (n %d, dir %d): position %d differs", c, n, dir, i)
			}
		}
	}
}
