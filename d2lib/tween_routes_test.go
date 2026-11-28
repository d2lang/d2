package d2lib

import (
	"math"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2exporter"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/lib/geo"
)

// An independently laid-out sample can swap sides even with a fixed leaf
// and just a few pixels of motion on its parent. Exercise the public compiler,
// including edges that first exist in the moving step.
func TestCompileTweenRouteContinuity(t *testing.T) {
	const boxes = `vars: {
  d2-config: {layout-engine: tala; animate-interval: 2000ms; animate-duration: 850ms}
  x: 143; y: 112; w: 310
}
parent: "" {left: ${x}; top: ${y}; width: ${w}; height: 78}
leaf: "" {left: 44; top: 196; width: 164; height: 78}
`
	for _, tc := range []struct{ name, step string }{
		{"existing", "parent -> leaf\nsteps.move.vars: {x: 251; y: 57; w: 98}"},
		{"incoming", "steps.move: {vars: {x: 251; y: 57; w: 98}; parent -> leaf}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			board, _ := compileContinuity(t, boxes+tc.step, d2talalayout.ContinueBoard)
			step := board.Steps[0]
			if len(step.TweenFrames) != 22 {
				t.Fatalf("got %d tween frames", len(step.TweenFrames))
			}
			standalone, _ := compileContinuity(t, `parent: "" {left: 251; top: 57; width: 98; height: 78}
leaf: "" {left: 44; top: 196; width: 164; height: 78}
parent -> leaf`, d2talalayout.ContinueBoard)
			if !reflect.DeepEqual(step.Connections[0].Route, standalone.Connections[0].Route) {
				t.Fatal("held route differs from a standalone static layout")
			}

			var previous []*geo.Point
			if len(board.Connections) > 0 {
				previous = board.Connections[0].Route
				if !reflect.DeepEqual(step.TweenFrames[0].Connections[0].Route, previous) {
					t.Fatal("first sample moved the wire at time zero")
				}
			}
			var switches int
			for _, sample := range append(slices.Clone(step.TweenFrames), step) {
				r := sample.Connections[0].Route
				if previous != nil && measureTweenRoute(r).displacement2(measureTweenRoute(previous)) > 24*24 {
					switches++
				}
				for j := 1; j < len(r); j++ {
					if math.Abs(r[j].X-r[j-1].X) > 1e-7 && math.Abs(r[j].Y-r[j-1].Y) > 1e-7 {
						t.Fatalf("invented a slanted leg in an orthogonal route: %v", r)
					}
				}
				previous = r
				for _, port := range []struct {
					id string
					p  *geo.Point
				}{{"parent", r[0]}, {"leaf", r[len(r)-1]}} {
					s := shapeByID(t, sample, port.id)
					x, y := port.p.X-float64(s.Pos.X), port.p.Y-float64(s.Pos.Y)
					if x < -0.02 || y < -0.02 || x > float64(s.Width)+0.02 || y > float64(s.Height)+0.02 ||
						min(math.Abs(x), math.Abs(y), math.Abs(x-float64(s.Width)), math.Abs(y-float64(s.Height))) > .02 {
						t.Fatalf("port %v detached from %s", port.p, port.id)
					}
				}
			}
			// A pre-existing vertical route must switch to the held elbow.
			// A newly introduced connection can use the elbow from the start.
			if switches > 1 || tc.name == "incoming" && switches != 0 {
				t.Fatalf("unnecessary route switches: %d", switches)
			}
		})
	}
}

// Two new edges have to follow a moving, resizing node while other nodes
// cross their usual landing ports. A single frozen route cannot clear them.
func TestCompileTweenRouteSplit(t *testing.T) {
	source, err := os.ReadFile("testdata/tween-route-split.d2")
	if err != nil {
		t.Fatal(err)
	}
	root, _ := compileContinuity(t, string(source), d2talalayout.ContinueBoard)
	step := root.Steps[1]
	if len(step.TweenFrames) != 22 || len(step.Connections) != 2 {
		t.Fatal("missing split animation")
	}
	for j, held := range step.Connections {
		// Each new edge can keep the same pair of attachment sides all
		// the way through the split, including the final authored layout.
		sides := func(r []*geo.Point) [2]float64 {
			direction := func(a, b *geo.Point) float64 { return math.Atan2(b.Y-a.Y, b.X-a.X) }
			return [2]float64{direction(r[0], r[1]), direction(r[len(r)-1], r[len(r)-2])}
		}
		var previous routeLandmarks
		switches := 0
		for i, f := range append(slices.Clone(step.TweenFrames), step) {
			r := f.Connections[j].Route
			if sides(r) != sides(held.Route) {
				t.Fatalf("frame %d: %s changed attachment side", i, held.ID)
			}
			p := measureTweenRoute(r)
			if i > 0 && math.Sqrt(p.displacement2(previous)) > 22 {
				switches++
			}
			previous = p
		}
		// A key passes through this wire's held lane once. Entering the
		// detour and returning to the final lane need at most two cuts.
		if switches > 2 {
			t.Fatalf("%s took %d lane changes for one obstruction", held.ID, switches)
		}
	}
}

// The visible chart can contain several distinct wires between the same two
// boxes. Independently smoothing them would let their assigned lanes merge.
func TestCompileTweenRouteParallelLanes(t *testing.T) {
	root, g := compileContinuity(t, `vars: {
  d2-config: {layout-engine: tala; animate-interval: 2000ms; animate-duration: 850ms}
  x: 143; y: 112; w: 310
}
a: "" {left: ${x}; top: ${y}; width: ${w}; height: 78}
b: "" {left: 44; top: 196; width: 164; height: 78}
a -> b
b -> a
steps.move.vars: {x: 251; y: 57; w: 98}`, d2talalayout.ContinueBoard)
	if len(root.Steps[0].TweenFrames) == 0 {
		t.Fatal("expected samples")
	}
	for i, f := range root.Steps[0].TweenFrames {
		native, err := d2exporter.Export(t.Context(), g.Steps[0].TweenFrames[i], nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		for j, c := range f.Connections {
			if !reflect.DeepEqual(c.Route, native.Connections[j].Route) {
				t.Fatalf("parallel lane %s changed in frame %d", c.ID, i)
			}
		}
	}
}
