//go:build !race

package d2talalayout_test

import "testing"

// The 230-edge stress case measures work budgets. Running its placement search
// under the race detector takes several minutes on its own.
func TestLargeNestedSketchRouting(t *testing.T) {
	checkLargeContainerRouting(t, 230, 168, true)
}
