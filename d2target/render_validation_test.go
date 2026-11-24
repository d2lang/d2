package d2target

import (
	"math"
	"strings"
	"testing"
)

func TestAnimatedStrokeBounds(t *testing.T) {
	d := NewDiagram()
	s := BaseShape()
	s.ID, s.Pos, s.Width, s.Height = "box", Point{10, 20}, 30, 40
	s.StrokeWidth, s.StrokeWidthOffset = 2, .25
	d.Shapes = []Shape{*s}
	if err := ValidateRenderTarget(d); err != nil {
		t.Fatal(err)
	}
	if tl, br := d.BoundingBox(); tl != (Point{8, 18}) || br != (Point{42, 62}) {
		t.Fatalf("bounds of 2.25 px stroke = %v ... %v", tl, br)
	}
	for _, tc := range []struct {
		width  int
		offset float64
	}{{0, -.25}, {2, math.NaN()}, {2, math.Inf(1)}, {2, 1}, {math.MaxInt, .25}} {
		d.Shapes[0].StrokeWidth, d.Shapes[0].StrokeWidthOffset = tc.width, tc.offset
		if err := ValidateRenderTarget(d); err == nil || !strings.Contains(err.Error(), "strokeWidthOffset") {
			t.Errorf("width %d with offset %g: got %v", tc.width, tc.offset, err)
		}
	}
}

func TestValidateRenderTargetBoundsBoardTree(t *testing.T) {
	t.Parallel()

	t.Run("maximum depth", func(t *testing.T) {
		root := NewDiagram()
		cursor := root
		for range maxRenderTargetBoardDepth {
			child := NewDiagram()
			cursor.Layers = []*Diagram{child}
			cursor = child
		}
		if err := ValidateRenderTarget(root); err != nil {
			t.Fatalf("ValidateRenderTarget() at maximum depth: %v", err)
		}
		if _, err := root.HashID(nil); err != nil {
			t.Fatalf("HashID() at maximum validated depth: %v", err)
		}

		cursor.Layers = []*Diagram{NewDiagram()}
		err := ValidateRenderTarget(root)
		if err == nil || !strings.Contains(err.Error(), "exceeds depth") {
			t.Fatalf("ValidateRenderTarget() error = %v, want depth error", err)
		}
	})

	t.Run("maximum board count", func(t *testing.T) {
		root := NewDiagram()
		root.Layers = make([]*Diagram, MaxRenderTargetBoards-1)
		for i := range root.Layers {
			root.Layers[i] = NewDiagram()
		}
		if err := ValidateRenderTarget(root); err != nil {
			t.Fatalf("ValidateRenderTarget() at maximum board count: %v", err)
		}
		if _, err := root.HashID(nil); err != nil {
			t.Fatalf("HashID() at maximum validated board count: %v", err)
		}

		root.Layers = append(root.Layers, NewDiagram())
		err := ValidateRenderTarget(root)
		if err == nil || !strings.Contains(err.Error(), "total boards") {
			t.Fatalf("ValidateRenderTarget() error = %v, want board-count error", err)
		}
	})
}

func TestValidateRenderTargetRejectsReusedBoard(t *testing.T) {
	t.Parallel()

	root := NewDiagram()
	root.Layers = []*Diagram{root}
	err := ValidateRenderTarget(root)
	if err == nil || !strings.Contains(err.Error(), "reuses board") {
		t.Fatalf("ValidateRenderTarget() error = %v, want reused-board error", err)
	}
}

func TestValidateRenderTargetTweenFrames(t *testing.T) {
	invalid := []*Diagram{
		{TweenDurationMS: 20, TweenFrames: []*Diagram{{}}, TweenFrameEndsMS: []int{0}},
		{TweenDurationMS: 20, TweenFrames: []*Diagram{{}, {}}, TweenFrameEndsMS: []int{0}},
		{TweenDurationMS: 20, TweenFrames: []*Diagram{{}, {}}, TweenFrameEndsMS: []int{3, 3}},
		{TweenDurationMS: 20, TweenFrames: []*Diagram{{}, {}}, TweenFrameEndsMS: []int{2, 21}},
		{TweenDurationMS: 20},
		{TweenDurationMS: -1},
		{TweenDurationMS: 600001, TweenFrames: []*Diagram{{}}},
		{TweenDurationMS: 1, TweenFrames: []*Diagram{{}, {}}},
		{TweenDurationMS: 1, TweenFrames: []*Diagram{nil}},
		{TweenDurationMS: 1, TweenFrames: []*Diagram{{Layers: []*Diagram{{}}}}},
		{TweenDurationMS: 1, TweenFrames: []*Diagram{{Shapes: []Shape{{Width: -1}}}}},
	}
	for i, diagram := range invalid {
		if err := ValidateRenderTarget(diagram); err == nil {
			t.Errorf("accepted invalid tween target %d", i)
		}
	}
}
