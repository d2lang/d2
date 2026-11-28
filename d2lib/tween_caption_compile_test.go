package d2lib

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// A moving worker shares a connection with a table inside a stationary
// container. The static layouts move the container's caption out of the way
// of an intermediate route. The final animation routes clear the original
// caption, so its rendered anchor can remain steady throughout the tween.
func TestCompileTweenContainerCaption(t *testing.T) {
	const src = `vars: {
  d2-config: {layout-engine: tala; animate-interval: 1600ms; animate-duration: 1000ms}
  x: 60
}
store: COLLECTIONS {
  left: 462; top: 1162
  item: "" {left: 167; top: 172; width: 117; height: 66}
}
worker: Worker {
  left: ${x}; top: 568
  task: "" {left: 155; top: 172; width: 108; height: 66}
}
store.item -> worker.task
steps.move.vars.x: 860`

	const siblings = "\nstore.files: \"\" {left: 0; top: 86; width: 195; height: 66}\n" +
		"store.tags: \"\" {left: 0; top: 172; width: 147; height: 66}"

	anchor := func(s d2target.Shape) *geo.Point {
		pos := label.FromString(s.LabelPosition)
		return s.LabelTopLeft(pos, s.Geometry().GetInnerBox())
	}
	t.Run("automatic", func(t *testing.T) {
		root, _ := compileContinuity(t, src+siblings, d2talalayout.ContinueBoard)
		step := root.Steps[0]
		if len(step.TweenFrames) < 3 {
			t.Fatal("expected a sampled transition")
		}
		before, after := shapeByID(t, root, "store"), shapeByID(t, step, "store")
		want := anchor(before)
		if !want.Equals(anchor(after)) {
			t.Fatal("fixture must have matching held caption anchors")
		}
		for i, frame := range step.TweenFrames {
			current := shapeByID(t, frame, "store")
			if got := anchor(current); !want.Equals(got) {
				t.Fatalf("frame %d moved the stationary caption from %v to %v", i, want, got)
			}
		}
	})
	t.Run("remaining wire obstruction", func(t *testing.T) {
		// Without these two children the router chooses a different path
		// that really does pass through the preferred top caption.
		root, _ := compileContinuity(t, src, d2talalayout.ContinueBoard)
		start := shapeByID(t, root, "store")
		want := anchor(start)
		blocked := 0
		for i, f := range root.Steps[0].TweenFrames {
			current := shapeByID(t, f, "store")
			source, target := shapeByID(t, f, "store.item"), shapeByID(t, f, "worker.task")
			ink := tweenRouteInkFor(&f.Connections[0], tweenRouteShapePair{&source, &target})
			stroke, _ := ink.overlap(tweenRouteTextBox(want, current.LabelWidth, current.LabelHeight), -1)
			if stroke == 0 {
				continue
			}
			blocked++
			if got := anchor(current); got.Equals(want) {
				t.Fatalf("frame %d kept its caption on top of the wire", i)
			}
		}
		if blocked == 0 {
			t.Fatal("expected the final wire to obstruct the held caption")
		}
	})
	t.Run("authored change", func(t *testing.T) {
		input := src + siblings + "\nstore.label.near: top-center\nsteps.move.store.label.near: bottom-center"
		root, _ := compileContinuity(t, input, d2talalayout.ContinueBoard)
		step := root.Steps[0]
		from, to := shapeByID(t, root, "store"), shapeByID(t, step, "store")
		if from.LabelPosition != label.InsideTopCenter.String() || to.LabelPosition != label.InsideBottomCenter.String() {
			t.Fatalf("unexpected authored anchors: %s and %s", from.LabelPosition, to.LabelPosition)
		}
		// Label placement is discrete. This step explicitly sets the new
		// anchor while the worker moves.
		for i, frame := range step.TweenFrames {
			current := shapeByID(t, frame, "store")
			if current.LabelPosition != to.LabelPosition || !anchor(current).Equals(anchor(to)) {
				t.Fatalf("frame %d lost the step's authored bottom-center caption", i)
			}
		}
	})
}
