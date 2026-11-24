package d2talalayout_test

import (
	"fmt"
	"testing"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func TestCompoundSeparatesFixedExteriorCaptions(t *testing.T) {
	const source = `Batch: {
  extract: Extract orders from all regions {width: 80; height: 60; label.near: outside-top-left}
  load: Load payments into the archive {width: 80; height: 60; label.near: outside-top-right}
}`
	d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	var captions []geo.Box
	for _, tile := range d.Shapes[1:] {
		body := geo.NewBox(geo.NewPoint(float64(tile.Pos.X), float64(tile.Pos.Y)), float64(tile.Width), float64(tile.Height))
		w, h := float64(tile.LabelWidth), float64(tile.LabelHeight)
		topLeft := label.FromString(tile.LabelPosition).GetPointOnBox(body, label.PADDING, w, h)
		captions = append(captions, *geo.NewBox(topLeft, w, h))
	}
	if len(captions) != 2 || captions[0].Overlaps(captions[1]) {
		t.Fatalf("exterior captions overlap or are missing: %v", captions)
	}
}

func TestCompoundFitsAuthoredExteriorIcon(t *testing.T) {
	for _, tc := range []struct {
		height, iconSize int
	}{{28, 28}, {60, 32}, {95, 48}, {160, 64}} {
		t.Run(fmt.Sprint(tc.height), func(t *testing.T) {
			source := fmt.Sprintf(`Deployment: {
  api: API {width: %d; height: %d; icon: "https://example.org/icon.svg"; icon.near: outside-right-center}
}`, tc.height+20, tc.height)
			d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
			frame, tile := d.Shapes[0], d.Shapes[1]
			if frame.Height != max(tile.Height, tc.iconSize+4*label.PADDING)+120 || frame.Width != tile.Width+120+tc.iconSize+3*label.PADDING {
				t.Fatalf("frame around %d px exterior icon is %d × %d", tc.iconSize, frame.Width, frame.Height)
			}
		})
	}
}
