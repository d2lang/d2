package d2talalayout_test

import (
	"fmt"
	"testing"

	"github.com/d2lang/d2/lib/label"
)

func TestCompoundUsesAvailableSpaceOnRequestedAxis(t *testing.T) {
	for _, tc := range []struct {
		axis                     string
		requested, width, height int
	}{
		{"width", 880, 880, 180},
		{"height", 640, 200, 640},
	} {
		t.Run(tc.axis, func(t *testing.T) {
			source := fmt.Sprintf("Regions: { %s: %d\n", tc.axis, tc.requested)
			for _, id := range []string{"West", "East", "EMEA", "APAC", "LATAM", "Oceania"} {
				source += id + ": {width: 80; height: 60}\n"
			}
			d := compileRoutingDiagram(t, t.Context(), source+"}", 1, nil)
			if len(d.Shapes) != 7 {
				t.Fatalf("expected a container and six tiles, got %d shapes", len(d.Shapes))
			}
			parent := d.Shapes[0]
			if parent.Width != tc.width || parent.Height != tc.height {
				t.Errorf("container = %d × %d, want %d × %d", parent.Width, parent.Height, tc.width, tc.height)
			}
			first := d.Shapes[1].Pos
			for _, s := range d.Shapes[2:] {
				if tc.axis == "width" && s.Pos.Y != first.Y || tc.axis == "height" && s.Pos.X != first.X {
					t.Errorf("%s wrapped to a second line at %v", s.ID, s.Pos)
				}
			}
		})
	}
}

func TestCompoundFitsTwoRequestedDimensions(t *testing.T) {
	for _, dimensions := range []struct{ width, height int }{{500, 260}, {300, 420}} {
		source := fmt.Sprintf("Modules: { width: %d; height: %d\n", dimensions.width, dimensions.height)
		for i := range 8 {
			source += fmt.Sprintf("  m%d: {width: 80; height: 60}\n", i)
		}
		d := compileRoutingDiagram(t, t.Context(), source+"}", 1, nil)
		if got := d.Shapes[0]; got.Width != dimensions.width || got.Height != dimensions.height {
			t.Errorf("requested %d × %d; packed %d × %d", dimensions.width, dimensions.height, got.Width, got.Height)
		}
	}
}

func TestCompoundFitsFixedExteriorCaptionOnce(t *testing.T) {
	const source = `Data sync: {
  worker: Background worker for the EU reconciliation queue {
    width: 80
    height: 60
    label.near: outside-left-center
  }
}`
	d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	frame, child := d.Shapes[0], d.Shapes[1]
	if clearance := frame.Pos.X + frame.Width - child.Pos.X - child.Width; clearance != 60 {
		t.Errorf("right padding is %d; want 60", clearance)
	}
	if clearance := child.Pos.X - label.PADDING - child.LabelWidth - frame.Pos.X; clearance < 60 {
		t.Errorf("fixed caption has only %d px of left clearance", clearance)
	}
}

func TestCompoundKeepsAuthoredCaptionClearOfSibling(t *testing.T) {
	const source = `Data sync: {
  worker: Background worker for the EU reconciliation queue {
    width: 80
    height: 60
    label.near: outside-top-right
  }
  p1: Shard 1 {width: 90; height: 60}
}`
	d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	worker, shard := d.Shapes[1], d.Shapes[2]
	if worker.LabelPosition != label.OutsideTopRight.String() {
		t.Fatalf("worker caption moved to %s", worker.LabelPosition)
	}
	left := worker.Pos.X + worker.Width - worker.LabelWidth - label.PADDING
	top := worker.Pos.Y - worker.LabelHeight - label.PADDING
	if left < shard.Pos.X+shard.Width && left+worker.LabelWidth > shard.Pos.X &&
		top < shard.Pos.Y+shard.Height && top+worker.LabelHeight > shard.Pos.Y {
		t.Errorf("worker caption at (%d, %d) overlaps Shard 1 at %v", left, top, shard.Pos)
	}
}
