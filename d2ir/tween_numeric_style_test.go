package d2ir_test

import (
	"slices"
	"strconv"
	"testing"

	"github.com/d2lang/d2/d2ir"
)

func TestDirectNumericProperties(t *testing.T) {
	const source = `
vars: {d2-config: {animate-interval: 1s; animate-duration: 32ms}}
classes: {card: {style.stroke-width: 1}}
card: {class: card; width: 100; height: 60; style: {opacity: 0; border-radius: 0; font-size: 12}}
card -> peer: {style.stroke-dash: 0}
steps: {
  resize: {
    card: {width: 200; height: 80; style: {opacity: 1; border-radius: 20; font-size: 28; stroke-width: 9}}
    (card -> peer)[0].style.stroke-dash: 8
  }
  same: {card.width: 200}
}
`
	root, err := compileTween(t, source, d2ir.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	change := tweenField(root, "steps", "resize").Map()
	if len(change.TweenFrames) != 2 {
		t.Fatalf("got %d frames, want 2", len(change.TweenFrames))
	}
	mid := change.TweenFrames[1]
	if got := tweenText(t, mid, "card", "width"); got != "150" {
		t.Errorf("midpoint width = %s", got)
	}
	if got := tweenText(t, mid, "card", "height"); got != "70" {
		t.Errorf("midpoint height = %s", got)
	}
	for key, want := range map[string]string{"opacity": "0.5", "border-radius": "10", "font-size": "20", "stroke-width": "5"} {
		if got := tweenText(t, mid, "card", "style", key); got != want {
			t.Errorf("%s = %s at midpoint, want %s", key, got, want)
		}
	}
	if got := tweenText(t, mid.Edges[0].Map(), "style", "stroke-dash"); got != "4" {
		t.Errorf("midpoint edge dash = %s", got)
	}
	if got := len(tweenField(root, "steps", "same").Map().TweenFrames); got != 0 {
		t.Fatalf("unchanged dimensions produced %d frames", got)
	}
	off := false
	static, err := compileTween(t, source, d2ir.CompileOptions{AnimateTweens: &off})
	if err != nil {
		t.Fatal(err)
	}
	step := tweenField(static, "steps", "resize").Map()
	if len(step.TweenFrames) != 0 || tweenText(t, step, "card", "width") != "200" {
		t.Fatal("static export must use the authored destination")
	}
}

func TestDirectDimensionsKeepEqualSidedShapesValid(t *testing.T) {
	for _, shape := range []string{"circle", "square"} {
		for _, tc := range []struct{ name, before, mid string }{
			{"equal", "width: 60; height: 60", "80"},
			{"unequal", "width: 60; height: 80", "100"},
			{"implicit", "width: 60", "100"},
		} {
			t.Run(shape+"/"+tc.name, func(t *testing.T) {
				root, err := compileTween(t, `
vars: {d2-config: {animate-interval: 1s; animate-duration: 32ms}}
node: {`+tc.before+`; style.opacity: 0}
steps: {change: {node: {width: 100; height: 100; shape: `+shape+`; style.opacity: 1}}}
`, d2ir.CompileOptions{})
				if err != nil {
					t.Fatal(err)
				}
				step := tweenField(root, "steps", "change").Map()
				for _, frame := range step.TweenFrames {
					width := tweenText(t, frame, "node", "width")
					if width != tweenText(t, frame, "node", "height") {
						t.Fatalf("generated unequal dimensions for %s", shape)
					}
					if _, err := strconv.Atoi(width); err != nil {
						t.Fatalf("noninteger size %s", width)
					}
				}
				if width := tweenText(t, step.TweenFrames[1], "node", "width"); width != tc.mid {
					t.Errorf("midpoint width = %s, want %s", width, tc.mid)
				}
			})
		}
	}
}

func TestDirectDimensionsWithMovingVariableOnShapeChange(t *testing.T) {
	for _, width := range []string{"width: ${wide}", "class: shared-width"} {
		root, err := compileTween(t, `
vars: {d2-config: {animate-interval: 1s; animate-duration: 32ms}; wide: 60}
classes: {shared-width: {width: ${wide}}}
item: {`+width+`; height: 80}
another: {width: ${wide}}
steps: {change: {vars: {wide: 100}; item: {shape: circle; height: 100}}}
`, d2ir.CompileOptions{})
		if err != nil {
			t.Fatal(err)
		}
		step := tweenField(root, "steps", "change").Map()
		mid := step.TweenFrames[1]
		if tweenText(t, mid, "item", "width") != "100" || tweenText(t, mid, "item", "height") != "100" ||
			tweenText(t, mid, "another", "width") != "80" {
			t.Fatalf("circle with %q failed to hold a valid size while the independent shape moved", width)
		}
	}
}

func TestDirectIntegerPropertyNearPlatformLimit(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	root, err := compileTween(t, `
vars: {d2-config: {animate-interval: 1s; animate-duration: 32ms}}
node.style.border-radius: "`+strconv.Itoa(maxInt-100)+`"
steps: {change: {node.style.border-radius: "`+strconv.Itoa(maxInt)+`"}}
`, d2ir.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	step := tweenField(root, "steps", "change").Map()
	if len(step.TweenFrames) != 2 || tweenText(t, step.TweenFrames[1], "node", "style", "border-radius") != strconv.Itoa(maxInt-50) {
		t.Fatal("expected an exact representable midpoint near the platform's maximum integer")
	}
}

func TestConstraintOnlyStepKeepsInterval(t *testing.T) {
	root, err := compileTween(t, `
vars: {d2-config: {animate-interval: 200ms; animate-duration: 1s}; size: 60}
classes: {sized: {width: ${size}}}
node: {class: sized; shape: circle; height: 60}
steps: {pause: {}}
`, d2ir.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pause := tweenField(root, "steps", "pause").Map()
	if len(pause.TweenFrames) != 0 || pause.TweenDurationMS != 0 {
		t.Fatalf("unchanged step generated %d frames over %dms", len(pause.TweenFrames), pause.TweenDurationMS)
	}
}

func TestGeometryEasesIndependentlyOfVariableValues(t *testing.T) {
	root, err := compileTween(t, `
vars: {d2-config: {animate-interval: 1s; animate-duration: 64ms}; n: 100; alias: ${n}}
classes: {sized: {height: ${alias}}}
group: {
  width: 100
  child: "Value ${n}" {left: ${alias}; top: 50; class: sized; style.font-size: ${n}}
}
source -> group.child: {target-arrowhead.width: ${alias}}
steps: {move: {vars.n: 200; group.width: 200; group.child.top: -50}}
`, d2ir.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	step := tweenField(root, "steps", "move").Map()
	frames := append(slices.Clone(step.TweenFrames), step)
	if len(frames) != 5 {
		t.Fatalf("got %d samples including endpoint, want 5", len(frames))
	}
	for i, want := range []struct{ size, top, value string }{
		{"100", "50", "100"}, {"106", "44", "125"}, {"150", "0", "150"},
		{"194", "-44", "175"}, {"200", "-50", "200"},
	} {
		frame := frames[i]
		child := tweenField(frame, "group", "child").Map()
		if got := tweenText(t, frame, "group", "width"); got != want.size {
			t.Errorf("frame %d container width = %s, want %s", i, got, want.size)
		}
		if got := tweenText(t, child, "left"); got != want.size {
			t.Errorf("frame %d variable left = %s, want %s", i, got, want.size)
		}
		if i < len(step.TweenFrames) {
			if got := tweenText(t, child, "height"); got != want.size {
				t.Errorf("frame %d class height = %s, want %s", i, got, want.size)
			}
		}
		if got := tweenText(t, child, "top"); got != want.top {
			t.Errorf("frame %d direct top = %s, want %s", i, got, want.top)
		}
		if got := tweenText(t, frame, "group", "child"); got != "Value "+want.value {
			t.Errorf("frame %d label = %q, want Value %s", i, got, want.value)
		}
		if got := tweenText(t, frame.Edges[0].Map(), "target-arrowhead", "width"); got != want.value {
			t.Errorf("frame %d arrowhead width = %s, want %s", i, got, want.value)
		}
		if got := tweenText(t, child, "style", "font-size"); got != want.value {
			t.Errorf("frame %d font size = %s, want %s", i, got, want.value)
		}
	}
}
