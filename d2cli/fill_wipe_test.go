package d2cli

import (
	"bytes"
	"image/gif"
	"testing"
)

func TestFillWipeSVGAndGIF(t *testing.T) {
	const source = `vars: {d2-config: {animate-duration: 200ms; animate-interval: 300ms}}
tile: "" {width: 100; height: 60; style: {fill: blue; fill-transition: wipe-left; stroke-width: 0}}
steps: {paint: {tile.style.fill: red}}`
	dir := t.TempDir()
	svg, err := imageCLI(t, dir, source, "--dark-theme=200", "--pad=0", "--stdout-format=svg", "-")
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{`x1="100%" y1="50%" x2="0%" y2="50%"`, `offset="50%" stop-color="red"`, `offset="50%" stop-color="blue"`} {
		if !bytes.Contains(svg, []byte(token)) {
			t.Fatalf("SVG missing %s", token)
		}
	}
	gifBytes, err := imageCLI(t, dir, source, "--pad=0", "--stdout-format=gif", "-")
	if err != nil {
		t.Fatal(err)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(gifBytes))
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for i, delay := range animation.Delay {
		if delay < 2 {
			t.Fatalf("frame %d has a %dcs delay that browsers stretch", i, delay)
		}
		total += delay
	}
	if total != 60 {
		t.Fatalf("wipe playback lasts %dcs, want 60cs", total)
	}
	f := animationFrameAt(t, gifBytes, 40)
	for _, p := range []struct{ x, r, b int }{{25, 0, 255}, {75, 255, 0}} {
		r, g, b, _ := f.At(p.x*f.Bounds().Dx()/100, f.Bounds().Dy()/2).RGBA()
		if r>>8 != uint32(p.r) || g != 0 || b>>8 != uint32(p.b) {
			t.Fatalf("GIF at %d%%, midpoint = #%02x%02x%02x", p.x, r>>8, g>>8, b>>8)
		}
	}
}
