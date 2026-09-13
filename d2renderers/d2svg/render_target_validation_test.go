package d2svg_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func TestRenderRejectsMalformedJSONTargetsWithoutPanicking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*d2target.Diagram)
		wantError string
	}{
		{
			name: "unsupported source arrowhead",
			configure: func(diagram *d2target.Diagram) {
				diagram.Connections[0].SrcArrow = d2target.Arrowhead("attacker-controlled")
			},
			wantError: "unsupported arrowhead",
		},
		{
			name: "unsupported destination arrowhead",
			configure: func(diagram *d2target.Diagram) {
				diagram.Connections[0].DstArrow = d2target.Arrowhead("attacker-controlled")
			},
			wantError: "unsupported arrowhead",
		},
		{
			name: "missing arrowhead value",
			configure: func(diagram *d2target.Diagram) {
				diagram.Connections[0].DstArrow = ""
			},
			wantError: "unsupported arrowhead",
		},
		{
			name: "empty route",
			configure: func(diagram *d2target.Diagram) {
				diagram.Connections[0].Route = nil
			},
			wantError: "at least two points",
		},
		{
			name: "one-point route",
			configure: func(diagram *d2target.Diagram) {
				diagram.Connections[0].Route = diagram.Connections[0].Route[:1]
			},
			wantError: "at least two points",
		},
		{
			name: "nil route point",
			configure: func(diagram *d2target.Diagram) {
				diagram.Connections[0].Route[0] = nil
			},
			wantError: "route[0] is nil",
		},
		{
			name: "zero-length route segment",
			configure: func(diagram *d2target.Diagram) {
				point := *diagram.Connections[0].Route[0]
				diagram.Connections[0].Route[1] = &point
			},
			wantError: "finite, non-zero segment length",
		},
		{
			name: "non-normalizable route segment",
			configure: func(diagram *d2target.Diagram) {
				diagram.Connections[0].Route[1] = &geo.Point{X: math.SmallestNonzeroFloat64, Y: 30}
			},
			wantError: "finite, non-zero segment length",
		},
		{
			name: "out-of-bounds route coordinate",
			configure: func(diagram *d2target.Diagram) {
				diagram.Connections[0].Route[1].X = math.MaxFloat64
			},
			wantError: "must fit integer bounds arithmetic",
		},
		{
			name: "invalid cubic route length",
			configure: func(diagram *d2target.Diagram) {
				diagram.Connections[0].IsCurve = true
			},
			wantError: "1+3n points",
		},
		{
			name: "missing labeled-connection position",
			configure: func(diagram *d2target.Diagram) {
				diagram.Connections[0].Label = "label"
				diagram.Connections[0].LabelPosition = ""
			},
			wantError: "not a connection label position",
		},
		{
			name: "invalid labeled-connection position",
			configure: func(diagram *d2target.Diagram) {
				diagram.Connections[0].Label = "label"
				diagram.Connections[0].LabelPosition = "attacker-controlled"
			},
			wantError: "not a connection label position",
		},
		{
			name: "out-of-range label percentage",
			configure: func(diagram *d2target.Diagram) {
				diagram.Connections[0].LabelPercentage = math.MaxFloat64
			},
			wantError: "labelPercentage",
		},
		{
			name: "root dash geometry",
			configure: func(diagram *d2target.Diagram) {
				diagram.Root.StrokeWidth = 18
				diagram.Root.StrokeDash = 1
			},
			wantError: "finite non-negative dash lengths",
		},
		{
			name: "shape dash geometry",
			configure: func(diagram *d2target.Diagram) {
				diagram.Shapes[0].StrokeWidth = 18
				diagram.Shapes[0].StrokeDash = 1
			},
			wantError: "finite non-negative dash lengths",
		},
		{
			name: "connection dash geometry",
			configure: func(diagram *d2target.Diagram) {
				diagram.Connections[0].StrokeWidth = 18
				diagram.Connections[0].StrokeDash = 1
			},
			wantError: "finite non-negative dash lengths",
		},
		{
			name: "unregistered regular font family",
			configure: func(diagram *d2target.Diagram) {
				family := d2fonts.FontFamily("attacker-controlled")
				diagram.FontFamily = &family
			},
			wantError: "not a registered font family",
		},
		{
			name: "unregistered mono font family",
			configure: func(diagram *d2target.Diagram) {
				family := d2fonts.FontFamily("attacker-controlled")
				diagram.MonoFontFamily = &family
			},
			wantError: "not a registered font family",
		},
		{
			name: "image without icon",
			configure: func(diagram *d2target.Diagram) {
				diagram.Shapes[0].Type = d2target.ShapeImage
				diagram.Shapes[0].Icon = nil
			},
			wantError: "image is missing an icon",
		},
		{
			name: "nil layer",
			configure: func(diagram *d2target.Diagram) {
				diagram.Layers = []*d2target.Diagram{nil}
			},
			wantError: "root.layers[0] is nil",
		},
		{
			name: "nil scenario",
			configure: func(diagram *d2target.Diagram) {
				diagram.Scenarios = []*d2target.Diagram{nil}
			},
			wantError: "root.scenarios[0] is nil",
		},
		{
			name: "nil step",
			configure: func(diagram *d2target.Diagram) {
				diagram.Steps = []*d2target.Diagram{nil}
			},
			wantError: "root.steps[0] is nil",
		},
		{
			name: "unsupported legend arrowhead",
			configure: func(diagram *d2target.Diagram) {
				connection := *d2target.BaseConnection()
				connection.Label = "connection"
				connection.DstArrow = d2target.Arrowhead("attacker-controlled")
				diagram.Legend = &d2target.Legend{Connections: []d2target.Connection{connection}}
			},
			wantError: "unsupported arrowhead",
		},
		{
			name: "legend shape dash geometry",
			configure: func(diagram *d2target.Diagram) {
				shape := *d2target.BaseShape()
				shape.Label = "shape"
				shape.StrokeWidth = 18
				shape.StrokeDash = 1
				diagram.Legend = &d2target.Legend{Shapes: []d2target.Shape{shape}}
			},
			wantError: "finite non-negative dash lengths",
		},
		{
			name: "legend connection dash geometry",
			configure: func(diagram *d2target.Diagram) {
				connection := *d2target.BaseConnection()
				connection.Label = "connection"
				connection.StrokeWidth = 18
				connection.StrokeDash = 1
				diagram.Legend = &d2target.Legend{Connections: []d2target.Connection{connection}}
			},
			wantError: "finite non-negative dash lengths",
		},
		{
			name: "legend image without icon",
			configure: func(diagram *d2target.Diagram) {
				shape := *d2target.BaseShape()
				shape.Type = d2target.ShapeImage
				shape.Label = "image"
				diagram.Legend = &d2target.Legend{Shapes: []d2target.Shape{shape}}
			},
			wantError: "image is missing an icon",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			diagram := validRenderTarget()
			test.configure(diagram)
			diagram = roundTripRenderTarget(t, diagram)

			out, err := renderWithoutPanic(t, diagram)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Render() output/error = %q/%v, want error containing %q", out, err, test.wantError)
			}
			if out != nil {
				t.Fatalf("Render() returned output on error: %q", out)
			}
		})
	}
}

