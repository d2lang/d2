package d2lib

import (
	"math"
	"reflect"
	"slices"
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
	// Paint-tween frames have their own layout. Even when their geometry
	// disagrees, take the pixels' color from the sample and the slot from the
	// open board. Check both directions through every part of the gesture.
	paint := *open
	paint.Shapes = slices.Clone(open.Shapes)
	for i := range paint.Shapes {
		if paint.Shapes[i].ID == "region.a" {
			paint.Shapes[i].Pos.Y -= 11
			paint.Shapes[i].Width += 9
			paint.Shapes[i].Fill = "#bada55"
		}
	}
	want := shapeByID(t, open, "region.a")
	oldOpacity := 0.
	var sawFade, sawClip, sawHidden bool
	for ms := 0; ms <= 1000; ms += 5 {
		forward := unfold.sample(&paint, ms, 1000)
		backward := fold.sample(closed, 1000-ms, 1000)
		parent := shapeByID(t, forward, "region")
		cells := gridEdgeShapeMap(forward.Shapes)
		reverse := gridEdgeShapeMap(backward.Shapes)
		a, b := cells["region.a"], cells["region.b"]
		if (a == nil) != (b == nil) || (a == nil) != (reverse["region.a"] == nil) {
			t.Fatalf("peers with the same bounds appeared separately at %d", ms)
		}
		if a == nil {
			if len(forward.Connections) != 0 || oldOpacity != 0 {
				t.Fatalf("detail vanished or left a dangling link at %d", ms)
			}
			sawHidden = true
			continue
		}
		if a.Fill != "#bada55" || a.Width != want.Width || a.Height != want.Height || a.Pos != want.Pos {
			t.Fatalf("paint or final detail slot changed at %d: %+v", ms, a)
		}
		if a.Opacity != b.Opacity || a.Opacity < oldOpacity || math.Abs(a.Opacity-reverse[a.ID].Opacity) > 1e-12 {
			t.Fatalf("discontinuous peer fade at %d", ms)
		}
		oldOpacity = a.Opacity
		sawFade = sawFade || (a.Opacity > 0 && a.Opacity < 1)
		if c := a.MotionClip; c != nil {
			sawClip = true
			if a.Pos.X < c.X || a.Pos.Y < c.Y || a.Pos.X+a.Width > c.X+c.Width || a.Pos.Y+a.Height > c.Y+c.Height {
				t.Fatalf("visible detail was sliced at %d", ms)
			}
			if c.Y+c.Height > parent.Pos.Y+parent.Height {
				t.Fatalf("clip extends past shell at %d", ms)
			}
		}
		if reverse[a.ID].Pos != a.Pos || !reflect.DeepEqual(reverse[a.ID].MotionClip, a.MotionClip) ||
			shapeByID(t, backward, "after").Pos != shapeByID(t, forward, "after").Pos {
			t.Fatalf("closing does not retrace reveal at %d", ms)
		}
		if len(forward.Connections) != 1 || !reflect.DeepEqual(forward.Connections[0].MotionClip, a.MotionClip) {
			t.Fatalf("internal connection escaped the viewport at %d", ms)
		}
		if err := d2target.ValidateRenderTarget(forward); err != nil {
			t.Fatal(err)
		}
	}
	if !sawHidden || !sawFade || !sawClip || oldOpacity != 1 {
		t.Fatal("expected hidden, fading and settled details")
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
