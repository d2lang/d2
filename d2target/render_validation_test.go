package d2target

import (
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
		root.Layers = make([]*Diagram, maxRenderTargetBoardCount-1)
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