func TestRenderRejectsNonFiniteDirectTargetWithoutPanicking(t *testing.T) {
	t.Parallel()

	diagram := validRenderTarget()
	diagram.Connections[0].Route[0].X = math.Inf(1)
	out, err := renderWithoutPanic(t, diagram)
	if err == nil || !strings.Contains(err.Error(), "finite coordinates") {
		t.Fatalf("Render() output/error = %q/%v, want finite-coordinate error", out, err)
	}
}

func TestRenderDefaultsMissingOrEmptyJSONFontFamilies(t *testing.T) {
	t.Parallel()

	empty := d2fonts.FontFamily("")
	for _, test := range []struct {
		name        string
		regularFont *d2fonts.FontFamily
		monoFont    *d2fonts.FontFamily
	}{
		{name: "missing"},
		{name: "empty", regularFont: &empty, monoFont: &empty},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			diagram := validRenderTarget()
			diagram.FontFamily = test.regularFont
			diagram.MonoFontFamily = test.monoFont
			diagram.Shapes[0].FontFamily = "default"
			diagram.Shapes[0].Bold = false
			monoShape := diagram.Shapes[0]
			monoShape.ID = "mono"
			monoShape.Pos.Y = 80
			monoShape.FontFamily = "mono"
			diagram.Shapes = append(diagram.Shapes, monoShape)
			diagram = roundTripRenderTarget(t, diagram)

			out, err := renderWithoutPanic(t, diagram)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if !strings.Contains(string(out), "font-regular") || !strings.Contains(string(out), "font-mono") {
				t.Fatalf("Render() did not embed both default font families")
			}
		})
	}
}

