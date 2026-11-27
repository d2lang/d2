//go:build xsort_small

package xsort

import (
	"cmp"
	"math"
	"math/rand"
	"slices"
	"strconv"
	"testing"
)

type rec struct {
	key int
	id  int
	pad [3]float64
}

// TestSmallVariantMatchesSlices requires the sort.Slice variant, the one of
// WebAssembly builds, to leave every slice exactly as the function of package
// slices that the default build calls: the same element at every index, ties
// included, for unstable and stable sorts alike.
func TestSmallVariantMatchesSlices(t *testing.T) {
	trials := 20000
	if testing.Short() {
		trials = 2000
	}
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < trials; trial++ {
		// Lengths cross the insertion sort (12), stable block (20) and
		// ninther (50) thresholds; a few are long.
		n := rng.Intn(400)
		if trial%500 == 0 {
			n = 1000 + rng.Intn(6000)
		}
		// Few distinct keys: most comparisons are ties.
		keys := 1 + rng.Intn(1+n)
		if trial%3 == 0 {
			keys = 1 + rng.Intn(4)
		}

		// Struct elements, in random, ascending, descending and sawtooth
		// initial orders (the patterns pdqsort detects).
		a := make([]rec, n)
		for i := range a {
			a[i] = rec{key: rng.Intn(keys), id: i}
		}
		byKey := func(x, y rec) int { return cmp.Compare(x.key, y.key) }
		switch trial % 7 {
		case 1:
			slices.SortFunc(a, byKey)
		case 2:
			slices.SortFunc(a, func(x, y rec) int { return cmp.Compare(y.key, x.key) })
		case 3:
			for i := range a {
				a[i].key = i % 7
			}
		}
		by := byKey
		if trial%5 == 0 {
			// The comparison D2's sortSliceExact helpers build from a
			// "less": never zero, so not symmetric on ties.
			by = func(x, y rec) int {
				if x.key < y.key {
					return -1
				}
				return 1
			}
		}
		b, c, d := slices.Clone(a), slices.Clone(a), slices.Clone(a)
		slices.SortFunc(a, by)
		Func(b, by)
		if !slices.Equal(a, b) {
			t.Fatalf("struct: Func differs from slices.SortFunc at n=%d trial=%d", n, trial)
		}
		slices.SortStableFunc(c, by)
		StableFunc(d, by)
		if !slices.Equal(c, d) {
			t.Fatalf("struct: StableFunc differs from slices.SortStableFunc at n=%d trial=%d", n, trial)
		}

		// Pointer elements: identity shows which of two equal keys moved.
		p := make([]*rec, n)
		for i := range p {
			p[i] = &rec{key: rng.Intn(keys), id: i}
		}
		q, r, s := slices.Clone(p), slices.Clone(p), slices.Clone(p)
		byPointer := func(x, y *rec) int { return cmp.Compare(x.key, y.key) }
		slices.SortFunc(p, byPointer)
		Func(q, byPointer)
		if !slices.Equal(p, q) {
			t.Fatalf("pointer: Func differs from slices.SortFunc at n=%d trial=%d", n, trial)
		}
		slices.SortStableFunc(r, byPointer)
		StableFunc(s, byPointer)
		if !slices.Equal(r, s) {
			t.Fatalf("pointer: StableFunc differs from slices.SortStableFunc at n=%d trial=%d", n, trial)
		}

		// Strings: Ordered, and Func on a key that ties distinct strings.
		str := make([]string, n)
		for i := range str {
			str[i] = strconv.Itoa(rng.Intn(keys)) + "/" + strconv.Itoa(rng.Intn(3))
		}
		str2, str3, str4 := slices.Clone(str), slices.Clone(str), slices.Clone(str)
		slices.Sort(str)
		Ordered(str2)
		if !slices.Equal(str, str2) {
			t.Fatalf("string: Ordered differs from slices.Sort at n=%d trial=%d", n, trial)
		}
		byLength := func(x, y string) int { return cmp.Compare(len(x), len(y)) }
		slices.SortFunc(str3, byLength)
		Func(str4, byLength)
		if !slices.Equal(str3, str4) {
			t.Fatalf("string: Func differs from slices.SortFunc at n=%d trial=%d", n, trial)
		}

		// float64 with NaN (sorts first) and both zeros (equal, so their
		// relative order shows the swaps); compared bit by bit.
		f := make([]float64, n)
		for i := range f {
			switch rng.Intn(12) {
			case 0:
				f[i] = math.NaN()
			case 1:
				f[i] = math.Copysign(0, -1)
			case 2:
				f[i] = 0
			default:
				f[i] = float64(rng.Intn(keys))
			}
		}
		g, h, k := slices.Clone(f), slices.Clone(f), slices.Clone(f)
		slices.Sort(f)
		Ordered(g)
		slices.SortFunc(h, cmp.Compare[float64])
		Func(k, cmp.Compare[float64])
		for i := range f {
			if math.Float64bits(f[i]) != math.Float64bits(g[i]) {
				t.Fatalf("float64: Ordered differs from slices.Sort at n=%d trial=%d index=%d", n, trial, i)
			}
			if math.Float64bits(h[i]) != math.Float64bits(k[i]) {
				t.Fatalf("float64: Func differs from slices.SortFunc at n=%d trial=%d index=%d", n, trial, i)
			}
		}
	}
}
