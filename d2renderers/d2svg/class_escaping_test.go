package d2svg_test

import (
	"encoding/base64"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestRenderEscapesShapeAndConnectionClasses(t *testing.T) {
	t.Parallel()

	classes := []string{
		"ordinary-class",
		`double"quote`,
		`single'quote`,
		`amp&ersand`,
		`less<than`,
		`evil"><script onload="alert(1)">alert(1)</script></g><g class="`,
	}

	for _, tc := range []struct {
		name      string
		targetID  string
		configure func(*d2target.Diagram)
	}{
		{
			name:     "shape",
			targetID: "shape",
			configure: func(diagram *d2target.Diagram) {
				diagram.Shapes[0].Classes = classes
			},
		},
		{
			name:     "connection",
			targetID: "connection",
			configure: func(diagram *d2target.Diagram) {
				connection := *d2target.BaseConnection()
				connection.ID = "connection"
				connection.Classes = classes
				connection.Src = "shape"
				connection.Dst = "destination"
				connection.Route = []*geo.Point{{X: 40, Y: 20}, {X: 100, Y: 20}}
				connection.Stroke = "#000000"
				diagram.Connections = []d2target.Connection{connection}
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			diagram := classEscapingDiagram()
			tc.configure(diagram)

			out, err := d2svg.Render(diagram, nil)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}

			wantClass := base64.URLEncoding.EncodeToString([]byte(tc.targetID)) + " " + strings.Join(classes, " ")
			assertSafeClassAttribute(t, out, wantClass)
		})
	}
}

func classEscapingDiagram() *d2target.Diagram {
	diagram := d2target.NewDiagram()
	diagram.Root.Fill = "#ffffff"
	diagram.Shapes = []d2target.Shape{
		{
			ID: "shape", Type: d2target.ShapeRectangle,
			Pos: d2target.Point{}, Width: 40, Height: 40,
			Fill: "#ffffff", Stroke: "#000000", StrokeWidth: 2, Opacity: 1,
		},
		{
			ID: "destination", Type: d2target.ShapeRectangle,
			Pos: d2target.Point{X: 100}, Width: 40, Height: 40,
			Fill: "#ffffff", Stroke: "#000000", StrokeWidth: 2, Opacity: 1,
		},
	}
	return diagram
}

func assertSafeClassAttribute(t *testing.T, source []byte, wantClass string) {
	t.Helper()

	decoder := xml.NewDecoder(strings.NewReader(string(source)))
	decoder.Strict = true
	found := false
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
			if attr.Name.Local == "class" && attr.Value == wantClass {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("Render() did not preserve class value %q after XML decoding: %s", wantClass, source)
	}
}
