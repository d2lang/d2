package d2lib

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2target"
)

func TestGridBandRecognition(t *testing.T) {
	for _, tc := range []struct {
		name       string
		rows, cols int
		order      string
		shouldOpen bool
	}{
		{"column", 2, 3, "a x b c y d", true},
		{"row", 3, 2, "a b x y c d", true},
		{"leading row", 3, 2, "x y a b c d", true},
		{"stationary trailing row", 3, 2, "a b c d x y", false},
		{"half row", 3, 2, "a b x c d", false},
		{"scattered insertions", 2, 3, "a x b y c d", false},
		{"shuffled survivors", 3, 2, "b a x y c d", false},
		{"replacement and insertion", 3, 2, "a b x y c z", false},
		{"one-cell replacement", 2, 2, "a b x d", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cell := func(id string) string { return fmt.Sprintf("%s: cell {width: 80; height: 50}\n", id) }
			var src strings.Builder
			src.WriteString("grid-rows: 2; grid-columns: 2\n")
			src.WriteString("title: Release matrix {shape: text; near: top-center}\n")
			for _, id := range strings.Fields("a b c d") {
				src.WriteString(cell(id))
			}
			fmt.Fprintf(&src, "steps: {edit: {grid-rows: %d; grid-columns: %d\n", tc.rows, tc.cols)
			src.WriteString("a: null; b: null; c: null; d: null\n")
			for _, id := range strings.Fields(tc.order) {
				src.WriteString(cell(id))
			}
			src.WriteString("}}")
			board, g := compileGridMotionTest(t, src.String())
			old, next := gridNodes(board, g), gridNodes(board.Steps[0], g.Steps[0])
			for _, test := range []struct {
				before, after map[string]*gridNode
				want          gridBand
			}{{old, next, gridBandOpen}, {next, old, gridBandClose}} {
				plans := planGridBands(test.before, test.after)
				if plans["title"] != gridBandNone {
					t.Fatalf("detached title joined the grid band: %v", plans)
				}
				if !tc.shouldOpen {
					if plans != nil {
						t.Fatalf("unexpected band: %v", plans)
					}
					continue
				}
				if plans["x"] != test.want || plans["y"] != test.want {
					t.Fatalf("missing band cells: %v", plans)
				}
				for _, id := range strings.Fields("a b c d") {
					moved := gridBandBoundsChanged(old[id], next[id])
					if (plans[id] == test.want) != moved {
						t.Fatalf("unexpected timing for %s, moved=%v: %v", id, moved, plans)
					}
				}
			}
		})
	}
}

func TestGridBandNestedMembership(t *testing.T) {
	board, g := compileGridMotionTest(t, `
rack: {
 grid-rows: 2; grid-columns: 2
 a: {grid-columns: 1; primary}
 b; c: {grid-columns: 1; primary}; d
}
side: {grid-columns: 2; service}
steps: {edit: {
 rack: {
  grid-rows: 3; grid-columns: 2
  c: null; d: null
  x: {grid-columns: 2; audit; metrics}; y; c: {grid-columns: 1; primary; extra}; d
  a.extra
 }
 side.observer
}}
`)
	old, next := gridNodes(board, g), gridNodes(board.Steps[0], g.Steps[0])
	plans := planGridBands(old, next)
	for _, id := range []string{"rack", "rack.x", "rack.x.audit", "rack.x.metrics", "rack.y", "rack.c", "rack.d"} {
		if plans[id] != gridBandOpen {
			t.Fatalf("%s missing from band: %v", id, plans)
		}
	}
	for _, id := range []string{"rack.a.extra", "rack.c.extra", "side", "side.observer", "side.service"} {
		if plans[id] != gridBandNone {
			t.Fatalf("unrelated %s joined band: %v", id, plans)
		}
	}
	// An independently added child uses the ordinary pop clock but follows
	// the position of its parent, which is sliding on the band clock.
	motion := newGridMotion(board, board.Steps[0], g, g.Steps[0])
	sample := motion.sample(board.Steps[0], 670, 900)
	parent, child := shapeByID(t, sample, "rack.c"), shapeByID(t, sample, "rack.c.extra")
	destParent, destChild := next["rack.c"].shape, next["rack.c.extra"].shape
	if child.Width != destChild.Width || child.Opacity != destChild.Opacity ||
		child.Pos.Y-parent.Pos.Y != destChild.Pos.Y-destParent.Pos.Y {
		t.Fatalf("child failed to follow parent after its own pop settled: frame %v/%v, endpoint %v/%v", child.Pos, parent.Pos, destChild.Pos, destParent.Pos)
	}
}

