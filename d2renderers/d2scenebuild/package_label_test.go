package d2scenebuild

import (
	"testing"

	"github.com/d2lang/d2/d2target"
)

func TestShapeLabelPlacementPackageTopTab(t *testing.T) {
	t.Parallel()

	s := d2target.Shape{
		ID:            "pkg",
		Type:          d2target.ShapePackage,
		Pos:           d2target.NewPoint(10, 20),
		Width:         200,
		Height:        200,
		Label:         "pkg",
		LabelWidth:    40,
		LabelHeight:   20,
		LabelPosition: "INSIDE_TOP_LEFT",
	}

	_, tl := shapeLabelPlacement(s)
	if tl.X != 15 || tl.Y != 25 {
		t.Fatalf("shapeLabelPlacement(top-left) = (%v, %v), want (15, 25)", tl.X, tl.Y)
	}

	s.LabelPosition = "INSIDE_TOP_CENTER"
	_, tl = shapeLabelPlacement(s)
	if tl.X != 40 || tl.Y != 25 {
		t.Fatalf("shapeLabelPlacement(top-center) = (%v, %v), want (40, 25)", tl.X, tl.Y)
	}

	s.LabelWidth = 95
	s.LabelPosition = "INSIDE_TOP_LEFT"
	_, tl = shapeLabelPlacement(s)
	if tl.X != 15 || tl.Y != 65 {
		t.Fatalf("shapeLabelPlacement(overflow) = (%v, %v), want (15, 65)", tl.X, tl.Y)
	}
}
