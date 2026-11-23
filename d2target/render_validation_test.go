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

func TestValidateRenderTargetTweenFrames(t *testing.T) {
	invalid := []*Diagram{
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

func TestValidateSequenceMetadata(t *testing.T) {
	for name, mutate := range map[string]func(*Diagram){
		"missing connection":    func(d *Diagram) { d.SequenceTimelines[0].Messages[0].ConnectionID = "absent" },
		"missing span":          func(d *Diagram) { d.SequenceTimelines[0].Spans[0].ShapeID = "absent" },
		"shared connection":     func(d *Diagram) { d.SequenceTimelines = append(d.SequenceTimelines, d.SequenceTimelines[0]) },
		"invalid message index": func(d *Diagram) { d.SequenceTimelines[0].Spans[0].End.Message = 1 },
		"arrival before send":   func(d *Diagram) { d.SequenceTimelines[0].Spans[0].Start.Arrival = true },
		"curved message":        func(d *Diagram) { d.Connections[0].IsCurve = true },
	} {
		t.Run(name, func(t *testing.T) {
			d := &Diagram{
				Shapes: []Shape{{ID: "activation"}}, Connections: []Connection{{ID: "message"}},
				SequenceTimelines: []SequenceTimeline{{
					Messages: []SequenceMessage{{ConnectionID: "message"}},
					Spans:    []SequenceSpan{{ShapeID: "activation"}},
				}},
			}
			if err := validateSequenceTimelines(d, "root"); err != nil {
				t.Fatal(err)
			}
			mutate(d)
			if err := validateSequenceTimelines(d, "root"); err == nil {
				t.Fatal("accepted invalid sequence metadata")
			}
		})
	}
}
