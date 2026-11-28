package d2cli

import (
	"image"
	"image/color"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
)

func TestVideoMatteColors(t *testing.T) {
	theme := d2themescatalog.NeutralDefault
	theme.Colors.B1 = "#204060"
	for _, tc := range []struct {
		fill string
		want color.RGBA
	}{
		{"B1", color.RGBA{32, 64, 96, 255}},
		{"#0000ff80", color.RGBA{255, 255, 255, 255}},
		{"transparent", color.RGBA{255, 255, 255, 255}},
		{"linear-gradient(to right, red, blue)", color.RGBA{255, 255, 255, 255}},
	} {
		if got := videoBoardMatte(theme, tc.fill); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.fill, got, tc.want)
		}
	}
}

func TestVideoLayerMatte(t *testing.T) {
	root, layer := simpleRasterDiagramWithSize(21, 21), simpleRasterDiagramWithSize(81, 61)
	root.Root.Fill, layer.Root.Fill = "#204060", "#204060"
	root.Shapes[0].Link = "root.layers.detail"
	layer.Name = "detail"
	root.Layers = []*d2target.Diagram{layer}
	pad := int64(12)
	opts := d2svg.RenderOpts{Pad: &pad}
	frames := 0
	err := renderVideoFrames(t.Context(), "-", false, root, opts, 200, gifLayerOptions{duration: 100}, 0, false, videoSize{}, func(frame image.Image, repeats int) error {
		// This corner starts in the padding around the small board, passes
		// outside the zooming layer, then rests on the larger board's fill.
		got := color.RGBAModel.Convert(frame.At(0, 0))
		if got != (color.RGBA{32, 64, 96, 255}) {
			t.Fatalf("frame %d has corner %v", frames, got)
		}
		frames += repeats
		return nil
	})
	if err != nil || frames != 12 {
		t.Fatalf("got %d frames, error %v", frames, err)
	}
}
