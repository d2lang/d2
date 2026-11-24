package d2lib

import (
	"reflect"
	"testing"
)

func TestGridPopsSettleWithinLongStep(t *testing.T) {
	board, graph := compileGridMotionTest(t, `grid-columns: 1
worker: Worker {width: 140; height: 60}
steps: {
  up: { spare: Spare {width: 140; height: 60; style.opacity: 0.8} }
  down: { spare: null }
}`)
	up, down := board.Steps[0], board.Steps[1]
	enter := newGridMotion(board, up, graph, graph.Steps[0])
	exit := newGridMotion(up, down, graph.Steps[0], graph.Steps[1])
	if enter == nil || exit == nil {
		t.Fatal("missing grid motion")
	}
	const duration = 4000
	target := shapeByID(t, up, "spare")
	// Midway through either gesture the card should be fully legible while
	// it still has a visible distance to grow (entry) or shrink (exit).
	inWidth := func(ms int) int { return shapeByID(t, enter.sample(up, ms, duration), "spare").Width }
	outWidth := func(ms int) int { return shapeByID(t, exit.sample(down, ms, duration), "spare").Width }
	halfIn := shapeByID(t, enter.sample(up, 110, duration), "spare")
	halfOut := shapeByID(t, exit.sample(down, 80, duration), "spare")
	if halfIn.Opacity != target.Opacity || halfOut.Opacity != target.Opacity ||
		halfIn.Width > target.Width*9/10 || halfIn.Width != halfOut.Width {
		t.Fatalf("midpoint width/opacity: entry %d/%.2f, exit %d/%.2f", halfIn.Width, halfIn.Opacity, halfOut.Width, halfOut.Opacity)
	}
	if inWidth(110)-inWidth(55) <= inWidth(165)-inWidth(110) {
		t.Fatal("arrival should decelerate toward the destination")
	}
	if outWidth(40)-outWidth(80) >= outWidth(80)-outWidth(120) {
		t.Fatal("removal should accelerate inward")
	}
	lastWidth, lastOpacity := 0, 0.
	for ms := 10; ms <= 220; ms += 10 {
		s := shapeByID(t, enter.sample(up, ms, duration), "spare")
		if s.Width < lastWidth || s.Width > target.Width || s.Opacity < lastOpacity || s.Opacity > target.Opacity {
			t.Fatalf("arrival overshot or reversed at %dms: width=%d opacity=%v", ms, s.Width, s.Opacity)
		}
		lastWidth, lastOpacity = s.Width, s.Opacity
	}
	for _, ms := range []int{220, 400, duration} {
		if s := shapeByID(t, enter.sample(up, ms, duration), "spare"); !reflect.DeepEqual(s, target) {
			t.Fatalf("arrival did not settle at %dms", ms)
		}
	}
	lastWidth, lastOpacity = target.Width, target.Opacity
	for ms := 0; ms < 160; ms += 10 {
		s := shapeByID(t, exit.sample(down, ms, duration), "spare")
		if s.Width > lastWidth || s.Opacity > lastOpacity {
			t.Fatalf("exit grew or brightened at %dms: width=%d opacity=%v", ms, s.Width, s.Opacity)
		}
		lastWidth, lastOpacity = s.Width, s.Opacity
	}
	if got := exit.sample(down, 160, duration); len(got.Shapes) != len(down.Shapes) {
		t.Fatal("exit remained visible after 160ms")
	}
}
