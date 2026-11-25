package d2cli

import (
	"bytes"
	"image"
	"image/gif"
	"testing"
)

func TestSequenceReturnPreservesPaintedDashes(t *testing.T) {
	const source = `vars: {d2-config: {layout-engine: tala; animate-interval: 400ms; animate-duration: 320ms}}
shape: sequence_diagram
desk; stacks
desk -> stacks: Reserve this long book and keep it on hold
desk <- stacks: Ready {style.stroke: red; style.stroke-dash: 5; style.opacity: 0}
steps: {answer: {(desk <- stacks)[0].style.opacity: 1}}`
	assertTraceDashes(t, source, 56, 72, 32, 2)
}

func TestBackwardBoardArrivalPreservesPaintedDashes(t *testing.T) {
	const source = `vars: {d2-config: {layout-engine: tala; animate-interval: 600ms; animate-duration: 400ms}}
direction: right
a: Fixed
steps: {append: {b -> a: {style.stroke: red; style.stroke-dash: 5}}}`
	// The new node sits at the declared source, so the shaft grows leftward
	// from the arrowhead on the stationary destination. Exclude the marker.
	assertTraceDashes(t, source, 68, 78, 29, 13)
}

func assertTraceDashes(t *testing.T, source string, startCS, endCS, far, near int) {
	t.Helper()
	dir := t.TempDir()
	svg, err := imageCLI(t, dir, source, "--stdout-format=svg", "-")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(svg, []byte(";stroke-dashoffset:")) {
		t.Fatal("truncated return path has no SVG dash phase")
	}
	raw, err := imageCLI(t, dir, source, "--stdout-format=gif", "-")
	if err != nil {
		t.Fatal(err)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	// Locate the shaft by its authored red paint. Check the dashes near
	// the stationary end against the authored final pattern.
	red := func(f *image.Paletted, x, y int) bool {
		r, g, b, _ := f.At(x, y).RGBA()
		return r>>8 > 180 && g>>8 < 110 && b>>8 < 110
	}
	end := animation.Image[len(animation.Image)-1]
	row, right, mostRed := 0, 0, 0
	for y := 0; y < end.Bounds().Dy(); y++ {
		count, last := 0, 0
		for x := 0; x < end.Bounds().Dx(); x++ {
			if red(end, x, y) {
				count++
				last = x
			}
		}
		if count > mostRed {
			row, right, mostRed = y, last, count
		}
	}
	if mostRed < 45 {
		t.Fatalf("could not find the final dashed return (%d red pixels)", mostRed)
	}
	cs, checked := 0, 0
	for i, f := range animation.Image {
		if cs >= startCS && cs < endCS {
			for x := right - far; x < right-near; x++ {
				if red(f, x, row) != red(end, x, row) {
					t.Fatalf("painted dash changed at (%d,%d), %dcs", x, row, cs)
				}
			}
			checked++
		}
		cs += animation.Delay[i]
	}
	if checked < 3 {
		t.Fatalf("checked only %d intermediate frames", checked)
	}
}
