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
