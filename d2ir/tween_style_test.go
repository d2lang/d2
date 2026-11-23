package d2ir_test

import (
	"testing"

	"github.com/d2lang/d2/d2ir"
)

func TestDirectStyleColorTransitions(t *testing.T) {
	const source = `
vars: {d2-config: {animate-interval: 800ms; animate-duration: 80ms}}
style.fill: blue
rack.node.style: {fill: blue; stroke: black; font-color: white}
rack.node -> rack.peer: {style.stroke: blue}
rack.node -> rack.peer: {style.stroke: white}
steps: {
  change: {
    style.fill: red
    rack.node.style: {fill: red; stroke: white; font-color: black}
    rack.(node -> peer)[1].style.stroke: black
  }
  hold: {}
}
`
	root, err := compileTween(t, source, d2ir.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	change := tweenField(root, "steps", "change").Map()
	if len(change.TweenFrames) != 2 {
		t.Fatalf("got %d frames for direct colors", len(change.TweenFrames))
	}
	mid := change.TweenFrames[1]
	if got := tweenText(t, mid, "style", "fill"); got != "#800080" {
		t.Errorf("board fill at midpoint = %s", got)
	}
	for key, want := range map[string]string{"fill": "#800080", "stroke": "#808080", "font-color": "#808080"} {
		if got := tweenText(t, mid, "rack", "node", "style", key); got != want {
			t.Errorf("%s at midpoint = %q, want %q", key, got, want)
		}
	}
	edges := tweenField(mid, "rack").Map().Edges
	if got := tweenText(t, edges[0].Map(), "style", "stroke"); got != "blue" {
		t.Errorf("unmodified parallel edge stroke = %s", got)
	}
	if got := tweenText(t, edges[1].Map(), "style", "stroke"); got != "#808080" {
		t.Errorf("modified parallel edge stroke at midpoint = %s", got)
	}
	if got := tweenText(t, change, "rack", "node", "style", "fill"); got != "red" {
		t.Errorf("endpoint lost its authored spelling: %s", got)
	}
	if hold := tweenField(root, "steps", "hold").Map(); len(hold.TweenFrames) != 0 {
		t.Fatal("unchanged step generated intermediate boards")
	}
}

func TestDirectStyleColorBoundaries(t *testing.T) {
	const source = `
vars: {
  d2-config: {animate-interval: 1s; animate-duration: 80ms}
  digit: 1
  values: {style.fill: blue}
}
classes: {base: {style.fill: blue}; other: {style.stroke: black}}
node.class: [base; other]
group.style.stroke: "linear-gradient(to right, red, blue)"
swatch.style.fill: "#${digit}00"
steps: {change: {
  node.style.fill: red
  group.style.stroke: red
  vars: {digit: 7; values.style.fill: red}
  added.style.fill: red
}}
`
	root, err := compileTween(t, source, d2ir.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	change := tweenField(root, "steps", "change").Map()
	if len(change.TweenFrames) != 2 {
		t.Fatalf("got %d frames, want 2", len(change.TweenFrames))
	}
	start, mid := change.TweenFrames[0], change.TweenFrames[1]
	if got := tweenText(t, mid, "node", "style", "fill"); got != "#800080" {
		t.Errorf("previous class color for direct override = %s", got)
	}
	if got := tweenText(t, mid, "swatch", "style", "fill"); got != "#400" {
		t.Errorf("direct style interpolation replaced an authored binding: %s", got)
	}
	for _, path := range [][]string{{"group", "style", "stroke"}, {"added", "style", "fill"}} {
		if got := tweenText(t, start, path...); got != "red" {
			t.Errorf("%v first frame = %s", path, got)
		}
	}
}

func TestDirectColorsSkipNonVisualMaps(t *testing.T) {
	root, err := compileTween(t, `
vars: {d2-config: {animate-interval: 1s; animate-duration: 80ms; data.style.fill: blue}}
node
steps: {change: {vars: {d2-config.data.style.fill: red}}}
`, d2ir.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(tweenField(root, "steps", "change").Map().TweenFrames); n != 0 {
		t.Errorf("configuration data generated %d frames", n)
	}
}

func TestStyleColorSwitchesPaletteBinding(t *testing.T) {
	root, err := compileTween(t, `
vars: {
  d2-config: {animate-interval: 1s; animate-duration: 80ms}
  palette: {cool: blue; hot: red}
}
node.style.fill: ${palette.cool}
literal.style.stroke: blue
steps: {change: {
  node.style.fill: ${palette.hot}
  literal.style.stroke: ${palette.hot}
}}
`, d2ir.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	change := tweenField(root, "steps", "change").Map()
	if n := len(change.TweenFrames); n != 2 {
		t.Fatalf("switching palette bindings produced %d frames, want 2", n)
	}
	for _, path := range [][]string{{"node", "style", "fill"}, {"literal", "style", "stroke"}} {
		for i, want := range []string{"blue", "#800080"} {
			if got := tweenText(t, change.TweenFrames[i], path...); got != want {
				t.Errorf("frame %d %v = %s, want %s", i, path, got, want)
			}
		}
		if got := tweenText(t, change, path...); got != "red" {
			t.Errorf("endpoint %v = %s", path, got)
		}
	}
}
