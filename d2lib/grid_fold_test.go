package d2lib

import (
	"reflect"
	"testing"

	"github.com/d2lang/d2/d2target"
)

func TestGridFoldKeepsDetailsAndLinksInViewport(t *testing.T) {
	board, g := compileGridMotionTest(t, `
grid-rows: 3
grid-columns: 1
before: overview {width: 370; height: 50}
region: east {width: 370; height: 50}
after: standby {width: 370; height: 50}
steps: {
 open: {
  region: {
   height: 145
   grid-rows: 1
   grid-columns: 2
   a: scheduler {width: 105; height: 55}
   b: workers {width: 105; height: 55}
   a -> b
  }
 }
 close: {
  region.(a -> b)[0]: null
  region.a: null
  region.b: null
  region.grid-rows: null
  region.grid-columns: null
  region.height: 50
 }
}`)
	open, closed := board.Steps[0], board.Steps[1]
	unfold := newGridMotion(board, open, g, g.Steps[0])
	fold := newGridMotion(open, closed, g.Steps[0], g.Steps[1])
	if unfold.folds["region"] == nil || fold.folds["region"] == nil {
		t.Fatal("missing fold plan")
	}
	first := unfold.sample(open, 0, 1000)
	last := fold.sample(closed, 1000, 1000)
	if len(first.Shapes) != len(board.Shapes) || len(last.Shapes) != len(closed.Shapes) || len(last.Connections) != 0 {
		t.Fatal("collapsed frame contains details")
	}
	for _, ms := range []int{400, 550, 700, 850} {
		forward := unfold.sample(open, ms, 1000)
		backward := fold.sample(closed, 1000-ms, 1000)
		parent := shapeByID(t, forward, "region")
		a := shapeByID(t, forward, "region.a")
		b := shapeByID(t, forward, "region.b")
		if a.Width != shapeByID(t, open, "region.a").Width || a.Opacity < b.Opacity || a.MotionClip == nil || b.MotionClip == nil || *a.MotionClip != *b.MotionClip {
			t.Fatalf("children lost their shared viewport or stagger at %d", ms)
		}
		if parent.MotionLabelTopLeft == nil || a.MotionClip.Y <= parent.MotionLabelTopLeft.Y || a.MotionClip.Y+a.MotionClip.Height > parent.Pos.Y+parent.Height {
			t.Fatalf("viewport crossed the parent boundary or caption at %d", ms)
		}
		reverseA := shapeByID(t, backward, "region.a")
		if !reflect.DeepEqual(a.MotionClip, reverseA.MotionClip) || a.Pos != reverseA.Pos || shapeByID(t, backward, "after").Pos != shapeByID(t, forward, "after").Pos {
			t.Fatalf("closing does not retrace reveal at %d", ms)
		}
		if len(forward.Connections) != 1 || !reflect.DeepEqual(forward.Connections[0].MotionClip, a.MotionClip) {
			t.Fatal("internal connection escaped the viewport")
		}
		if err := d2target.ValidateRenderTarget(forward); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGridFoldOnlyForExpandingSummaryCells(t *testing.T) {
	board, g := compileGridMotionTest(t, `
grid-columns: 1
region: {width: 450; height: 200; grid-columns: 2; a: reader}
steps: {add: {region.b: writer}}`)
	motion := newGridMotion(board, board.Steps[0], g, g.Steps[0])
	if motion == nil || len(motion.folds) != 0 {
		t.Fatal("adding an ordinary grid member triggered a fold")
	}
}
