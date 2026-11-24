package d2talalayout_test

import (
	"fmt"
	"math"
	"slices"
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
				if crossings := fanRouteCrossings(diagram.Connections); crossings != 0 {
					t.Fatalf("%d branches have %d proper crossings", size, crossings)
				}
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

func fanRouteCrossings(edges []d2target.Connection) int {
	total := 0
	for i, a := range edges {
		for _, b := range edges[:i] {
			for j := 1; j < len(a.Route); j++ {
				p, q := a.Route[j-1], a.Route[j]
				for k := 1; k < len(b.Route); k++ {
					u, v := b.Route[k-1], b.Route[k]
					x := geo.IntersectionPoint(p, q, u, v)
					if x != nil && !x.Equals(p) && !x.Equals(q) && !x.Equals(u) && !x.Equals(v) {
						total++
					}
				}
			}
		}
	}
	return total
}

func TestBranchOrderPreservesRoutedGeometry(t *testing.T) {
	for _, direction := range []string{"down", "right"} {
		for _, kind := range []string{"detached legend", "split and rejoin", "fanin with arrowhead label"} {
			t.Run(direction+"/"+kind, func(t *testing.T) {
				var source strings.Builder
				fmt.Fprintf(&source, "shape: hierarchy\ndirection: %s\nvars: {d2-config: {data: {tala-seeds: [1;2;3]}}}\nstart: Hub\n", direction)
				if kind == "split and rejoin" {
					source.WriteString("end: Sink\n")
				}
				for i := 0; i < 9; i++ {
					fmt.Fprintf(&source, "n%d: peer {width: 100; height: 76}\n", i)
				}
				for i := 0; i < 9; i++ {
					if kind == "fanin with arrowhead label" {
						fmt.Fprintf(&source, "n%d -> start", i)
						if i == 0 {
							source.WriteString(": {target-arrowhead.label: ok}")
						}
						source.WriteByte('\n')
					} else {
						fmt.Fprintf(&source, "start -> n%d\n", i)
						if kind == "split and rejoin" {
							fmt.Fprintf(&source, "n%d -> end\n", i)
						}
					}
				}
				if kind == "detached legend" {
					source.WriteString("key: Legend\n")
				}
				diagram := compileRoutingDiagram(t, t.Context(), source.String(), 1, nil)
				limit := 1
				if kind == "fanin with arrowhead label" {
					limit = 0
				}
				if count := fanRouteCrossings(diagram.Connections); count > limit {
					t.Errorf("sorting branch identities increased proper crossings to %d, want at most %d", count, limit)
				}
			})
		}
	}
}

func TestBranchSlotsKeepAuthoredObjectsAndEdges(t *testing.T) {
	// Each source slot has distinct object and edge metadata. The route used
	// for each identity must terminate at that object's new physical slot.
	ids := []string{"z", "a", "m", "b", "x", "c", "q", "d", "f"}
	colors := []string{"#ff3300", "#22bb66", "#4455dd"}
	for _, size := range []int{8, 9} {
		var source strings.Builder
		source.WriteString("shape: hierarchy\ndirection: left\n")
		for i, id := range ids[:size] {
			fmt.Fprintf(&source, "%s: Team %d {width: 100; height: 60; style.fill: \"%s\"; link: \"https://example.com/%s\"}\n", id, i, colors[i%len(colors)], id)
		}
		for i, id := range ids[:size] {
			fmt.Fprintf(&source, "start -> %s: {style.stroke: \"%s\"}\n%s -> finish\n", id, colors[i%len(colors)], id)
		}
		diagram := compileRoutingDiagram(t, t.Context(), source.String(), 2, nil)
		shapes := make(map[string]d2target.Shape)
		for _, s := range diagram.Shapes {
			shapes[s.ID] = s
		}
		for i, id := range ids[:size] {
			s := shapes[id]
			if s.Label != fmt.Sprintf("Team %d", i) || s.Link != "https://example.com/"+id || s.Fill != colors[i%len(colors)] {
				t.Errorf("metadata changed for %s", id)
			}
			if i > 0 && s.Pos.Y <= shapes[ids[i-1]].Pos.Y {
				t.Errorf("%s no longer follows %s", id, ids[i-1])
			}
		}
		for _, e := range diagram.Connections {
			if e.Src == "start" {
				i := slices.Index(ids[:size], e.Dst)
				if i < 0 || e.Stroke != colors[i%len(colors)] {
					t.Fatalf("wrong stroke on %s", e.ID)
				}
			}
			for _, end := range []struct {
				id string
				p  *geo.Point
			}{{e.Src, e.Route[0]}, {e.Dst, e.Route[len(e.Route)-1]}} {
				s := shapes[end.id]
				x, y := float64(s.Pos.X), float64(s.Pos.Y)
				right, bottom := x+float64(s.Width), y+float64(s.Height)
				if end.p.X < x-1 || end.p.X > right+1 || end.p.Y < y-1 || end.p.Y > bottom+1 ||
					min(math.Abs(end.p.X-x), math.Abs(end.p.X-right), math.Abs(end.p.Y-y), math.Abs(end.p.Y-bottom)) > 1 {
					t.Errorf("%s path at %v does not attach to %s", e.ID, end.p, end.id)
				}
			}
		}
	}
}
