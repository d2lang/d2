package xsort

import (
	"cmp"
	"math/rand"
	"slices"
	"testing"
)

// TestSorts checks the contract of both variants: Func and Ordered sort, and
// StableFunc sorts keeping equal elements in their original order.
func TestSorts(t *testing.T) {
	type rec struct{ key, id int }
	byKey := func(a, b rec) int { return cmp.Compare(a.key, b.key) }
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 300; trial++ {
		n := rng.Intn(300)
		recs := make([]rec, n)
		ints := make([]int, n)
		for i := range recs {
			recs[i] = rec{key: rng.Intn(1 + n/4), id: i}
			ints[i] = rng.Intn(1 + n/2)
		}
		unstable := slices.Clone(recs)
		Func(unstable, byKey)
		if !slices.IsSortedFunc(unstable, byKey) {
			t.Fatalf("Func left n=%d unsorted", n)
		}
		StableFunc(recs, byKey)
		if !slices.IsSortedFunc(recs, func(a, b rec) int {
			return cmp.Or(cmp.Compare(a.key, b.key), cmp.Compare(a.id, b.id))
		}) {
			t.Fatalf("StableFunc is not a stable sort at n=%d", n)
		}
		Ordered(ints)
		if !slices.IsSorted(ints) {
			t.Fatalf("Ordered left n=%d unsorted", n)
		}
	}
}
