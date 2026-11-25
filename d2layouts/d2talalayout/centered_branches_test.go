package d2talalayout_test

import (
	"math"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2target"
)

func TestCenteredWorkflowBranches(t *testing.T) {
	for _, tc := range []struct {
		name, source, fork, short, join string
	}{
		{"nested", "submit -> review -> approve\nsubmit -> build\nbuild -> linux -> verify\nbuild -> mac -> verify\nverify -> approve", "submit", "review", "approve"},
		{"serial", "request -> stock -> reserve -> confirm\nrequest -> funds -> authorize -> confirm\nrequest -> fraud -> confirm\nconfirm -> pack -> ship -> done\nconfirm -> invoice -> receipt -> done", "request", "fraud", "confirm"},
		{"reversed declarations", "approve <- review <- submit\nbuild <- submit\nverify <- linux <- build\nverify <- mac <- build\napprove <- verify", "submit", "review", "approve"},
	} {
		for _, direction := range []string{"down", "up", "left", "right"} {
			t.Run(tc.name+"/"+direction, func(t *testing.T) {
				diagram := compileRoutingDiagram(t, t.Context(), "direction: "+direction+"\n"+tc.source, 1, nil)
				first, second := workflowGaps(t, diagram, direction, tc.fork, tc.short, tc.join)
				if first <= 0 || math.Abs(float64(first-second)) > 1 {
					t.Fatalf("branch gaps = %d and %d, want equal clear space", first, second)
				}
				if got := fanRouteCrossings(diagram.Connections); got != 0 {
					t.Fatalf("centered branches have %d crossings", got)
				}
				for _, edge := range diagram.Connections {
					for i := 1; i < len(edge.Route); i++ {
						a, b := edge.Route[i-1], edge.Route[i]
						if a.X != b.X && a.Y != b.Y || a.Equals(b) {
							t.Fatalf("edge %s has invalid orthogonal segment %v to %v", edge.ID, a, b)
						}
					}
				}
			})
		}
	}
}

func TestCenteredBranchesKeepAuthoredRanksAndLabels(t *testing.T) {
	const source = "direction: down\nA -> short -> Z\nA -> one -> two -> three -> Z"
	for _, tc := range []struct{ name, code string }{
		{"explicit hierarchy", "shape: hierarchy\n" + source},
		{"edge caption", strings.Replace(source, "A -> one", "A -> one: build\none", 1)},
		{"authored canvas", "width: 1450\n" + source},
		{"edge badge", source + "\n(A -> one)[0].tooltip: documentation"},
		{"outside text", source + "\nshort.label.near: outside-right-center"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diagram := compileRoutingDiagram(t, t.Context(), tc.code, 1, nil)
			before, after := workflowGaps(t, diagram, "down", "A", "short", "Z")
			if before == after {
				t.Fatalf("expected the ordinary rank placement, got identical %d px gaps", before)
			}
		})
	}
}

func workflowGaps(t *testing.T, d *d2target.Diagram, direction, fork, short, join string) (int, int) {
	t.Helper()
	bounds := make(map[string][2]int, len(d.Shapes))
	for _, n := range d.Shapes {
		start, extent := n.Pos.Y, n.Height
		if direction == "left" || direction == "right" {
			start, extent = n.Pos.X, n.Width
		}
		if direction == "up" || direction == "left" {
			start = -start - extent
		}
		bounds[n.ID] = [2]int{start, start + extent}
	}
	for _, id := range []string{fork, short, join} {
		if _, ok := bounds[id]; !ok {
			t.Fatalf("missing workflow node %s", id)
		}
	}
	return bounds[short][0] - bounds[fork][1], bounds[join][0] - bounds[short][1]
}
