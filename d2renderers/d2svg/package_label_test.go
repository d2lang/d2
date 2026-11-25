package d2svg_test

import (
	"context"
	"encoding/xml"
	"strconv"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/util-go/assert"
)

type svgDoc struct {
	Inner struct {
		Groups []svgGroup `xml:"g"`
	} `xml:"svg"`
}

type svgGroup struct {
	Shape struct {
		Path struct {
			D string `xml:"d,attr"`
		} `xml:"path"`
	} `xml:"g"`
	Texts []svgText `xml:"text"`
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
