package d2svg_test

import (
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestCrossingGapsUseVisiblePaint(t *testing.T) {
	transparent := "transparent"
	hidden := &d2target.ThemeOverrides{B1: &transparent}
	dark := int64(200)
	tests := []struct {
		name, stroke string
		opts         d2svg.RenderOpts
		want         bool
	}{
		{"clear gradient", "linear-gradient(transparent, rgba(0,0,0,0))", d2svg.RenderOpts{}, false},
		{"transparent middle interval", "linear-gradient(red 0%, transparent 35%, transparent 65%, blue 100%)", d2svg.RenderOpts{}, false},
		{"transparent leading interval", "linear-gradient(transparent 25%, red 100%)", d2svg.RenderOpts{}, false},
		{"transparent trailing interval", "linear-gradient(red 0%, transparent 65%)", d2svg.RenderOpts{}, false},
		{"isolated transparent midpoint", "linear-gradient(red, transparent 50%, transparent 50%, red)", d2svg.RenderOpts{}, true},
		{"partially visible gradient", "linear-gradient(transparent, #123456)", d2svg.RenderOpts{}, true},
		{"transparent theme override", "B1", d2svg.RenderOpts{ThemeOverrides: hidden}, false},
		{"dark mode hides edge", "B1", d2svg.RenderOpts{DarkThemeID: &dark, DarkThemeOverrides: hidden}, false},
		{"light mode hides edge", "B1", d2svg.RenderOpts{ThemeOverrides: hidden, DarkThemeID: &dark}, false},
		{"both modes paint edge", "B1", d2svg.RenderOpts{DarkThemeID: &dark}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			back := *d2target.BaseConnection()
			back.ID, back.Stroke = "back", "#123456"
			back.Route = []*geo.Point{{X: 0, Y: 50}, {X: 100, Y: 50}}
			front := *d2target.BaseConnection()
			front.ID, front.Stroke = "front", tc.stroke
			front.Route = []*geo.Point{{X: 20, Y: 0}, {X: 80, Y: 100}}
			diagram := d2target.NewDiagram()
			diagram.Connections = []d2target.Connection{back, front}
			doc := renderCrossingSVG(t, diagram, &tc.opts)
			got := doc.Find(`[id$="-cg0"]`).Length() != 0
			if got != tc.want {
				t.Fatalf("crossing gap present = %t, want %t", got, tc.want)
			}
		})
	}
}
