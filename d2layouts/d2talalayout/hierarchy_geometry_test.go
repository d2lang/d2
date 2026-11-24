package d2talalayout_test

import (
	"fmt"
	"slices"
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

func TestReversedHierarchyPreservesLabeledBlockInteriors(t *testing.T) {
	for _, tc := range []struct {
		forward, reverse, label, position string
	}{
		{"right", "left", "A container with a descriptive long human-readable label", "center-left"},
		{"down", "up", `"One\nTwo\nThree\nFour\nFive\nSix\nSeven"`, "top-center"},
	} {
		t.Run(tc.reverse, func(t *testing.T) {
			source := "shape: hierarchy\ndirection: %s\n" +
				fmt.Sprintf("box: %s {label.near: %s; inner: %s {label.near: %s; x}}\n", tc.label, tc.position, tc.label, tc.position) +
				"start -> box.inner.x -> end"
			forward := hierarchyGeometry(t, fmt.Sprintf(source, tc.forward))
			reversed := hierarchyGeometry(t, fmt.Sprintf(source, tc.reverse))
			start, box, end := reversed["start"], reversed["box"], reversed["end"]
			if tc.reverse == "left" {
				if start.Pos.X <= box.Pos.X+box.Width || box.Pos.X <= end.Pos.X+end.Width {
					t.Fatal("expected right-to-left ranks")
				}
			} else if start.Pos.Y <= box.Pos.Y+box.Height || box.Pos.Y <= end.Pos.Y+end.Height {
				t.Fatal("expected bottom-to-top ranks")
			}
			for _, pair := range [][2]string{{"box", "box.inner"}, {"box.inner", "box.inner.x"}} {
				p, c := forward[pair[0]], forward[pair[1]]
				rp, rc := reversed[pair[0]], reversed[pair[1]]
				if tc.reverse == "left" && rc.Pos.X < rp.Pos.X+rp.LabelWidth ||
					tc.reverse == "up" && rc.Pos.Y < rp.Pos.Y+rp.LabelHeight {
					t.Fatalf("child %s overlaps the label in %s", pair[1], pair[0])
				}
				if dx, dy := c.Pos.X-p.Pos.X, c.Pos.Y-p.Pos.Y; rc.Pos.X-rp.Pos.X != dx || rc.Pos.Y-rp.Pos.Y != dy {
					t.Fatalf("child %s offset in %s changed from (%d,%d) to (%d,%d)", pair[1], pair[0], dx, dy, rc.Pos.X-rp.Pos.X, rc.Pos.Y-rp.Pos.Y)
				}
			}
		})
	}
}

func TestCompactHierarchyBranchSpacing(t *testing.T) {
	for _, seeds := range []string{"1", "1;2;3"} {
		for _, direction := range []string{"down", "right"} {
			t.Run(direction+"/"+seeds, func(t *testing.T) {
				prefix := fmt.Sprintf("vars: {d2-config: {data: {tala-seeds: [%s]}}}\nshape: hierarchy\ndirection: %s\n", seeds, direction)
				const dims = "*.width: 96\n*.height: 52\n"
				nodes := hierarchyGeometry(t, prefix+"root -> a -> end; root -> b -> end; root -> c -> end; root -> d -> end\n"+dims)
				cross := func(s d2target.Shape) (int, int) {
					if direction == "down" {
						return s.Pos.X, s.Width
					}
					return s.Pos.Y, s.Height
				}
				var starts []int
				var size int
				for _, id := range []string{"a", "b", "c", "d"} {
					start, n := cross(nodes[id])
					size = n
					starts = append(starts, start)
				}
				slices.Sort(starts)
				for i := 1; i < len(starts); i++ {
					if gap := starts[i] - starts[i-1] - size; gap != 60 {
						t.Fatalf("sibling gap %d, want 60", gap)
					}
				}

				nodes = hierarchyGeometry(t, prefix+"root -> a; root -> b; root -> c; a -> a1; a -> a2; b -> b1; b -> b2; c -> c1; c -> c2\n"+dims)
				center := func(id string) int { start, size := cross(nodes[id]); return 2*start + size }
				for _, id := range []string{"a", "b", "c"} {
					if 2*center(id) != center(id+"1")+center(id+"2") {
						t.Errorf("parent %s is off-center", id)
					}
				}
			})
		}
	}
}

func TestSingleEdgeRankClearance(t *testing.T) {
	for _, row := range []struct {
		edges          string
		minGap, maxGap int
	}{
		{"a -> b -> c", 60, 60}, {"a -> b; a -> b; b -> c", 90, 90}, {`a -> b: "four\nlines\nof\nlabel"; b -> c`, 110, 300},
	} {
		t.Run(row.edges, func(t *testing.T) {
			nodes := hierarchyGeometry(t, "shape: hierarchy\ndirection: down\n"+row.edges)
			a, b := nodes["a"], nodes["b"]
			gap := b.Pos.Y - a.Pos.Y - a.Height
			if gap < row.minGap || gap > row.maxGap {
				t.Fatalf("a→b gap %d, want [%d,%d]", gap, row.minGap, row.maxGap)
			}
		})
	}
}

// Shrinking the outer row margin saves two bends in this drawing, at the cost
// of routing the bypass n0→n4 across n2→n3. The two lanes must stay separate.
func TestCompactHierarchyPreservesCrossingFreeRoutes(t *testing.T) {
	const source = `shape: hierarchy
 direction: right
 vars: {d2-config: {data: {tala-seeds: [1;2;3]}}}
 n0; n1; n2; n3; n4; n5; n6; n7; n8
 n0 -> n1
 n1 -> n2: upstream session restart
 n2 -> n3 -> n4 -> n5
 n5 -> n6: queue replication and synchronous delivery
 n7 -> n8
 n0 -> n7
 n5 -> n8
 n3 -> n8
 n3 -> n7
 n0 -> n4: ack
 n2 -> n4
 `
	d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	shapes := make(map[string]d2target.Shape, len(d.Shapes))
	for _, s := range d.Shapes {
		shapes[s.ID] = s
	}
	var lanes []d2target.Connection
	for _, e := range d.Connections {
		if (e.Src == "n0" && e.Dst == "n4") || (e.Src == "n2" && e.Dst == "n3") {
			lanes = append(lanes, e)
		}
	}
	if len(lanes) != 2 {
		t.Fatalf("got %d test lanes, want 2", len(lanes))
	}
	for _, gaps := range d2target.ConnectionCrossings(lanes, shapes, false, nil) {
		if len(gaps) > 0 {
			t.Fatal("the bypass crosses n2→n3")
		}
	}
}
