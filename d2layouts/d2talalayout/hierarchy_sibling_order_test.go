package d2talalayout_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestAppendingEquivalentHierarchyBranchesPreservesOrder(t *testing.T) {
	for _, direction := range []string{"down", "up", "right", "left"} {
		t.Run(direction, func(t *testing.T) {
			// Deliberately use declaration order rather than lexical or hashed ID order.
			names := []string{"z", "a", "t", "b", "r", "c", "q", "d", "p", "e", "n", "f"}
			for size := 2; size <= len(names); size++ {
				var source strings.Builder
				fmt.Fprintf(&source, "shape: hierarchy\ndirection: %s\nroot: Start {width: 96; height: 52}\n", direction)
				for i, id := range names[:size] {
					fmt.Fprintf(&source, "%s: Team %d {width: 96; height: 52}\n", id, i+1)
				}
				for _, id := range names[:size] {
					fmt.Fprintf(&source, "root -> %s\n", id)
				}
				diagram := compileRoutingDiagram(t, t.Context(), source.String(), int64(size%3+1), &d2svg.RenderOpts{})
				assertFanRoutesDoNotCross(t, diagram.Connections)
				positions := make(map[string]int)
				for _, shape := range diagram.Shapes {
					positions[shape.ID] = shape.Pos.X
					if direction == "left" || direction == "right" {
						positions[shape.ID] = shape.Pos.Y
					}
				}
				for i := 1; i < size; i++ {
					before, after := names[i-1], names[i]
					if positions[before] >= positions[after] {
						t.Fatalf("%d branches: expected %s before %s, got positions %d and %d", size, before, after, positions[before], positions[after])
					}
				}
			}
		})
	}
}

func assertFanRoutesDoNotCross(t *testing.T, edges []d2target.Connection) {
	t.Helper()
	for i, a := range edges {
		for _, b := range edges[:i] {
			for j := 1; j < len(a.Route); j++ {
				p, q := a.Route[j-1], a.Route[j]
				for k := 1; k < len(b.Route); k++ {
					u, v := b.Route[k-1], b.Route[k]
					x := geo.IntersectionPoint(p, q, u, v)
					if x != nil && !x.Equals(p) && !x.Equals(q) && !x.Equals(u) && !x.Equals(v) {
						t.Fatalf("fan routes %s and %s cross at %v", a.ID, b.ID, x)
					}
				}
			}
		}
	}
}
