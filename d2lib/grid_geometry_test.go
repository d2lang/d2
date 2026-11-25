package d2lib

import (
	"fmt"
	"math"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

func TestGridAspectConstrainedShapes(t *testing.T) {
	for _, tc := range []struct {
		layout     string
		horizontal bool
	}{
		{"grid-rows: 1", true},
		{"grid-columns: 1", false},
		{"grid-rows: 1; grid-columns: 5", true},
		{"grid-columns: 1; grid-rows: 5", false},
	} {
		t.Run(tc.layout, func(t *testing.T) {
			d, _ := compileGridMotionTest(t, tc.layout+`
c: "" {shape: circle; width: 60; height: 60}
s: "" {shape: square; width: 60; height: 60}
p: "" {shape: person; width: 60; height: 60}
o: "" {shape: oval; width: 60; height: 60}
peer: "" {width: 400; height: 400}
`)
			peer := shapeByID(t, d, "peer")
			for _, item := range []struct {
				id        string
				stretched int
			}{{"c", 60}, {"s", 60}, {"p", 90}, {"o", 180}} {
				s := shapeByID(t, d, item.id)
				w, h := 60, item.stretched
				center, trackCenter := s.Pos.Y*2+s.Height, peer.Pos.Y*2+peer.Height
				if !tc.horizontal {
					w, h = h, w
					center, trackCenter = s.Pos.X*2+s.Width, peer.Pos.X*2+peer.Width
				}
				if s.Width != w || s.Height != h || center != trackCenter {
					t.Errorf("%s: got %dx%d at %+v, want %dx%d centered on the peer", item.id, s.Width, s.Height, s.Pos, w, h)
				}
			}
		})
	}
}

func TestGridStretchedDiamondKeepsChildrenAndRouteInside(t *testing.T) {
	for _, axis := range []string{"grid-rows: 1", "grid-columns: 1"} {
		t.Run(axis, func(t *testing.T) {
			d, _ := compileGridMotionTest(t, axis+`
pod: "" {
  shape: diamond
  grid-rows: 1
  grid-gap: 10
  a: "" {width: 30; height: 40}
  b: "" {width: 30; height: 40}
  a -> b
}
peer: "" {width: 400; height: 400}
`)
			pod := shapeByID(t, d, "pod")
			a, b := shapeByID(t, d, "pod.a"), shapeByID(t, d, "pod.b")
			for _, cell := range []string{"pod.a", "pod.b"} {
				s := shapeByID(t, d, cell)
				for _, x := range []int{s.Pos.X, s.Pos.X + s.Width} {
					for _, y := range []int{s.Pos.Y, s.Pos.Y + s.Height} {
						dx := float64(2*(x-pod.Pos.X))/float64(pod.Width) - 1
						dy := float64(2*(y-pod.Pos.Y))/float64(pod.Height) - 1
						if math.Abs(dx)+math.Abs(dy) > 1.000001 {
							t.Fatalf("%s corner (%d,%d) lies outside diamond %+v %dx%d", cell, x, y, pod.Pos, pod.Width, pod.Height)
						}
					}
				}
			}
			route := d.Connections[0].Route
			start, end := route[0], route[len(route)-1]
			if start.X != float64(a.Pos.X+a.Width) || start.Y != float64(a.Pos.Y)+float64(a.Height)/2 ||
				end.X != float64(b.Pos.X) || end.Y != float64(b.Pos.Y)+float64(b.Height)/2 {
				t.Fatalf("route %v -> %v detached from children at %+v and %+v", *start, *end, a.Pos, b.Pos)
			}
		})
	}
}

func TestGridContainerDimensionsEncloseChildren(t *testing.T) {
	for _, tc := range []struct{ axis, dim string }{{"grid-rows", "width"}, {"grid-columns", "height"}} {
		for _, requested := range []int{100, 200, 320} {
			name := fmt.Sprintf("%s_%d", tc.dim, requested)
			t.Run(name, func(t *testing.T) {
				d, _ := compileGridMotionTest(t, fmt.Sprintf(`
%[1]s: 1
panel: "" {
  %[1]s: 1
  grid-gap: 0
  %[2]s: %[3]d
  a: "" {width: 100; height: 100}
  b: "" {width: 100; height: 100}
}
next: "" {width: 100; height: 100}
`, tc.axis, tc.dim, requested))
				panel, b, next := shapeByID(t, d, "panel"), shapeByID(t, d, "panel.b"), shapeByID(t, d, "next")
				size, panelStart, bEnd, nextStart := panel.Width, panel.Pos.X, b.Pos.X+b.Width, next.Pos.X
				if tc.dim == "height" {
					size, panelStart, bEnd, nextStart = panel.Height, panel.Pos.Y, b.Pos.Y+b.Height, next.Pos.Y
				}
				if size != max(requested, 200) || bEnd > panelStart+size || nextStart-(panelStart+size) != 40 {
					t.Fatalf("panel %d..%d, last child ends at %d, next starts at %d", panelStart, panelStart+size, bEnd, nextStart)
				}
			})
		}
	}
}

func TestGridBorderLabelsRetainGap(t *testing.T) {
	for _, tc := range []struct{ axis, first, second string }{
		{"grid-rows", "border-right-center", "border-left-center"},
		{"grid-columns", "border-bottom-center", "border-top-center"},
	} {
		for _, gap := range []int{0, 40} {
			t.Run(fmt.Sprintf("%s_%d", tc.axis, gap), func(t *testing.T) {
				d, _ := compileGridMotionTest(t, fmt.Sprintf(`
%s: 1
grid-gap: %d
a: Primary Region {width: 140; height: 60; label.near: %s}
b: Backup Region {width: 140; height: 60; label.near: %s}
`, tc.axis, gap, tc.first, tc.second))
				bounds := func(id string) *geo.Box {
					s := shapeByID(t, d, id)
					box := geo.NewBox(geo.NewPoint(float64(s.Pos.X), float64(s.Pos.Y)), float64(s.Width), float64(s.Height))
					p := label.FromString(s.LabelPosition).GetPointOnBox(box, label.PADDING, float64(s.LabelWidth), float64(s.LabelHeight))
					return geo.NewBox(p, float64(s.LabelWidth), float64(s.LabelHeight))
				}
				a, b := bounds("a"), bounds("b")
				clearance := b.TopLeft.X - (a.TopLeft.X + a.Width)
				if tc.axis == "grid-columns" {
					clearance = b.TopLeft.Y - (a.TopLeft.Y + a.Height)
				}
				if clearance < float64(gap) {
					t.Fatalf("labels have %gpx clearance, want at least %d", clearance, gap)
				}
			})
		}
	}
}

func TestGridC4PersonContainsChild(t *testing.T) {
	for _, axis := range []string{"grid-rows", "grid-columns"} {
		for _, tc := range []struct {
			name, attr string
			w, h       int
		}{
			{"wide child", "", 80, 40},
			{"tall child", "", 40, 160},
			{"authored wide frame", "width: 350", 80, 40},
		} {
			t.Run(axis+"/"+tc.name, func(t *testing.T) {
				_, g := compileGridMotionTest(t, fmt.Sprintf(`
%s: 1
pod: "" {
  shape: c4-person
  grid-rows: 1
  grid-gap: 0
  %s
  child: "" {width: %d; height: %d}
}
peer: "" {width: 500; height: 500}`, axis, tc.attr, tc.w, tc.h))
				pod := g.Root.Children["pod"]
				child := pod.Children["child"]
				inside := pod.ToShape().GetInnerBox()
				far := geo.NewPoint(child.TopLeft.X+child.Width, child.TopLeft.Y+child.Height)
				if !inside.Contains(child.TopLeft) || !inside.Contains(far) {
					t.Fatalf("child %v escapes C4 body %v", child.Box.ToString(), inside.ToString())
				}
			})
		}
	}
}

func TestGridEmptyPersonKeepsLeafDimensions(t *testing.T) {
	d, _ := compileGridMotionTest(t, `grid-rows: 1
person: Foo {shape: person; style.font-size: 16; grid-columns: 1}
plain: Foo {shape: person; style.font-size: 16}`)
	person, plain := shapeByID(t, d, "person"), shapeByID(t, d, "plain")
	if person.Width != plain.Width || person.Height != plain.Height {
		t.Fatalf("empty grid %dx%d, ordinary person %dx%d", person.Width, person.Height, plain.Width, plain.Height)
	}
}

func TestGridBorderIconSpacing(t *testing.T) {
	for _, tc := range []struct{ axis, first, second string }{
		{"grid-rows", "border-right-center", "border-left-center"},
		{"grid-columns", "border-bottom-center", "border-top-center"},
	} {
		for _, size := range []int{100, 200} {
			t.Run(fmt.Sprintf("%s_%d", tc.axis, size), func(t *testing.T) {
				// The first icon grows when its track stretches to the second shape.
				w, h := size, 40
				if tc.axis == "grid-columns" {
					w, h = h, w
				}
				d, _ := compileGridMotionTest(t, fmt.Sprintf(`
%[1]s: 1
grid-gap: 0
a: "" {width: %[5]d; height: %[6]d; icon: https://example.com/icon.svg; icon.near: %[2]s}
b: "" {width: %[4]d; height: %[4]d; icon: https://example.com/icon.svg; icon.near: %[3]s}`, tc.axis, tc.first, tc.second, size, w, h))
				bounds := func(id string) *geo.Box {
					s := shapeByID(t, d, id)
					box := geo.NewBox(geo.NewPoint(float64(s.Pos.X), float64(s.Pos.Y)), float64(s.Width), float64(s.Height))
					rendered := float64(d2target.GetIconSize(box, s.IconPosition))
					if rendered != float64(min(size/2, d2target.MAX_ICON_SIZE)) {
						t.Fatalf("unexpected icon size %v", rendered)
					}
					p := label.FromString(s.IconPosition).GetPointOnBox(box, label.PADDING, rendered, rendered)
					return geo.NewBox(p, rendered, rendered)
				}
				a, b := bounds("a"), bounds("b")
				if a.Overlaps(*b) {
					t.Fatalf("border icons overlap: %v and %v", a.ToString(), b.ToString())
				}
			})
		}
	}
}

func TestGridContainerBorderDecorationsClearChildren(t *testing.T) {
	d, _ := compileGridMotionTest(t, `
g: Title {
  grid-rows: 1
  grid-gap: 0
  label.near: border-top-center
  icon: https://example.com/icon.svg
  icon.near: border-right-center
  child: "" {width: 100; height: 40}
}`)
	panel, child := shapeByID(t, d, "g"), shapeByID(t, d, "g.child")
	box := geo.NewBox(geo.NewPoint(float64(panel.Pos.X), float64(panel.Pos.Y)), float64(panel.Width), float64(panel.Height))
	iconSize := d2target.GetIconSize(box, panel.IconPosition)
	if 2*child.Pos.Y < 2*panel.Pos.Y+panel.LabelHeight ||
		2*(child.Pos.X+child.Width) > 2*(panel.Pos.X+panel.Width)-iconSize {
		t.Fatalf("grid child %+v %dx%d overlaps its container's title or %dpx icon", child.Pos, child.Width, child.Height, iconSize)
	}
}

func TestZeroGapGridStaysInsidePaintedShape(t *testing.T) {
	for _, axis := range []string{"grid-rows", "grid-columns"} {
		for _, kind := range []string{"person", "page", "cloud"} {
			t.Run(axis+"/"+kind, func(t *testing.T) {
				d, _ := compileGridMotionTest(t, fmt.Sprintf(`
%s: 1
panel: "" {shape: %s; grid-rows: 1; grid-gap: 0; child: "" {width: 40; height: 180}}
peer: "" {width: 400; height: 500}`, axis, kind))
				panel, child := shapeByID(t, d, "panel"), shapeByID(t, d, "panel.child")
				box := geo.NewBox(geo.NewPoint(float64(panel.Pos.X), float64(panel.Pos.Y)), float64(panel.Width), float64(panel.Height))
				perimeter := shape.NewShape(d2target.DSL_SHAPE_TO_SHAPE_TYPE[kind], box).Perimeter()
				// Check each exported corner against the paint path.
				for _, x := range []float64{float64(child.Pos.X) + .5, float64(child.Pos.X+child.Width) - .5} {
					for _, y := range []float64{float64(child.Pos.Y) + .5, float64(child.Pos.Y+child.Height) - .5} {
						ray := geo.NewSegment(geo.NewPoint(box.TopLeft.X-1, y), geo.NewPoint(x, y))
						crossings := 0
						for _, side := range perimeter {
							crossings += len(side.Intersections(*ray))
						}
						if crossings%2 == 0 {
							t.Fatalf("child corner (%g, %g) outside %s outline %v", x, y, kind, box.ToString())
						}
					}
				}
			})
		}
	}
}
