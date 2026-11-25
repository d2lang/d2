package d2cli

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

func TestGIFPreservesPinnedTravel(t *testing.T) {
	const source = `vars: {
  d2-config: {layout-engine: tala; animate-interval: 500ms; animate-duration: 360ms}
  x: 0
}
probe: "" {left: ${x}; top: 0; width: 40; height: 40; style: {fill: red; stroke-width: 0}}
steps: {wait: {}; travel: {vars: {x: 160}}; hold: {}}`
	intervalOnly := strings.Replace(source, "; animate-duration: 360ms", "", 1)
	base, _, _ := strings.Cut(intervalOnly, "steps:")
	replacement := base + `steps: {replace: {
  probe: null
  other: "" {left: 160; top: 0; width: 40; height: 40; style: {fill: red; stroke-width: 0}}
}}`
	mixed := strings.NewReplacer(
		"steps:", "oldFree: \"\" {width: 22; height: 22}\nsteps:",
		"probe: null", "probe: null; oldFree: null; newFree: \"\" {width: 22; height: 22}",
	).Replace(replacement)
	for _, tc := range []struct {
		name, source       string
		travel, minChanges int
	}{
		{"tween and holds", source, 160, 2},
		{"instant step", intervalOnly, 160, 1},
		{"inherited scenario", base + "scenarios: {travel: {probe.left: 160}}", 160, 1},
		{"replace all pinned nodes", replacement, 160, 1},
		{"replace pin and free neighbor", mixed, 160, 1},
		{"replace unpinned node", strings.Replace(replacement, "left: ${x}; top: 0;", "", 1), 0, 0},
		{"replace with unpinned node", strings.Replace(replacement, "left: 160; top: 0;", "", 1), 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := imageCLI(t, t.TempDir(), tc.source, "--pad=4", "--stdout-format=gif", "-")
			if err != nil {
				t.Fatal(err)
			}
			animation, err := gif.DecodeAll(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			var first, last, changes int
			for i, frame := range animation.Image {
				left := frame.Rect.Max.X
				for x := frame.Rect.Min.X; x < frame.Rect.Max.X; x++ {
					r, g, b, _ := frame.At(x, frame.Rect.Dy()/2).RGBA()
					if r > 0xf000 && g < 0x1000 && b < 0x1000 {
						left = x
						break
					}
				}
				if left == frame.Rect.Max.X {
					t.Fatalf("missing red shape in frame %d", i)
				}
				if i == 0 {
					first = left
				} else if left < last {
					t.Fatalf("camera jumped back at frame %d: x=%d, previous=%d", i, left, last)
				} else if left != last {
					changes++
				}
				last = left
			}
			if last-first != tc.travel || changes < tc.minChanges {
				t.Fatalf("got %d px of travel in %d changes, want %d px in at least %d changes", last-first, changes, tc.travel, tc.minChanges)
			}
		})
	}
}

func TestGIFPinMustBeHonoredByEachTweenSample(t *testing.T) {
	positioned := func(id string) *d2graph.Graph {
		graph := d2graph.NewGraph()
		graph.Root.Children[id] = &d2graph.Object{
			Box: geo.NewBox(geo.NewPoint(20, 30), 40, 40),
			Attributes: d2graph.Attributes{
				Left: &d2graph.Scalar{Value: "20"}, Top: &d2graph.Scalar{Value: "30"},
			},
		}
		return graph
	}
	from, sample, to := positioned("before"), positioned("between"), positioned("after")
	to.TweenFrames = []*d2graph.Graph{sample}
	if !sharePinnedCoordinates(from, to) {
		t.Fatal("fully pinned replacements should share their viewport")
	}
	sample.Root.Children["between"].TopLeft.Y++
	if sharePinnedCoordinates(from, to) {
		t.Fatal("a sample with an ignored top coordinate must fit independently")
	}
	to.TweenFrames = []*d2graph.Graph{d2graph.NewGraph()}
	if sharePinnedCoordinates(from, to) {
		t.Fatal("an empty sample has no pin to establish its coordinate system")
	}
}

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
