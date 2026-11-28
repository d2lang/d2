package d2lib

import (
	"context"
	"fmt"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/log"
)

func TestVariableOpacityKeepsPaintPrecision(t *testing.T) {
	for _, tc := range []struct {
		start, end, firstLabel string
		opacities              [5]float64
	}{
		{"0", "1", "Phase 0", [5]float64{0, .0625, .5, .9375, 1}},
		{"0.0", "1.0", "Phase 0.2", [5]float64{0, .0625, .5, .9375, 1}},
		{"0.1", "0.6", "Phase 0.2", [5]float64{.1, .225, .35, .475, .6}},
	} {
		t.Run(tc.start+" to "+tc.end, func(t *testing.T) {
			input := fmt.Sprintf(`vars: {d2-config: {animate-duration: 160ms; animate-interval: 200ms}; phase: %s; alpha: ${phase}}
classes: {dim: {style.opacity: "${alpha}"}}
a: "Phase ${phase}" {class: dim}
b.style.opacity: ${phase}
a -> b: {style.opacity: ${alpha}}
steps: {on: {vars: {phase: %s}}; off: {vars: {phase: %s}}}`, tc.start, tc.end, tc.start)
			calls := 0
			d, _, err := Compile(log.WithDefault(context.Background()), input, dagreCompileOptions(&calls), nil)
			if err != nil {
				t.Fatal(err)
			}
			for i, step := range d.Steps {
				if len(step.TweenFrames) != 4 {
					t.Fatalf("step %d has %d samples", i, len(step.TweenFrames))
				}
				for j, f := range step.TweenFrames {
					want := tc.opacities[j]
					if i == 1 {
						want = tc.opacities[4-j]
					}
					a, b := shapeByID(t, f, "a"), shapeByID(t, f, "b")
					if a.Opacity != want || b.Opacity != want || f.Connections[0].Opacity != want {
						t.Fatalf("step %d sample %d: opacity of class, shape and edge = %g, %g, %g, want %g", i, j, a.Opacity, b.Opacity, f.Connections[0].Opacity, want)
					}
					if j == 1 && i == 0 && a.Label != tc.firstLabel {
						t.Fatalf("counter label = %q, want %q", a.Label, tc.firstLabel)
					}
				}
			}
		})
	}
}

func TestGridCounterKeepsExistingSlotsWithoutMembershipChange(t *testing.T) {
	d, _ := compileGridMotionTest(t, `
vars: {n: -9; d2-config: {animate-interval: 1s; animate-duration: 800ms}}
grid-rows: 1
grid-columns: 3
grid-gap: 10
count: "${n}" {style.font: mono}
worker: Writer
steps: {start: {vars: {n: 10}}}
`)
	step := d.Steps[0]
	singleDigit := false
	for _, f := range step.TweenFrames {
		for _, id := range []string{"count", "worker"} {
			old, sample := shapeByID(t, d, id), shapeByID(t, f, id)
			if !sameSlot(&old, &sample) {
				t.Fatalf("%s changed its slot from (%v, %d×%d) to (%v, %d×%d)", id, old.Pos, old.Width, old.Height, sample.Pos, sample.Width, sample.Height)
			}
			if id == "count" && len(sample.Label) == 1 {
				singleDigit = true
			}
		}
	}
	if !singleDigit {
		t.Fatal("missing single-digit sample")
	}

	// A wider paint sample may need space beyond its endpoints.
	cell := &d2target.Shape{ID: "cell", Width: 20, Height: 10}
	m := gridMotion{items: map[string]*gridMotionItem{"cell": {from: cell, to: cell}}}
	wide := *cell
	wide.Width++
	endpoint := &d2target.Diagram{Shapes: []d2target.Shape{*cell}}
	endpoint.TweenFrames = []*d2target.Diagram{{Shapes: []d2target.Shape{wide}}}
	if m.paintReflowsSlots(endpoint, endpoint, gridShapeNodes(endpoint)) {
		t.Fatal("a wider intermediate label would be cropped")
	}
}
