package d2elklayout

import (
	"math"
	"reflect"
	"testing"

	"github.com/d2lang/d2/d2exporter"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestNativeSelfLoopGeometry(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	for _, algorithm := range []string{"stress", "force", "mrtree"} {
		for _, fixture := range []struct{ name, input string }{
			{"parallel", "a.shape: diamond\na -> a: retry until the database responds\na -> a: cancel the pending operation\na -> neighbor"},
			{"container", "outer: {child}\nouter -> outer: retry the entire operation"},
		} {
			t.Run(algorithm+"/"+fixture.name, func(t *testing.T) {
				g := compileELKGoDiagram(t, ruler, fixture.input)
				opts := DefaultOpts
				opts.Algorithm = algorithm
				if err := Layout(t.Context(), g, &opts); err != nil {
					t.Fatal(err)
				}
				diagram, err := d2exporter.Export(t.Context(), g, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				var previous *d2graph.Edge
				var previousLabel *geo.Box
				for i, edge := range g.Edges {
					if edge.Src != edge.Dst {
						continue
					}
					route, box := edge.Route, edge.Src.Box
					if len(route) < 4 {
						t.Fatalf("self-loop has only %d points", len(route))
					}
					for _, end := range []*geo.Point{route[0], route[len(route)-1]} {
						dx := math.Abs(end.X-box.Center().X) / (box.Width / 2)
						dy := math.Abs(end.Y-box.Center().Y) / (box.Height / 2)
						boundary := math.Max(dx, dy)
						if edge.Src.Shape.Value == "diamond" {
							boundary = dx + dy
						}
						// Shape tracing rounds diamond intersection coordinates.
						if math.Abs(boundary-1) > 2/box.Width+2/box.Height {
							t.Fatalf("endpoint %v misses %s outline %v", end, edge.Src.Shape.Value, box)
						}
					}
					connection := &diagram.Connections[i]
					labelBox := geo.NewBox(connection.GetLabelTopLeft(), float64(connection.LabelWidth), float64(connection.LabelHeight))
					for _, obj := range g.Objects {
						if labelBox.Overlaps(*obj.Box) {
							t.Fatalf("rendered label %q (%v) overlaps node %s (%v)", connection.Label, labelBox, obj.AbsID(), obj.Box)
						}
						if obj == edge.Src || obj.IsDescendantOf(edge.Src) {
							continue
						}
						for j, p := range route[1:] {
							if obj.Box.Intersects(geo.Segment{Start: route[j], End: p}, 0) || obj.Box.Contains(p) {
								t.Fatalf("loop %s crosses node %s", edge.AbsID(), obj.AbsID())
							}
						}
					}
					if previous != nil && (reflect.DeepEqual(route, previous.Route) || labelBox.Overlaps(*previousLabel)) {
						t.Fatalf("parallel loops share a route or label area")
					}
					previous, previousLabel = edge, labelBox
				}
			})
		}
	}
}