func TestRenderAcceptsValidJSONTarget(t *testing.T) {
	t.Parallel()

	diagram := roundTripRenderTarget(t, validRenderTarget())
	out, err := renderWithoutPanic(t, diagram)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(string(out), `<svg`) {
		t.Fatalf("Render() output is not SVG: %q", out)
	}
}

func TestRenderAcceptsLegendConnectionWithSynthesizedRoute(t *testing.T) {
	diagram := validRenderTarget()
	connection := *d2target.BaseConnection()
	connection.Label = "connection"
	diagram.Legend = &d2target.Legend{Connections: []d2target.Connection{connection}}
	diagram = roundTripRenderTarget(t, diagram)

	if _, err := renderWithoutPanic(t, diagram); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
}

func TestRenderRejectsCyclicBoardBeforeHashing(t *testing.T) {
	diagram := validRenderTarget()
	diagram.Layers = []*d2target.Diagram{diagram}

	if _, err := renderWithoutPanic(t, diagram); err == nil || !strings.Contains(err.Error(), "reuses board") {
		t.Fatalf("Render() error = %v, want reused-board error", err)
	}
}

func TestRenderMultiboardRetainsNestedLinkValidation(t *testing.T) {
	root := d2target.NewDiagram()
	child := validRenderTarget()
	child.Shapes[0].Link = "javascript:alert(1)"
	root.Layers = []*d2target.Diagram{child}

	if _, err := d2svg.RenderMultiboard(root, nil); err == nil || !strings.Contains(err.Error(), "unsafe link URL scheme") {
		t.Fatalf("RenderMultiboard() error = %v, want unsafe-link error", err)
	}
}

func TestRenderAcceptsRegisteredCustomFontFamily(t *testing.T) {
	family, err := d2fonts.AddFontFamily("render-validation-custom", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("AddFontFamily() error = %v", err)
	}
	diagram := validRenderTarget()
	diagram.FontFamily = family
	diagram = roundTripRenderTarget(t, diagram)

	if _, err := renderWithoutPanic(t, diagram); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
}

func validRenderTarget() *d2target.Diagram {
	fontFamily := d2fonts.SourceSansPro
	monoFontFamily := d2fonts.SourceCodePro
	diagram := d2target.NewDiagram()
	diagram.FontFamily = &fontFamily
	diagram.MonoFontFamily = &monoFontFamily
	diagram.Root.Stroke = "transparent"

	targetShape := *d2target.BaseShape()
	targetShape.ID = "shape"
	targetShape.Type = d2target.ShapeRectangle
	targetShape.Pos = d2target.Point{X: 0, Y: 0}
	targetShape.Width = 100
	targetShape.Height = 60
	targetShape.Fill = "#ffffff"
	targetShape.Stroke = "#000000"
	targetShape.Label = "shape"
	targetShape.LabelPosition = label.InsideMiddleCenter.String()
	targetShape.FontSize = d2fonts.FONT_SIZE_M
	targetShape.LabelWidth = 40
	targetShape.LabelHeight = 20
	diagram.Shapes = []d2target.Shape{targetShape}

	connection := *d2target.BaseConnection()
	connection.ID = "connection"
	connection.Src = targetShape.ID
	connection.Dst = targetShape.ID
	connection.Stroke = "#000000"
	connection.Route = []*geo.Point{{X: 0, Y: 30}, {X: 100, Y: 30}}
	diagram.Connections = []d2target.Connection{connection}

	return diagram
}

func roundTripRenderTarget(t *testing.T, diagram *d2target.Diagram) *d2target.Diagram {
	t.Helper()
	encoded, err := json.Marshal(diagram)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var decoded d2target.Diagram
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return &decoded
}

func renderWithoutPanic(t *testing.T, diagram *d2target.Diagram) (out []byte, err error) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("Render() panicked: %v", recovered)
		}
	}()
	return d2svg.Render(diagram, nil)
}
