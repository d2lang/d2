package d2grid

import (
	"math"
	"math/bits"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestGridPartitionMinimumDeviation(t *testing.T) {
	rng := rand.New(rand.NewPCG(2026, 1125))
	for sample := 0; sample < 2000; sample++ {
		n := 2 + rng.IntN(11)
		tracks := 1 + rng.IntN(n)
		gd := benchmarkGrid(n, tracks)
		gd.columns = tracks
		gd.horizontalGap, gd.verticalGap = rng.IntN(41), rng.IntN(41)
		columns := sample%2 == 0
		unit := 1
		if sample%4 >= 2 {
			unit = 4 // Include fractional dimensions from nested diagrams.
		}
		gap := gd.horizontalGap * unit
		if columns {
			gap = gd.verticalGap * unit
		}
		total := gap * (n - tracks)
		sizes := make([]int, n)
		for i, obj := range gd.objects {
			sizes[i] = rng.IntN(240)
			total += sizes[i]
			if columns {
				obj.Height = float64(sizes[i]) / float64(unit)
			} else {
				obj.Width = float64(sizes[i]) / float64(unit)
			}
		}

		// Enumerate cut masks independently of the production traversal. Score
		// in integer units so ties at a fractional target stay exact.
		minimum := math.MaxInt
		for mask := 0; mask < 1<<(n-1); mask++ {
			if bits.OnesCount(uint(mask)) != tracks-1 {
				continue
			}
			size, cost := 0, 0
			for i, v := range sizes {
				size += v
				if i == n-1 || mask&(1<<i) != 0 {
					delta := size*tracks - total
					cost += max(delta, -delta)
					size = 0
				} else {
					size += gap
				}
			}
			minimum = min(minimum, cost)
		}

		target := gridTarget(gd, columns)
		got := gd.getBestLayout(target, columns)
		if len(got) != tracks {
			t.Fatalf("got %d tracks, want %d", len(got), tracks)
		}
		index := 0
		for _, row := range got {
			if len(row) == 0 || index+len(row) > n || !slices.Equal(row, gd.objects[index:index+len(row)]) {
				t.Fatalf("empty or out-of-order track at %d", index)
			}
			index += len(row)
		}
		dist := getDistToTarget(got, target, float64(gd.horizontalGap), float64(gd.verticalGap), columns)
		if index != n || int(math.Round(dist*float64(unit*tracks))) != minimum {
			t.Fatalf("sizes %v, gap %d, tracks %d: got %v, want %v", sizes, gap, tracks, dist, float64(minimum)/float64(unit*tracks))
		}
	}
}

func TestGridPartitionBoundsAndTies(t *testing.T) {
	large := make([]float64, 4000)
	large[0] = 1 << 52 // Scaled costs could overflow even though the prefix sum is exact.
	for _, tc := range []struct {
		sizes  []float64
		gap    float64
		tracks int
		want   []int
	}{
		{[]float64{40, 60, 140, 40, 100}, 40, 4, []int{1, 2, 3}},
		{[]float64{20, 20, 20}, 0, 2, []int{1}}, // Both cuts tie; prefer the later one.
		{[]float64{0, 0, 0, 0}, 0, 3, []int{1, 2}},
		{[]float64{1, 1.25, 1}, 0, 2, nil},
		{large, 0, 3999, nil},
		{make([]float64, 3000), 0, 1500, nil}, // Bound the recovery table.
	} {
		if got := newGridRowMeasurements(tc.sizes, tc.gap).integralCuts(tc.tracks); !slices.Equal(got, tc.want) {
			t.Fatalf("%d items / %d tracks: got cuts %v, want %v", len(tc.sizes), tc.tracks, got, tc.want)
		}
	}
}
