package d2elklayout

import (
	"math"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2exporter"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestNativeCrossContainerRoutes(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	const nested = "outer: {inner: {a}}\n"
	const table = "outer: {inner: {t: {shape: sql_table; id: int; name: text; email: text}}}\n"
	for _, algorithm := range []string{"stress", "force", "mrtree", "radial"} {
		for _, tc := range []struct{ name, input string }{
			{"external-to-nested", nested + "external -> outer.inner.a"},
			{"container-to-leaf", nested + "outer -> outer.inner.a"},
			{"leaf-to-container", nested + "outer.inner.a -> outer"},
			{"column-targets", table + "x -> outer.inner.t.id\nx -> outer.inner.t.email"},
			{"column-sources", table + "outer.inner.t.id -> x\nouter.inner.t.email -> x"},
		} {
			t.Run(algorithm+"/"+tc.name, func(t *testing.T) {
				g := compileELKGoDiagram(t, ruler, tc.input)
				opts := DefaultOpts
				opts.Algorithm = algorithm
				if err := Layout(t.Context(), g, &opts); err != nil {
					t.Fatal(err)
				}
				diagram, err := d2exporter.Export(t.Context(), g, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				// The renderer rejects coincident consecutive points, even when a
				// route contains two entries and passed the layout stage.
				if _, err := d2svg.Render(diagram, nil); err != nil {
					t.Fatal(err)
				}
				for _, edge := range g.Edges {
					route := edge.Route
					for _, end := range []struct {
						obj   *d2graph.Object
						point *geo.Point
					}{{edge.Src, route[0]}, {edge.Dst, route[len(route)-1]}} {
						box := end.obj.Box
						// Border tracing can round an intersection to the closest pixel.
						dx := math.Abs(end.point.X-box.Center().X) - box.Width/2
						dy := math.Abs(end.point.Y-box.Center().Y) - box.Height/2
						if math.Abs(math.Max(dx, dy)) > 1 {
							t.Errorf("%s misses the border of %s at %v", edge.AbsID(), end.obj.AbsID(), end.point)
						}
					}
					for _, obj := range g.Objects {
						if !obj.IsContainer() {
							continue
						}
						title := geo.NewBox(obj.GetLabelTopLeft(), float64(obj.LabelDimensions.Width), float64(obj.LabelDimensions.Height))
						for i, p := range route[1:] {
							if title.Contains(p) || title.Intersects(geo.Segment{Start: route[i], End: p}, 0) {
								t.Errorf("%s crosses the title of %s", edge.AbsID(), obj.AbsID())
								break
							}
						}
					}
					obj, column, point := edge.Dst, edge.DstTableColumnIndex, route[len(route)-1]
					if edge.SrcTableColumnIndex != nil {
						obj, column, point = edge.Src, edge.SrcTableColumnIndex, route[0]
					}
					if column != nil {
						rowHeight := obj.Height / float64(len(obj.SQLTable.Columns)+1)
						wantY := obj.TopLeft.Y + (float64(*column)+1.5)*rowHeight
						if math.Abs(point.Y-wantY) > 1 || math.Min(math.Abs(point.X-obj.TopLeft.X), math.Abs(point.X-obj.TopLeft.X-obj.Width)) > 1 {
							t.Errorf("%s attaches at %v, want the side of %s at row center y=%g", edge.AbsID(), point, obj.AbsID(), wantY)
						}
					}
				}
			})
		}
	}
}

func TestMissingELKRouteReturnsError(t *testing.T) {
	// Box placement supplies node positions without routing connections.
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	g := compileELKGoDiagram(t, ruler, "a -> b")
	opts := DefaultOpts
	opts.Algorithm = "box"
	err = Layout(t.Context(), g, &opts)
	if err == nil || !strings.Contains(err.Error(), g.Edges[0].AbsID()) {
		t.Fatalf("want an error identifying the unrouted edge, got %v", err)
	}
}
