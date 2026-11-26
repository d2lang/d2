package d2svg_test

import (
	"bytes"
	"math"
	"net/url"
	"strconv"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func renderEdgeLabelTestDoc(t *testing.T, edit func(*d2target.Connection)) (*goquery.Document, *d2target.Connection) {
	t.Helper()
	diagram := d2target.NewDiagram()
	fontFamily := d2fonts.SourceSansPro
	monoFontFamily := d2fonts.SourceCodePro
	diagram.FontFamily = &fontFamily
	diagram.MonoFontFamily = &monoFontFamily

	conn := *d2target.BaseConnection()
	conn.ID = "(a -> b)[0]"
	conn.Stroke = "#0D32B2"
	conn.StrokeWidth = 2
	conn.DstArrow = d2target.TriangleArrowhead
	conn.Route = []*geo.Point{{X: 0, Y: 50}, {X: 200, Y: 50}}
	conn.LabelPosition = "INSIDE_MIDDLE_CENTER"
	conn.LabelPercentage = 0.5
	edit(&conn)
	diagram.Connections = []d2target.Connection{conn}

	out, err := d2svg.Render(diagram, &d2svg.RenderOpts{})
	if err != nil {
		t.Fatalf("d2svg.Render() error = %v", err)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("parse svg: %v", err)
	}
	return doc, &diagram.Connections[0]
}

func parseSVGFloatAttr(t *testing.T, sel *goquery.Selection, name string) float64 {
	t.Helper()
	raw, ok := sel.Attr(name)
	if !ok {
		t.Fatalf("missing attribute %q on element", name)
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		t.Fatalf("parse attribute %q=%q: %v", name, raw, err)
	}
	return v
}

func TestEdgeLabelMaskHorizontalPaddingMatchesLineHeight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		edit       func(*d2target.Connection)
		wantPad    float64
		wantExtraX float64
		wantExtraW float64
		wantFill   bool
	}{
		{
			name: "single-line-plain-no-fill",
			edit: func(c *d2target.Connection) {
				c.Text = d2target.Text{
					Label: "sync", FontSize: 16, FontFamily: "DEFAULT", Bold: true,
					LabelWidth: 32, LabelHeight: 21,
				}
			},
			wantPad: 21,
		},
		{
			name: "multiline-plain-no-fill",
			edit: func(c *d2target.Connection) {
				c.Text = d2target.Text{
					Label: "line 1\nline 2", FontSize: 16, FontFamily: "DEFAULT",
					LabelWidth: 40, LabelHeight: 37,
				}
			},
			wantPad: 19,
		},
		{
			name: "single-line-with-fill",
			edit: func(c *d2target.Connection) {
				c.Fill = "#112233"
				c.Text = d2target.Text{
					Label: "true", FontSize: 16, FontFamily: "DEFAULT",
					LabelWidth: 28, LabelHeight: 21,
				}
			},
			wantPad:  21,
			wantFill: true,
		},
		{
			name: "icon-and-label",
			edit: func(c *d2target.Connection) {
				c.Icon = &url.URL{Path: "icon.svg"}
				c.Text = d2target.Text{
					Label: "hello", FontSize: 16, FontFamily: "DEFAULT",
					LabelWidth: 37, LabelHeight: 21,
				}
			},
			wantPad:    21,
			wantExtraX: float64(d2target.CONNECTION_ICON_LABEL_GAP + d2target.DEFAULT_ICON_SIZE),
			wantExtraW: float64(d2target.CONNECTION_ICON_LABEL_GAP + d2target.DEFAULT_ICON_SIZE),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, conn := renderEdgeLabelTestDoc(t, tc.edit)
			tl := conn.GetLabelTopLeft()
			maskRect := doc.Find(`mask rect[fill="black"]`).First()
			if maskRect.Length() != 1 {
				t.Fatalf("expected 1 black mask rect, got %d", maskRect.Length())
			}

			gotX := parseSVGFloatAttr(t, maskRect, "x")
			gotY := parseSVGFloatAttr(t, maskRect, "y")
			gotW := parseSVGFloatAttr(t, maskRect, "width")
			gotH := parseSVGFloatAttr(t, maskRect, "height")

			wantX := math.Round(tl.X) - tc.wantExtraX - tc.wantPad/2
			wantW := float64(conn.LabelWidth) + tc.wantExtraW + tc.wantPad
			if gotX != wantX || gotY != math.Round(tl.Y) || gotW != wantW || gotH != float64(conn.LabelHeight) {
				t.Fatalf("mask rect = (x=%v, y=%v, w=%v, h=%v), want (x=%v, y=%v, w=%v, h=%v)",
					gotX, gotY, gotW, gotH, wantX, math.Round(tl.Y), wantW, float64(conn.LabelHeight))
			}

			if tc.wantFill {
				fillRect := doc.Find(`rect[rx="10"][fill="#112233"]`).First()
				if fillRect.Length() != 1 {
					t.Fatalf("expected 1 pill fill rect, got %d", fillRect.Length())
				}
				fillX := parseSVGFloatAttr(t, fillRect, "x")
				fillY := parseSVGFloatAttr(t, fillRect, "y")
				fillW := parseSVGFloatAttr(t, fillRect, "width")
				fillH := parseSVGFloatAttr(t, fillRect, "height")
				wantFillPad := tc.wantPad + 4
				wantFillX := math.Round(tl.X) - wantFillPad/2
				wantFillW := float64(conn.LabelWidth) + wantFillPad
				if fillX != wantFillX || fillY != math.Round(tl.Y)-3 || fillW != wantFillW || fillH != float64(conn.LabelHeight)+6 {
					t.Fatalf("fill rect = (x=%v, y=%v, w=%v, h=%v), want (x=%v, y=%v, w=%v, h=%v)",
						fillX, fillY, fillW, fillH, wantFillX, math.Round(tl.Y)-3, wantFillW, float64(conn.LabelHeight)+6)
				}
			}
		})
	}
}

func TestEdgeIconOnlyMaskKeepsCompactPadding(t *testing.T) {
	t.Parallel()
	doc, _ := renderEdgeLabelTestDoc(t, func(c *d2target.Connection) {
		c.Icon = &url.URL{Path: "icon.svg"}
		c.IconPosition = "INSIDE_MIDDLE_CENTER"
	})
	maskRect := doc.Find(`mask rect[fill="black"]`).First()
	if maskRect.Length() != 1 {
		t.Fatalf("expected 1 black mask rect, got %d", maskRect.Length())
	}
	gotW := parseSVGFloatAttr(t, maskRect, "width")
	wantW := float64(d2target.DEFAULT_ICON_SIZE + 4)
	if gotW != wantW {
		t.Fatalf("icon-only mask width = %v, want %v", gotW, wantW)
	}
}
