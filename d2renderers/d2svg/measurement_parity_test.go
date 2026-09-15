package d2svg_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

func measurementDiagram(kind string) *d2target.Diagram {
	d := validRenderTarget()
	d.Shapes[0].Language = "markdown"
	d.Shapes[0].Label = "**bold** and `code`\n\n- 日本語"
	d.Shapes[0].LabelWidth = 180
	d.Shapes[0].LabelHeight = 80
	d.Shapes[0].Width = 200
	d.Shapes[0].Height = 100
	shape := *d2target.BaseShape()
	shape.ID, shape.Type, shape.Label = "legend-shape", d2target.ShapeRectangle, "First\n日本語"
	shape.Fill, shape.Stroke, shape.FontFamily = "#ffffff", "#000000", "mono"
	blankShape := shape
	blankShape.ID, blankShape.Label = "blank-shape", ""
	connection := *d2target.BaseConnection()
	connection.ID, connection.Label, connection.Stroke = "legend-edge", "Second\nline", "#000000"
	connection.FontFamily = "default"
	blankConnection := connection
	blankConnection.ID, blankConnection.Label = "blank-edge", ""
	if kind != "markdown" {
		d.Legend = &d2target.Legend{Label: "Legend <&>"}
	}
	switch kind {
	case "shapes":
		d.Legend.Shapes = []d2target.Shape{blankShape, shape}
	case "connections":
		d.Legend.Connections = []d2target.Connection{connection, blankConnection}
	case "mixed", "tooltip":
		d.Legend.Shapes = []d2target.Shape{shape, blankShape}
		d.Legend.Connections = []d2target.Connection{blankConnection, connection}
	case "blank connections":
		d.Legend.Shapes = []d2target.Shape{shape}
		d.Legend.Connections = []d2target.Connection{blankConnection}
	case "all blank":
		d.Legend.Shapes = []d2target.Shape{blankShape}
		d.Legend.Connections = []d2target.Connection{blankConnection}
	}
	if kind == "tooltip" {
		family := d2fonts.HandDrawn
		d.FontFamily = &family
		d.Shapes[0].Tooltip = "**positioned** `tooltip`"
		d.Shapes[0].TooltipPosition = "top-left"
	}
	return d
}

// These digests capture complete SVG bytes from the pre-reuse renderer at
// d6a52f13359dc957e346c792507728c6fd3b0c76, including its distinct legend padding
// formulas, blank items, Unicode measurements, and positioned tooltip bounds.
func TestRenderMeasurementParity(t *testing.T) {
	for _, tc := range []struct{ name, svg, legend string }{
		{name: "markdown", svg: "984e36f5cc782def1d5cdb6e0a3d906ca22fdba3e407cd7b5b009323545c2396", legend: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{name: "shapes", svg: "24772abdb043c754a7ebb00bbb335b402e64b12411d3571f03ffb50fa71bde55", legend: "b522b0ff093e8335413c423df531bea16d7a96bec1c2a4fdc60a63b59822a3c0"},
		{name: "connections", svg: "8a80dece2aa0f9b31597157b31b3d4f07a31c608347986248ba5a5444000ec3e", legend: "3254b992b8bd20a4c4f1f0a0f2fdff44d5dc4b8e722a410678a0dc07597fa980"},
		{name: "mixed", svg: "90aa757421ba5f19c86e654e34a884550b43c5c1811bf82c1043c9295ce00c3b", legend: "bd9eb5a642f979b9c10037bae5016bf3aca0bcae49c923880a357c0d4d23bc3a"},
		{name: "blank connections", svg: "8fdab20844b7a7d25796c59e3c437fb962c77f8fc6d827f008e6d72756ed4976", legend: "d941acd11b5a0a74a562f9cfeb01c522d0b87284e25cd963baa1386d890067fb"},
		{name: "all blank", svg: "1446439ae6fdb38eefc71ea72e469b368c74e6a5db90c7201575d8d6223f150a", legend: "27df5a328d39685c637aabdd080e6b7d0b269398afc8106e15156b9724fa6a15"},
		{name: "tooltip", svg: "e1e7642736d84a2d1fae7a33253c2229287353b70e261bfb8156956adcb4f800", legend: "d6942a2bfd4304c6a1283e1b94f2fd4d514a973eb64dceea445fdba15f98434b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := measurementDiagram(tc.name)
			omitVersion := true
			out, err := d2svg.Render(d, &d2svg.RenderOpts{OmitVersion: &omitVersion})
			if err != nil {
				t.Fatal(err)
			}
			got := fmt.Sprintf("%x", sha256.Sum256(out))
			if got != tc.svg {
				t.Fatalf("SVG changed: got digest %s, want %s", got, tc.svg)
			}
			var legend bytes.Buffer
			if err := d2svg.RenderLegend(&legend, d, "legend-parity", nil); err != nil {
				t.Fatal(err)
			}
			got = fmt.Sprintf("%x", sha256.Sum256(legend.Bytes()))
			if got != tc.legend {
				t.Fatalf("legend changed: got digest %s, want %s", got, tc.legend)
			}
		})
	}
}
