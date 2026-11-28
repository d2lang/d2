package d2ir_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/d2lang/d2/d2ir"
	"github.com/d2lang/d2/lib/color"
)

func TestTweenWholeGradient(t *testing.T) {
	const from = "linear-gradient(to right, blue 20%, transparent 20%)"
	const to = "linear-gradient(to right, #f00 80%, rgb(255,0,0) 80%)"
	const mid = "linear-gradient(to right, #800080 50%, #ff000080 50%)"
	source := fmt.Sprintf(`vars: {
  d2-config: {animate-duration: 32ms; animate-interval: 1s}
  paint: %q
  alias: ${paint}
}
meter.style.fill: ${alias}
direct.style.stroke: %q
steps: {change: {vars: {paint: %q}; direct.style.stroke: %q}}`, from, from, to, to)
	m, err := compileTween(t, source, d2ir.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	change := tweenField(m, "steps", "change").Map()
	if len(change.TweenFrames) != 2 {
		t.Fatalf("got %d samples", len(change.TweenFrames))
	}
	for _, tc := range []struct {
		board *d2ir.Map
		want  string
	}{{m, from}, {change.TweenFrames[0], from}, {change.TweenFrames[1], mid}, {change, to}} {
		for _, path := range [][]string{{"meter", "style", "fill"}, {"direct", "style", "stroke"}} {
			if got := tweenText(t, tc.board, path...); got != tc.want {
				t.Errorf("%v = %q, want %q", path, got, tc.want)
			}
		}
	}
}

func TestTweenGradientCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
		animates       bool
	}{
		{"default direction", "linear-gradient(blue)", "linear-gradient(to bottom, red)", true},
		{"corner word order", "linear-gradient(to top left, blue)", "linear-gradient(to left top, red)", true},
		{"full turn", "linear-gradient(90deg, blue)", "linear-gradient(450deg, red)", true},
		{"radial geometry", "radial-gradient(circle, blue)", "radial-gradient(ellipse, red)", true},
		{"kind", "radial-gradient(blue)", "linear-gradient(red)", false},
		{"direction", "linear-gradient(to left, blue)", "linear-gradient(to right, red)", false},
		{"different origin", "linear-gradient(90deg, blue)", "linear-gradient(to right, red)", false},
		{"stop count", "linear-gradient(blue, white)", "linear-gradient(red)", false},
		{"invalid color", "linear-gradient(currentColor, blue)", "linear-gradient(red, blue)", false},
		{"invalid angle", "linear-gradient(NaNdeg, blue)", "linear-gradient(NaNdeg, red)", false},
		{"equivalent paint", "linear-gradient(red, #fff)", "linear-gradient(to bottom, #FF0000 0, white 100%)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := fmt.Sprintf(`vars: {d2-config: {animate-duration: 80ms; animate-interval: 1s}; paint: %q}
legendText: ${paint}
steps: {change: {vars: {paint: %q}}}`, tc.from, tc.to)
			m, err := compileTween(t, input, d2ir.CompileOptions{})
			if err != nil {
				t.Fatal(err)
			}
			change := tweenField(m, "steps", "change").Map()
			if (len(change.TweenFrames) > 0) != tc.animates || tweenText(t, change, "legendText") != tc.to {
				t.Errorf("got %d samples and endpoint %q", len(change.TweenFrames), tweenText(t, change, "legendText"))
			}
		})
	}
}

func TestTweenGradientUsesRenderedOffsets(t *testing.T) {
	m, err := compileTween(t, `vars: {d2-config: {animate-duration: 32ms; animate-interval: 1s}}
meter.style.fill: "linear-gradient(blue 80%, blue, white 0.3)"
steps: {change: {meter.style.fill: "linear-gradient(blue 0.2, blue, white 80%)"}}`, d2ir.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	change := tweenField(m, "steps", "change").Map()
	if len(change.TweenFrames) != 2 {
		t.Fatalf("got %d samples", len(change.TweenFrames))
	}
	raw := tweenText(t, change.TweenFrames[1], "meter", "style", "fill")
	g, err := color.ParseGradient(raw)
	if err != nil {
		t.Fatal(err)
	}
	previous := 0.0
	for i, want := range []float64{.5, .65, .8} {
		offset, err := g.ColorStops[i].Offset(i, len(g.ColorStops), previous)
		if err != nil || math.Abs(offset-want) > 1e-12 {
			t.Fatalf("stop %d in %q: got %v, %v; want %v", i, raw, offset, err, want)
		}
		previous = offset
	}
}

func TestTweenGradientWithSpacedColorFunctions(t *testing.T) {
	const source = `vars: {d2-config: {animate-duration: 32ms; animate-interval: 120ms}}
bar.style.fill: "linear-gradient(to right, rgb(90, 130, 210) 20%, white 20%)"
steps: {loaded: {bar.style.fill: "linear-gradient(to right, rgba(90, 130, 210, 0.5) 80%, white 80%)"}}`
	m, err := compileTween(t, source, d2ir.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	step := tweenField(m, "steps", "loaded").Map()
	if len(step.TweenFrames) != 2 {
		t.Fatalf("got %d frames, want 2", len(step.TweenFrames))
	}
	got := tweenText(t, step.TweenFrames[1], "bar", "style", "fill")
	if want := "linear-gradient(to right, #5a82d2c0 50%, #ffffff 50%)"; got != want {
		t.Errorf("midpoint = %q, want %q", got, want)
	}
}
