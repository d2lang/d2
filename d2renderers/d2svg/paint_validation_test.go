package d2svg_test

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/util-go/go2"
)

const externalPaintServer = `url(http://127.0.0.1:65535/paint.svg#paint)`

func TestRenderRejectsExternalPaintServersFromRawJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		wantField string
		configure func(*d2target.Diagram, *d2svg.RenderOpts)
	}{
		{name: "root fill", wantField: "root fill", configure: func(d *d2target.Diagram, _ *d2svg.RenderOpts) { d.Root.Fill = externalPaintServer }},
		{name: "root stroke", wantField: "root stroke", configure: func(d *d2target.Diagram, _ *d2svg.RenderOpts) { d.Root.Stroke = externalPaintServer }},
		{name: "shape fill", wantField: `shape "source" fill`, configure: func(d *d2target.Diagram, _ *d2svg.RenderOpts) { d.Shapes[0].Fill = externalPaintServer }},
		{name: "shape stroke", wantField: `shape "source" stroke`, configure: func(d *d2target.Diagram, _ *d2svg.RenderOpts) { d.Shapes[0].Stroke = externalPaintServer }},
		{name: "shape font color", wantField: `shape "source" font color`, configure: func(d *d2target.Diagram, _ *d2svg.RenderOpts) { d.Shapes[0].Color = externalPaintServer }},
		{name: "shape label fill", wantField: `shape "source" label fill`, configure: func(d *d2target.Diagram, _ *d2svg.RenderOpts) { d.Shapes[0].LabelFill = externalPaintServer }},
		{name: "shape primary accent", wantField: `shape "source" primary accent color`, configure: func(d *d2target.Diagram, _ *d2svg.RenderOpts) {
			d.Shapes[0].Type = d2target.ShapeClass
			d.Shapes[0].PrimaryAccentColor = externalPaintServer
		}},
		{name: "shape secondary accent", wantField: `shape "source" secondary accent color`, configure: func(d *d2target.Diagram, _ *d2svg.RenderOpts) {
			d.Shapes[0].Type = d2target.ShapeClass
			d.Shapes[0].SecondaryAccentColor = externalPaintServer
		}},
		{name: "shape neutral accent", wantField: `shape "source" neutral accent color`, configure: func(d *d2target.Diagram, _ *d2svg.RenderOpts) {
			d.Shapes[0].Type = d2target.ShapeSQLTable
			d.Shapes[0].NeutralAccentColor = externalPaintServer
		}},
		{name: "connection stroke", wantField: `connection "edge" stroke`, configure: func(d *d2target.Diagram, _ *d2svg.RenderOpts) { d.Connections[0].Stroke = externalPaintServer }},
		{name: "connection fill", wantField: `connection "edge" fill`, configure: func(d *d2target.Diagram, _ *d2svg.RenderOpts) { d.Connections[0].Fill = externalPaintServer }},
		{name: "connection font color", wantField: `connection "edge" font color`, configure: func(d *d2target.Diagram, _ *d2svg.RenderOpts) { d.Connections[0].Color = externalPaintServer }},
		{name: "source arrowhead label", wantField: `connection "edge" source label color`, configure: func(d *d2target.Diagram, _ *d2svg.RenderOpts) {
			d.Connections[0].SrcLabel = &d2target.Text{Label: "source", Color: externalPaintServer}
		}},
		{name: "destination arrowhead label", wantField: `connection "edge" destination label color`, configure: func(d *d2target.Diagram, _ *d2svg.RenderOpts) {
			d.Connections[0].DstLabel = &d2target.Text{Label: "destination", Color: externalPaintServer}
		}},
		{name: "legend shape", wantField: `legend shape "legend-shape" fill`, configure: func(d *d2target.Diagram, _ *d2svg.RenderOpts) {
			d.Legend = &d2target.Legend{Shapes: []d2target.Shape{{ID: "legend-shape", Fill: externalPaintServer}}}
		}},
		{name: "legend connection stroke", wantField: `legend connection "legend-edge" stroke`, configure: func(d *d2target.Diagram, _ *d2svg.RenderOpts) {
			connection := *d2target.BaseConnection()
			connection.ID = "legend-edge"
			connection.Stroke = externalPaintServer
			d.Legend = &d2target.Legend{Connections: []d2target.Connection{connection}}
		}},
		{name: "legend connection fill", wantField: `legend connection "legend-edge" fill`, configure: func(d *d2target.Diagram, _ *d2svg.RenderOpts) {
			connection := *d2target.BaseConnection()
			connection.ID = "legend-edge"
			connection.Fill = externalPaintServer
			d.Legend = &d2target.Legend{Connections: []d2target.Connection{connection}}
		}},
		{name: "light theme override", wantField: "theme override N1", configure: func(_ *d2target.Diagram, opts *d2svg.RenderOpts) {
			opts.ThemeOverrides = &d2target.ThemeOverrides{N1: go2.Pointer(externalPaintServer)}
		}},
		{name: "dark theme override", wantField: "dark theme override AB5", configure: func(_ *d2target.Diagram, opts *d2svg.RenderOpts) {
			opts.DarkThemeOverrides = &d2target.ThemeOverrides{AB5: go2.Pointer(externalPaintServer)}
		}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			diagram := paintValidationDiagram()
			opts := &d2svg.RenderOpts{}
			tc.configure(diagram, opts)
			diagram = roundTripTargetJSON(t, diagram)

			out, err := d2svg.Render(diagram, opts)
			if err == nil || !strings.Contains(err.Error(), tc.wantField+" uses unsupported") {
				t.Fatalf("Render() output/error = %q/%v, want rejection for %s", out, err, tc.wantField)
			}
			if out != nil {
				t.Fatalf("Render() returned browser-consumable output containing an external paint server: %q", out)
			}
		})
	}
}

