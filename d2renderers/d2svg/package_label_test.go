package d2svg_test

import (
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/d2lang/util-go/assert"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

type svgDoc struct {
	Inner struct {
		Groups []svgGroup `xml:"g"`
	} `xml:"svg"`
}

type svgGroup struct {
	Groups []svgSubGroup `xml:"g"`
	Texts  []svgText     `xml:"text"`
	SVGs   []svgNested   `xml:"svg"`
}

type svgSubGroup struct {
	Class     string    `xml:"class,attr"`
	Transform string    `xml:"transform,attr"`
	Rects     []svgRect `xml:"rect"`
}

type svgRect struct {
	X      string `xml:"x,attr"`
	Y      string `xml:"y,attr"`
	Width  string `xml:"width,attr"`
	Height string `xml:"height,attr"`
}

type svgNested struct {
	X      string `xml:"x,attr"`
	Y      string `xml:"y,attr"`
	Width  string `xml:"width,attr"`
	Height string `xml:"height,attr"`
}

type svgText struct {
	X    string `xml:"x,attr"`
	Y    string `xml:"y,attr"`
	Body string `xml:",chardata"`
}

func TestPackageTopLabelUsesTabWhenItFits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		script         string
		wantInTab      bool
		wantCenterTabX bool
		wantRightSide  bool
	}{
		{
			name: "top-left fits in tab",
			script: `pkg: x {
				shape: package
				width: 200
				height: 200
				label.near: top-left
			}`,
			wantInTab: true,
		},
		{
			name: "top-center fits in tab",
			script: `pkg: x {
				shape: package
				width: 200
				height: 200
				label.near: top-center
			}`,
			wantInTab:      true,
			wantCenterTabX: true,
		},
		{
			name: "top-left overflows tab width",
			script: `pkg: "very long package title" {
				shape: package
				width: 200
				height: 200
				label.near: top-left
			}`,
			wantInTab: false,
		},
		{
			name: "top-right stays below cutout",
			script: `pkg: x {
				shape: package
				width: 200
				height: 200
				label.near: top-right
			}`,
			wantInTab:     false,
			wantRightSide: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ruler, err := textmeasure.NewRuler()
			assert.Success(t, err)

			diagram, _, err := d2lib.Compile(log.WithTB(context.Background(), t), tc.script, &d2lib.CompileOptions{
				Ruler: ruler,
				LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
					return d2dagrelayout.DefaultLayout, nil
				},
			}, &d2svg.RenderOpts{})
			assert.Success(t, err)

			out, err := d2svg.Render(diagram, &d2svg.RenderOpts{})
			assert.Success(t, err)

			var doc svgDoc
			assert.Success(t, xml.Unmarshal(out, &doc))
			if len(doc.Inner.Groups) == 0 || len(doc.Inner.Groups[0].Texts) == 0 {
				t.Fatalf("expected shape group and text in rendered SVG")
			}

			s := diagram.Shapes[0]
			textNode := doc.Inner.Groups[0].Texts[0]
			x, err := strconv.ParseFloat(textNode.X, 64)
			assert.Success(t, err)
			y, err := strconv.ParseFloat(textNode.Y, 64)
			assert.Success(t, err)

			topWidth := float64(s.Width) * 0.5
			topHeight := float64(s.Height) * 0.2
			tabBottomY := float64(s.Pos.Y) + topHeight

			if tc.wantInTab {
				if y >= tabBottomY {
					t.Fatalf("expected label baseline y=%v inside top tab (y < %v)", y, tabBottomY)
				}
			} else {
				if y <= tabBottomY {
					t.Fatalf("expected label baseline y=%v below top tab (y > %v)", y, tabBottomY)
				}
			}

			if tc.wantCenterTabX {
				wantX := float64(s.Pos.X) + topWidth/2
				if x != wantX {
					t.Fatalf("expected centered tab x=%v, got %v", wantX, x)
				}
			}

			if tc.wantRightSide {
				if x <= float64(s.Pos.X)+topWidth {
					t.Fatalf("expected top-right label x=%v on right side (> %v)", x, float64(s.Pos.X)+topWidth)
				}
			}
		})
	}
}

func parseSVGTranslate(transform string) (float64, float64, error) {
	trimmed := strings.TrimSpace(transform)
	if !strings.HasPrefix(trimmed, "translate(") || !strings.HasSuffix(trimmed, ")") {
		return 0, 0, fmt.Errorf("unexpected transform %q", transform)
	}
	coords := strings.Fields(strings.TrimSuffix(strings.TrimPrefix(trimmed, "translate("), ")"))
	if len(coords) != 2 {
		return 0, 0, fmt.Errorf("unexpected translate coordinates in %q", transform)
	}
	x, err := strconv.ParseFloat(coords[0], 64)
	if err != nil {
		return 0, 0, err
	}
	y, err := strconv.ParseFloat(coords[1], 64)
	if err != nil {
		return 0, 0, err
	}
	return x, y, nil
}

func parseOptionalFloat(raw string) (float64, error) {
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseFloat(raw, 64)
}

