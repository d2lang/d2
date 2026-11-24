package d2talalayout

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestHierarchyNearFollowsRequestedRank(t *testing.T) {
	const chain = "a -> b -> c -> d\na -> d\n"
	for _, tc := range []struct{ name, header, graph, note, targetPrefix string }{
		{"root", "shape: hierarchy\ndirection: down\n", chain, "annotation", ""},
		{"nested hierarchy", "", "P: {shape: hierarchy\n" + chain + "}\n", "annotation", "P."},
		{"sibling containers", "", "P: {shape: hierarchy\n" + chain + "}\n", "Q.annotation", "P."},
	} {
		for _, target := range []string{"a", "b", "c", "d"} {
			for _, noteFirst := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/noteFirst=%t", tc.name, target, noteFirst), func(t *testing.T) {
					note := tc.note + ": {shape: text; near: " + tc.targetPrefix + target + "}\n"
					diagram := tc.graph + note
					if noteFirst {
						diagram = note + tc.graph
					}
					g, _, err := d2compiler.Compile("near.d2", strings.NewReader(tc.header+diagram), nil)
					if err != nil {
						t.Fatal(err)
					}
					ruler, err := textmeasure.NewRuler()
					if err != nil {
						t.Fatal(err)
					}
					if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
						t.Fatal(err)
					}
					if err := Layout(t.Context(), g, &Options{Seeds: []int64{1}}); err != nil {
						t.Fatal(err)
					}
					objects := map[string]*d2graph.Object{}
					for _, obj := range g.Objects {
						objects[obj.ID] = obj
					}
					annotation := objects["annotation"]
					if p := objects["P"]; p != nil {
						obstacle := annotation
						if q := objects["Q"]; q != nil {
							obstacle = q
						}
						if p.Box.Overlaps(*obstacle.Box) {
							t.Fatalf("annotation's component overlaps the hierarchy container")
						}
					}
					distance := func(n *d2graph.Object) float64 {
						return math.Hypot(n.Center().X-annotation.Center().X, n.Center().Y-annotation.Center().Y)
					}
					var previous *d2graph.Object
					for _, id := range []string{"a", "b", "c", "d"} {
						n := objects[id]
						if id != target && distance(n) <= distance(objects[target]) {
							t.Errorf("annotation is as close or closer to %s than its near target %s", id, target)
						}
						if annotation.Box.Overlaps(*n.Box) {
							t.Errorf("annotation overlaps %s", id)
						}
						if previous != nil && previous.TopLeft.Y+previous.Height >= n.TopLeft.Y {
							t.Errorf("%s no longer follows the preceding rank", id)
						}
						previous = n
					}
				})
			}
		}
	}
}
