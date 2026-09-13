package d2svg_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestRenderRejectsTargetColorAttributeInjection(t *testing.T) {
	t.Parallel()

	const payload = `red" onload="alert(1)`
	tests := []struct {
		name      string
		configure func(*d2target.Diagram)
	}{
		{name: "root-fill", configure: func(d *d2target.Diagram) { d.Root.Fill = payload }},
		{name: "root-stroke", configure: func(d *d2target.Diagram) { d.Root.Stroke = payload }},
		{name: "shape-fill", configure: func(d *d2target.Diagram) { d.Shapes[0].Fill = payload }},
		{name: "shape-stroke", configure: func(d *d2target.Diagram) { d.Shapes[0].Stroke = payload }},
		{name: "shape-color", configure: func(d *d2target.Diagram) { d.Shapes[0].Color = payload }},
		{name: "shape-label-fill", configure: func(d *d2target.Diagram) { d.Shapes[0].LabelFill = payload }},
		{
			name: "shape-primary-accent",
			configure: func(d *d2target.Diagram) {
				setClassShape(&d.Shapes[0])
				d.Shapes[0].PrimaryAccentColor = payload
			},
		},
		{
			name: "shape-secondary-accent",
			configure: func(d *d2target.Diagram) {
				setClassShape(&d.Shapes[0])
				d.Shapes[0].SecondaryAccentColor = payload
			},
		},
		{
			name: "shape-neutral-accent",
			configure: func(d *d2target.Diagram) {
				setSQLTableShape(&d.Shapes[0])
				d.Shapes[0].NeutralAccentColor = payload
			},
		},
		{name: "connection-stroke", configure: func(d *d2target.Diagram) { d.Connections[0].Stroke = payload }},
		{name: "connection-fill", configure: func(d *d2target.Diagram) { d.Connections[0].Fill = payload }},
		{name: "connection-color", configure: func(d *d2target.Diagram) { d.Connections[0].Color = payload }},
		{name: "connection-source-label-color", configure: func(d *d2target.Diagram) { d.Connections[0].SrcLabel.Color = payload }},
		{name: "connection-destination-label-color", configure: func(d *d2target.Diagram) { d.Connections[0].DstLabel.Color = payload }},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			diagram := colorAttributeDiagram()
			test.configure(diagram)

			// The public JavaScript renderer receives this target structure as JSON.
			encoded, err := json.Marshal(diagram)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			var decoded d2target.Diagram
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			out, err := d2svg.Render(&decoded, nil)
			if err == nil || !strings.Contains(err.Error(), "uses unsupported paint") {
				t.Fatalf("Render() output/error = %q/%v, want paint rejection", out, err)
			}
			if out != nil {
				t.Fatalf("Render() returned browser-consumable output for rejected paint: %q", out)
			}
		})
	}
}

func colorAttributeDiagram() *d2target.Diagram {
	diagram := d2target.NewDiagram()
	diagram.Root.Fill = "#ffffff"
	fontFamily, monoFontFamily := d2fonts.SourceSansPro, d2fonts.SourceCodePro
	diagram.FontFamily = &fontFamily
	diagram.MonoFontFamily = &monoFontFamily

	shape := *d2target.BaseShape()
	shape.ID = "shape"
	shape.Type = d2target.ShapeRectangle
	shape.Width = 100
	shape.Height = 66
	shape.Fill = "#ffffff"
	shape.Stroke = "#000000"
	shape.Color = "#000000"
	shape.Label = "shape"
	shape.FontSize = 16
	shape.LabelWidth = 40
	shape.LabelHeight = 20

	destination := *d2target.BaseShape()
	destination.ID = "destination"
	destination.Type = d2target.ShapeRectangle
	destination.Pos.X = 200
	destination.Width = 100
	destination.Height = 66
	destination.Fill = "#ffffff"
	destination.Stroke = "#000000"

	connection := *d2target.BaseConnection()
	connection.ID = "connection"
	connection.Src = shape.ID
	connection.Dst = destination.ID
	connection.Route = []*geo.Point{{X: 100, Y: 33}, {X: 200, Y: 33}}
	connection.Stroke = "#000000"
	connection.Color = "#000000"
	connection.Fill = "#ffffff"
	connection.Label = "connection"
	connection.FontSize = 16
	connection.LabelWidth = 80
	connection.LabelHeight = 20
	connection.LabelPosition = "INSIDE_MIDDLE_CENTER"
	connection.SrcLabel = &d2target.Text{Label: "source", Color: "#000000", LabelWidth: 48, LabelHeight: 20}
	connection.DstLabel = &d2target.Text{Label: "destination", Color: "#000000", LabelWidth: 80, LabelHeight: 20}

	diagram.Shapes = []d2target.Shape{shape, destination}
	diagram.Connections = []d2target.Connection{connection}
	return diagram
}

func setClassShape(shape *d2target.Shape) {
	shape.Type = d2target.ShapeClass
	shape.Height = 120
	shape.PrimaryAccentColor = "#000000"
	shape.SecondaryAccentColor = "#000000"
	shape.Fields = []d2target.ClassField{{Name: "field", Type: "string"}}
}

func setSQLTableShape(shape *d2target.Shape) {
	shape.Type = d2target.ShapeSQLTable
	shape.Height = 120
	shape.PrimaryAccentColor = "#000000"
	shape.SecondaryAccentColor = "#000000"
	shape.NeutralAccentColor = "#000000"
	shape.Columns = []d2target.SQLColumn{
		{
			Name:       d2target.Text{Label: "column", LabelWidth: 48},
			Type:       d2target.Text{Label: "string", LabelWidth: 40},
			Constraint: []string{"primary_key"},
		},
	}
}
