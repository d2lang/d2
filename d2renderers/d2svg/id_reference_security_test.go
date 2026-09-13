package d2svg_test

import (
	"encoding/xml"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

var localIRIRegexp = regexp.MustCompile(`url\(['"]?#([^)'"]+)['"]?\)`)

func TestRenderUsesUniqueResolvableIDsForUntrustedShapeIDs(t *testing.T) {
	t.Parallel()

	for _, shapeType := range []string{d2target.ShapeRectangle, d2target.ShapeHexagon} {
		shapeType := shapeType
		t.Run(shapeType, func(t *testing.T) {
			t.Parallel()
			shape := *d2target.BaseShape()
			shape.ID = `x),url(https://example.invalid/a)`
			shape.Type = shapeType
			shape.Pos = d2target.Point{X: 10, Y: 10}
			shape.Width = 160
			shape.Height = 100
			shape.ThreeDee = true
			shape.Fill = "#ffffff"
			shape.Stroke = "#000000"

			diagram := d2target.NewDiagram()
			diagram.Root.Fill = "#ffffff"
			diagram.Shapes = []d2target.Shape{shape}

			out, err := d2svg.Render(diagram, nil)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			assertUniqueResolvableLocalIRIs(t, out)
		})
	}
}

func assertUniqueResolvableLocalIRIs(t *testing.T, source []byte) {
	t.Helper()

	ids := make(map[string]int)
	var references []string
	decoder := xml.NewDecoder(strings.NewReader(string(source)))
	decoder.Strict = true
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("rendered SVG is not strict XML: %v\n%s", err, source)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attribute := range start.Attr {
			if attribute.Name.Local == "id" {
				ids[attribute.Value]++
			}
			for _, match := range localIRIRegexp.FindAllStringSubmatch(attribute.Value, -1) {
				references = append(references, match[1])
			}
			if (attribute.Name.Local == "href" || attribute.Name.Local == "xlink:href") && strings.HasPrefix(attribute.Value, "#") {
				references = append(references, strings.TrimPrefix(attribute.Value, "#"))
			}
			if strings.Contains(attribute.Value, "example.invalid") {
				t.Fatalf("untrusted shape ID escaped into an SVG attribute: %q", attribute.Value)
			}
		}
	}
	for id, count := range ids {
		if count != 1 {
			t.Fatalf("SVG id %q occurs %d times, want exactly once", id, count)
		}
	}
	if len(references) == 0 {
		t.Fatal("rendered SVG has no local IRI references")
	}
	for _, reference := range references {
		if ids[reference] != 1 {
			t.Fatalf("local IRI %q resolves to %d definitions, want exactly one", reference, ids[reference])
		}
	}
}
