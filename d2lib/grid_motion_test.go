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
	mid := m.sample(&base, 500, 1000)
	a, b := shapeByID(t, mid, "a"), shapeByID(t, mid, "b")
	if a.Pos != b.Pos || a.Pos.Y != shapeByID(t, board, "a").Pos.Y {
		t.Fatalf("swapping cells should cross on their original row: %v vs %v", a.Pos, b.Pos)
	}
	if a.Fill != "orange" || shapeByID(t, mid, "stay").Pos != shapeByID(t, board, "stay").Pos {
		t.Fatal("sample lost its existing style or moved a stationary cell")
	}
	for _, tc := range []struct {
		t   int
		end *d2target.Diagram
	}{{0, board}, {1000, next}} {
		f := m.sample(&base, tc.t, 1000)
		for _, s := range f.Shapes {
			want := shapeByID(t, tc.end, s.ID)
			if s.Pos != want.Pos || s.Width != want.Width || s.Height != want.Height {
				t.Fatalf("t=%dms %s geometry mismatch", tc.t, s.ID)
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
		time   int
	}{{add, next, 144}, {remove, last, 128}} {
		mid := tc.motion.sample(tc.base, tc.time, 240)
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
	if len(add.sample(next, 0, 240).Shapes) != len(board.Shapes) || len(remove.sample(last, 240, 240).Shapes) != len(last.Shapes) {
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
	// The promoted cell and each displaced neighbor should travel along the
	// direct segment to the destination, including the job that wraps rows.
	for _, ms := range []int{150, 350, 550, 750, 950} {
		fraction := float64(ms) / 1000
		frame := m.sample(next, ms, 1000)
		for i, shape := range frame.Shapes {
			if shape.ID != board.Shapes[i].ID {
				t.Fatalf("at %.2f, paint order changed during the crossing", fraction)
			}
			src := shapeByID(t, board, shape.ID).Pos
			dst := shapeByID(t, next, shape.ID).Pos
			travel := gridEase(fraction)
			want := d2target.Point{X: gridLerpInt(src.X, dst.X, travel), Y: gridLerpInt(src.Y, dst.Y, travel)}
			if shape.Pos != want {
				t.Fatalf("at %.2f, %s is at %v; want %v on the direct path", fraction, shape.ID, shape.Pos, want)
			}
		}
	}
}
