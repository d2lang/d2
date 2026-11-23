package d2target

import (
	"math"
	"slices"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

func TestConnectionCrossings(t *testing.T) {
	line := func(id string, x1, y1, x2, y2 float64) Connection {
		c := *BaseConnection()
		c.ID = id
		c.Stroke = "#123456"
		c.Src, c.Dst = id+"-src", id+"-dst"
		c.Route = []*geo.Point{{X: x1, Y: y1}, {X: x2, Y: y2}}
		return c
	}
	horizontal := line("horizontal", 0, 50, 100, 50)
	vertical := line("vertical", 50, 0, 50, 100)
	arrow := horizontal
	arrow.DstArrow = TriangleArrowhead
	corner := *BaseConnection()
	corner.ID = "corner"
	corner.Stroke = horizontal.Stroke
	corner.BorderRadius = 30
	corner.Route = []*geo.Point{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}}
	highPriority := horizontal
	highPriority.ZIndex = 1
	invisible := vertical
	invisible.Opacity = 0
	unpainted := vertical
	unpainted.Stroke = ""
	// The 1px stroke inset moves the cubic endpoints to y=21. At t=1/4 and
	// t=3/4 its off-chord intersections are exactly y=65.4375.
	arch := line("arch", 0, 20, 100, 20)
	arch.IsCurve = true
	arch.Route = []*geo.Point{{X: 0, Y: 20}, {X: 0, Y: 100}, {X: 100, Y: 100}, {X: 100, Y: 20}}

	tangent := arch
	tangent.Route = []*geo.Point{{X: 0, Y: 20}, {X: 10, Y: 50}, {X: 25, Y: 80}, {X: 50, Y: 80}, {X: 75, Y: 80}, {X: 90, Y: 50}, {X: 100, Y: 20}}

	for _, tt := range []struct {
		name  string
		edges []Connection
		under int
		want  []geo.Point
	}{
		{"orthogonal", []Connection{horizontal, vertical}, 0, []geo.Point{{X: 50, Y: 50}}},
		{"z-index overrides declaration order", []Connection{highPriority, vertical}, 1, []geo.Point{{X: 50, Y: 50}}},
		{"two crossings on a cubic", []Connection{arch, line("rail", -10, 65.4375, 110, 65.4375)}, 0, []geo.Point{{X: 15.625, Y: 65.4375}, {X: 84.375, Y: 65.4375}}},
		{"shared endpoint", []Connection{horizontal, line("branch", 100, 50, 150, 100)}, 0, nil},
		{"T junction", []Connection{horizontal, line("branch", 50, 50, 50, 100)}, 0, nil},
		{"overlapping strokes", []Connection{horizontal, line("overlap", 20, 50, 120, 50)}, 0, nil},
		{"tangent to a cubic", []Connection{tangent, line("tangent", -10, 80, 110, 80)}, 0, nil},
		{"near miss", []Connection{arch, line("outside", -10, 85, 110, 85)}, 0, nil},
		{"near arrowhead", []Connection{arrow, line("neighbor", 93, 0, 93, 100)}, 0, nil},
		{"clear of rounded bend", []Connection{corner, line("neighbor", 95, -80, 95, 5)}, 0, nil},
		{"upper edge without stroke paint", []Connection{horizontal, unpainted}, 0, nil},
		{"invisible upper edge", []Connection{horizontal, invisible}, 0, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gaps := ConnectionCrossings(tt.edges, nil)
			for i := range tt.edges {
				var want []geo.Point
				if i == tt.under {
					want = tt.want
				}
				var got []ConnectionGap
				if i < len(gaps) {
					got = gaps[i]
				}
				if len(got) != len(want) {
					t.Fatalf("gaps on %s = %v, want %v", tt.edges[i].ID, got, want)
				}
				for _, point := range want {
					found := slices.ContainsFunc(got, func(gap ConnectionGap) bool {
						return math.Hypot(gap.Center.X-point.X, gap.Center.Y-point.Y) < 0.5 && gap.Radius > float64(tt.edges[i].StrokeWidth)/2
					})
					if !found {
						t.Errorf("missing gap at %v on %s: %v", point, tt.edges[i].ID, got)
					}
				}
			}
		})
	}
}
