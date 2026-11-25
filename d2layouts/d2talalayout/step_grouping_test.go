package d2talalayout_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
)

func TestStepOrderFollowsDirectedFlow(t *testing.T) {
	for _, order := range []string{"ABC", "ACB", "BAC", "BCA", "CAB", "CBA"} {
		t.Run(order, func(t *testing.T) {
			var source strings.Builder
			for _, id := range order {
				fmt.Fprintf(&source, "%c: {shape: step}\n", id)
			}
			source.WriteString("A -> B -> C")
			d := compileRoutingDiagram(t, t.Context(), source.String(), 1, &d2svg.RenderOpts{})
			if len(d.Connections) != 0 || len(d.Shapes) != 3 {
				t.Fatalf("expected three fused steps, got %d shapes and %d connections", len(d.Shapes), len(d.Connections))
			}
			x := map[string]int{}
			for _, n := range d.Shapes {
				x[n.ID] = n.Pos.X
			}
			if !(x["A"] < x["B"] && x["B"] < x["C"]) {
				t.Fatalf("flow order reversed: %v", x)
			}
		})
	}
}

func TestStepGroupingPreservesAuthoredTransitions(t *testing.T) {
	const nodes = "A: {shape: step}\nB: {shape: step}\nC: {shape: step}\n"
	for _, tc := range []struct {
		name, links string
		want        []string
	}{
		{"join", "A -> B\nC -> B", []string{"A-B", "C-B"}},
		{"cycle", "A -> B -> C -> A", []string{"A-B", "B-C", "C-A"}},
		{"parallel", "A -> B\nA -> B\nB -> C", []string{"A-B", "A-B", "B-C"}},
		{"two directions", "A <-> B -> C", []string{"A-B", "B-C"}},
		{"label", "A -> B: after audit\nB -> C", []string{"A-B", "B-C"}},
		{"endpoint label", "A -> B: {target-arrowhead.label: approved}\nB -> C", []string{"A-B", "B-C"}},
		{"styled line", "A -> B: {style.stroke-dash: 5}\nB -> C", []string{"A-B", "B-C"}},
		{"custom head", "A -> B: {target-arrowhead.shape: diamond}\nB -> C", []string{"A-B", "B-C"}},
		{"head fill", "A -> B: {target-arrowhead.style.fill: red}\nB -> C", []string{"A-B", "B-C"}},
		{"tooltip", "A -> B: {tooltip: read the checklist}\nB -> C", []string{"A-B", "B-C"}},
		{"link", "A -> B: {link: https://example.com}\nB -> C", []string{"A-B", "B-C"}},
		{"icon", "A -> B: {icon: https://example.com/ok.svg}\nB -> C", []string{"A-B", "B-C"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := compileRoutingDiagram(t, t.Context(), nodes+tc.links, 1, &d2svg.RenderOpts{})
			var got []string
			for _, e := range d.Connections {
				got = append(got, e.Src+"-"+e.Dst)
				if len(e.Route) < 2 {
					t.Fatalf("connector %s lost its route", e.ID)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFusedStepsKeepExplicitPositions(t *testing.T) {
	// The outer container survives while the two steps temporarily share a vessel.
	for _, direction := range []string{"right", "left", "up", "down"} {
		t.Run(direction, func(t *testing.T) {
			d := compileRoutingDiagram(t, t.Context(), fmt.Sprintf(`direction: %s
Workflow: {
  A: {shape: step; width: 120; height: 65; label.near: outside-top-center}
  B: {shape: step; width: 120; height: 65; label.near: outside-bottom-right; icon: https://example.com/icon.svg; icon.near: outside-right-center}
  A -> B
}`, direction), 1, &d2svg.RenderOpts{})
			if len(d.Connections) != 0 || len(d.Shapes) != 3 {
				t.Fatalf("expected a container and two fused steps, got %d shapes and %d edges", len(d.Shapes), len(d.Connections))
			}
			for _, s := range d.Shapes {
				switch s.ID {
				case "Workflow.A":
					if s.LabelPosition != "OUTSIDE_TOP_CENTER" {
						t.Errorf("step A label position = %s", s.LabelPosition)
					}
				case "Workflow.B":
					if s.LabelPosition != "OUTSIDE_BOTTOM_RIGHT" || s.IconPosition != "OUTSIDE_RIGHT_MIDDLE" {
						t.Errorf("step B label = %s, icon = %s", s.LabelPosition, s.IconPosition)
					}
				}
			}
		})
	}
}

func TestFusedStepsRespectAuthoredWedgeWidths(t *testing.T) {
	for _, tc := range []struct {
		name   string
		widths [3]int
		edges  int
	}{
		{"equal narrow steps", [3]int{30, 30, 30}, 0},
		{"different wedge depths", [3]int{30, 90, 100}, 2},
		{"same wedge depth at different widths", [3]int{36, 90, 100}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var source strings.Builder
			for i, w := range tc.widths {
				fmt.Fprintf(&source, "%c: {shape: step; width: %d; height: 56}\n", 'A'+i, w)
			}
			source.WriteString("A -> B -> C")
			d := compileRoutingDiagram(t, t.Context(), source.String(), 1, nil)
			if len(d.Shapes) != len(tc.widths) || len(d.Connections) != tc.edges {
				t.Fatalf("got %d shapes and %d explicit transitions", len(d.Shapes), len(d.Connections))
			}
			for i, n := range d.Shapes {
				if n.Width != tc.widths[i] {
					t.Errorf("step %s has width %d, want %d", n.ID, n.Width, tc.widths[i])
				}
			}
			if tc.widths[0] == 30 && tc.edges == 0 {
				for i := 1; i < len(d.Shapes); i++ {
					if d.Shapes[i].Pos.X-d.Shapes[i-1].Pos.X != 15 {
						t.Errorf("step %s does not meet its predecessor", d.Shapes[i].ID)
					}
				}
			}
		})
	}
}

func TestFusedStepsRespectAuthoredHeights(t *testing.T) {
	for _, tc := range []struct {
		name       string
		attributes [3]string
		heights    [3]int
		links      int
	}{
		{"mixed authored heights", [3]string{"height: 42", "height: 105", "height: 56"}, [3]int{42, 105, 56}, 2},
		{"automatic neighbors can grow", [3]string{"", "height: 105", ""}, [3]int{105, 105, 105}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var source strings.Builder
			for i, attr := range tc.attributes {
				fmt.Fprintf(&source, "%c: {shape: step; width: 100; %s}\n", 'A'+i, attr)
			}
			source.WriteString("A -> B -> C")
			d := compileRoutingDiagram(t, t.Context(), source.String(), 1, nil)
			if len(d.Shapes) != len(tc.heights) || len(d.Connections) != tc.links {
				t.Fatalf("got %d shapes and %d explicit transitions", len(d.Shapes), len(d.Connections))
			}
			for i, n := range d.Shapes {
				if n.Height != tc.heights[i] {
					t.Errorf("%s has height %d, want %d", n.ID, n.Height, tc.heights[i])
				}
			}
		})
	}
}
