package d2svg_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2latex"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/lib/label"
)

func TestRenderRejectsExcessiveLatexGroupNesting(t *testing.T) {
	formula := strings.Repeat("{", d2latex.MaxGroupNestingDepth+1) + "x" + strings.Repeat("}", d2latex.MaxGroupNestingDepth+1)
	for _, target := range []string{"shape", "connection"} {
		t.Run(target, func(t *testing.T) {
			diagram := validRenderTarget()
			if target == "shape" {
				diagram.Shapes[0].Label = formula
				diagram.Shapes[0].Language = "latex"
			} else {
				diagram.Connections[0].Label = formula
				diagram.Connections[0].Language = "latex"
				diagram.Connections[0].LabelPosition = label.InsideMiddleCenter.String()
				diagram.Connections[0].LabelPercentage = 0.5
			}

			_, err := d2svg.Render(diagram, nil)
			want := "latex group nesting depth 129 exceeds limit 128"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Render() error = %v, want %q", err, want)
			}
		})
	}
}
