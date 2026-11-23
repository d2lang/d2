package d2scenebuild

import (
	"context"
	"image/color"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2raster"
	"github.com/d2lang/d2/lib/geo"
)

func TestRasterConnectionCrossingRevealsBackground(t *testing.T) {
	t.Parallel()

	diagram := validDiagram()
	diagram.Root.Fill = "#654321"
	front := diagram.Connections[0]
	front.ID = "vertical"
	front.Stroke = "#ff0000"
	front.Route = []*geo.Point{{X: 50, Y: -20}, {X: 50, Y: 40}}
	diagram.Connections = append(diagram.Connections, front)
	pad := int64(5)
	doc, err := Build(context.Background(), diagram, Options{Pad: &pad})
	if err != nil {
		t.Fatal(err)
	}
	img, err := d2raster.Render(context.Background(), doc, shapeEffectFrameOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range []struct {
		x, y int
		want color.NRGBA
	}{
		{40, 10, color.NRGBA{A: 255}}, // Back edge before the gap.
		{48, 10, color.NRGBA{R: 0x65, G: 0x43, B: 0x21, A: 255}},
		{50, 10, color.NRGBA{R: 255, A: 255}}, // Uninterrupted front edge.
		{52, 10, color.NRGBA{R: 0x65, G: 0x43, B: 0x21, A: 255}},
		{60, 10, color.NRGBA{A: 255}}, // Back edge after the gap.
	} {
		got := img.NRGBAAt(point.x-int(doc.ViewBox.X), point.y-int(doc.ViewBox.Y))
		if got != point.want {
			t.Errorf("pixel at diagram coordinates (%d,%d) = %+v, want %+v", point.x, point.y, got, point.want)
		}
	}
}
