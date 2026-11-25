package d2talalayout_test

import (
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func TestOrdinaryTerminalClearsOutsideLabel(t *testing.T) {
	d := compileRoutingDiagram(t, t.Context(), `Gateway: Event dispatch { label.near: outside-top-left }
Observer -> Gateway
Writer -> Gateway
Gateway -> Observer
Gateway -> Queue
`, 3, &d2svg.RenderOpts{})
	gateway := d.Shapes[0]
	if gateway.ID != "Gateway" {
		t.Fatal("missing Gateway")
	}
	body := geo.NewBox(geo.NewPoint(float64(gateway.Pos.X), float64(gateway.Pos.Y)), float64(gateway.Width), float64(gateway.Height))
	w, h := float64(gateway.LabelWidth), float64(gateway.LabelHeight)
	box := geo.NewBox(label.FromString(gateway.LabelPosition).GetPointOnBox(body, label.PADDING, w, h), w, h)
	edge := d.Connections[1]
	if edge.Src != "Writer" || edge.Dst != "Gateway" || len(edge.Route) != 2 || edge.Route[0].X != edge.Route[1].X {
		t.Fatalf("expected straight Writer to Gateway arrival, got %s %v", edge.ID, edge.Route)
	}
	// The marker and shaft share the same x coordinate. The default triangle
	// occupies less than 8px on either side of its tip.
	x := edge.Route[1].X
	if x+8 > box.TopLeft.X && x-8 < box.TopLeft.X+box.Width {
		t.Fatalf("arrival at x=%g cuts outside label %v", x, box)
	}
}

func TestOrdinaryIncidentLabelClearance(t *testing.T) {
	for _, tc := range []struct {
		name, source              string
		seed                      int64
		first, second             int
		firstTarget, secondTarget bool
	}{
		{"single loop", `Observer -> Store: {target-arrowhead.label: reply }
Writer -> Store: {target-arrowhead.label: ack }
Store -> Observer: {target-arrowhead.label: ack }
Store -> Queue
Store -> Store: {target-arrowhead.label: recover}
`, 3, 1, 4, true, true},
		{"two incident labels", `Client -> Gateway: {target-arrowhead.label: timeout }
Gateway -> Cache: {source-arrowhead.label: ack }
Cache -> Gateway: {target-arrowhead.label: reply }
Gateway -> Queue: {target-arrowhead.label: ack }
Queue -> Worker: {source-arrowhead.label: completed }
Worker -> Gateway
Gateway -> Client: {source-arrowhead.label: request }
Gateway -> Gateway: backoff
Gateway -> Gateway: {target-arrowhead.label: recover}
`, 2, 0, 1, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := compileRoutingDiagram(t, t.Context(), tc.source, tc.seed, &d2svg.RenderOpts{})
			labelBox := func(edge d2target.Connection, target bool) *geo.Box {
				t.Helper()
				text := edge.SrcLabel
				if target {
					text = edge.DstLabel
				}
				if text == nil {
					t.Fatalf("missing annotation on %s", edge.ID)
				}
				return geo.NewBox(edge.GetArrowheadLabelPosition(target), float64(text.LabelWidth), float64(text.LabelHeight))
			}
			first, second := d.Connections[tc.first], d.Connections[tc.second]
			a, b := labelBox(first, tc.firstTarget), labelBox(second, tc.secondTarget)
			if a.Overlaps(*b) {
				t.Fatalf("overlapping annotations on %s and %s: %v, %v", first.ID, second.ID, a, b)
			}
			for i := 1; i < len(first.Route); i++ {
				if b.Intersects(geo.Segment{Start: first.Route[i-1], End: first.Route[i]}, 0) {
					t.Fatalf("%s runs through the annotation on %s", first.ID, second.ID)
				}
			}
		})
	}
}
