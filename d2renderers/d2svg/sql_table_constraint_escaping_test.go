package d2svg_test

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

func TestRenderEscapesSQLTableConstraints(t *testing.T) {
	t.Parallel()

	const constraint = `</text><script onload="document.documentElement.dataset.pwned='yes'">void 0</script><text>& preserved`
	for _, sketch := range []bool{false, true} {
		sketch := sketch
		name := "normal"
		if sketch {
			name = "sketch"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			diagram := sqlConstraintEscapingDiagram(constraint)
			out, err := d2svg.Render(diagram, &d2svg.RenderOpts{Sketch: &sketch})
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			assertSafeSQLConstraint(t, out, constraint)
		})
	}
}

func sqlConstraintEscapingDiagram(constraint string) *d2target.Diagram {
	diagram := d2target.NewDiagram()
	fontFamily, monoFontFamily := d2fonts.SourceSansPro, d2fonts.SourceCodePro
	diagram.FontFamily = &fontFamily
	diagram.MonoFontFamily = &monoFontFamily

	shape := *d2target.BaseShape()
	shape.ID = "table"
	shape.Type = d2target.ShapeSQLTable
	shape.Width = 420
	shape.Height = 120
	shape.Fill = "#ffffff"
	shape.Stroke = "#000000"
	shape.Color = "#000000"
	shape.FontSize = 16
	shape.PrimaryAccentColor = "#000000"
	shape.SecondaryAccentColor = "#000000"
	shape.NeutralAccentColor = "#000000"
	shape.Columns = []d2target.SQLColumn{
		{
			Name:       d2target.Text{Label: "column", LabelWidth: 48},
			Type:       d2target.Text{Label: "string", LabelWidth: 40},
			Constraint: []string{constraint},
		},
	}
	diagram.Shapes = []d2target.Shape{shape}
	return diagram
}

func assertSafeSQLConstraint(t *testing.T, source []byte, constraint string) {
	t.Helper()

	decoder := xml.NewDecoder(bytes.NewReader(source))
	decoder.Strict = true
	var decodedText strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Render() emitted invalid XML: %v\n%s", err, source)
		}
		switch token := token.(type) {
		case xml.StartElement:
			if strings.EqualFold(token.Name.Local, "script") {
				t.Fatalf("Render() emitted an injected script element: %s", source)
			}
			for _, attr := range token.Attr {
				if strings.HasPrefix(strings.ToLower(attr.Name.Local), "on") {
					t.Fatalf("Render() emitted an event-handler attribute %q: %s", attr.Name.Local, source)
				}
			}
		case xml.CharData:
			decodedText.Write(token)
		}
	}

	if !strings.Contains(decodedText.String(), constraint) {
		t.Fatalf("Render() did not preserve constraint %q as text after XML decoding: %s", constraint, source)
	}
	if !bytes.Contains(source, []byte(`&lt;/text&gt;&lt;script`)) {
		t.Fatalf("Render() did not XML-escape the constraint markup: %s", source)
	}
}
