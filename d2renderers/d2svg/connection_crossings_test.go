package d2svg_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func renderCrossingSVG(t *testing.T, diagram *d2target.Diagram, opts *d2svg.RenderOpts) *goquery.Document {
	t.Helper()
	out, err := d2svg.Render(diagram, opts)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("url(#")) {
		assertUniqueResolvableLocalIRIs(t, out)
	}
	// A multiboard frame is a <g> fragment. Parse it in the SVG namespace.
	doc, err := goquery.NewDocumentFromReader(io.MultiReader(strings.NewReader("<svg>"), bytes.NewReader(out), strings.NewReader("</svg>")))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

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

	for _, tc := range []struct {
		name, masterID              string
		underAnimated, overAnimated bool
		clip                        bool
	}{
		{name: "static"},
		{name: "animated foreground", overAnimated: true},
		{name: "animated back connection", underAnimated: true, clip: true},
		{name: "animation frame", masterID: "d2-test-animation", clip: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			under.Animated, over.Animated = tc.underAnimated, tc.overAnimated
			diagram := d2target.NewDiagram()
			diagram.Connections = []d2target.Connection{over, under}
			boardID, err := diagram.HashID(nil)
			if err != nil {
				t.Fatal(err)
			}
			resourceID := boardID + "-cg1"
			for _, sketch := range []bool{false, true} {
				doc := renderCrossingSVG(t, diagram, &d2svg.RenderOpts{Sketch: &sketch, MasterID: tc.masterID})
				resource := doc.Find("#" + resourceID)
				attr := "mask"
				if tc.clip {
					attr = "clip-path"
					geometry := resource.ChildrenFiltered("path")
					if goquery.NodeName(resource) != "clipPath" || geometry.Length() != 1 ||
						geometry.AttrOr("clip-rule", "nonzero") != "nonzero" || !strings.Contains(strings.ToLower(geometry.AttrOr("d", "")), "a") {
						t.Fatalf("sketch %v: expected a clipping path with a circular hole", sketch)
					}
				} else if goquery.NodeName(resource) != "mask" || resource.ChildrenFiltered("circle").Length() != 1 {
					t.Fatalf("sketch %v: expected a mask with one circular hole", sketch)
				}
				gap := `[` + attr + `="url(#` + resourceID + `)"]`
				back := doc.Find(".test-under")
				if back.Find(gap).Length() != 1 || doc.Find(".test-over").Find(gap).Length() != 0 {
					t.Fatalf("sketch %v: expected only the back connection to reference the crossing", sketch)
				}
				if back.Find(`[mask="url(#`+boardID+`)"]`).Length() != 0 {
					t.Fatalf("sketch %v: connection references an empty caption mask in addition to its crossing", sketch)
				}
				if !sketch {
					shaft := back.Find(`path[marker-end][style*="stroke-dasharray:"]`)
					if shaft.Length() != 1 || !shaft.Is(gap) {
						t.Fatal("expected a single dashed stroke with its arrowhead to reference the crossing directly")
					}
				}
			}
		})
	}
}

func TestAnimatedBidirectionalGapPreservesDashPathsAndCaption(t *testing.T) {
	back := *d2target.BaseConnection()
	back.ID, back.Stroke = "back", "black"
	back.Classes = []string{"test-back"}
	back.SrcArrow, back.DstArrow = d2target.TriangleArrowhead, d2target.TriangleArrowhead
	back.Animated = true
	back.Route = []*geo.Point{{X: 0, Y: 50}, {X: 200, Y: 50}}
	back.Label, back.LabelPosition = "traffic", "OUTSIDE_TOP_CENTER"
	back.FontSize, back.LabelWidth, back.LabelHeight = 16, 48, 20
	front := *d2target.BaseConnection()
	front.ID, front.Stroke = "front", "blue"
	front.Classes = []string{"test-front"}
	front.Route = []*geo.Point{{X: 60, Y: 0}, {X: 60, Y: 100}}

	diagram := d2target.NewDiagram()
	diagram.Connections = []d2target.Connection{back}
	const strokes = ".test-back path.animated-connection"
	original := renderCrossingSVG(t, diagram, &d2svg.RenderOpts{}).Find(strokes)
	diagram.Connections = append(diagram.Connections, front)
	doc := renderCrossingSVG(t, diagram, &d2svg.RenderOpts{})
	clipped := doc.Find(strokes)
	if original.Length() != 2 || clipped.Length() != 2 {
		t.Fatalf("expected two animated halves before and after the crossing, got %d and %d", original.Length(), clipped.Length())
	}
	boardID, err := diagram.HashID(nil)
	if err != nil {
		t.Fatal(err)
	}
	clip := `[clip-path="url(#` + boardID + `-cg0)"]`
	for i := 0; i < clipped.Length(); i++ {
		path := clipped.Eq(i)
		if path.Closest(clip).Length() != 1 || path.AttrOr("mask", "") != "url(#"+boardID+")" {
			t.Fatalf("animated half %d lost its crossing clip or caption mask", i)
		}
		for _, attr := range []string{"d", "style"} {
			if got, want := path.AttrOr(attr, ""), original.Eq(i).AttrOr(attr, ""); got == "" || got != want {
				t.Errorf("animated half %d changed %s: got %q, want %q", i, attr, got, want)
			}
		}
	}
	if clipped.Filter(`[marker-start][style*="reverse"]`).Length() != 1 || clipped.Filter("[marker-end]").Length() != 1 {
		t.Fatal("bidirectional connection lost an arrowhead or its reversed animation")
	}
	if doc.Find("#"+boardID+` rect[fill="rgba(0,0,0,0.75)"]`).Length() != 1 {
		t.Fatal("caption mask lost its partially opaque rectangle")
	}
	caption := doc.Find(".test-back text")
	if caption.Text() != back.Label || caption.Closest(clip).Length() != 0 || doc.Find(".test-front").Find(clip).Length() != 0 {
		t.Fatal("crossing clip should affect only the back connection's strokes")
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
				doc := renderCrossingSVG(t, d, &d2svg.RenderOpts{Sketch: &sketch})
				if got := doc.Find(`[id$="-cg0"]`).Length() != 0; got != tc.wantGap {
					t.Fatalf("sketch %t: gap=%t, want %t", sketch, got, tc.wantGap)
				}
			}
		})
	}
}

