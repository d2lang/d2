package d2talalayout_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/lib/geo"
)

func TestHierarchySeparatesMultilineEdgeLabels(t *testing.T) {
	cases := []struct {
		name  string
		edges []string
	}{
		{"parallel", []string{"a -> b", "a -> b", "a -> b"}},
		{"all-to-all", []string{"a -> v", "a -> w", "b -> v", "b -> w"}},
	}
	for _, tc := range cases {
		for _, direction := range []string{"down", "up", "left", "right"} {
			t.Run(tc.name+"/"+direction, func(t *testing.T) {
				var source strings.Builder
				fmt.Fprintf(&source, "shape: hierarchy\ndirection: %s\n", direction)
				for _, edge := range tc.edges {
					fmt.Fprintln(&source, edge+`: "first label\nsecond line\nthird line\nfourth line"`)
				}
				diagram := compileRoutingDiagram(t, t.Context(), source.String(), 1, &d2svg.RenderOpts{})
				if len(diagram.Connections) != len(tc.edges) {
					t.Fatalf("got %d edges, want %d", len(diagram.Connections), len(tc.edges))
				}
				obstacles := map[string]geo.Box{}
				for _, node := range diagram.Shapes {
					obstacles[node.ID] = geo.Box{TopLeft: geo.NewPoint(float64(node.Pos.X), float64(node.Pos.Y)), Width: float64(node.Width), Height: float64(node.Height)}
				}
				for _, edge := range diagram.Connections {
					box := geo.Box{TopLeft: edge.GetLabelTopLeft(), Width: float64(edge.LabelWidth), Height: float64(edge.LabelHeight)}
					for id, obstacle := range obstacles {
						if box.Overlaps(obstacle) {
							t.Errorf("edge label %s at %v (%g×%g) overlaps %s at %v (%g×%g)", edge.ID, *box.TopLeft, box.Width, box.Height, id, *obstacle.TopLeft, obstacle.Width, obstacle.Height)
						}
					}
					obstacles[edge.ID] = box
				}
			})
		}
	}
}
