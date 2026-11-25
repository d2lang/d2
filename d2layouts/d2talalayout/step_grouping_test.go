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

func TestStepReadingDirection(t *testing.T) {
	for _, direction := range []string{"right", "left", "up", "down"} {
		t.Run(direction, func(t *testing.T) {
			source := fmt.Sprintf("Workflow: {direction: %s\n A: {shape: step}\nB: {shape: step}\n C: {shape: step}\n A -> B -> C}", direction)
			d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
			wantEdges := 2
			if direction == "right" {
				wantEdges = 0
			}
			if len(d.Connections) != wantEdges {
				t.Fatalf("%s layout has %d connectors, want %d", direction, len(d.Connections), wantEdges)
			}
			for i := 2; i < len(d.Shapes); i++ {
				a, b := d.Shapes[i-1], d.Shapes[i]
				dx := 2*(b.Pos.X-a.Pos.X) + b.Width - a.Width
				dy := 2*(b.Pos.Y-a.Pos.Y) + b.Height - a.Height
				if direction == "right" && dx <= 0 || direction == "left" && dx >= 0 || direction == "up" && dy >= 0 || direction == "down" && dy <= 0 {
					t.Errorf("step %s does not advance %s from %s", b.ID, direction, a.ID)
				}
			}
		})
	}
}

func TestFixedStepCaptionsDoNotFuseOverlappingText(t *testing.T) {
	const template = `direction: right
A: Validate incoming payment {shape: step; width: %d; height: 60; label.near: outside-top-center}
B: Check settlement window {shape: step; width: %d; height: 60; label.near: outside-top-center}
C: Release payment to merchant {shape: step; width: %d; height: 60; label.near: outside-top-center}
A -> B -> C
`
	for _, width := range []int{90, 140} {
		d := compileRoutingDiagram(t, t.Context(), fmt.Sprintf(template, width, width, width), 1, nil)
		if len(d.Connections) != 2 {
			t.Fatalf("got %d explicit transitions", len(d.Connections))
		}
		for i := 1; i < len(d.Shapes); i++ {
			a, b := d.Shapes[i-1], d.Shapes[i]
			right := 2*a.Pos.X + a.Width + a.LabelWidth
			left := 2*b.Pos.X + b.Width - b.LabelWidth
			if right > left {
				t.Fatalf("captions for %s and %s overlap", a.ID, b.ID)
			}
		}
	}
}

func TestPrescaledStepsRetainOnlyTheirActualConnectors(t *testing.T) {
	for _, tc := range []struct {
		name, steps string
		external    int
		fused       bool
	}{
		{"captions fit after scaling", `direction: right
A: some longer step caption {shape: step; label.near: outside-top-center; style.font-size: 16}
B: some longer step caption {shape: step; label.near: outside-top-center; style.font-size: 16; width: 75}`, 8, true},
		{"unequal heights after scaling", "A: A {shape: step}\nB: B {shape: step; height: 150}", 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := compileRoutingDiagram(t, t.Context(), tc.steps+"\nA -> B\n"+strings.Repeat("X -> A\n", tc.external), 1, nil)
			var external, between int
			for _, e := range d.Connections {
				switch {
				case e.Src == "X" && e.Dst == "A":
					external++
				case e.Src == "A" && e.Dst == "B" && len(e.Route) >= 2:
					between++
				default:
					t.Fatalf("unexpected connection %s", e.ID)
				}
			}
			if external != tc.external || tc.fused && between != 0 || !tc.fused && between != 1 {
				t.Fatalf("got %d external connectors and %d step connectors", external, between)
			}
			if tc.fused {
				a, b := d.Shapes[0], d.Shapes[1]
				if a.Pos.Y != b.Pos.Y || a.Pos.X+a.Width-35 != b.Pos.X {
					t.Fatalf("the consumed transition left disconnected step shapes at %v and %v", a.Pos, b.Pos)
				}
			}
		})
	}
}

func TestUnpaintedStepKeepsItsConnectors(t *testing.T) {
	for _, style := range []string{"style.opacity: 0", "style.fill: transparent; style.stroke: transparent", "style.fill: transparent; style.stroke-width: 0"} {
		d := compileRoutingDiagram(t, t.Context(), "C: {shape: step}\nA: {shape: step}\nB: Middle {shape: step; "+style+"}\nA -> B -> C", 1, nil)
		if len(d.Connections) != 2 || d.Connections[0].Src != "A" || d.Connections[0].Dst != "B" || d.Connections[1].Src != "B" || d.Connections[1].Dst != "C" {
			t.Fatalf("%s: expected A to connect through B to C", style)
		}
	}
}

func TestNarrowStepWithInteriorIconRenders(t *testing.T) {
	const source = `A: A {shape: step; width: 30; height: 60; icon: "status.svg"; icon.near: center-center}
B: B {shape: step; width: 30; height: 60}
A -> B`
	d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	svg, err := d2svg.Render(d, &d2svg.RenderOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Shapes) != 2 || len(d.Connections) != 0 || !strings.Contains(string(svg), `<image `) || strings.Contains(string(svg), `width="-`) || strings.Contains(string(svg), `height="-`) {
		t.Fatal("expected a fused narrow row and nonnegative SVG dimensions")
	}
}

func TestBlankStepTransitionLabelRenders(t *testing.T) {
	d := compileRoutingDiagram(t, t.Context(), `A: {shape: step}
B: {shape: step}
A -> B: " "`, 1, nil)
	if len(d.Connections) != 1 || len(d.Connections[0].Route) < 2 || d.Connections[0].Label != " " {
		t.Fatalf("lost the explicit, blank-labeled transition")
	}
}
