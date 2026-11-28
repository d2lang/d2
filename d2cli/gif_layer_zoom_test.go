package d2cli

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"math"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2animate"
	"github.com/d2lang/d2/d2target"
)

func TestGIFLinkedLayerZoom(t *testing.T) {
	const source = `vars: {d2-config: {layout-engine: tala; animate-interval: 300ms; animate-duration: 500ms}}
layers: {
  outer: {
    anchor: "" {left: 0; top: 0; width: 25; height: 40; style.fill: green}
    portal: "" {left: 150; top: 0; width: 50; height: 40; style.fill: red; link: layers.inner}
    layers: {inner: {detail: "" {left: 1200; top: 800; width: 70; height: 70; style.fill: blue; link: root.layers.outer}}}
  }
}`
	encoded, err := imageCLI(t, t.TempDir(), source, "--target=layers.outer.*", "--pad=6", "--stdout-format=gif", "-")
	if err != nil {
		t.Fatal(err)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	// The requested transition extends each outgoing slot from 300ms to
	// 500ms. The last board's slot carries the zoom back out to the root.
	total := 0
	for _, delay := range animation.Delay {
		total += delay
	}
	if total != 100 {
		t.Fatalf("GIF duration %dcs, want 100cs", total)
	}
	frameAt := func(cs int) *image.Paletted {
		at := 0
		for i, delay := range animation.Delay {
			if at+delay > cs {
				return animation.Image[i]
			}
			at += delay
		}
		t.Fatalf("no frame at %dcs", cs)
		return nil
	}
	colorBounds := func(cs int, blue bool) image.Rectangle {
		f := frameAt(cs)
		var bounds image.Rectangle
		for y := f.Rect.Min.Y; y < f.Rect.Max.Y; y++ {
			for x := f.Rect.Min.X; x < f.Rect.Max.X; x++ {
				r, g, b, _ := f.At(x, y).RGBA()
				if blue {
					r, b = b, r
				}
				if r > 0xc000 && g < 0x7000 && b < 0x7000 {
					bounds = bounds.Union(image.Rect(x, y, x+1, y+1))
				}
			}
		}
		if bounds.Empty() {
			t.Fatalf("missing test color (blue=%v) at %dcs", blue, cs)
		}
		return bounds
	}
	start, inwardPan := colorBounds(0, false), colorBounds(20, false)
	outwardPan, end := colorBounds(80, false), colorBounds(99, false)
	center := float64(animation.Config.Width) / 2
	distance := func(r image.Rectangle) float64 { return math.Abs(center - float64(r.Min.X+r.Max.X)/2) }
	if distance(inwardPan) >= distance(start)/2 || distance(outwardPan) >= distance(end)/2 {
		t.Fatalf("portal bounds on entering %v → %v and leaving %v → %v, canvas width %d", start, inwardPan, outwardPan, end, animation.Config.Width)
	}
	// Both zooms pass through an enlarged portal, even though the two boards
	// use completely different world coordinates.
	if inward, outward := colorBounds(46, false), colorBounds(64, false); inward.Dx() <= start.Dx()+5 || outward.Dx() <= start.Dx()+5 {
		t.Fatalf("portal width at rest %d, inward %d, outward %d", start.Dx(), inward.Dx(), outward.Dx())
	}
	detail := colorBounds(50, true)
	if detail.Dx() < 60 {
		t.Fatalf("inner board should arrive at its authored scale, got %v", detail)
	}
	// The return link's numbered appendix row must travel with the remote
	// layer too. Search below the blue node and its pale separator.
	inside := frameAt(50)
	for y := detail.Max.Y + 20; y < inside.Bounds().Max.Y; y++ {
		for x := inside.Bounds().Min.X; x < inside.Bounds().Max.X; x++ {
			r, g, b, _ := inside.At(x, y).RGBA()
			if max(r, g, b) < 0x7000 {
				return
			}
		}
	}
	t.Fatal("inner layer's return-link footnote was clipped")
}

func TestGIFLinkedLayerZoomUsesDefaultInterval(t *testing.T) {
	const source = `vars: {d2-config: {animate-duration: 500ms}}
client -> gateway
gateway.link: layers.gateway
layers.gateway: {auth -> route}`
	render := func(flag string) []byte {
		t.Helper()
		args := []string{"--scale=.3", "--pad=12", "--animate-progress=false", "--stdout-format=gif"}
		if flag != "" {
			args = append(args, flag)
		}
		encoded, err := imageCLI(t, t.TempDir(), source, append(args, "-")...)
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	auto := render("")
	if !bytes.Equal(auto, render("--animate-interval=1000")) {
		t.Fatal("the default GIF interval changed layer playback")
	}
	if bytes.Equal(auto, render("--animate-duration=0")) {
		t.Fatal("the default GIF interval skipped the linked layer zoom")
	}
}

func TestGIFLayerZoomAcrossBranches(t *testing.T) {
	const source = `vars: {d2-config: {layout-engine: tala; animate-interval: 900ms; animate-duration: 900ms; pad: 4}}
a: "" {left: 0; top: 0; width: 50; height: 40; link: layers.a; style.fill: "#ff0000"}
b: "" {left: 80; top: 0; width: 50; height: 40; link: layers.b; style.fill: "#ff0000"}
c: "" {left: 160; top: 0; width: 50; height: 40; link: layers.c; style.fill: "#ff0000"}
layers: {
  a: {
    inside: "" {left: 500; top: 900; width: 50; height: 40; link: layers.inside; style.fill: "#00ff00"}
    layers.inside: {deep: "" {left: 1500; top: 400; width: 50; height: 40; style.fill: "#0000ff"}}
  }
  b: {done: "" {left: 2500; top: 200; width: 50; height: 40; style.fill: "#ff00ff"}}
  c: {other: "" {left: 100; top: 200; width: 50; height: 40; style.fill: "#00ffff"}}
}`
	for _, complete := range []bool{true, false} {
		input := source
		if !complete {
			input = strings.Replace(input, "link: layers.b;", "", 1)
		}
		encoded, err := imageCLI(t, t.TempDir(), input, "--stdout-format=gif", "-")
		if err != nil {
			t.Fatal(err)
		}
		animation, err := gif.DecodeAll(bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		// The deep blue child crosses its green parent and the red overview
		// on its way to magenta. Magenta then visits its cyan sibling via red.
		wantPaint := map[int]color.RGBA{
			180: {0, 0, 255, 255}, 210: {0, 255, 0, 255},
			240: {255, 0, 0, 255}, 270: {255, 0, 255, 255},
			310: {255, 0, 0, 255}, 360: {0, 255, 255, 255},
		}
		if !complete {
			wantPaint[210], wantPaint[240] = wantPaint[180], wantPaint[180]
			wantPaint[310] = wantPaint[270]
		}
		at := 0
		for i, frame := range animation.Image {
			for sample, want := range wantPaint {
				if sample < at || sample >= at+animation.Delay[i] {
					continue
				}
				count := 0
				for _, pixel := range frame.Pix {
					// Quantization can shift a solid fill by a few channel levels.
					got := color.RGBAModel.Convert(frame.Palette[pixel]).(color.RGBA)
					if got.R>>4 == want.R>>4 && got.G>>4 == want.G>>4 && got.B>>4 == want.B>>4 {
						count++
					}
				}
				if count < 500 {
					t.Fatalf("complete=%t: found %d pixels of board color %v at %dcs", complete, count, want, sample)
				}
				delete(wantPaint, sample)
			}
			at += animation.Delay[i]
		}
		if at != 450 || len(wantPaint) != 0 {
			t.Fatalf("complete=%t: duration %dcs, missing sample times %v", complete, at, wantPaint)
		}
	}
}

func TestGIFZoomSceneBuildCount(t *testing.T) {
	root, a, b := new(d2target.Diagram), new(d2target.Diagram), new(d2target.Diagram)
	segment := func(boards ...*d2target.Diagram) gifSegment {
		var s gifSegment
		for i := 1; i < len(boards); i++ {
			s.route = append(s.route, d2animate.LayerLeg{From: boards[i-1], To: boards[i]})
			// Several consecutive samples in each leg need just its two scenes.
			for range 3 {
				s.zoom = append(s.zoom, gifZoomSample{leg: i - 1})
			}
		}
		return s
	}
	indices := map[*d2target.Diagram]int{root: 0, a: 1, b: 2}
	two := []gifSegment{segment(root, a), segment(a, root)}
	if n := gifZoomSceneBuildCount(two, indices); n != 2 {
		t.Fatalf("two neighboring boards require two builds, got %d", n)
	}
	three := []gifSegment{segment(root, a), segment(a, root, b), segment(b, root)}
	if n := gifZoomSceneBuildCount(three, indices); n != 6 {
		t.Fatalf("three boards require three measurements and three rebuilds, got %d", n)
	}
}
