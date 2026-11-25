package d2lib

import "testing"

func TestRepeatedGridDimensionKeepsStepStationary(t *testing.T) {
	board, _ := compileGridMotionTest(t, `vars: {d2-config: {animate-interval: 600ms; animate-duration: 300ms}}
grid-rows: 2
grid-columns: 2
a; b; c; d
steps: {repeat: {grid-rows: 2}}`)
	next := board.Steps[0]
	if next.GridMotion {
		t.Fatal("repeating a dimension moved grid cells")
	}
	for _, cell := range board.Shapes {
		if got := shapeByID(t, next, cell.ID); got.Pos != cell.Pos {
			t.Fatalf("%s moved from %v to %v", cell.ID, cell.Pos, got.Pos)
		}
	}
}