func TestPackageCodeLabelDoesNotCoverTabNotch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		script string
	}{
		{
			name: "short go code label with top-center stays in package body",
			script: `x: |go
  hi
| {
  shape: package
  label.near: top-center
  width: 250
  height: 250
}`,
		},
		{
			name: "short go code label with top-left stays in package body",
			script: `x: |go
  hi
| {
  shape: package
  label.near: top-left
  width: 250
  height: 250
}`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ruler, err := textmeasure.NewRuler()
			assert.Success(t, err)

			diagram, _, err := d2lib.Compile(log.WithTB(context.Background(), t), tc.script, &d2lib.CompileOptions{
				Ruler: ruler,
				LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
					return d2dagrelayout.DefaultLayout, nil
				},
			}, &d2svg.RenderOpts{})
			assert.Success(t, err)

			out, err := d2svg.Render(diagram, &d2svg.RenderOpts{})
			assert.Success(t, err)

			var doc svgDoc
			assert.Success(t, xml.Unmarshal(out, &doc))
			if len(doc.Inner.Groups) == 0 {
				t.Fatalf("expected shape group in rendered SVG")
			}

			s := diagram.Shapes[0]
			topWidth := float64(s.Width) * 0.5
			topHeight := float64(s.Height) * 0.2
			notchLeft := float64(s.Pos.X) + topWidth
			notchRight := float64(s.Pos.X) + float64(s.Width)
			notchTop := float64(s.Pos.Y)
			notchBottom := float64(s.Pos.Y) + topHeight

			foundCodeGroups := 0
			for _, sub := range doc.Inner.Groups[0].Groups {
				if sub.Class != "light-code" && sub.Class != "dark-code" {
					continue
				}
				foundCodeGroups++
				if len(sub.Rects) == 0 {
					t.Fatalf("expected code background rect in %s group", sub.Class)
				}
				tx, ty, err := parseSVGTranslate(sub.Transform)
				assert.Success(t, err)
				rx, err := parseOptionalFloat(sub.Rects[0].X)
				assert.Success(t, err)
				ry, err := parseOptionalFloat(sub.Rects[0].Y)
				assert.Success(t, err)
				rw, err := strconv.ParseFloat(sub.Rects[0].Width, 64)
				assert.Success(t, err)
				rh, err := strconv.ParseFloat(sub.Rects[0].Height, 64)
				assert.Success(t, err)

				rectLeft := tx + rx
				rectTop := ty + ry
				rectRight := rectLeft + rw
				rectBottom := rectTop + rh

				if rectRight > notchLeft && rectLeft < notchRight && rectBottom > notchTop && rectTop < notchBottom {
					t.Fatalf("%s code background rect [%v,%v %v×%v] overlaps package notch [%v,%v %v×%v]",
						sub.Class, rectLeft, rectTop, rw, rh, notchLeft, notchTop, notchRight-notchLeft, notchBottom-notchTop)
				}
				if rectTop < notchBottom {
					t.Fatalf("%s code background top y=%v is above package body y=%v", sub.Class, rectTop, notchBottom)
				}
			}
			if foundCodeGroups != 2 {
				t.Fatalf("found %d code groups (light-code/dark-code), want 2", foundCodeGroups)
			}
		})
	}
}

func TestPackageMarkdownLabelFitsInTabWithoutCoveringNotch(t *testing.T) {
	t.Parallel()

	script := `x: |md
  hi
| {
  shape: package
  label.near: top-center
  width: 250
  height: 250
}`

	ruler, err := textmeasure.NewRuler()
	assert.Success(t, err)

	diagram, _, err := d2lib.Compile(log.WithTB(context.Background(), t), script, &d2lib.CompileOptions{
		Ruler: ruler,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return d2dagrelayout.DefaultLayout, nil
		},
	}, &d2svg.RenderOpts{})
	assert.Success(t, err)

	out, err := d2svg.Render(diagram, &d2svg.RenderOpts{})
	assert.Success(t, err)

	var doc svgDoc
	assert.Success(t, xml.Unmarshal(out, &doc))
	if len(doc.Inner.Groups) == 0 || len(doc.Inner.Groups[0].SVGs) == 0 {
		t.Fatalf("expected nested markdown svg in rendered SVG")
	}

	s := diagram.Shapes[0]
	topWidth := float64(s.Width) * 0.5
	topHeight := float64(s.Height) * 0.2

	mdSVG := doc.Inner.Groups[0].SVGs[0]
	x, err := strconv.ParseFloat(mdSVG.X, 64)
	assert.Success(t, err)
	y, err := strconv.ParseFloat(mdSVG.Y, 64)
	assert.Success(t, err)
	w, err := strconv.ParseFloat(mdSVG.Width, 64)
	assert.Success(t, err)
	h, err := strconv.ParseFloat(mdSVG.Height, 64)
	assert.Success(t, err)

	if x < float64(s.Pos.X) || x+w > float64(s.Pos.X)+topWidth || y < float64(s.Pos.Y) || y+h > float64(s.Pos.Y)+topHeight {
		t.Fatalf("markdown viewport [%v,%v %v×%v] does not fit inside package tab [%v,%v %v×%v]",
			x, y, w, h, s.Pos.X, s.Pos.Y, topWidth, topHeight)
	}
}
