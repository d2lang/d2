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

func TestGapSeparatesOnlyPaintedStaticDashes(t *testing.T) {
	under := *d2target.BaseConnection()
	under.ID = "under"
	under.Stroke = "black"
	under.Route = []*geo.Point{{X: 0, Y: 10}, {X: 100, Y: 10}}
	over := *d2target.BaseConnection()
	over.ID = "dashed"
	over.Stroke = "red"
	over.StrokeDash = 14
	d := d2target.NewDiagram()
	for _, tc := range []struct {
		name     string
		top      float64
		animated bool
		wantGap  bool
	}{
		{"off interval", -32, false, false},
		{"painted dash", -4, false, true},
		{"moving dash", -32, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			over.Animated = tc.animated
			over.Route = []*geo.Point{{X: 50, Y: tc.top}, {X: 50, Y: 80}}
			d.Connections = []d2target.Connection{under, over}
			for _, sketch := range []bool{false, true} {
				out, err := d2svg.Render(d, &d2svg.RenderOpts{Sketch: &sketch})
				if err != nil {
					t.Fatal(err)
				}
				if got := bytes.Contains(out, []byte(`id="crossings-`)); got != tc.wantGap {
					t.Fatalf("sketch %t: gap=%t, want %t", sketch, got, tc.wantGap)
				}
			}
		})
	}
}

func TestSketchGapFitsHandDrawnStrokes(t *testing.T) {
	for _, length := range []float64{300, 800, 1500, 10000} {
		h := *d2target.BaseConnection()
		h.ID, h.Stroke = "horizontal", "black"
		h.Route = []*geo.Point{{X: 0, Y: 0}, {X: length, Y: 0}}
		v := h
		v.ID = "vertical"
		v.Route = []*geo.Point{{X: length / 2, Y: -length / 2}, {X: length / 2, Y: length / 2}}
		d := d2target.NewDiagram()
		d.Connections = []d2target.Connection{h, v}
		for _, sketch := range []bool{false, true} {
			out, err := d2svg.Render(d, &d2svg.RenderOpts{Sketch: &sketch})
			if err != nil {
				t.Fatal(err)
			}
			wantGap := !sketch || length < 10000
			if got := bytes.Contains(out, []byte(`id="crossings-`)); got != wantGap {
				t.Errorf("sketch %t, length %v: gap = %t, want %t", sketch, length, got, wantGap)
			}
		}
	}
}

func TestCompoundDashedSketchCrossing(t *testing.T) {
	solid := *d2target.BaseConnection()
	solid.ID, solid.Stroke = "solid", "black"
	solid.Route = []*geo.Point{{X: 0, Y: 10}, {X: 100, Y: 10}}
	dashed := solid
	dashed.ID, dashed.StrokeDash = "dashed", 14
	dashed.Route = []*geo.Point{{X: 50, Y: -8}, {X: 50, Y: 60}, {X: 75, Y: 60}}
	for _, tc := range []struct {
		name                                  string
		sketch, animated, dashBehind, wantGap bool
	}{
		{"ordinary foreground", false, false, false, true},
		{"sketch foreground", true, false, false, false},
		{"moving sketch foreground", true, true, false, true},
		{"sketch background", true, false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := d2target.NewDiagram()
			dashed.Animated = tc.animated
			d.Connections = []d2target.Connection{solid, dashed}
			if tc.dashBehind {
				d.Connections[0], d.Connections[1] = dashed, solid
			}
			out, err := d2svg.Render(d, &d2svg.RenderOpts{Sketch: &tc.sketch})
			if err != nil {
				t.Fatal(err)
			}
			if got := bytes.Contains(out, []byte(`id="crossings-`)); got != tc.wantGap {
				t.Errorf("gap = %t, want %t", got, tc.wantGap)
			}
		})
	}
}

func TestNeighboringSketchGapsFitHandDrawnStrokes(t *testing.T) {
	for _, length := range []float64{100, 1500} {
		h := *d2target.BaseConnection()
		h.ID, h.Stroke = "back", "black"
		h.Route = []*geo.Point{{X: 0, Y: 0}, {X: length, Y: 0}}
		d := d2target.NewDiagram()
		d.Connections = []d2target.Connection{h}
		for i, id := range []string{"first", "second"} {
			x := length/2 - 5 + 10*float64(i)
			v := h
			v.ID = id
			v.Route = []*geo.Point{{X: x, Y: -length / 2}, {X: x, Y: length / 2}}
			d.Connections = append(d.Connections, v)
		}
		for _, sketch := range []bool{false, true} {
			out, err := d2svg.Render(d, &d2svg.RenderOpts{Sketch: &sketch})
			if err != nil {
				t.Fatal(err)
			}
			doc, err := goquery.NewDocumentFromReader(bytes.NewReader(out))
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if sketch && length == 1500 {
				want = 0 // The openings shrink to radius 4, below the placement bound.
			}
			if got := doc.Find(`mask[id^="crossings-"] circle`).Length(); got != want {
				t.Errorf("sketch %t, length %v: got %d gaps, want %d", sketch, length, got, want)
			}
		}
	}
}
