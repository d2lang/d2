package d2svg_test

import (
	"bytes"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// Keep the exported path intact so its arrowhead and dash phase survive the cut.
func TestRenderDefaultConnectionGap(t *testing.T) {
	under := *d2target.BaseConnection()
	under.ID = "under"
	under.Classes = []string{"test-under"}
	under.Stroke = "#346789"
	under.StrokeDash = 3
	under.DstArrow = d2target.TriangleArrowhead
	under.Route = []*geo.Point{{X: 0, Y: 50}, {X: 100, Y: 50}}

	over := *d2target.BaseConnection()
	over.ID = "over"
	over.Classes = []string{"test-over"}
	over.Stroke = "#884322"
	over.ZIndex = 1
	over.Route = []*geo.Point{{X: 50, Y: 0}, {X: 50, Y: 100}}

	diagram := d2target.NewDiagram()
	diagram.Connections = []d2target.Connection{over, under}
	for _, sketch := range []bool{false, true} {
		out, err := d2svg.Render(diagram, &d2svg.RenderOpts{Sketch: &sketch})
		if err != nil {
			t.Fatal(err)
		}
		assertUniqueResolvableLocalIRIs(t, out)
		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(out))
		if err != nil {
			t.Fatal(err)
		}
		gapSelector := `[mask^="url(#crossings-"]`
		if doc.Find(".test-under").Find(gapSelector).Length() != 1 || doc.Find(".test-over").Find(gapSelector).Length() != 0 {
			t.Fatalf("sketch %v: expected only the lower edge to reference a crossing mask", sketch)
		}
		if !sketch && doc.Find(`.test-under path[marker-end][style*="stroke-dasharray:"]`).Length() != 1 {
			t.Fatal("masked connection lost its dashed stroke or arrowhead")
		}
	}
}
