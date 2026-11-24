package d2talalayout_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/internal/testlog"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/log"
)

// Check the text and shafts seen by consumers after translation, route
// direction restoration and the renderer's integer label positioning.
func TestStraightBundleLabelClearance(t *testing.T) {
	for _, direction := range []string{"up", "down", "left", "right", "down-filled"} {
		t.Run(direction, func(t *testing.T) {
			ctx := log.With(t.Context(), testlog.New(t))
			source := "direction: " + strings.TrimSuffix(direction, "-filled") + "\nshape: hierarchy\n" + strings.Repeat(`a -> b: "first label\nsecond line\nthird line\nfourth line"`+"\n", 3)
			if direction == "down-filled" {
				source += "(a -> b)[*].style.fill: pink\n"
			}
			diagram := compileRoutingDiagram(t, ctx, source, 1, nil)
			for i, e := range diagram.Connections {
				if len(e.Route) != 2 {
					t.Fatalf("%s gained a bend: %v", e.ID, e.Route)
				}
				// D2 masks the text plus 2px on each side. A filled label
				// paints 4px on each side and 3px above and below the text.
				padX, padY := 2., 0.
				if direction == "down-filled" {
					if e.Fill != "pink" {
						t.Fatalf("%s lost its pink fill", e.ID)
					}
					padX, padY = 4, 3
				}
				topLeft := e.GetLabelTopLeft()
				box := geo.NewBox(geo.NewPoint(topLeft.X-padX, topLeft.Y-padY), float64(e.LabelWidth)+2*padX, float64(e.LabelHeight)+2*padY)
				for j, other := range diagram.Connections {
					if i == j {
						continue
					}
					p, q := other.Route[0], other.Route[1]
					wire := geo.NewBox(geo.NewPoint(min(p.X, q.X)-1, min(p.Y, q.Y)-1), max(p.X, q.X)-min(p.X, q.X)+2, max(p.Y, q.Y)-min(p.Y, q.Y)+2)
					if box.Overlaps(*wire) || box.Overlaps(*geo.NewBox(other.GetLabelTopLeft(), float64(other.LabelWidth), float64(other.LabelHeight))) {
						t.Fatalf("%s label overlaps %s", e.ID, other.ID)
					}
				}
			}
		})
	}
}

func TestReciprocalEndpointLabelClearance(t *testing.T) {
	for _, prefix := range []string{"", "shape: hierarchy\n"} {
		ctx := log.With(t.Context(), testlog.New(t))
		d := compileRoutingDiagram(t, ctx, prefix+"Gateway -> Worker: {source-arrowhead.label: request}\nWorker -> Gateway: {target-arrowhead.label: reply}", 1, nil)
		request, reply := d.Connections[0], d.Connections[1]
		if request.Src != "Gateway" || reply.Dst != "Gateway" || request.SrcLabel.Label != "request" || reply.DstLabel.Label != "reply" || len(request.Route) != 2 || len(reply.Route) != 2 {
			t.Fatalf("unexpected reciprocal edges: %v", d.Connections)
		}
		a := geo.NewBox(request.GetArrowheadLabelPosition(false), float64(request.SrcLabel.LabelWidth), float64(request.SrcLabel.LabelHeight))
		b := geo.NewBox(reply.GetArrowheadLabelPosition(true), float64(reply.DstLabel.LabelWidth), float64(reply.DstLabel.LabelHeight))
		if a.Overlaps(*b) {
			t.Fatalf("reciprocal labels overlap: %v, %v", a, b)
		}
	}
}
