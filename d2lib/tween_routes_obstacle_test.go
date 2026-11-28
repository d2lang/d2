package d2lib

import (
	"math"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// Both held layouts have the same straight route. Only intermediate geometry
// reveals that a shape crosses it. An endpoint-only sweep would draw through it.
func TestCompileTweenRouteMovingBlocker(t *testing.T) {
	root, _ := compileContinuity(t, `vars: {
  d2-config: {layout-engine: tala; animate-interval: 1600ms; animate-duration: 850ms}
  obstacle-y: 0
}
a: "" {left: 0; top: 90; width: 60; height: 60}
b: "" {left: 300; top: 90; width: 60; height: 60}
blocker: "" {left: 151; top: ${obstacle-y}; width: 60; height: 48}
a -> b: {style.border-radius: 0}
steps.move.vars: {obstacle-y: 200}
`, d2talalayout.ContinueBoard)
	step := root.Steps[0]
	if len(step.TweenFrames) < 5 {
		t.Fatalf("got only %d samples", len(step.TweenFrames))
	}
	for _, board := range []*d2target.Diagram{root, step} {
		if !slices.EqualFunc(board.Connections[0].Route, []*geo.Point{geo.NewPoint(60, 120), geo.NewPoint(300, 120)}, func(a, b *geo.Point) bool { return a.Equals(b) }) {
			t.Fatalf("expected an unobstructed held route, got %v", board.Connections[0].Route)
		}
	}
	var detoured int
	for i, f := range step.TweenFrames {
		a, b, c := shapeByID(t, f, "a"), shapeByID(t, f, "b"), shapeByID(t, f, "blocker")
		if a.Pos.X != 0 || a.Pos.Y != 90 || a.Width != 60 || a.Height != 60 || b.Pos.X != 300 || b.Pos.Y != 90 || b.Width != 60 || b.Height != 60 || c.Pos.X != 151 || c.Width != 60 || c.Height != 48 {
			t.Fatalf("unexpected sampled positions or dimensions at %d: a=%v b=%v blocker=%v", i, a.Pos, b.Pos, c.Pos)
		}
		edge := f.Connections[0]
		var last geo.Point
		src, dst := edge.ArrowheadAdjustmentsForWidths(a.RenderedStrokeWidth(), b.RenderedStrokeWidth())
		detour := false
		for cmd := range edge.PathCommands(src, dst) {
			if cmd.Kind != 'M' {
				count := int(math.Ceil(math.Hypot(cmd.End.X-last.X, cmd.End.Y-last.Y)))
				for n := 1; n <= count; n++ {
					u := float64(n) / float64(count)
					p := last.Interpolate(&cmd.End, u)
					if p.X > float64(c.Pos.X)+1 && p.X < float64(c.Pos.X+c.Width)-1 && p.Y > float64(c.Pos.Y)+1 && p.Y < float64(c.Pos.Y+c.Height)-1 {
						t.Fatalf("sample %d routes through blocker at %.2f,%.2f", i, p.X, p.Y)
					}
					if p.X >= 155 && p.X <= 200 && math.Abs(p.Y-120) > 5 {
						detour = true
					}
				}
			}
			last = cmd.End
		}
		if c.Pos.Y < 119 && c.Pos.Y+c.Height > 121 && detour {
			detoured++
		}
	}
	if detoured < 2 {
		t.Fatalf("expected several samples detouring around the moving shape, got %d", detoured)
	}
	if shapeByID(t, root, "blocker").Pos.Y != 0 || shapeByID(t, step, "blocker").Pos.Y != 200 {
		t.Fatal("obstacle did not travel through the wire")
	}
}
