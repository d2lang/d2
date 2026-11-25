package d2cli

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"
)

func TestGIFViewportForSelectedStepAndIndependentLayer(t *testing.T) {
	const grid = `vars: {d2-config: {layout-engine: tala; animate-interval: 150ms; animate-duration: 120ms}}
grid-columns: 2
horizontal-gap: 12
anchor: "" {width: 40; height: 40; style: {fill: red; stroke-width: 0}}
other: "" {width: 40; height: 40; style: {fill: blue; stroke-width: 0}}
layers: {independent: {pin: "" {left: 9500; top: 9500; width: 40; height: 40; style: {fill: green; stroke-width: 0}}}}
steps: {
  shrink: {other: null}
  hold: {}
}`
	const schema = `vars: {d2-config: {layout-engine: tala; animate-interval: 280ms; animate-duration: 220ms}}
direction: right
a: Albums {shape: sql_table; style.fill: red; id: int; title: text}
b: Tracks {shape: sql_table; style.fill: blue; id: int; album_id: int; title: text}
a.id -> b.album_id
steps: {append: {b.seconds: int}}`
	for _, tc := range []struct {
		name, source, target string
		hasLayer             bool
	}{{"grid with independent layer and final hold", grid, "*", true}, {"selected SQL transition", schema, "steps.append.*", false}} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := imageCLI(t, t.TempDir(), tc.source, "--target="+tc.target, "--pad=4", "--stdout-format=gif", "-")
			if err != nil {
				t.Fatal(err)
			}
			anim, err := gif.DecodeAll(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			var original image.Rectangle
			layerSeen := false
			for i, frame := range anim.Image {
				var anchor image.Rectangle
				for y := frame.Rect.Min.Y; y < frame.Rect.Max.Y; y++ {
					for x := frame.Rect.Min.X; x < frame.Rect.Max.X; x++ {
						c := color.NRGBAModel.Convert(frame.At(x, y)).(color.NRGBA)
						if c.R > 240 && c.G < 16 && c.B < 16 {
							anchor = anchor.Union(image.Rect(x, y, x+1, y+1))
						}
						if tc.hasLayer && c.G > 110 && c.R < 16 && c.B < 16 {
							layerSeen = true
						}
					}
				}
				if tc.hasLayer && anchor.Empty() {
					continue
				}
				if i == 0 {
					original = anchor
				}
				if anchor.Empty() || anchor != original {
					t.Fatalf("stationary anchor in frame %d: %v, initially %v", i, anchor, original)
				}
			}
			if original.Empty() || layerSeen != tc.hasLayer || len(anim.Image) < 6 {
				t.Fatalf("missing expected artwork: anchor %v, layer %v, frames %d", original, layerSeen, len(anim.Image))
			}
		})
	}
}
