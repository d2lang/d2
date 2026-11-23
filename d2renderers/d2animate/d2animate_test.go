package d2animate

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

func TestWrapDefaultsMissingPadding(t *testing.T) {
	diagram := d2target.NewDiagram()
	out, err := Wrap(diagram, [][]byte{[]byte(`<g></g>`)}, d2svg.RenderOpts{}, 1_000)
	if err != nil {
		t.Fatalf("Wrap() error = %v", err)
	}
	if !strings.Contains(string(out), `viewBox="0 0 200 200"`) {
		t.Fatalf("Wrap() did not use default padding: %q", out)
	}
}

func TestWrapRejectsMalformedTargetBeforeTraversal(t *testing.T) {
	diagram := d2target.NewDiagram()
	diagram.Layers = []*d2target.Diagram{nil}

	if _, err := Wrap(diagram, [][]byte{[]byte(`<g></g>`)}, d2svg.RenderOpts{}, 1_000); err == nil || !strings.Contains(err.Error(), "root.layers[0] is nil") {
		t.Fatalf("Wrap() error = %v, want malformed-target error", err)
	}
}

func TestTimelineSampleBoundaries(t *testing.T) {
	root := d2target.NewDiagram()
	step := d2target.NewDiagram()
	root.Steps = []*d2target.Diagram{step}
	step.TweenFrames = []*d2target.Diagram{d2target.NewDiagram(), d2target.NewDiagram(), d2target.NewDiagram()}
	step.TweenDurationMS = 101
	frames, err := Timeline(root, 400)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 5 {
		t.Fatalf("timeline length %d", len(frames))
	}
	wantMS := []int{400, 33, 34, 34, 400}
	for i, frame := range frames {
		if frame.DurationMS != wantMS[i] {
			t.Fatalf("frame %d duration %d", i, frame.DurationMS)
		}
		if i >= 1 && i <= 3 && frame.Diagram != step.TweenFrames[i-1] {
			t.Fatalf("frame %d is out of order", i)
		}
	}
	if frames[4].Diagram != step {
		t.Fatal("last frame should hold authored endpoint")
	}
}
