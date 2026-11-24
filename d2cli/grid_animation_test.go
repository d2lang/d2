package d2cli

import (
	"bytes"
	"image"
	"image/gif"
	"testing"
)

// Adding a column changes the fitted board size. Independent centering makes
// a stationary cell drift by half the added width in the middle of a GIF.
func TestGridAnimationGIFKeepsStationaryCell(t *testing.T) {
	const source = `vars: {d2-config: {animate-duration: 240ms; animate-interval: 300ms}}
grid-columns: 2
horizontal-gap: 12
anchor: "" {width: 60; height: 48; style: {fill: red; stroke-width: 0}}
steps: {expand: {warm: "" {width: 60; height: 48; style: {fill: blue; stroke-width: 0}}}}`
	out, err := imageCLI(t, t.TempDir(), source, "--pad=4", "--stdout-format=gif", "-")
	if err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	var start image.Rectangle
	hasIntermediate := false
	for i, frame := range g.Image {
		var red image.Rectangle
		blue := 0
		for y := frame.Bounds().Min.Y; y < frame.Bounds().Max.Y; y++ {
			for x := frame.Bounds().Min.X; x < frame.Bounds().Max.X; x++ {
				r, green, b, _ := frame.At(x, y).RGBA()
				if r > 0xf000 && green < 0x1000 && b < 0x1000 {
					red = red.Union(image.Rect(x, y, x+1, y+1))
				}
				if b > 0xf000 && r < 0xf000 && green < 0xf000 && b > r && b > green {
					blue++
				}
			}
		}
		if i == 0 {
			start = red
		}
		if red.Empty() || red != start {
			t.Fatalf("stationary cell in frame %d = %v, initially %v", i, red, start)
		}
		if blue > 0 && blue < 60*48*8/10 {
			hasIntermediate = true
		}
	}
	if !hasIntermediate {
		t.Fatal("did not render a growing blue cell")
	}
}
