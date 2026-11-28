package d2svg_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func TestMotionOffsetTranslatesBorderLabelAndLink(t *testing.T) {
	s := d2target.BaseShape()
	s.ID, s.Type = "box", d2target.ShapeRectangle
	s.Width, s.Height = 80, 40
	s.Label, s.LabelWidth, s.LabelHeight = "node", 30, 10
	s.LabelPosition = label.BorderTopCenter.String()
	s.Link = "https://example.com"
	s.MotionOffset = geo.NewPoint(.25, -.5)
	d := d2target.NewDiagram()
	d.Shapes = []d2target.Shape{*s}
	b, err := d2svg.Render(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	// The shape, its separately painted appendix icon, and its hole in the
	// board mask all travel together. Its own mask uses the inverse mapping.
	if strings.Count(out, `transform="translate(0.25 -0.5)"`) < 3 ||
		!strings.Contains(out, `transform="translate(-0.25 0.5)"`) ||
		!strings.Contains(out, `-moving-border-`) {
		t.Fatal("missing translation for the shape, appendix or mask")
	}
}
