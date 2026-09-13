package d2grid

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"
)

func assertRowMeasurement(t *testing.T, measurements *gridRowMeasurements, start, end int) {
	t.Helper()
	got := measurements.get(start, end)
	var size, withGap float64
	for _, value := range measurements.sizes[start:end] {
		size += value
		withGap += value + measurements.gap
	}
	if start < end {
		withGap -= measurements.gap
	}
	if got.start != start || got.end != end || math.Float64bits(got.size) != math.Float64bits(size) || math.Float64bits(got.withGap) != math.Float64bits(withGap) {
		t.Fatalf("row [%d:%d], gap %v: %+v, want %v/%v", start, end, measurements.gap, got, size, withGap)
	}
}

func TestGridIntegralMeasurementsMatchAdditionOrder(t *testing.T) {
	rng := rand.New(rand.NewPCG(85, 19))
	for fixture := 0; fixture < 100; fixture++ {
		sizes := make([]float64, 2+rng.IntN(80))
		for i := range sizes {
			sizes[i] = float64(rng.IntN(10000))
		}
		measurements := newGridRowMeasurements(sizes, float64(rng.IntN(1000)))
		// Visit overlapping intervals in a changing order, then cover every
		// interval including the empty and singleton paths.
		for i := 0; i < 100; i++ {
			start := rng.IntN(len(sizes))
			assertRowMeasurement(t, measurements, start, start+rng.IntN(len(sizes)-start+1))
		}
		for start := range sizes {
			for end := start; end <= len(sizes); end++ {
				assertRowMeasurement(t, measurements, start, end)
			}
		}
		if len(measurements.integralPrefix) != len(sizes)+1 || measurements.cache != nil {
			t.Fatal("integral rows did not use the prefix sums exclusively")
		}
	}
}

func TestGridIntegralMeasurementBounds(t *testing.T) {
	const limit = float64(1 << 53)
	for _, tc := range []struct {
		name   string
		sizes  []float64
		gap    float64
		prefix bool
	}{
		{"zeros", []float64{0, math.Copysign(0, -1), 0}, 0, true},
		{"exact_limit", []float64{limit - 2, 1, 1}, 0, true},
		{"exact_limit_with_gaps", []float64{limit - 5, 1, 1}, 1, true},
		{"sum_over_limit", []float64{limit, 1, 0}, 0, false},
		{"gap_sum_over_limit", []float64{limit - 2, 0, 0}, 1, false},
		{"size_over_limit", []float64{limit * 2, 1}, 0, false},
		{"gap_over_limit", []float64{1, 1}, limit * 2, false},
		{"fractional_size", []float64{0.5, 0.25, 100}, 40, false},
		{"fractional_gap", []float64{10, 20, 30}, 0.1, false},
		{"negative_size", []float64{-1, 2, 3}, 40, false},
		{"negative_gap", []float64{10, 20, 30}, -1, false},
		{"infinite_size", []float64{math.Inf(1), 0}, 0, false},
		{"infinite_gap", []float64{10, 20}, math.Inf(1), false},
		{"nan_size", []float64{math.NaN(), 0}, 0, false},
		{"nan_gap", []float64{10, 20}, math.NaN(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			measurements := newGridRowMeasurements(tc.sizes, tc.gap)
			for start := range tc.sizes {
				for end := start; end <= len(tc.sizes); end++ {
					assertRowMeasurement(t, measurements, start, end)
				}
			}
			if got := measurements.integralPrefix != nil; got != tc.prefix {
				t.Fatalf("prefix enabled = %v, want %v", got, tc.prefix)
			}
		})
	}
}

func TestGridIntegralSingletonMeasurementsStayLazy(t *testing.T) {
	measurements := newGridRowMeasurements(make([]float64, 100000), 40)
	for _, end := range []int{0, 1} {
		assertRowMeasurement(t, measurements, 0, end)
	}
	if measurements.prefixChecked || measurements.integralPrefix != nil || measurements.cache != nil {
		t.Fatal("empty and singleton rows initialized a measurement cache")
	}
	assertRowMeasurement(t, measurements, 0, 2)
	if len(measurements.integralPrefix) != len(measurements.sizes)+1 || measurements.cache != nil {
		t.Fatal("nontrivial integral rows did not allocate only a linear prefix array")
	}
}

func TestGridIntegralSearchMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(23, 94))
	for fixture := 0; fixture < 100; fixture++ {
		n := 2 + rng.IntN(16)
		gd := benchmarkGrid(n, 1+rng.IntN(n))
		gd.columns = 1 + rng.IntN(n)
		gd.horizontalGap = rng.IntN(80)
		gd.verticalGap = rng.IntN(80)
		for _, object := range gd.objects {
			object.Width = float64(1 + rng.IntN(500))
			object.Height = float64(1 + rng.IntN(500))
		}
		for _, columns := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/columns=%v", fixture, columns), func(t *testing.T) {
				assertSearchMatchesReference(t, gd, columns)
			})
		}
	}
}

func BenchmarkGridIntegralMeasurements(b *testing.B) {
	sizes := make([]float64, 3001)
	for i := range sizes {
		sizes[i] = float64(80 + i%37)
	}
	for _, prefix := range []bool{false, true} {
		b.Run(fmt.Sprintf("prefix=%v", prefix), func(b *testing.B) {
			measurements := newGridRowMeasurements(sizes, 40)
			if !prefix {
				measurements.prefixChecked = true // Exercise the unchanged bounded-cache path.
			}
			measurements.get(0, len(sizes))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				start := (i * 977) % (len(sizes) - 1)
				end := start + 2 + (i*1597)%(len(sizes)-start-1)
				measurements.get(start, end)
			}
		})
	}
}