func TestGridBandStages(t *testing.T) {
	const duration = 640
	start, _ := gridBandOpen.window(duration)
	shift, _, alpha := gridBandOpen.sample(start/2, duration)
	if shift <= 0 || shift >= 1 || alpha != 0 {
		t.Fatalf("expected opening gap, got travel=%v alpha=%v", shift, alpha)
	}
	shift, scale, alpha := gridBandOpen.sample(start+(duration-start)*3/4, duration)
	if shift != 1 || scale <= 0 || scale >= 1 || alpha != 1 {
		t.Fatalf("expected band settling after neighbors arrived, got travel=%v scale=%v alpha=%v", shift, scale, alpha)
	}
	for ms := 0; ms <= duration; ms++ {
		travel, scale, alpha := gridBandOpen.sample(ms, duration)
		back, shrink, fade := gridBandClose.sample(duration-ms, duration)
		if math.Abs(back-(1-travel)) > 1e-12 || scale != shrink || alpha != fade {
			t.Fatalf("removal not reverse at %dms: %g %g %g / %g %g %g", ms, 1-travel, scale, alpha, back, shrink, fade)
		}
	}
}

// Variable-height cards in one row need only overlap along that axis.
func TestGridBandCenteredCells(t *testing.T) {
	var cells []*gridNode
	for _, s := range []d2target.Shape{
		{ID: "a", Pos: d2target.Point{X: 0, Y: 0}, Width: 40, Height: 40}, {ID: "b", Pos: d2target.Point{X: 40, Y: 10}, Width: 30, Height: 20},
		{ID: "c", Pos: d2target.Point{X: 5, Y: 40}, Width: 30, Height: 20}, {ID: "d", Pos: d2target.Point{X: 40, Y: 40}, Width: 30, Height: 20},
	} {
		cells = append(cells, &gridNode{shape: &s})
	}
	m := gridBandLayout(cells)
	if m == nil || m.rows != 2 || m.cols != 2 || m.slots["b"] != (gridBandSlot{0, 1}) {
		t.Fatalf("lost shared track: %v", m)
	}
}

func TestGridBandConnectedCell(t *testing.T) {
	board, g := compileGridMotionTest(t, `
grid-rows: 2; grid-columns: 2
a; b; c; d
steps: {edit: {
 grid-columns: 3
 b: null; c: null; d: null
 x; b; c; y; d
 x -> b
}}`)
	full, fullGraph := board.Steps[0], g.Steps[0]
	for _, c := range []struct {
		motion          *gridMotion
		base            *d2target.Diagram
		hidden, visible int
	}{{newGridMotion(board, full, g, fullGraph), full, 650, 700}, {newGridMotion(full, board, fullGraph, g), board, 250, 200}} {
		if c.motion.bands["x"] == gridBandNone {
			t.Fatal("connected stripe undetected")
		}
		if edges := c.motion.sample(c.base, c.hidden, 900).Connections; len(edges) != 0 {
			t.Fatalf("edge visible without its stripe: %v", edges)
		}
		frame := c.motion.sample(c.base, c.visible, 900)
		cell := shapeByID(t, frame, "x")
		if len(frame.Connections) != 1 || frame.Connections[0].Opacity != cell.Opacity || cell.Opacity <= 0 || cell.Opacity >= 1 {
			t.Fatalf("edge and band opacity disagree: %v vs %v", frame.Connections, cell.Opacity)
		}
	}
}