func TestSketchGapFitsHandDrawnStrokes(t *testing.T) {
	for _, length := range []float64{300, 800, 1500, 10000} {
		// With no shapes, padding supplies the viewport for these synthetic routes.
		padding := int64(length)
		h := *d2target.BaseConnection()
		h.ID, h.Stroke = "horizontal", "black"
		h.Route = []*geo.Point{{X: 0, Y: 0}, {X: length, Y: 0}}
		v := h
		v.ID = "vertical"
		v.Route = []*geo.Point{{X: length / 2, Y: -length / 2}, {X: length / 2, Y: length / 2}}
		d := d2target.NewDiagram()
		d.Connections = []d2target.Connection{h, v}
		for _, sketch := range []bool{false, true} {
			doc := renderCrossingSVG(t, d, &d2svg.RenderOpts{Sketch: &sketch, Pad: &padding})
			wantGap := !sketch || length < 10000
			if got := doc.Find(`[id$="-cg0"]`).Length() != 0; got != wantGap {
				t.Errorf("sketch %t, length %v: gap = %t, want %t", sketch, length, got, wantGap)
			}
		}
	}
}

func TestSketchGapOnLongLegBeforeBend(t *testing.T) {
	for _, length := range []float64{1500, 10000} {
		// With no shapes, padding supplies the viewport for these synthetic routes.
		padding := int64(length)
		bent := *d2target.BaseConnection()
		bent.ID, bent.Stroke = "bent", "black"
		bent.Route = []*geo.Point{{X: 0, Y: 0}, {X: length, Y: 0}, {X: length, Y: 30}}
		straight := bent
		straight.ID = "straight"
		straight.Route = []*geo.Point{{X: length / 2, Y: -100}, {X: length / 2, Y: 100}}
		d := d2target.NewDiagram()
		for _, edges := range [][]d2target.Connection{{straight, bent}, {bent, straight}} {
			d.Connections = edges
			for _, sketch := range []bool{false, true} {
				doc := renderCrossingSVG(t, d, &d2svg.RenderOpts{Sketch: &sketch, Pad: &padding})
				want := !sketch || length == 1500
				if got := doc.Find(`[id$="-cg0"]`).Length() != 0; got != want {
					t.Errorf("back %s, sketch %t, length %v: gap = %t, want %t", edges[0].ID, sketch, length, got, want)
				}
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
			doc := renderCrossingSVG(t, d, &d2svg.RenderOpts{Sketch: &tc.sketch})
			if got := doc.Find(`[id$="-cg0"]`).Length() != 0; got != tc.wantGap {
				t.Errorf("gap = %t, want %t", got, tc.wantGap)
			}
		})
	}
}

func TestNeighboringSketchGapsFitHandDrawnStrokes(t *testing.T) {
	for _, length := range []float64{100, 1500} {
		// With no shapes, padding supplies the viewport for these synthetic routes.
		padding := int64(length)
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
			doc := renderCrossingSVG(t, d, &d2svg.RenderOpts{Sketch: &sketch, Pad: &padding})
			wantGaps := 2
			if sketch && length == 1500 {
				wantGaps = 0 // The openings shrink to radius 4, below the placement bound.
			}
			if got := doc.Find(`mask[id$="-cg0"] > circle`).Length(); got != wantGaps {
				t.Errorf("sketch %t, length %v: got %d gaps, want %d", sketch, length, got, wantGaps)
			}
		}
	}
}
