package d2cli

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2animate"
	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2target"
)

func TestCompositeLayerMatchesDrawMask(t *testing.T) {
	// Use subimages with different strides. Every source alpha is tested with
	// every camera opacity, on both opaque and translucent destination pixels.
	src := image.NewNRGBA(image.Rect(0, 0, 270, 20)).SubImage(image.Rect(2, 4, 258, 17)).(*image.NRGBA)
	dstRect := image.Rect(3, 2, 259, 15)
	base := image.NewRGBA(image.Rect(0, 0, 280, 22))
	for y := range src.Rect.Dy() {
		for x := range src.Rect.Dx() {
			src.SetNRGBA(x+src.Rect.Min.X, y+src.Rect.Min.Y, color.NRGBA{R: uint8(23 * x), G: uint8(13*y - x), B: uint8(x + 17*y), A: uint8(x)})
			alpha := uint8(x + 37*y)
			if y == 0 {
				alpha = 255
			}
			base.Set(x+dstRect.Min.X, y+dstRect.Min.Y, color.NRGBA{R: uint8(x + 94*y), G: uint8(72*x + y), B: uint8(3*x + y), A: alpha})
		}
	}
	for opacity := range 256 {
		want, got := *base, *base
		want.Pix, got.Pix = bytes.Clone(base.Pix), bytes.Clone(base.Pix)
		draw.DrawMask(&want, dstRect, src, src.Rect.Min, image.NewUniform(color.Alpha{A: uint8(opacity)}), image.Point{}, draw.Over)
		if err := compositeLayer(t.Context(), got.SubImage(dstRect).(*image.RGBA), src, uint8(opacity)); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(want.Pix, got.Pix) {
			t.Fatalf("mask %d differs from image/draw", opacity)
		}
	}
}

func TestLayerCameraOwnedFrames(t *testing.T) {
	box := d2scene.Box{Width: 24, Height: 18}
	solid := func(c color.NRGBA) *d2scene.Document {
		return d2scene.NewDocument(box, d2scene.NewNode(d2scene.Rect{Box: box, Fill: d2scene.SolidPaint{Color: c}}))
	}
	zoom := d2animate.LayerZoom{Focus: &d2target.Shape{Width: 8, Height: 6}, Inward: true}
	camera := newGIFLayerCamera(image.Rect(0, 0, 24, 18), zoom, solid(color.NRGBA{R: 255, A: 255}), image.Point{}, solid(color.NRGBA{B: 255, A: 255}), image.Point{})
	session, err := newGIFRenderSession()
	if err != nil {
		t.Fatal(err)
	}
	opts := gifFrameOptions(0, 1)
	first, err := camera.render(t.Context(), session, opts, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := bytes.Clone(first.Pix)
	second, err := camera.render(t.Context(), session, opts, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(snapshot, second.Pix) || !bytes.Equal(snapshot, first.Pix) {
		t.Fatal("rendered camera frames must retain their individual poses")
	}
}
