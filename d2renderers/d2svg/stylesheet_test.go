package d2svg

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func readStyles(t *testing.T, svg []byte) []string {
	t.Helper()
	d := xml.NewDecoder(bytes.NewReader(svg))
	var styles []string
	for {
		token, err := d.Token()
		if err == io.EOF {
			return styles
		}
		if err != nil {
			t.Fatal(err)
		}
		if el, ok := token.(xml.StartElement); ok && el.Name.Local == "style" {
			var css string
			if err := d.DecodeElement(&css, &el); err != nil {
				t.Fatal(err)
			}
			styles = append(styles, css)
		}
	}
}

func TestCompactStylesheet(t *testing.T) {
	first := d2target.Shape{
		ID: "one", Type: d2target.ShapeRectangle, Width: 100, Height: 80,
		Opacity: 1, Text: d2target.Text{Label: "Hello"},
	}
	for _, tc := range []struct {
		name      string
		blend     bool
		classes   []string
		wantBlend bool
	}{
		{"plain", false, nil, false},
		{"blend property", true, nil, true},
		{"blend class", false, []string{"blend"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diagram := d2target.NewDiagram()
			diagram.Root.FillPattern = "dots"
			diagram.Shapes = []d2target.Shape{first}
			diagram.Shapes[0].Classes, diagram.Shapes[0].Blend = tc.classes, tc.blend
			out, err := Render(diagram, nil)
			if err != nil {
				t.Fatal(err)
			}
			styles := readStyles(t, out)
			if len(styles) != 1 || !strings.Contains(styles[0], "@font-face{") || !strings.Contains(styles[0], ".dots-overlay{") || !strings.Contains(styles[0], ".shape{") {
				t.Fatal("expected one stylesheet for shapes, fonts, and fill patterns")
			}
			if strings.Contains(styles[0], ".blend{") != tc.wantBlend {
				t.Fatalf("blend rule presence must match %s", tc.name)
			}
			if bytes.Contains(out, []byte("<![CDATA[")) {
				t.Fatal("unexpected CDATA for generated rules")
			}
		})
	}

	t.Run("connection class", func(t *testing.T) {
		edge := d2target.BaseConnection()
		edge.ID, edge.Src, edge.Dst = "(one -> two)[0]", "one", "two"
		edge.Classes = []string{"blend"}
		edge.DstArrow = d2target.TriangleArrowhead
		edge.Route = []*geo.Point{geo.NewPoint(100, 40), geo.NewPoint(150, 40)}
		diagram := d2target.NewDiagram()
		second := first
		second.ID, second.Pos.X = "two", 150
		diagram.Shapes = []d2target.Shape{first, second}
		diagram.Connections = []d2target.Connection{*edge}
		out, err := Render(diagram, nil)
		if err != nil {
			t.Fatal(err)
		}
		if styles := readStyles(t, out); len(styles) != 1 || !strings.Contains(styles[0], ".blend{") {
			t.Fatal("connection class lost its blend rule")
		}
		if !bytes.Contains(out, []byte(`<marker id="mk-`)) || bytes.Contains(out, []byte(`<marker id="mk-d2-`)) {
			t.Fatal("standalone output must use a compact marker ID")
		}
		frame, err := Render(diagram, &RenderOpts{MasterID: "d2-master"})
		if err != nil || !bytes.Contains(frame, []byte(`<marker id="mk-d2-`)) {
			t.Fatalf("frame lost its original marker ID: %v", err)
		}
		old := BaseStylesheet
		t.Cleanup(func() { BaseStylesheet = old })
		BaseStylesheet += "/* custom stylesheet */"
		withCSS, err := Render(diagram, nil)
		if err != nil || !bytes.Contains(withCSS, []byte(`<marker id="mk-d2-`)) {
			t.Fatalf("custom stylesheet lost its original marker ID: %v", err)
		}
	})

	diagram := d2target.NewDiagram()
	for _, source := range []string{`<g class="edge blend"/>`, `<g><set attributeName="class" to="blend"/></g>`, `<use href="other.svg#shape"/>`} {
		var svg bytes.Buffer
		if err := WriteAnimationStylesWithTransitions(&svg, "d2-test", source, diagram, RenderOpts{}, []byte("@keyframes clock{}")); err != nil {
			t.Fatal(err)
		}
		styles := readStyles(t, svg.Bytes())
		if len(styles) != 1 || !strings.Contains(styles[0], ".blend{") || !strings.Contains(styles[0], "@keyframes clock{}") {
			t.Fatal("animation lost a possible blend or its clock")
		}
	}
}

func TestFillPatternAfterDarkTheme(t *testing.T) {
	diagram := d2target.NewDiagram()
	diagram.Root.FillPattern = "dots"
	dark := int64(200)
	out, err := Render(diagram, &RenderOpts{DarkThemeID: &dark})
	if err != nil {
		t.Fatal(err)
	}
	styles := readStyles(t, out)
	if len(styles) != 1 || !strings.Contains(styles[0], "}} .dots-overlay{") {
		t.Fatal("ImageIO requires whitespace between the dark media query and the fill pattern")
	}
}

func TestCustomStylesheetPreservesSheetBoundaries(t *testing.T) {
	original := BaseStylesheet
	t.Cleanup(func() { BaseStylesheet = original })
	BaseStylesheet = "@import url('sheet.css');\n@namespace svg url(http://www.w3.org/2000/svg);\nsvg|text { --example: '<&]]>'; }\n"
	var svg bytes.Buffer
	if err := WriteAnimationStylesWithTransitions(&svg, "d2-test", `<text class="text">A</text>`, d2target.NewDiagram(), RenderOpts{}, []byte("@keyframes clock{}")); err != nil {
		t.Fatal(err)
	}
	styles := readStyles(t, svg.Bytes())
	if len(styles) != 3 || !strings.Contains(styles[0], "@font-face") || !strings.HasPrefix(styles[1], BaseStylesheet) || styles[2] != "@keyframes clock{}" {
		t.Fatal("custom stylesheet must retain its bytes and its position between font and animation sheets")
	}
}

func TestRegisteredFontStylesheetURL(t *testing.T) {
	family := d2fonts.FontFamily("compact-style-test")
	face := family.Font(0, d2fonts.FONT_STYLE_REGULAR)
	const uri = "https://example.org/a (one).woff?a=1&b=2]]>"
	d2fonts.FontEncodings.Set(face, uri)
	t.Cleanup(func() { d2fonts.FontEncodings.Delete(face) })
	diagram := d2target.NewDiagram()
	diagram.FontFamily = &family
	var buf bytes.Buffer
	if err := WriteAnimationStyles(&buf, "d2-test", `<text class="text">A</text>`, diagram, RenderOpts{}); err != nil {
		t.Fatal(err)
	}
	styles := readStyles(t, buf.Bytes())
	if len(styles) != 1 || !strings.Contains(styles[0], `url("`+uri+`")`) {
		t.Fatal("registered font URL must round-trip XML and stay quoted in CSS")
	}
}
