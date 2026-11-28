package d2cli

import (
	"bytes"
	"image"
	"image/gif"
	"regexp"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2animate"
	"github.com/d2lang/d2/d2target"
)

// Adding a column changes the fitted board size. Independent centering makes
// a stationary cell drift by half the added width in the middle of a GIF.
func TestGridAnimationGIFKeepsStationaryCell(t *testing.T) {
	const source = `vars: {d2-config: {animate-duration: 240ms; animate-interval: 300ms}}
grid-columns: 2
horizontal-gap: 12
anchor: "" {width: 60; height: 48; style: {fill: red; stroke-width: 0}}
steps: {expand: {warm: "" {width: 60; height: 48; style: {fill: blue; stroke-width: 0}}}}`
	for _, tc := range []struct{ name, diagram string }{
		{"plain", source},
		{"legend", "vars: {d2-legend: {cache: Edge cache}}\n" + source},
		{"incoming-appendix", source + "\nsteps.expand.warm.tooltip: A newly warmed replica begins accepting traffic"},
	} {
		t.Run(tc.name, func(t *testing.T) { checkGridGIFStationaryCell(t, tc.diagram) })
	}
}

func checkGridGIFStationaryCell(t *testing.T, source string) {
	t.Helper()
	out, err := imageCLI(t, t.TempDir(), source, "--pad=8", "--stdout-format=gif", "-")
	if err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for i, delay := range g.Delay {
		if delay < 2 {
			t.Fatalf("frame %d takes %dcs, which browsers stretch to 100ms", i, delay)
		}
		total += delay
	}
	if total != 60 {
		t.Fatalf("total delay %dcs; want exactly two 300ms boards", total)
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

func TestGridPopFadesTooltipAndScalesLatexInSVG(t *testing.T) {
	const source = `vars: {d2-config: {animate-duration: 220ms; animate-interval: 300ms}}
grid-columns: 2
anchor: A
steps: {add: {
  formula: |latex
    f(x) = x^2 + 2x + 1
  | {tooltip: Serving traffic; tooltip.near: top-center}
}}`
	out, err := imageCLI(t, t.TempDir(), source, "--stdout-format=svg", "-")
	if err != nil {
		t.Fatal(err)
	}
	// MathJax uses its own nested transforms. Only the outer label group
	// places the formula in the cell and carries its sampled pop scale.
	if !regexp.MustCompile(`transform="translate\([-0-9.]+ [-0-9.]+\) scale\(0\.[0-9]+\)"`).Match(out) {
		t.Fatal("missing sampled LaTeX scale at cell label origin")
	}
	if !regexp.MustCompile(`<g opacity="0\.[0-9]+"><g class="positioned-tooltip"`).Match(out) {
		t.Fatal("missing partially visible positioned tooltip")
	}
}

func TestGridGIFSelectsSamplesOnBrowserClock(t *testing.T) {
	for _, tc := range []struct {
		name string
		// Input frame start times, including the authored endpoint.
		starts         []int
		totalMS        int
		times, indices []int
	}{
		{"dense-and-sparse", []int{0, 13, 21, 35, 49, 95, 180}, 213, []int{0, 20, 40, 100, 180}, []int{0, 2, 3, 5, 6}},
		{"endpoint-has-one-tick", []int{0, 17, 33, 50, 67, 83, 96}, 100, []int{0, 20, 40, 60, 80}, []int{0, 1, 2, 4, 6}},
		{"one-browser-frame", []int{0, 11, 21}, 23, []int{0}, []int{2}},
		{"sub-tick-board", []int{0, 3}, 7, []int{0}, []int{1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := make([]d2animate.Frame, len(tc.starts))
			ids := make(map[*d2target.Diagram]int)
			for i, start := range tc.starts {
				end := tc.totalMS
				if i+1 < len(tc.starts) {
					end = tc.starts[i+1]
				}
				f := d2animate.Frame{Diagram: &d2target.Diagram{}, ElapsedMS: start, DurationMS: end - start, TweenSample: i+1 < len(tc.starts)}
				frames[i] = f
				ids[f.Diagram] = i
			}
			var times, indices []int
			result := motionGIFFrames(frames)
			for i, f := range result {
				times = append(times, f.ElapsedMS)
				indices = append(indices, ids[f.Diagram])
				end := tc.totalMS
				if i+1 < len(result) {
					end = result[i+1].ElapsedMS
				}
				if f.ElapsedMS+f.DurationMS != end {
					t.Fatal("schedule lost or duplicated time")
				}
			}
			if !slices.Equal(times, tc.times) || !slices.Equal(indices, tc.indices) || result[len(result)-1].TweenSample {
				t.Fatalf("got times %v, input indices %v", times, indices)
			}
		})
	}
}
