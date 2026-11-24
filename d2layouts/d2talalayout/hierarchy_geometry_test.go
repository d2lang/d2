package d2talalayout_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

func hierarchyGeometry(t *testing.T, source string) map[string]d2target.Shape {
	t.Helper()
	diagram := compileRoutingDiagram(t, t.Context(), source, 1, &d2svg.RenderOpts{})
	shapes := make(map[string]d2target.Shape, len(diagram.Shapes))
	for _, shape := range diagram.Shapes {
		shapes[shape.ID] = shape
	}
	return shapes
}

func TestHorizontalHierarchyKeepsContainerDimensions(t *testing.T) {
	for _, direction := range []string{"left", "right"} {
		t.Run(direction, func(t *testing.T) {
			source := "shape: hierarchy\ndirection: " + direction + "\nc: {width: 400; height: 200; a}\nc.a -> b -> k\nc.a -> k"
			box := hierarchyGeometry(t, source)["c"]
			if box.Width != 400 || box.Height != 200 {
				t.Fatalf("container is %d × %d, want 400 × 200", box.Width, box.Height)
			}

			// Container fitting must also use the label's width in drawing coordinates.
			source = "shape: hierarchy\ndirection: " + direction + "\nc: A container with a descriptive long human-readable label {a}\nstart -> c.a -> end"
			box = hierarchyGeometry(t, source)["c"]
			if box.Width < box.LabelWidth || box.Height >= box.Width {
				t.Fatalf("container is %d × %d for a label %d px wide", box.Width, box.Height, box.LabelWidth)
			}
		})
	}
}

func TestHierarchyLabelSpacingUsesItsOwnRankBoundary(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprintf("nested=%t", nested), func(t *testing.T) {
			source, endpoint, first := "shape: hierarchy\n", "a", "a"
			if nested {
				source += "group: {a; d}\n"
				endpoint, first = "group.a", "group"
			}
			edges := source + endpoint + " -> b%s\nb -> c"
			plain := hierarchyGeometry(t, fmt.Sprintf(edges, ""))
			labeled := hierarchyGeometry(t, fmt.Sprintf(edges, `: "1\n2\n3\n4\n5\n6\n7"`))
			gap := func(shapes map[string]d2target.Shape, a, b string) int {
				return shapes[b].Pos.Y - (shapes[a].Pos.Y + shapes[a].Height)
			}
			if gap(labeled, first, "b") <= gap(plain, first, "b") ||
				gap(labeled, "b", "c") != gap(plain, "b", "c") {
				t.Fatalf("gaps changed from (%d, %d) to (%d, %d)", gap(plain, first, "b"), gap(plain, "b", "c"),
					gap(labeled, first, "b"), gap(labeled, "b", "c"))
			}
		})
	}
}

func TestHierarchyReservesFullExtentOfLargeEdgeLabel(t *testing.T) {
	for _, direction := range []string{"down", "right"} {
		t.Run(direction, func(t *testing.T) {
			label := strings.Repeat("m", 80)
			if direction == "down" {
				label = strings.Repeat(`m\n`, 39) + "m"
			}
			source := fmt.Sprintf("shape: hierarchy\ndirection: %s\na -> b: \"%s\"\nb -> c", direction, label)
			diagram := compileRoutingDiagram(t, t.Context(), source, 1, &d2svg.RenderOpts{})
			a, b := diagram.Shapes[0], diagram.Shapes[1]
			edge := diagram.Connections[0]
			gap, extent := b.Pos.Y-a.Pos.Y-a.Height, edge.LabelHeight
			if direction == "right" {
				gap, extent = b.Pos.X-a.Pos.X-a.Width, edge.LabelWidth
			}
			if gap < extent {
				t.Fatalf("gap is %d px for a label spanning %d px", gap, extent)
			}
		})
	}
}
