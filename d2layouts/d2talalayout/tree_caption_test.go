package d2talalayout_test

import (
	"fmt"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
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

func TestTreeSpacingClearsOpposingOutsideIcons(t *testing.T) {
	for _, direction := range []string{"down", "up", "right", "left"} {
		first, second := "right-center", "left-center"
		if direction == "right" || direction == "left" {
			first, second = "bottom-center", "top-center"
		}
		source := fmt.Sprintf(`direction: %s
Review -> Approve
Review -> Reject
Review -> Hold
Approve: {width: 96; height: 96; icon: status.svg; icon.near: outside-%s}
Reject: {width: 96; height: 96; icon: status.svg; icon.near: outside-%s}
Hold: {width: 96; height: 96; icon: status.svg; icon.near: outside-%s}`, direction, first, second, second)
		d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
		var bodies, icons []geo.Box
		for _, s := range d.Shapes {
			body := geo.Box{TopLeft: geo.NewPoint(float64(s.Pos.X), float64(s.Pos.Y)), Width: float64(s.Width), Height: float64(s.Height)}
			bodies = append(bodies, body)
			if s.Icon != nil {
				size := float64(d2target.GetIconSize(&body, s.IconPosition))
				topLeft := label.FromString(s.IconPosition).GetPointOnBox(&body, label.PADDING, size, size)
				icons = append(icons, geo.Box{TopLeft: topLeft, Width: size, Height: size})
			}
		}
		if len(icons) != 3 {
			t.Fatalf("%s: got %d icons", direction, len(icons))
		}
		for i, icon := range icons {
			for _, body := range bodies {
				if icon.Overlaps(body) {
					t.Fatalf("%s: exterior icon overlaps a node", direction)
				}
			}
			for _, other := range icons[i+1:] {
				if icon.Overlaps(other) {
					t.Fatalf("%s: exterior icons overlap", direction)
				}
			}
		}
	}
}

func TestTreeVerticalBranchSpacingUsesUprightEndpointCaptions(t *testing.T) {
	for _, direction := range []string{"down", "up"} {
		height := func(annotation string) int {
			source := fmt.Sprintf(`direction: %s
Intake -> Review: {source-arrowhead: %s; target-arrowhead: %s}
Intake -> Audit
Intake -> Archive`, direction, annotation, annotation)
			d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
			var minTop, maxBottom = d.Shapes[0].Pos.Y, d.Shapes[0].Pos.Y + d.Shapes[0].Height
			for _, s := range d.Shapes {
				minTop = min(minTop, s.Pos.Y)
				maxBottom = max(maxBottom, s.Pos.Y+s.Height)
			}
			return maxBottom - minTop
		}
		if short, long := height("Sent"), height("Submitted for settlement"); short != long {
			t.Errorf("%s: one-line endpoint captions change the tree height from %d to %d", direction, short, long)
		}
	}
}

func TestTreeEndpointCaptionClearsTallerSibling(t *testing.T) {
	for _, direction := range []string{"down", "up"} {
		t.Run(direction, func(t *testing.T) {
			d := compileRoutingDiagram(t, t.Context(), fmt.Sprintf(`direction: %s
Intake -> Review: {source-arrowhead: Confirmed by server; target-arrowhead: Submitted for settlement}
Intake -> Audit
Intake -> Archive
Audit: {width: 200; height: 160}`, direction), 1, nil)
			edge := d.Connections[0]
			if edge.Dst != "Review" || edge.DstLabel == nil {
				t.Fatal("missing Review endpoint caption")
			}
			caption := geo.NewBox(edge.GetArrowheadLabelPosition(true), float64(edge.DstLabel.LabelWidth), float64(edge.DstLabel.LabelHeight))
			for _, n := range d.Shapes {
				body := geo.NewBox(geo.NewPoint(float64(n.Pos.X), float64(n.Pos.Y)), float64(n.Width), float64(n.Height))
				if caption.Overlaps(*body) {
					t.Fatalf("Review caption overlaps %s", n.ID)
				}
			}
		})
	}
}
