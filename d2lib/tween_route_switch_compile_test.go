package d2lib

import (
	"slices"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/lib/geo"
)

// A blocker approaches a straight wire between stationary boxes. The top
// detour clears before the straight route closes, so a single cut suffices.
func TestCompileTweenRouteSwitch(t *testing.T) {
	const source = `vars: {
  d2-config: {layout-engine: tala; animate-interval: 1600ms; animate-duration: 850ms}
  blocker-y: 0
}
a: "" {left: 0; top: 90; width: 60; height: 60}
b: "" {left: 300; top: 90; width: 60; height: 60}
blocker: "" {left: 151; top: ${blocker-y}; width: 60; height: 48}
a -> b: {style.border-radius: 0}
steps.move.vars.blocker-y: 100`
	root, _ := compileContinuity(t, source, d2talalayout.ContinueBoard)
	step := root.Steps[0]
	from, to := root.Connections[0].Route, step.Connections[0].Route
	if len(from) != 2 || len(to) != 4 || len(step.TweenFrames) < 3 {
		t.Fatal("fixture should turn a straight wire into a three-leg detour")
	}

	switched := false
	for i, frame := range append(slices.Clone(step.TweenFrames), step) {
		route := frame.Connections[0].Route
		switch {
		case slices.EqualFunc(route, from, (*geo.Point).Equals):
			if switched {
				t.Fatalf("sample %d flashed back to the old route", i)
			}
		case slices.EqualFunc(route, to, (*geo.Point).Equals):
			switched = true
		default:
			t.Fatalf("sample %d invented an intermediate route between stationary endpoints", i)
		}

		// Only the crossbar passes the blocker's x position in either route.
		crossbarY := route[len(route)/2].Y
		blocker := shapeByID(t, frame, "blocker")
		if crossbarY >= float64(blocker.Pos.Y) && crossbarY <= float64(blocker.Pos.Y+blocker.Height) {
			t.Fatalf("sample %d crosses the moving blocker at y=%g", i, crossbarY)
		}
	}
}
