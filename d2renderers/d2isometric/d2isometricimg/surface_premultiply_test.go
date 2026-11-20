package d2isometricimg

import (
	"bytes"
	"context"
	"image"
	"image/draw"
	"testing"
)

// nativePremultiply replaces draw.Draw's draw.Src for surface textures, so it
// must produce the same bytes for every channel value at every alpha.
func TestNativePremultiplyMatchesDraw(t *testing.T) {
	// Row y holds alpha y, and the red channel of column x holds x, so the
	// image covers every pairing of a channel value with an alpha.
	full := image.NewNRGBA(image.Rect(-3, 5, 253, 261))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			i := y*full.Stride + x*4
			full.Pix[i], full.Pix[i+1], full.Pix[i+2], full.Pix[i+3] = uint8(x), uint8(255-x), uint8(x*7+y), uint8(y)
		}
	}
	// A sub-image has a stride wider than its rows and its own origin.
	sub := full.SubImage(image.Rect(10, 20, 200, 250)).(*image.NRGBA)
	for name, frame := range map[string]*image.NRGBA{"full": full, "sub": sub} {
		want := image.NewRGBA(frame.Bounds())
		draw.Draw(want, want.Bounds(), frame, frame.Bounds().Min, draw.Src)
		got, err := nativePremultiply(context.Background(), frame)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.Bounds() != want.Bounds() || got.Stride != want.Stride || !bytes.Equal(got.Pix, want.Pix) {
			t.Fatalf("%s: pixels differ from draw.Draw", name)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := nativePremultiply(ctx, full); err == nil {
		t.Fatal("a canceled context was ignored")
	}
}
