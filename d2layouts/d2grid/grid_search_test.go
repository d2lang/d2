package d2grid

import (
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

func TestBoundedGridSearchMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(17, 53))
	for fixture := 0; fixture < 120; fixture++ {
		n := 20 + rng.IntN(4)
		gd := benchmarkGrid(n, 7+rng.IntN(5))
		gd.columns = gd.rows
		gd.horizontalGap = rng.IntN(80)
		gd.verticalGap = rng.IntN(80)
		for _, o := range gd.objects {
			// Fractional sizes are common for nested shapes. Keep the original
			// addition order, including when widths differ by several magnitudes.
			o.Width = (float64(rng.IntN(1000)) + rng.Float64()) / 7
			o.Height = (float64(rng.IntN(1000)) + rng.Float64()) / 3
			if fixture%7 == 0 {
				o.Width *= 1e12
			}
		}
		for _, columns := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/columns=%v", fixture, columns), func(t *testing.T) {
				assertSearchMatchesReference(t, gd, columns)
			})
		}
	}
	// The larger cases exercise the candidate and skipped-partition limits,
	// where changing traversal order would change the chosen layout.
	for _, tc := range []struct{ n, rows int }{{50, 8}, {100, 10}, {400, 20}} {
		if testing.Short() && tc.n == 400 {
			continue
		}
		t.Run(fmt.Sprintf("bounded_%d_%d", tc.n, tc.rows), func(t *testing.T) {
			gd := benchmarkGrid(tc.n, tc.rows)
			gd.objects[0].Width += 0.25 // Exercise the bounded fractional search.
			assertSearchMatchesReference(t, gd, false)
		})
	}
}

func assertSearchMatchesReference(t *testing.T, gd *gridDiagram, columns bool) {
	t.Helper()
	target := gridTarget(gd, columns)
	got := gd.getBestLayout(target, columns)
	want := gd.referenceBestLayout(target, columns)
	if !reflect.DeepEqual(got, want) {
		ids := func(layout [][]*d2graph.Object) [][]string {
			result := make([][]string, len(layout))
			for i, row := range layout {
				for _, o := range row {
					result[i] = append(result[i], o.ID)
				}
			}
			return result
		}
		t.Fatalf("layout = %v, want %v", ids(got), ids(want))
	}
}

func gridTarget(gd *gridDiagram, columns bool) float64 {
	var total float64
	for _, o := range gd.objects {
		if columns {
			total += o.Height
		} else {
			total += o.Width
		}
	}
	if columns {
		return (total + float64(gd.verticalGap*(len(gd.objects)-gd.columns))) / float64(gd.columns)
	}
	return (total + float64(gd.horizontalGap*(len(gd.objects)-gd.rows))) / float64(gd.rows)
}

func TestGridDivisionTraversalMatchesReference(t *testing.T) {
	type event struct {
		start, end int
		starting   bool
		cuts       []int
	}
	for n := 2; n <= 12; n++ {
		objects := make([]*d2graph.Object, n)
		for i := range objects {
			objects[i] = &d2graph.Object{Box: geo.NewBox(geo.NewPoint(0, 0), float64(i), 1)}
		}
		for cuts := 1; cuts < n; cuts++ {
			for _, stop := range []int{1, 5, 1000} {
				run := func(optimized bool) []event {
					events := []event{}
					attempts := 0
					checked := 0
					accept := func(start, end int, starting bool) bool {
						events = append(events, event{start: start, end: end, starting: starting})
						checked++
						return checked%7 != 0 || checked > 100
					}
					callback := func(division []int) bool {
						events = append(events, event{cuts: append([]int(nil), division...)})
						attempts++
						return attempts >= stop
					}
					if optimized {
						iterDivisions(n, cuts, callback, accept)
					} else {
						referenceIterDivisions(objects, cuts, callback, func(row []*d2graph.Object, starting bool) bool {
							start := int(row[0].Width)
							return accept(start, start+len(row), starting)
						})
					}
					return events
				}
				got, want := run(true), run(false)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("different traversal for objects=%d cuts=%d stop=%d", n, cuts, stop)
				}
			}
		}
	}
}

func TestGridRowMeasurementsPreserveAdditionOrder(t *testing.T) {
	rng := rand.New(rand.NewPCG(39, 81))
	sizes := make([]float64, 400)
	for i := range sizes {
		sizes[i] = rng.Float64() * 100
		if i%31 == 0 {
			sizes[i] *= 1e15
		}
	}
	for _, gap := range []float64{0, 0.1, 40, 1000} {
		measurements := newGridRowMeasurements(sizes, gap)
		// Repeated and colliding entries must have exactly the same bits as a
		// fresh left-to-right calculation, independent of previous accesses.
		for iteration := 0; iteration < 10000; iteration++ {
			start := rng.IntN(len(sizes))
			end := start + 1 + rng.IntN(len(sizes)-start)
			got := measurements.get(start, end)
			var size, withGap float64
			for _, v := range sizes[start:end] {
				size += v
				withGap += v + gap
			}
			withGap -= gap
			if math.Float64bits(got.size) != math.Float64bits(size) || math.Float64bits(got.withGap) != math.Float64bits(withGap) {
				t.Fatalf("row [%d:%d] differs: %+v, want %v/%v", start, end, got, size, withGap)
			}
		}
	}
	largeSizes := make([]float64, 100000)
	largeSizes[0] = 0.5 // Fractional measurements retain the bounded cache.
	large := newGridRowMeasurements(largeSizes, 40)
	large.get(0, 2)
	if got := len(large.cache); got != 32*1024 {
		t.Fatalf("large grid cache has %d entries, want 32768", got)
	}
}

