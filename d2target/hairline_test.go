package d2target

import (
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

func TestHairlineRoutePaintGeometry(t *testing.T) {
	for _, tc := range []struct {
		name        string
		width       int
		curve       bool
		route, want []geo.Point
	}{
		{"elbows and mixed phases", 1, false,
			[]geo.Point{{X: 5, Y: 10}, {X: 15, Y: 10}, {X: 15, Y: 20.5}, {X: 30, Y: 20.5}},
			[]geo.Point{{X: 5, Y: 10.5}, {X: 15.5, Y: 10.5}, {X: 15.5, Y: 20.5}, {X: 30, Y: 20.5}}},
		{"straight Dagre cubic", 1, true,
			[]geo.Point{{X: -2, Y: 0}, {X: -2, Y: 20}, {X: -2, Y: 30}, {X: -2, Y: 50}},
			[]geo.Point{{X: -1.5, Y: 0}, {X: -1.5, Y: 20}, {X: -1.5, Y: 30}, {X: -1.5, Y: 50}}},
		{"ordinary width", 2, false, []geo.Point{{X: 0, Y: 10}, {X: 50, Y: 10}}, nil},
		{"already centered", 1, false, []geo.Point{{X: .5, Y: 0}, {X: .5, Y: 80}}, nil},
		{"straight rail with repeated controls and diagonal branch", 1, false,
			[]geo.Point{{X: 0, Y: 0}, {X: 20, Y: 0}, {X: 20, Y: 0}, {X: 40, Y: 20}},
			[]geo.Point{{X: 0, Y: .5}, {X: 20, Y: .5}, {X: 20, Y: .5}, {X: 40, Y: 20}}},
		{"diagonal", 1, false, []geo.Point{{X: 0, Y: 0}, {X: 40, Y: 20}}, nil},
		{"subpixel jog", 1, false, []geo.Point{{X: 0, Y: 0}, {X: 20, Y: 0}, {X: 20, Y: .25}, {X: 40, Y: .25}}, nil},
		{"straight cubic followed by a curve", 1, true,
			[]geo.Point{{X: 0}, {X: 10}, {X: 20}, {X: 30}, {X: 40, Y: 5}, {X: 50, Y: 3}, {X: 60}},
			[]geo.Point{{X: 0, Y: .5}, {X: 10, Y: .5}, {X: 20, Y: .5}, {X: 30, Y: .5}, {X: 40, Y: 5}, {X: 50, Y: 3}, {X: 60}}},
		{"curved cubic", 1, true, []geo.Point{{X: 0, Y: 0}, {X: 20, Y: 0}, {X: 20, Y: 20}, {X: 40, Y: 20}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Connection{StrokeWidth: tc.width, IsCurve: tc.curve}
			for _, p := range tc.route {
				c.Route = append(c.Route, p.Copy())
			}
			d := &Diagram{Connections: []Connection{c}}
			paint := d.WithAlignedHairlineRoutes()
			got := paint.Connections[0]
			want := tc.want
			if want == nil {
				want = tc.route
			}
			for i, p := range got.Route {
				if *p != want[i] || *c.Route[i] != tc.route[i] {
					t.Fatalf("point %d: paint %v, layout %v; want %v, %v", i, *p, *c.Route[i], want[i], tc.route[i])
				}
			}
			if again := paint.WithAlignedHairlineRoutes(); again != paint {
				t.Fatal("aligning the painted geometry twice changed its phase")
			}
		})
	}
}
