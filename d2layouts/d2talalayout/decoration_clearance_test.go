package d2talalayout_test

import (
	"testing"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func TestFusedStepCaptionsUseEmptySpaceWithinFrame(t *testing.T) {
	const source = `Payments: {
 direction: right
 Intake: Validate incoming payment {shape: step; width: 120; height: 60}
 Review: Check settlement window {shape: step; width: 120; height: 60}
 Release: Release payment to merchant {shape: step; width: 120; height: 60}
 Record: Record transaction state {shape: step; width: 120; height: 60}
 Archive: Archive customer ledger {shape: step; width: 120; height: 60}
 Intake -> Review -> Release -> Record -> Archive
}`
	d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	tl, br := d.BoundingBox()
	if len(d.Connections) != 0 || len(d.Shapes) != 6 || br.X-tl.X != 647 || br.Y-tl.Y != 182 {
		t.Fatalf("expected five fused steps in their 647 × 182 frame; got %v to %v", tl, br)
	}
	var captions []geo.Box
	for _, n := range d.Shapes[1:] {
		body := geo.NewBox(geo.NewPoint(float64(n.Pos.X), float64(n.Pos.Y)), float64(n.Width), float64(n.Height))
		w, h := float64(n.LabelWidth), float64(n.LabelHeight)
		p := label.FromString(n.LabelPosition).GetPointOnBox(body, label.PADDING, w, h)
		current := geo.NewBox(p, w, h)
		for _, prev := range captions {
			if current.Overlaps(prev) {
				t.Fatalf("caption %s at %v overlaps another step caption at %v", n.ID, current, prev)
			}
		}
		captions = append(captions, *current)
	}
}

func TestFixedIconUsesAvailableSpaceForAutomaticCaption(t *testing.T) {
	const nodes = `Lead: Settlement owner {shape: person; left: 70; top: 30; width: 80; height: 70; style.font-size: 14}
Gate: Approval {left: 60; top: 145; width: 105; height: 60; icon: "https://example.org/gate.svg"; icon.near: outside-top-center}`
	for _, tc := range []struct {
		name, source, position string
		width, height          int
	}{
		{"space inside frame", "Office: Review {\n" + nodes + "\n}", "OUTSIDE_TOP_CENTER", 299, 327},
		{"no space above board", nodes, "OUTSIDE_BOTTOM_CENTER", 113, 177},
		{"fixed caption", "Office: Review {\n" + nodes + "\nLead.label.near: outside-bottom-center\n}", "OUTSIDE_BOTTOM_CENTER", 299, 327},
		{"visible connection", "Office: Review {\n" + nodes + "\nGate -> Archive\n}", "OUTSIDE_BOTTOM_CENTER", 457, 330},
		{"external near connection", "Office: Review {\n" + nodes + "\n}\nExtras: {near: top-center}\nOffice.Lead -> Extras", "OUTSIDE_BOTTOM_CENTER", 299, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := compileRoutingDiagram(t, t.Context(), tc.source, 1, nil)
			checked := 0
			for _, n := range d.Shapes {
				switch n.ID {
				case "Office.Lead", "Lead":
					checked++
					if n.LabelPosition != tc.position {
						t.Fatalf("Lead caption %s; want %s", n.LabelPosition, tc.position)
					}
				case "Office.Gate", "Gate":
					checked++
					if n.IconPosition != "OUTSIDE_TOP_CENTER" {
						t.Fatalf("fixed icon moved to %s", n.IconPosition)
					}
				}
			}
			if checked != 2 {
				t.Fatal("missing caption or icon owner")
			}
			tl, br := d.BoundingBox()
			if br.X-tl.X != tc.width || br.Y-tl.Y != tc.height {
				t.Fatalf("board is %d × %d; want %d × %d", br.X-tl.X, br.Y-tl.Y, tc.width, tc.height)
			}
		})
	}
}