func TestRenderMultiboardRejectsExternalPaintServerInNestedBoard(t *testing.T) {
	t.Parallel()

	root := d2target.NewDiagram()
	child := paintValidationDiagram()
	child.Shapes[0].Fill = externalPaintServer
	root.Layers = []*d2target.Diagram{child}

	out, err := d2svg.RenderMultiboard(root, nil)
	if err == nil || !strings.Contains(err.Error(), `shape "source" fill uses unsupported`) {
		t.Fatalf("RenderMultiboard() output/error = %q/%v, want nested paint rejection", out, err)
	}
}

func TestRenderRejectsFillPatternAttributeInjectionFromRawJSON(t *testing.T) {
	t.Parallel()

	const injection = `dots" onload="fetch('http://127.0.0.1:65535/event')`
	for _, tc := range []struct {
		name      string
		wantField string
		configure func(*d2target.Diagram)
	}{
		{name: "root", wantField: "root fill pattern", configure: func(d *d2target.Diagram) { d.Root.FillPattern = injection }},
		{name: "shape", wantField: `shape "source" fill pattern`, configure: func(d *d2target.Diagram) { d.Shapes[0].FillPattern = injection }},
		{name: "legend shape", wantField: `legend shape "legend-shape" fill pattern`, configure: func(d *d2target.Diagram) {
			d.Legend = &d2target.Legend{Shapes: []d2target.Shape{{ID: "legend-shape", FillPattern: injection}}}
		}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			diagram := paintValidationDiagram()
			tc.configure(diagram)
			diagram = roundTripTargetJSON(t, diagram)

			out, err := d2svg.Render(diagram, nil)
			if err == nil || !strings.Contains(err.Error(), tc.wantField+" uses unsupported value") {
				t.Fatalf("Render() output/error = %q/%v, want fill-pattern rejection", out, err)
			}
			if out != nil {
				t.Fatalf("Render() returned output on error: %q", out)
			}
		})
	}
}

func TestRenderPreservesSupportedPaintGrammar(t *testing.T) {
	t.Parallel()

	for _, paint := range []string{
		"",
		"none",
		"LightSteelBlue",
		"#abc",
		"#0A0f25",
		"N1",
		"AA4",
		"linear-gradient(to right, red, #00f)",
		"radial-gradient(white, #000000)",
	} {
		paint := paint
		t.Run("paint "+paint, func(t *testing.T) {
			t.Parallel()
			diagram := paintValidationDiagram()
			diagram.Shapes[0].Fill = paint
			out, err := d2svg.Render(roundTripTargetJSON(t, diagram), nil)
			if err != nil {
				t.Fatalf("Render() rejected supported paint %q: %v", paint, err)
			}
			assertStrictSafeSVG(t, out)
		})
	}

	for _, pattern := range []string{"", "none", "dots", "LINES", "Grain", "paper"} {
		pattern := pattern
		t.Run("fill pattern "+pattern, func(t *testing.T) {
			t.Parallel()
			diagram := paintValidationDiagram()
			diagram.Shapes[0].FillPattern = pattern
			out, err := d2svg.Render(roundTripTargetJSON(t, diagram), nil)
			if err != nil {
				t.Fatalf("Render() rejected supported fill pattern %q: %v", pattern, err)
			}
			assertStrictSafeSVG(t, out)
		})
	}

	t.Run("theme overrides", func(t *testing.T) {
		t.Parallel()
		diagram := paintValidationDiagram()
		darkThemeID := int64(200)
		out, err := d2svg.Render(diagram, &d2svg.RenderOpts{
			DarkThemeID:        &darkThemeID,
			ThemeOverrides:     &d2target.ThemeOverrides{N1: go2.Pointer("PapayaWhip")},
			DarkThemeOverrides: &d2target.ThemeOverrides{AB5: go2.Pointer("#0A0f25")},
		})
		if err != nil {
			t.Fatalf("Render() rejected supported theme overrides: %v", err)
		}
		assertStrictSafeSVG(t, out)
	})
}

