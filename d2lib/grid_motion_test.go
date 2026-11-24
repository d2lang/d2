package d2lib

import (
	"context"
	"math"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/log"
)

func compileGridMotionTest(t *testing.T, source string) (*d2target.Diagram, *d2graph.Graph) {
	t.Helper()
	var calls int
	board, graph, err := Compile(log.WithDefault(context.Background()), source, dagreCompileOptions(&calls), nil)
	if err != nil {
		t.Fatal(err)
	}
	return board, graph
}

func TestGridShuffleGeometry(t *testing.T) {
	board, graph := compileGridMotionTest(t, `
grid-columns: 3
a: slot {width: 180; height: 60}
b: slot {width: 180; height: 60}
stay: slot {width: 180; height: 60}
steps: {shuffle: {
 b: null
 a: null
 stay: null
 b: slot {width: 180; height: 60}
 a: slot {width: 180; height: 60}
 stay: slot {width: 180; height: 60}
}}
`)
	next := board.Steps[0]
	m := newGridMotion(board, next, graph, graph.Steps[0])
	if m == nil {
		t.Fatal("missing shuffle")
	}
	base := *next
	base.Shapes = slices.Clone(base.Shapes)
	for i := range base.Shapes {
		base.Shapes[i].Fill = "orange"
	}
	mid := m.sample(&base, .5)
	a, b := shapeByID(t, mid, "a"), shapeByID(t, mid, "b")
	if a.Pos.X != b.Pos.X || math.Abs(float64(a.Pos.Y-b.Pos.Y)) < float64(a.Height) {
		t.Fatalf("swapping cells overlap or travel at different speeds: %v vs %v", a.Pos, b.Pos)
	}
	if a.Fill != "orange" || shapeByID(t, mid, "stay").Pos != shapeByID(t, board, "stay").Pos {
		t.Fatal("sample lost its existing style or moved a stationary cell")
	}
	for _, tc := range []struct {
		t   float64
		end *d2target.Diagram
	}{{0, board}, {1, next}} {
		f := m.sample(&base, tc.t)
		for _, s := range f.Shapes {
			want := shapeByID(t, tc.end, s.ID)
			if s.Pos != want.Pos || s.Width != want.Width || s.Height != want.Height {
				t.Fatalf("t=%g %s geometry mismatch", tc.t, s.ID)
			}
		}
	}
	if newGridMotion(next, next, graph.Steps[0], graph.Steps[0]) != nil {
		t.Fatal("stationary grid allocated motion")
	}
}

func TestGridBubbleSubtree(t *testing.T) {
	board, graph := compileGridMotionTest(t, `
service: {
 grid-columns: 2
 hot: Hot
}
steps: {
 add: {service.cold: Cold {grid-columns: 2; stores: Stores; logs: Logs}}
 remove: {service.cold: null}
}
`)
	next, last := board.Steps[0], board.Steps[1]
	add := newGridMotion(board, next, graph, graph.Steps[0])
	remove := newGridMotion(next, last, graph.Steps[0], graph.Steps[1])
	if add == nil || remove == nil {
		t.Fatal("missing nested grid transition")
	}
	for _, tc := range []struct {
		motion *gridMotion
		base   *d2target.Diagram
		time   float64
	}{{add, next, .44}, {remove, last, .48}} {
		mid := tc.motion.sample(tc.base, tc.time)
		group, child := shapeByID(t, mid, "service.cold"), shapeByID(t, mid, "service.cold.stores")
		endGroup, endChild := shapeByID(t, next, "service.cold"), shapeByID(t, next, "service.cold.stores")
		if !(group.Opacity > 0 && group.Opacity < 1 && group.Width < endGroup.Width && child.Opacity == group.Opacity) {
			t.Fatalf("bubble did not scale and fade subtree: width %d/%d opacity %g", group.Width, endGroup.Width, group.Opacity)
		}
		scale := float64(group.Width) / float64(endGroup.Width)
		want := scale * float64(endChild.Pos.X-endGroup.Pos.X)
		if math.Abs(float64(child.Pos.X-group.Pos.X)-want) > 2 {
			t.Fatal("child used its own pivot")
		}
	}
	if len(add.sample(next, 0).Shapes) != len(board.Shapes) || len(remove.sample(last, 1).Shapes) != len(last.Shapes) {
		t.Fatal("invisible bubbles leaked into endpoints")
	}
}

func TestGridIgnoresChangesOutsideGrid(t *testing.T) {
	board, graph := compileGridMotionTest(t, `pool: {grid-columns: 2; a; b}
one
steps: {edit: {one.style.fill: red}}
`)
	if newGridMotion(board, board.Steps[0], graph, graph.Steps[0]) != nil {
		t.Fatal("non-grid style edit triggered grid movement")
	}
}

func TestGridPriorityQueueRotation(t *testing.T) {
	board, graph := compileGridMotionTest(t, `
grid-rows: 2; grid-columns: 3; grid-gap: 16
classes: {cell: {width: 168; height: 71}}
a.class: cell; b.class: cell; c.class: cell
d.class: cell; e.class: cell; f.class: cell
steps: {promote: {
 a: null; b: null; c: null; d: null; f: null
 a.class: cell; b.class: cell; c.class: cell; d.class: cell; f.class: cell
}}
`)
	next := board.Steps[0]
	m := newGridMotion(board, next, graph, graph.Steps[0])
	var worst float64
	var time int
	var pair string
	for i := 0; i <= 200; i++ {
		s := m.sample(next, float64(i)/200)
		for j, a := range s.Shapes {
			for _, b := range s.Shapes[:j] {
				w := max(0, min(a.Pos.X+a.Width, b.Pos.X+b.Width)-max(a.Pos.X, b.Pos.X))
				h := max(0, min(a.Pos.Y+a.Height, b.Pos.Y+b.Height)-max(a.Pos.Y, b.Pos.Y))
				f := float64(w*h) / float64(min(a.Width*a.Height, b.Width*b.Height))
				if f > worst {
					worst = f
					time = i
					pair = a.ID + " & " + b.ID
				}
			}
		}
	}
	if worst > .08 {
		t.Fatalf("maximum overlap %.1f%% for %s at %d/200", 100*worst, pair, time)
	}
	t.Logf("maximum overlap %.1f%% for %s at %d/200", 100*worst, pair, time)
}