func TestGridSingletonMeasurementsDoNotAllocateCache(t *testing.T) {
	values := []float64{0, math.Copysign(0, -1), math.SmallestNonzeroFloat64, 0.1, 99.99, 1e20}
	for _, gap := range []float64{0, 0.1, 40, 1e20} {
		measurements := newGridRowMeasurements(values, gap)
		for start := range values {
			for _, end := range []int{start, start + 1} {
				got := measurements.get(start, end)
				var size, withGap float64
				for _, value := range values[start:end] {
					size += value
					withGap += value + gap
				}
				if start < end {
					withGap -= gap
				}
				if math.Float64bits(got.size) != math.Float64bits(size) || math.Float64bits(got.withGap) != math.Float64bits(withGap) {
					t.Fatalf("row [%d:%d], gap %v differs: %+v, want %v/%v", start, end, gap, got, size, withGap)
				}
			}
		}
		if measurements.cache != nil || measurements.integralPrefix != nil {
			t.Fatal("empty and singleton measurements allocated a cache")
		}
		if allocations := testing.AllocsPerRun(100, func() {
			measurements.get(0, 0)
			measurements.get(0, 1)
		}); allocations != 0 {
			t.Fatalf("empty and singleton measurements allocated %v times", allocations)
		}
		// The first longer row still enables the bounded cache.
		measurements.get(0, 2)
		if measurements.cache == nil {
			t.Fatal("multi-object measurement did not allocate its cache")
		}
	}
	for _, n := range []int{100, 400} {
		gd := benchmarkGrid(n, n)
		gd.columns = n
		assertSearchMatchesReference(t, gd, false)
		assertSearchMatchesReference(t, gd, true)
	}
}

// In a grid with one object much larger than the rest, the rows without it
// stay below the target size until okThreshold has grown for many passes, and
// until then no division passes the size checks. getBestLayout skips those
// passes; it must still choose what the original chose after running them.
func TestGridSearchSkipsRepeatedPasses(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 29))
	for fixture := 0; fixture < 200; fixture++ {
		n := 30 + rng.IntN(30)
		gd := benchmarkGrid(n, 7+rng.IntN(4))
		gd.columns = gd.rows
		if affordablePartitions(n, gd.rows-1) {
			t.Fatalf("fixture %d: %d objects in %d rows get the exhaustive search", fixture, n, gd.rows)
		}
		gd.horizontalGap = rng.IntN(40)
		gd.verticalGap = rng.IntN(40)
		for _, o := range gd.objects {
			// Fractional sizes: the bounded search, not integralCuts.
			o.Width = 10 + 20*rng.Float64()
			o.Height = 10 + 20*rng.Float64()
		}
		outlier := gd.objects[rng.IntN(n)]
		outlier.Width *= float64(10 + rng.IntN(5000))
		outlier.Height *= float64(10 + rng.IntN(5000))
		for _, columns := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/columns=%v", fixture, columns), func(t *testing.T) {
				assertSearchMatchesReference(t, gd, columns)
			})
		}
	}
}

// With one object vastly larger than the rest, the original search never
// finished: okThreshold, growing a quarter per pass, would need more passes
// than finish in any reasonable time, or never reach a value at which the
// size checks accept a division. getBestLayout then scores divisions without
// the checks.
func TestGridSearchFinishesWithOutlier(t *testing.T) {
	for _, size := range []float64{1e12, 1e17, math.MaxInt64} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			gd := benchmarkGrid(50, 7)
			gd.objects[0].Width = size
			done := make(chan [][]*d2graph.Object, 1)
			go func() {
				done <- gd.getBestLayout(gridTarget(gd, false), false)
			}()
			var layout [][]*d2graph.Object
			select {
			case layout = <-done:
			case <-time.After(30 * time.Second):
				t.Fatal("getBestLayout did not finish")
			}
			if len(layout) != gd.rows {
				t.Fatalf("%d rows, want %d", len(layout), gd.rows)
			}
			next := 0
			for _, row := range layout {
				if len(row) == 0 {
					t.Fatal("empty row")
				}
				for _, o := range row {
					if o != gd.objects[next] {
						t.Fatalf("object %s in place %d, want %s", o.ID, next, gd.objects[next].ID)
					}
					next++
				}
			}
			if next != len(gd.objects) {
				t.Fatalf("%d objects laid out, want %d", next, len(gd.objects))
			}
		})
	}
}
