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
	for _, target := range []string{"a", "b", "c", "d"} {
		for _, noteFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/noteFirst=%t", target, noteFirst), func(t *testing.T) {
				note := "annotation: {shape: text; near: " + target + "}\n"
				diagram := chain + note
				if noteFirst {
					diagram = note + chain
				}
				g, _, err := d2compiler.Compile("near.d2", strings.NewReader("shape: hierarchy\ndirection: down\n"+diagram), nil)
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
