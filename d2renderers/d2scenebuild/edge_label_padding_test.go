package d2scenebuild

import (
	"context"
	"math"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2target"
)

func TestBuildConnectionEdgeLabelHorizontalPadding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		edit       func(*d2target.Connection)
		wantPad    float64
		wantExtraX float64
		wantExtraW float64
		wantH      float64
		wantFill   bool
	}{
		{
			name: "single-line-with-fill",
			edit: func(c *d2target.Connection) {
				c.Fill = "#112233"
				c.Text = d2target.Text{
					Label: "true", FontSize: 16, LabelWidth: 28, LabelHeight: 21,
				}
				c.LabelPosition = "INSIDE_MIDDLE_CENTER"
				c.LabelPercentage = .5
			},
			wantPad:  21,
			wantH:    21,
			wantFill: true,
		},
		{
			name: "multiline-label",
			edit: func(c *d2target.Connection) {
				c.Text = d2target.Text{
					Label: "line 1\nline 2", FontSize: 16, LabelWidth: 40, LabelHeight: 37,
				}
				c.LabelPosition = "INSIDE_MIDDLE_CENTER"
				c.LabelPercentage = .5
			},
			wantPad: 19,
			wantH:   37,
		},
		{
			name: "icon-and-label",
			edit: func(c *d2target.Connection) {
				c.Icon = testRasterAssetURL(t)
				c.IconPosition = "INSIDE_MIDDLE_CENTER"
				c.Text = d2target.Text{
					Label: "hello", FontSize: 16, LabelWidth: 37, LabelHeight: 21,
				}
				c.LabelPosition = "INSIDE_MIDDLE_CENTER"
				c.LabelPercentage = .5
			},
			wantPad:    21,
			wantExtraX: float64(d2target.CONNECTION_ICON_LABEL_GAP + d2target.DEFAULT_ICON_SIZE),
			wantExtraW: float64(d2target.CONNECTION_ICON_LABEL_GAP + d2target.DEFAULT_ICON_SIZE),
			wantH:      21,
		},
		{
			name: "icon-only",
			edit: func(c *d2target.Connection) {
				c.Icon = testRasterAssetURL(t)
				c.IconPosition = "INSIDE_MIDDLE_CENTER"
				c.LabelPosition = "INSIDE_MIDDLE_CENTER"
				c.LabelPercentage = .5
			},
			wantPad: 4,
			wantH:   float64(d2target.DEFAULT_ICON_SIZE),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			diagram := validDiagram()
			conn := &diagram.Connections[0]
			tc.edit(conn)
			pad := int64(0)
			document, err := Build(context.Background(), diagram, Options{Pad: &pad, Assets: testAssetOptions(t)})
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}

			geometry := findSceneNode(t, document.Root, "a-b:geometry")
			hole := findSceneNode(t, geometry.Mask.Root, "a-b:label-mask-hole")
			rect, ok := hole.Primitive.(d2scene.Rect)
			if !ok {
				t.Fatalf("label mask hole = %T, want Rect", hole.Primitive)
			}

			var wantHole d2scene.Box
			if conn.Label == "" {
				iconTL := conn.GetIconPosition()
				wantHole = d2scene.Box{
					X:      math.Round(iconTL.X) - 2,
					Y:      math.Round(iconTL.Y),
					Width:  float64(d2target.DEFAULT_ICON_SIZE + 4),
					Height: tc.wantH,
				}
			} else {
				tl := conn.GetLabelTopLeft()
				wantHole = d2scene.Box{
					X:      math.Round(tl.X) - tc.wantExtraX - tc.wantPad/2,
					Y:      math.Round(tl.Y),
					Width:  float64(conn.LabelWidth) + tc.wantExtraW + tc.wantPad,
					Height: tc.wantH,
				}
			}
			if rect.Box != wantHole {
				t.Fatalf("mask hole box = %+v, want %+v", rect.Box, wantHole)
			}

			if tc.wantFill {
				fillNode := findSceneNode(t, document.Root, "a-b:label-fill")
				fillRect, ok := fillNode.Primitive.(d2scene.Rect)
				if !ok {
					t.Fatalf("label fill = %T, want Rect", fillNode.Primitive)
				}
				tl := conn.GetLabelTopLeft()
				wantFillPad := tc.wantPad + 4
				wantFillBox := d2scene.Box{
					X:      tl.X - wantFillPad/2,
					Y:      tl.Y - 3,
					Width:  float64(conn.LabelWidth) + wantFillPad,
					Height: float64(conn.LabelHeight + 6),
				}
				if fillRect.Box != wantFillBox {
					t.Fatalf("label fill box = %+v, want %+v", fillRect.Box, wantFillBox)
				}
			}
		})
	}
}
