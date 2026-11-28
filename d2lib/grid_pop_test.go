package d2lib

import (
	"math"
	"reflect"
	"testing"

	"github.com/d2lang/d2/d2target"
)

func TestGridPopStrokeScaleAtIntegerWidth(t *testing.T) {
	const scale = 0.6
	shape := d2target.Shape{StrokeWidth: 4}
	item := gridMotionItem{pivot: &shape}
	for _, width := range []float64{3.9, 4, 4.1} {
		shape.StrokeWidthOffset = width - 4
		frame := item.bubble(shape, scale, 1, 0)
		if got, want := frame.RenderedStrokeWidth(), width*scale; got != want {
			t.Errorf("scaled stroke width %v = %v, want %v", width, got, want)
		}
	}
}

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
	// The label reaches its authored opacity halfway through the growth.
	for _, want := range []struct {
		ms, width int
		opacity   float64
	}{{90, 55, .4}, {180, 95, .8}, {270, 134, .8}} {
		cell := shapeByID(t, enter.sample(up, want.ms, duration), "spare")
		if cell.Width != want.width || cell.Opacity != want.opacity {
			t.Errorf("arrival at %dms: width=%d opacity=%g, want %d/%g", want.ms, cell.Width, cell.Opacity, want.width, want.opacity)
		}
	}
	lastWidth, lastOpacity := 0, 0.
	for ms := 10; ms < 360; ms += 10 {
		in := shapeByID(t, enter.sample(up, ms, duration), "spare")
		out := shapeByID(t, exit.sample(down, 360-ms, duration), "spare")
		if in.Width != out.Width || math.Abs(in.Opacity-out.Opacity) > 1e-12 {
			t.Fatalf("entry and reversed exit differ at %dms: width=%d/%d opacity=%g/%g", ms, in.Width, out.Width, in.Opacity, out.Opacity)
		}
		if in.Width < lastWidth || in.Width > target.Width || in.Opacity < lastOpacity || in.Opacity > target.Opacity {
			t.Fatalf("arrival overshot or reversed at %dms: width=%d opacity=%v", ms, in.Width, in.Opacity)
		}
		lastWidth, lastOpacity = in.Width, in.Opacity
	}
	for _, ms := range []int{360, 400, duration} {
		if s := shapeByID(t, enter.sample(up, ms, duration), "spare"); !reflect.DeepEqual(s, target) {
			t.Fatalf("arrival did not settle at %dms", ms)
		}
		if got := exit.sample(down, ms, duration); len(got.Shapes) != len(down.Shapes) {
			t.Fatalf("exit remained visible at %dms", ms)
		}
	}
}
