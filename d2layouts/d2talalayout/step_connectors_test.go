package d2talalayout_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

func TestStepConnectorClearsAnchoredDecoration(t *testing.T) {
	for _, tc := range []struct{ name, text, placement, paint string }{
		{"caption", "Middle", "label.near: outside-right-center", ""},
		{"icon", "Middle", "icon: status.svg; icon.near: outside-right-center", ""},
		{"nearly opaque successor", "Middle", "label.near: outside-right-center", "; style.opacity: 0.99"},
		{"leading spaces", "  Review", "label.near: outside-right-center", "; style.fill: white"},
		{"collapsed space", "Middle  step", "label.near: outside-right-center", "; style.fill: white"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := fmt.Sprintf("direction: right\nA: First {shape: step; width: 120; height: 65}\nB: %q {shape: step; width: 120; height: 65; %s}\nC: Last {shape: step; width: 120; height: 65%s}\nA -> B -> C", tc.text, tc.placement, tc.paint)
			d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
			if len(d.Connections) != 2 || len(d.Shapes) != 3 {
				t.Fatalf("got %d steps, %d connections", len(d.Shapes), len(d.Connections))
			}
			left, right := d.Shapes[1], d.Shapes[2]
			route, untouched := d.Connections[1].Route, d.Connections[0].Route
			if len(route) != 2 || route[0].Y != route[1].Y || len(untouched) != 2 || untouched[0].Y != float64(left.Pos.Y+left.Height/2) {
				t.Fatalf("expected two straight connectors and a centered first connector: %v, %v", untouched, route)
			}
			box := renderedTileCaption(left)
			if left.Icon != nil {
				body := geo.NewBox(geo.NewPoint(float64(left.Pos.X), float64(left.Pos.Y)), float64(left.Width), float64(left.Height))
				size := float64(d2target.GetIconSize(body, left.IconPosition))
				box = *geo.NewBox(label.FromString(left.IconPosition).GetPointOnBox(body, label.PADDING, size, size), size, size)
			}
			shaft := geo.NewBox(geo.NewPoint(route[0].X, route[0].Y-1), route[1].X-route[0].X, 2)
			wantLength := float64(right.Pos.X-(left.Pos.X+left.Width)) + shape.StepWedgeWidth(float64(left.Width))
			if shaft.Overlaps(box) || shaft.Width != wantLength {
				t.Fatalf("wire %v crosses %v or changed length (want %v)", shaft, box, wantLength)
			}
			// Follow the painted chevron at this ordinate, including export rounding.
			dy := route[0].Y - float64(left.Pos.Y)
			wedge := shape.StepWedgeWidth(float64(left.Width))
			offset := wedge * (1 - math.Abs(2*dy/float64(left.Height)-1))
			if math.Abs(route[0].X-(float64(left.Pos.X+left.Width)-wedge+offset)) > 1 || math.Abs(route[1].X-(float64(right.Pos.X)+offset)) > 1 {
				t.Fatalf("endpoints left the step outlines: %v", route)
			}
		})
	}
}

func TestUncertainStepCaptionsRetainClearCenterline(t *testing.T) {
	for _, text := range []string{"  .", "  ˣ", "    ", "Review\nrequest"} {
		source := fmt.Sprintf("direction: right\nA: First {shape: step; width: 120; height: 90}\nB: %q {shape: step; width: 120; height: 90; label.near: outside-right-center}\nC: Last {shape: step; width: 200; height: 90}\nA -> B -> C\n(A -> B)[0].style.stroke-width: 2", text)
		d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
		if len(d.Connections) != 2 {
			t.Fatalf("%q: expected explicit connections", text)
		}
		route := d.Connections[1].Route
		if len(route) != 2 || route[0].Y != float64(d.Shapes[1].Pos.Y)+float64(d.Shapes[1].Height)/2 || route[1].Y != route[0].Y {
			t.Errorf("%q: clear centerline moved: %v", text, route)
		}
	}
}