func TestRenderCanonicalizesSupportedFillPatterns(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		pattern string
		want    string
	}{
		{pattern: "LINES", want: "lines"},
		{pattern: "Grain", want: "grain"},
	} {
		tc := tc
		t.Run(tc.pattern, func(t *testing.T) {
			t.Parallel()
			diagram := paintValidationDiagram()
			diagram.Shapes[0].FillPattern = tc.pattern
			out, err := d2svg.Render(roundTripTargetJSON(t, diagram), nil)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if !strings.Contains(string(out), tc.want+"-overlay") {
				t.Fatalf("Render() did not emit the canonical pattern class %q:\n%s", tc.want+"-overlay", out)
			}
			if !strings.Contains(string(out), "url(#"+tc.want+"-") {
				t.Fatalf("Render() did not define the canonical pattern %q:\n%s", tc.want, out)
			}
			if strings.Contains(string(out), tc.pattern+"-overlay") {
				t.Fatalf("Render() retained non-canonical pattern class %q:\n%s", tc.pattern+"-overlay", out)
			}
			assertStrictSafeSVG(t, out)
		})
	}

	t.Run("NONE", func(t *testing.T) {
		t.Parallel()
		diagram := paintValidationDiagram()
		diagram.Shapes[0].FillPattern = "NONE"
		out, err := d2svg.Render(roundTripTargetJSON(t, diagram), nil)
		if err != nil {
			t.Fatalf("Render() error = %v", err)
		}
		if strings.Contains(string(out), "NONE-overlay") || strings.Contains(string(out), "none-overlay") {
			t.Fatalf("Render() emitted an overlay for the none pattern:\n%s", out)
		}
		assertStrictSafeSVG(t, out)
	})
}

func TestThemeCSSRejectsUnsupportedOverrides(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		overrides *d2target.ThemeOverrides
		dark      *d2target.ThemeOverrides
		want      string
	}{
		{name: "external light paint", overrides: &d2target.ThemeOverrides{B1: go2.Pointer(externalPaintServer)}, want: "theme override B1 uses unsupported color"},
		{name: "external dark paint", dark: &d2target.ThemeOverrides{N7: go2.Pointer(externalPaintServer)}, want: "dark theme override N7 uses unsupported color"},
		{name: "CSS declaration", overrides: &d2target.ThemeOverrides{AA2: go2.Pointer(`red;}.attacker{background:url(http://127.0.0.1/)`)}, want: "theme override AA2 uses unsupported color"},
		{name: "gradient", overrides: &d2target.ThemeOverrides{AB4: go2.Pointer("linear-gradient(red, blue)")}, want: "theme override AB4 uses unsupported color"},
		{name: "theme token", overrides: &d2target.ThemeOverrides{N1: go2.Pointer("N2")}, want: "theme override N1 uses unsupported color"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stylesheet, err := d2svg.ThemeCSS("d2-test", nil, nil, tc.overrides, tc.dark)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ThemeCSS() stylesheet/error = %q/%v, want %q", stylesheet, err, tc.want)
			}
			if stylesheet != "" {
				t.Fatalf("ThemeCSS() returned stylesheet on error: %q", stylesheet)
			}
		})
	}
}

func paintValidationDiagram() *d2target.Diagram {
	diagram := d2target.NewDiagram()
	diagram.Root.Fill = "#ffffff"
	diagram.Root.Stroke = "none"
	diagram.Shapes = []d2target.Shape{
		{ID: "source", Type: d2target.ShapeRectangle, Pos: d2target.Point{}, Width: 40, Height: 40, Fill: "#ffffff", Stroke: "#000000", StrokeWidth: 2, Opacity: 1},
		{ID: "destination", Type: d2target.ShapeRectangle, Pos: d2target.Point{X: 100}, Width: 40, Height: 40, Fill: "#ffffff", Stroke: "#000000", StrokeWidth: 2, Opacity: 1},
	}
	connection := *d2target.BaseConnection()
	connection.ID = "edge"
	connection.Src = "source"
	connection.Dst = "destination"
	connection.Route = []*geo.Point{{X: 40, Y: 20}, {X: 100, Y: 20}}
	connection.Stroke = "#000000"
	connection.LabelPosition = label.InsideMiddleCenter.String()
	diagram.Connections = []d2target.Connection{connection}
	return diagram
}

func roundTripTargetJSON(t *testing.T, diagram *d2target.Diagram) *d2target.Diagram {
	t.Helper()
	raw, err := json.Marshal(diagram)
	if err != nil {
		t.Fatal(err)
	}
	var decoded d2target.Diagram
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	return &decoded
}

func assertStrictSafeSVG(t *testing.T, source []byte) {
	t.Helper()
	decoder := xml.NewDecoder(strings.NewReader(string(source)))
	decoder.Strict = true
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("Render() emitted invalid XML: %v\n%s", err, source)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if strings.EqualFold(start.Name.Local, "script") {
			t.Fatalf("Render() emitted a script element: %s", source)
		}
		for _, attr := range start.Attr {
			if strings.HasPrefix(strings.ToLower(attr.Name.Local), "on") {
				t.Fatalf("Render() emitted event-handler attribute %q: %s", attr.Name.Local, source)
			}
		}
	}
}
