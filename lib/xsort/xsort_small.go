//go:build tinygo || xsort_small

// Package xsort is the sorter of D2's own code: Func, StableFunc and Ordered
// are slices.SortFunc, slices.SortStableFunc and slices.Sort.
//
// This is the variant for WebAssembly builds (build tag tinygo; xsort_small
// selects it natively for tests). It runs the non-generic sorters of package
// sort, so that the module carries one copy of each algorithm rather than one
// per element type. Both packages generate their sorters from one template
// (sort/gen_sort_variants.go), and the comparison is consulted only as
// "less", so this variant makes the same comparisons and swaps as the
// functions of package slices and leaves the same order, ties included.
// xsort_small_test.go checks that. The default build calls package slices
// directly.
package xsort

import (
	"cmp"
	"sort"
)

// Func is slices.SortFunc.
func Func[S ~[]E, E any](s S, compare func(a, b E) int) {
	sort.Slice(s, func(i, j int) bool { return compare(s[i], s[j]) < 0 })
}

// StableFunc is slices.SortStableFunc.
func StableFunc[S ~[]E, E any](s S, compare func(a, b E) int) {
	sort.SliceStable(s, func(i, j int) bool { return compare(s[i], s[j]) < 0 })
}

// Ordered is slices.Sort.
func Ordered[S ~[]E, E cmp.Ordered](s S) {
	sort.Slice(s, func(i, j int) bool { return cmp.Less(s[i], s[j]) })
}
