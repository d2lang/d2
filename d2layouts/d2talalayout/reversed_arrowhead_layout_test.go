package d2talalayout_test

import (
	"fmt"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/lib/geo"
)

func TestReciprocalArrowheadLabelDeclarationDirections(t *testing.T) {
	for _, direction := range []string{"auto", "down", "up", "left", "right"} {
		prefix := ""
		if direction != "auto" {
			prefix = "shape: hierarchy\ndirection: " + direction + "\n"
		}
		for i, request := range []string{
			"Gateway -> Worker: {source-arrowhead.label: request}",
			"Worker <- Gateway: {target-arrowhead.label: request}",
		} {
			for j, reply := range []string{
				"Worker -> Gateway: {target-arrowhead.label: reply}",
				"Gateway <- Worker: {source-arrowhead.label: reply}",
			} {
				t.Run(fmt.Sprintf("%s/%d%d", direction, i, j), func(t *testing.T) {
					d := compileRoutingDiagram(t, t.Context(), prefix+request+"\n"+reply, 1, &d2svg.RenderOpts{})
					if len(d.Connections) != 2 {
						t.Fatalf("got %d connections", len(d.Connections))
					}
					var boxes [2]geo.Box
					for k, e := range d.Connections {
						isDst := k == 0 && i == 1 || k == 1 && j == 0
						text, nodeID := e.SrcLabel, e.Src
						if isDst {
							text, nodeID = e.DstLabel, e.Dst
						}
						if text == nil || nodeID != "Gateway" {
							t.Fatalf("lost authored endpoint annotation on %s", e.ID)
						}
						boxes[k] = geo.Box{TopLeft: e.GetArrowheadLabelPosition(isDst), Width: float64(text.LabelWidth), Height: float64(text.LabelHeight)}
						for _, shape := range d.Shapes {
							obstacle := geo.Box{TopLeft: geo.NewPoint(float64(shape.Pos.X), float64(shape.Pos.Y)), Width: float64(shape.Width), Height: float64(shape.Height)}
							if obstacle.Overlaps(boxes[k]) {
								t.Errorf("endpoint annotation %q overlaps %s", text.Label, shape.ID)
							}
						}
						other := d.Connections[1-k].Route
						for n := 1; n < len(other); n++ {
							if boxes[k].Intersects(geo.Segment{Start: other[n-1], End: other[n]}, -0.01) || boxes[k].Contains(other[n]) {
								t.Errorf("endpoint annotation %q overlaps the other route", text.Label)
							}
						}
					}
					if boxes[0].Overlaps(boxes[1]) {
						t.Fatal("rendered endpoint annotations overlap")
					}
				})
			}
		}
	}
}
