package e2etests

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// The E2E, CLI, sketch and dark-theme suites already render and verify these
// exact files. Check their combined raw size so accepting new goldens cannot
// silently undo reductions in the SVG output. After reducing output size,
// set the budget to the new total plus about 1%.
func TestSVGSizeBudget(t *testing.T) {
	const budgetPath = "../ci/svg-size-budget.txt"
	data, err := os.ReadFile(budgetPath)
	if err != nil {
		t.Fatal(err)
	}
	limit, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || limit <= 0 {
		t.Fatalf("%s: expected a positive byte count, got %q", budgetPath, data)
	}

	var total int64
	for _, path := range []string{
		"testdata/sanity/basic/dagre/sketch.exp.svg",
		"testdata/sanity/child_to_child/dagre/sketch.exp.svg",
		"testdata/sanity/connection_label/dagre/sketch.exp.svg",
		"testdata/stable/sql_tables/dagre/sketch.exp.svg",
		"testdata/stable/code_snippet/dagre/sketch.exp.svg",
		"testdata/stable/markdown/dagre/sketch.exp.svg",
		"testdata/stable/sequence_diagram_simple/dagre/sketch.exp.svg",
		"../d2renderers/d2sketch/testdata/basic/sketch.exp.svg",
		"../d2renderers/d2sketch/testdata/child_to_child/sketch.exp.svg",
		"../d2renderers/d2svg/dark_theme/testdata/basic/dark_theme.exp.svg",
		"../d2renderers/d2svg/dark_theme/testdata/code/dark_theme.exp.svg",
		"../e2etests-cli/testdata/TestCLI_E2E/animation.exp.svg",
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		total += info.Size()
		t.Logf("%6d bytes  %s", info.Size(), path)
	}
	t.Logf("total %d bytes, budget %d", total, limit)
	if total > limit {
		t.Fatalf("SVG fixtures exceed %s by %d bytes", budgetPath, total-limit)
	}
}
