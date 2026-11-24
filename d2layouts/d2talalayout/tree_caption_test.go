package d2talalayout_test

import (
	"fmt"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func TestTreeCaptionsClearNeighboringRanksAndBranches(t *testing.T) {
	for _, direction := range []string{"down", "up", "left", "right"} {
		for _, side := range []string{"outside-right-center", "outside-left-top", "outside-bottom-center", "outside-top-right"} {
			t.Run(direction+"_"+side, func(t *testing.T) {
				d := compileRoutingDiagram(t, t.Context(), fmt.Sprintf(`direction: %s
Root: Intake schedule {width: 95; height: 64; label.near: %[2]s}
A: Main authorization decision {width: 50; height: 63; label.near: %[2]s}
B: Manual exception handling {width: 76; height: 53; label.near: %[2]s}
C: Confirm receipt {width: 62; height: 69; label.near: %[2]s}
D: Record exception {width: 58; height: 58; label.near: %[2]s}
E: Dispatch workflow {width: 87; height: 50; label.near: %[2]s}
Root -> A
Root -> B
Root -> C
A -> D
A -> E`, direction, side), 1, &d2svg.RenderOpts{})

				if len(d.Shapes) != 6 || len(d.Connections) != 5 {
					t.Fatalf("got %d nodes and %d edges", len(d.Shapes), len(d.Connections))
				}
				type bounds struct{ body, caption geo.Box }
				var boxes []bounds
				for _, s := range d.Shapes {
					body := geo.Box{TopLeft: geo.NewPoint(float64(s.Pos.X), float64(s.Pos.Y)), Width: float64(s.Width), Height: float64(s.Height)}
					pos := label.FromString(s.LabelPosition)
					if !pos.IsOutside() {
						t.Fatalf("lost explicit label on %s: %s", s.ID, s.LabelPosition)
					}
					w, h := float64(s.LabelWidth), float64(s.LabelHeight)
					caption := geo.Box{TopLeft: pos.GetPointOnBox(&body, label.PADDING, w, h), Width: w, Height: h}
					for i, other := range boxes {
						if caption.Overlaps(other.body) || body.Overlaps(other.caption) || caption.Overlaps(other.caption) {
							t.Fatalf("label collision between %s and %s", s.ID, d.Shapes[i].ID)
						}
					}
					boxes = append(boxes, bounds{body, caption})
				}
			})
		}
	}
}
