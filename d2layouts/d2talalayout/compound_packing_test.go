package d2talalayout_test

import (
	"fmt"
	"testing"
)

func TestCompoundUsesAvailableSpaceOnRequestedAxis(t *testing.T) {
	for _, tc := range []struct {
		axis                     string
		requested, width, height int
	}{
		{"width", 880, 880, 180},
		{"height", 640, 200, 640},
	} {
		t.Run(tc.axis, func(t *testing.T) {
			source := fmt.Sprintf("Regions: { %s: %d\n", tc.axis, tc.requested)
			for _, id := range []string{"West", "East", "EMEA", "APAC", "LATAM", "Oceania"} {
				source += id + ": {width: 80; height: 60}\n"
			}
			d := compileRoutingDiagram(t, t.Context(), source+"}", 1, nil)
			if len(d.Shapes) != 7 {
				t.Fatalf("expected a container and six tiles, got %d shapes", len(d.Shapes))
			}
			parent := d.Shapes[0]
			if parent.Width != tc.width || parent.Height != tc.height {
				t.Errorf("container = %d × %d, want %d × %d", parent.Width, parent.Height, tc.width, tc.height)
			}
			first := d.Shapes[1].Pos
			for _, s := range d.Shapes[2:] {
				if tc.axis == "width" && s.Pos.Y != first.Y || tc.axis == "height" && s.Pos.X != first.X {
					t.Errorf("%s wrapped to a second line at %v", s.ID, s.Pos)
				}
			}
		})
	}
}
