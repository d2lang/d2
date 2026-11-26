//go:build !tinygo && !xsort_small

// Package xsort is the sorter of D2's own code: Func, StableFunc and Ordered
// are slices.SortFunc, slices.SortStableFunc and slices.Sort.
//
// In this, the default build, they call those functions directly, so native
// programs keep the specialized, reflection-free sorters. In WebAssembly
// builds (build tag tinygo; xsort_small selects the same code natively for
// tests) they run the non-generic sorters of package sort instead, so that
// the module carries one copy of each algorithm rather than one per element
// type. Both packages generate their sorters from one template
// (sort/gen_sort_variants.go), so the two variants make the same comparisons
// and swaps and leave the same order, ties included. xsort_small_test.go
// checks that.
package xsort

import (
	"cmp"
	"slices"
)

// Func is slices.SortFunc.
func Func[S ~[]E, E any](s S, compare func(a, b E) int) {
	slices.SortFunc(s, compare)
}

// StableFunc is slices.SortStableFunc.
func StableFunc[S ~[]E, E any](s S, compare func(a, b E) int) {
	slices.SortStableFunc(s, compare)
}

// Ordered is slices.Sort.
func Ordered[S ~[]E, E cmp.Ordered](s S) {
	slices.Sort(s)
}
