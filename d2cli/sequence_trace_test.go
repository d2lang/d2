package d2cli

import (
	"bytes"
	"image"
	"testing"
)

func TestSequenceHiddenMessageKeepsCrossedLifeline(t *testing.T) {
	const source = `vars: {d2-config: {layout-engine: tala; animate-interval: 1250ms; animate-duration: 700ms}}
shape: sequence_diagram
a: A
b: B {style.stroke: "#D90000"}
c: C
a -> b: Begin
a -> c: Signal reserved {style.opacity: 0}
steps: {send: {(a -> c)[0].style.opacity: 0.6}}`
	dir := t.TempDir()
	svg, err := imageCLI(t, dir, source, "--pad=20", "--stdout-format=svg", "-")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(svg, []byte("rgba(0,0,0,0.")) || !bytes.Contains(svg, []byte("fill=\"black\"")) {
		t.Fatal("SVG is missing the tracing mask or the opaque endpoint mask")
	}
	data, err := imageCLI(t, dir, source, "--pad=20", "--stdout-format=gif", "-")
	if err != nil {
		t.Fatal(err)
	}
	before, early, after := animationFrameAt(t, data, 10), animationFrameAt(t, data, 135), animationFrameAt(t, data, 240)
	rgb := func(frame image.Image, x, y int) (uint32, uint32, uint32) {
		r, g, b, _ := frame.At(x, y).RGBA()
		return r >> 8, g >> 8, b >> 8
	}
	box := before.Bounds()
	// B's lifeline crosses the reserved label halfway between A and C. It
	// remains fully red until the trace arrives. At rest, the caption
	// keeps its usual clear backing even on a translucent wire.
	cleared := 0
	for y := box.Dy() / 2; y < box.Dy(); y++ {
		for x := box.Dx()/2 - 2; x <= box.Dx()/2+2; x++ {
			r, g, b := rgb(before, x, y)
			r2, g2, b2 := rgb(early, x, y)
			firstRed, earlyRed := r > 150 && g < 50 && b < 50, r2 > 150 && g2 < 50 && b2 < 50
			if firstRed != earlyRed {
				t.Fatalf("hidden caption interrupted the lifeline at (%d,%d)", x, y)
			}
			r, g, b = rgb(after, x, y)
			if firstRed && r >= 250 && g >= 250 && b >= 250 {
				cleared++
			}
		}
	}
	if cleared < 4 {
		t.Fatalf("completed caption failed to clear the crossed lifeline; only %d white pixels", cleared)
	}
}
