package d2scenebuild

import (
	"context"
	"image/color"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2raster"
	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2target"
)

func TestConnectionPulsePaintsShaftAndArrowheadOnce(t *testing.T) {
	gray := color.NRGBA{R: 70, G: 100, B: 119, A: 255}
	orange := color.NRGBA{R: 255, G: 165, A: 255}
	for _, tc := range []struct {
		name, gradient string
		shaft, head    color.NRGBA
	}{
		{"pulse on shaft", "linear-gradient(to right, #466477 20%, orange 20%, orange 40%, #466477 40%)", orange, gray},
		{"pulse entering arrowhead", "linear-gradient(to right, #466477 70%, orange 70%, orange 85%, #466477 85%)", gray, orange},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diagram := validDiagram()
			conn := &diagram.Connections[0]
			conn.DstArrow, conn.StrokeWidth, conn.Stroke = d2target.TriangleArrowhead, 4, tc.gradient
			doc, err := Build(context.Background(), diagram, Options{})
			if err != nil {
				t.Fatal(err)
			}
			img, err := d2raster.Render(context.Background(), doc, shapeEffectFrameOptions())
			if err != nil {
				t.Fatal(err)
			}
			for _, sample := range []struct {
				x, y int
				want color.NRGBA
			}{
				{38, 10, tc.shaft}, // midway along the shaft
				{66, 6, tc.head},   // inside the arrowhead, above the shaft
				{75, 10, gray},     // tip still ahead of the pulse
			} {
				if got := img.NRGBAAt(sample.x-int(doc.ViewBox.X), sample.y-int(doc.ViewBox.Y)); got != sample.want {
					t.Errorf("pixel (%d,%d) = %v, want %v", sample.x, sample.y, got, sample.want)
				}
			}
		})
	}
}

func TestRadialGradientIsSharedBySplitRouteAndCompoundMarkers(t *testing.T) {
	diagram := validDiagram()
	conn := &diagram.Connections[0]
	conn.Animated, conn.SrcArrow, conn.DstArrow = true, d2target.CrossArrowhead, d2target.CircleArrowhead
	conn.Stroke = "radial-gradient(white, blue)"
	doc, err := Build(context.Background(), diagram, Options{})
	if err != nil {
		t.Fatal(err)
	}
	group := findSceneNode(t, doc.Root, conn.ID)
	path := func(suffix string) d2scene.Path {
		return findSceneNode(t, group, conn.ID+suffix).Primitive.(d2scene.Path)
	}
	gradient := path(":path:reverse").Stroke.Paint.(d2scene.RadialGradient)
	if gradient.Units != d2scene.UserSpaceOnUse {
		t.Fatalf("split stroke gradient units = %v", gradient.Units)
	}
	check := func(label string, transform d2scene.Matrix, paint d2scene.Paint) {
		t.Helper()
		g := paint.(d2scene.RadialGradient)
		combined := transform.Mul(g.Transform)
		for _, p := range []d2scene.Point{{}, {X: 1}, {Y: 1}} {
			if g.Units != gradient.Units || !closePoint(combined.Point(p), gradient.Transform.Point(p)) {
				t.Fatalf("%s samples its own gradient: units=%v transform=%+v, want %+v", label, g.Units, combined, gradient.Transform)
			}
		}
	}
	check("second half", d2scene.Identity(), path(":path:forward").Stroke.Paint)
	src := findSceneNode(t, group, conn.ID+":src-arrowhead")
	cross := findSceneNode(t, src, src.ID+":cross")
	check("rotated cross", src.Transform.Mul(cross.Transform), cross.Primitive.(d2scene.Path).Stroke.Paint)
	dst := findSceneNode(t, group, conn.ID+":dst-arrowhead")
	circle := dst.Primitive.(d2scene.Ellipse)
	check("hollow circle outline", dst.Transform, circle.Stroke.Paint)
	if _, ok := circle.Fill.(d2scene.SolidPaint); !ok {
		t.Fatalf("hollow circle background = %T", circle.Fill)
	}
}
