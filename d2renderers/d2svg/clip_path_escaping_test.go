package d2svg_test

import (
	"encoding/xml"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

func TestRenderEscapesRoundedSpecialShapeClipPathIDs(t *testing.T) {
	t.Parallel()

	for _, shapeType := range []string{d2target.ShapeClass, d2target.ShapeSQLTable, d2target.ShapeImage} {
		shapeType := shapeType
		t.Run(shapeType, func(t *testing.T) {
			t.Parallel()

			shape := *d2target.BaseShape()
			shape.ID = `x)" onload="document.documentElement.dataset.pwned=1//`
			shape.Type = shapeType
			if shapeType == d2target.ShapeImage {
				shape.Icon = &url.URL{Path: "grid-cell.png"}
				shape.IconBorderRadius = 5
			}
			shape.Pos = d2target.Point{X: 10, Y: 10}
			shape.Width = 220
			shape.Height = 120
			shape.BorderRadius = 5
			shape.Fill = "#ffffff"
			shape.Stroke = "#000000"
			shape.Color = "#000000"
			shape.PrimaryAccentColor = "#000000"
			shape.SecondaryAccentColor = "#000000"
			shape.NeutralAccentColor = "#000000"

			diagram := d2target.NewDiagram()
			diagram.Root.Fill = "#ffffff"
			diagram.Shapes = []d2target.Shape{shape}

			out, err := d2svg.Render(diagram, nil)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			assertSafeClipPathReference(t, out)
			// Moving a rounded shape changes its clip geometry. Frames share
			// CSS under MasterID but must resolve their own clip paths.
			ids := make(map[string]bool)
			for _, x := range []int{10, 60} {
				diagram.Shapes[0].Pos.X = x
				frame, err := d2svg.Render(diagram, &d2svg.RenderOpts{MasterID: "shared"})
				if err != nil {
					t.Fatal(err)
				}
				id := assertSafeClipPathReference(t, frame)
				if ids[id] {
					t.Fatal("different clip geometry reused frame ID", id)
				}
				ids[id] = true
			}
		})
	}
}

func assertSafeClipPathReference(t *testing.T, source []byte) string {
	t.Helper()

	decoder := xml.NewDecoder(strings.NewReader(string(source)))
	decoder.Strict = true
	clipIDs := make(map[string]struct{})
	var references []string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Render() emitted invalid XML: %v\n%s", err, source)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if strings.EqualFold(start.Name.Local, "script") {
			t.Fatalf("Render() emitted an injected script element: %s", source)
		}
		for _, attr := range start.Attr {
			if strings.HasPrefix(strings.ToLower(attr.Name.Local), "on") {
				t.Fatalf("Render() emitted an event-handler attribute %q: %s", attr.Name.Local, source)
			}
			if start.Name.Local == "clipPath" && attr.Name.Local == "id" {
				clipIDs[attr.Value] = struct{}{}
			}
			if attr.Name.Local == "clip-path" {
				references = append(references, attr.Value)
			}
		}
	}
	if len(clipIDs) != 1 || len(references) == 0 {
		t.Fatalf("Render() clip IDs/references = %v/%v, want one definition and at least one reference", clipIDs, references)
	}
	for _, reference := range references {
		if !strings.HasPrefix(reference, "url(#") || !strings.HasSuffix(reference, ")") {
			t.Fatalf("Render() clip-path reference = %q", reference)
		}
		id := strings.TrimSuffix(strings.TrimPrefix(reference, "url(#"), ")")
		if _, ok := clipIDs[id]; !ok {
			t.Fatalf("Render() clip-path reference %q has no matching definition in %v", reference, clipIDs)
		}
	}
	return strings.TrimSuffix(strings.TrimPrefix(references[0], "url(#"), ")")
}
