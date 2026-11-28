package d2lib

import (
	"math"
	"reflect"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/lib/animation"
	d2log "github.com/d2lang/d2/lib/log"
)

func TestCompileFractionalPins(t *testing.T) {
	const source = `vars: {x: 10; x-alias: ${x}; d2-config: {animate-duration: 100ms; animate-interval: 150ms}}
 a: "" {left: "${x-alias}"; top: 20; width: 40; height: 40}
 b: "" {left: 210; top: 20; width: 40; height: 40}
 a -> b
 steps: {move: {vars.x: 13; b.top: 23}}
 `
	board, graph := compileContinuity(t, source, d2talalayout.ContinueBoard)
	step := board.Steps[0]
	if len(step.TweenFrames) != 6 || step.TweenDurationMS != 100 {
		t.Fatalf("got %d samples and %dms", len(step.TweenFrames), step.TweenDurationMS)
	}
	for i, frame := range step.TweenFrames {
		a, b := shapeByID(t, frame, "a"), shapeByID(t, frame, "b")
		pa, pb := a.RenderedPosition(), b.RenderedPosition()
		distance := 3 * animation.EaseInOut(float64(i)/6)
		if a.MotionOffset == nil || b.MotionOffset == nil || math.Abs(pa.X-(10+distance)) > 1e-9 || pa.Y != 20 || pb.X != 210 || math.Abs(pb.Y-(20+distance)) > 1e-9 {
			t.Fatalf("frame %d: variable and direct pin positions %v %+v, %v %+v", i, pa, a.MotionOffset, pb, b.MotionOffset)
		}
		// The router returns a point on the integer layout at each end. Both
		// anchors must follow their leaf, even as they move in different axes.
		route := frame.Connections[0].Route
		layout := graph.Steps[0].TweenFrames[i].Edges[0].Route
		for _, port := range []struct {
			i      int
			dx, dy float64
		}{
			{0, a.MotionOffset.X, a.MotionOffset.Y}, {len(route) - 1, b.MotionOffset.X, b.MotionOffset.Y},
		} {
			if math.Abs(route[port.i].X-layout[port.i].X-port.dx) > 1e-9 || math.Abs(route[port.i].Y-layout[port.i].Y-port.dy) > 1e-9 {
				t.Fatalf("frame %d detached route port %d", i, port.i)
			}
		}
	}
	for _, s := range append(board.Shapes, step.Shapes...) {
		if s.MotionOffset != nil {
			t.Fatalf("authored endpoint %s carries tween data", s.ID)
		}
	}
	engine := "tala"
	still, _, err := Compile(d2log.WithDefault(t.Context()), source, &CompileOptions{
		Layout: &engine, Ruler: layoutReuseTestRuler, AnimateTweens: new(false),
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) { return d2talalayout.DefaultLayout, nil },
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(still.Steps[0].TweenFrames) != 0 || !reflect.DeepEqual(still.Steps[0].Shapes, step.Shapes) {
		t.Fatal("static compilation changed the authored end shapes")
	}
	// A layout that ignored the pin must not move the shape by its remainder.
	ignored, _, err := Compile(d2log.WithDefault(t.Context()), source, dagreCompileOptions(new(int)), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range ignored.Steps[0].TweenFrames {
		for _, s := range frame.Shapes {
			if s.MotionOffset != nil {
				t.Fatalf("ignored pin moved %s", s.ID)
			}
		}
	}
}
