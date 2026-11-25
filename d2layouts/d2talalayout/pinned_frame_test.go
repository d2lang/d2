package d2talalayout_test

import (
	"fmt"
	"testing"
)

func TestPinnedFrameClearsNeighborWithItsAuthoredHeight(t *testing.T) {
	const source = `Panel: Service {left: 200; top: 0; height: 400
  A: Intake {width: 90; height: 48}
  B: Audit {width: 90; height: 48}
  A -> B
}
Store: Partner {left: %d; top: 40; width: 90; height: 48}`
	for _, tc := range []struct {
		name                  string
		neighborX, frameWidth int
	}{
		{"overlapping", 500, 210},
		{"already clear", 700, 390},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := compileRoutingDiagram(t, t.Context(), fmt.Sprintf(source, tc.neighborX), 1, nil)
			frame, a, b, store := d.Shapes[0], d.Shapes[1], d.Shapes[2], d.Shapes[3]
			if frame.Pos.X != 200 || frame.Pos.Y != 0 || store.Pos.X != tc.neighborX || store.Pos.Y != 40 ||
				frame.Width != tc.frameWidth || frame.Height != 400 || frame.Pos.X+frame.Width+20 > store.Pos.X {
				t.Fatalf("frame %v %d×%d, neighboring pin %v", frame.Pos, frame.Width, frame.Height, store.Pos)
			}
			r := d.Connections[0].Route
			if len(r) != 2 {
				t.Fatalf("expected a straight connection, got %v", r)
			}
			if tc.name == "already clear" {
				if r[1].X-r[0].X != 90 || r[1].Y != r[0].Y || a.Pos.Y != b.Pos.Y {
					t.Fatalf("clear row changed orientation or edge length: %v", r)
				}
			} else if r[1].Y-r[0].Y != 90 || r[0].X != float64(a.Pos.X)+float64(a.Width)/2 ||
				r[1].X != float64(b.Pos.X)+float64(b.Width)/2 || r[0].Y != float64(a.Pos.Y+a.Height) || r[1].Y != float64(b.Pos.Y) {
				t.Fatalf("edge lost its 90 px length or its centered ports: %v", r)
			}
		})
	}
}
