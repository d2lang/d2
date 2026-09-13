package d2svg_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

func TestRenderRejectsDangerousLegendShapeLinksFromJSON(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		diagramJSON string
		object      string
	}{
		{
			name:        "shape",
			diagramJSON: `{"legend":{"shapes":[{"id":"unsafe-shape","type":"rectangle","label":"Shape","opacity":1,"strokeWidth":2,"fill":"B6","stroke":"B1","link":"javascript:alert(1)"}]}}`,
			object:      `legend shape "unsafe-shape"`,
		},
		{
			name:        "image shape",
			diagramJSON: `{"legend":{"shapes":[{"id":"unsafe-image","type":"image","label":"Image","opacity":1,"icon":{"Scheme":"https","Host":"example.com","Path":"/image.png"},"link":"data:text/html,<script>alert(1)</script>"}]}}`,
			object:      `legend shape "unsafe-image"`,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			diagram := d2target.NewDiagram()
			if err := json.Unmarshal([]byte(tc.diagramJSON), diagram); err != nil {
				t.Fatalf("invalid test diagram JSON: %v", err)
			}
			out, err := d2svg.Render(diagram, nil)
			if err == nil || !strings.Contains(err.Error(), tc.object+" uses an unsafe link URL scheme") {
				t.Fatalf("Render() output/error = %q/%v", out, err)
			}
			if out != nil {
				t.Fatalf("Render() returned output on error: %q", out)
			}
		})
	}
}
