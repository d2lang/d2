package d2target

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

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

func TestValidateRenderInk(t *testing.T) {
	progress := 0.5
	shape := *BaseShape()
	shape.SketchFillTransition = &SketchFillTransition{From: "N2", To: "transparent", Progress: progress}
	shape.UnderlineProgress = &progress
	shape.MarkdownStrike = &MarkdownStrikeTransition{From: "previous", Progress: progress}
	target := &Diagram{Shapes: []Shape{shape}}
	raw, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip Diagram
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRenderTarget(&roundTrip); err != nil {
		t.Fatal(err)
	}
	restored := roundTrip.Shapes[0]
	if restored.UnderlineProgress == nil || *restored.UnderlineProgress != progress || restored.MarkdownStrike == nil || *restored.MarkdownStrike != *shape.MarkdownStrike || restored.SketchFillTransition == nil || *restored.SketchFillTransition != *shape.SketchFillTransition {
		t.Fatal("lost ink in render target JSON")
	}
	for _, edit := range []func(*Shape){
		func(s *Shape) { s.SketchFillTransition.Progress = math.NaN() },
		func(s *Shape) { s.SketchFillTransition.To = "bogus" },
		func(s *Shape) { *s.UnderlineProgress = math.Inf(-1) },
		func(s *Shape) { s.MarkdownStrike.Progress = 1.01 },
	} {
		if err := json.Unmarshal(raw, &roundTrip); err != nil {
			t.Fatal(err)
		}
		edit(&roundTrip.Shapes[0])
		if err := ValidateRenderTarget(&roundTrip); err == nil {
			t.Error("invalid ink sample accepted")
		}
	}
}
