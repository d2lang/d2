package d2svg

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// TestLegendGolden pins RenderLegend and the legend-sized viewport of Render.
// Regenerate with UPDATE_DRAW_GOLDEN=1 only for intended output changes.
func TestLegendGolden(t *testing.T) {
	var b strings.Builder
	labels := [][]string{{}, {""}, {"a"}, {"alpha", ""}, {"a much longer legend label", "x", "mono"}}
	for si, shapeLabels := range labels {
		for ci, connLabels := range labels {
			for _, title := range []string{"", "Key"} {
				d := d2target.NewDiagram()
				box := d2target.BaseShape()
				box.ID, box.Type = "box", d2target.ShapeRectangle
				box.Width, box.Height = 100, 60
				box.Fill, box.Stroke = "#ffffff", "#000000"
				d.Shapes = []d2target.Shape{*box}
				legend := &d2target.Legend{Label: title}
				for i, l := range shapeLabels {
					s := d2target.BaseShape()
					s.ID, s.Label = fmt.Sprintf("s%d", i), l
					s.Type = []string{d2target.ShapeRectangle, d2target.ShapeCircle, d2target.ShapeHexagon}[i%3]
					s.Fill, s.Stroke = "#ffeedd", "#112233"
					if l == "mono" {
						s.FontFamily = "mono"
					}
					legend.Shapes = append(legend.Shapes, *s)
				}
				for i, l := range connLabels {
					c := d2target.BaseConnection()
					c.ID, c.Label = fmt.Sprintf("c%d", i), l
					c.Stroke = "#223344"
					c.DstArrow = d2target.TriangleArrowhead
					c.Route = []*geo.Point{{X: 0, Y: 0}, {X: 10, Y: 0}}
					if l == "x" {
						c.FontFamily = "mono"
					}
					legend.Connections = append(legend.Connections, *c)
				}
				d.Legend = legend
				buf := &bytes.Buffer{}
				err := RenderLegend(buf, d, "dh", nil)
				out, rerr := Render(d, &RenderOpts{})
				sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%v\x00%s\x00%v", buf.String(), err, out, rerr)))
				fmt.Fprintf(&b, "legend/s%d/c%d/%q %x\n", si, ci, title, sum[:8])
			}
		}
	}
	checkGolden(t, "testdata/legend_golden.txt", b.String())
}
