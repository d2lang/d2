package d2svg_test

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestConnectionGradientCoordinates(t *testing.T) {
	for _, tc := range []struct {
		name, paint, kind string
		animated          bool
		wantShafts        int
	}{
		{"linear", "linear-gradient(to right, red, blue)", "linearGradient", false, 1},
		{"split radial", "radial-gradient(red, blue)", "radialGradient", true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := *d2target.BaseConnection()
			c.ID, c.Stroke, c.StrokeWidth = "request", tc.paint, 4
			c.SrcArrow, c.DstArrow = d2target.TriangleArrowhead, d2target.CircleArrowhead
			c.Animated, c.IsCurve = tc.animated, true
			// The source points left and the curve arrives on a diagonal.
			c.Route = []*geo.Point{{X: 220, Y: 100}, {X: 150, Y: 100}, {X: 110, Y: 60}, {X: 80, Y: 20}}
			d := d2target.NewDiagram()
			d.Connections = []d2target.Connection{c}
			out, err := d2svg.Render(d, nil)
			if err != nil {
				t.Fatal(err)
			}
			assertUniqueResolvableLocalIRIs(t, out)
			doc, err := goquery.NewDocumentFromReader(bytes.NewReader(out))
			if err != nil {
				t.Fatal(err)
			}

			ref := func(s *goquery.Selection, attr string) *goquery.Selection {
				t.Helper()
				iri := s.AttrOr(attr, "")
				id := localIRIRegexp.FindStringSubmatch(iri)
				if len(id) < 2 {
					t.Fatalf("missing SVG reference for %s on %s: %q", attr, goquery.NodeName(s), iri)
				}
				return doc.Find("#" + id[1])
			}
			var stopSource string
			checkPaint := func(s *goquery.Selection, attr string, elementToDiagram d2scene.Matrix) {
				t.Helper()
				g := ref(s, attr)
				if goquery.NodeName(g) != tc.kind || g.AttrOr("gradientUnits", "") != "userSpaceOnUse" {
					t.Fatalf("%s on %s has no user-space %s", attr, goquery.NodeName(s), tc.kind)
				}
				source := g.AttrOr("href", "")
				if stopSource == "" {
					if !strings.HasPrefix(source, "#") || doc.Find(source).Find("stop").Length() != 2 {
						t.Fatalf("gradient stop source %q does not contain the authored stops", source)
					}
					stopSource = source
				} else if source != stopSource {
					t.Errorf("gradient stop source = %q, want %q", source, stopSource)
				}
				var m d2scene.Matrix
				if _, err := fmt.Sscanf(g.AttrOr("gradientTransform", ""), "matrix(%f %f %f %f %f %f)", &m.A, &m.B, &m.C, &m.D, &m.E, &m.F); err != nil {
					t.Fatal(err)
				}
				// The same normalized paint coordinates must land on the same
				// diagram points through the SVG marker's own coordinate system.
				m = elementToDiagram.Mul(m)
				for _, p := range []d2scene.Point{{}, {X: 1}, {Y: 1}} {
					got := m.Point(p)
					want := d2scene.Point{X: 80 + 140*p.X, Y: 20 + 80*p.Y}
					if math.Hypot(got.X-want.X, got.Y-want.Y) > 0.001 {
						t.Errorf("%s on %s maps %+v to %+v, want %+v", attr, goquery.NodeName(s), p, got, want)
					}
				}
			}

			shafts := doc.Find("path[marker-start], path[marker-end]")
			if shafts.Length() != tc.wantShafts {
				t.Fatalf("got %d shaft paths, want %d", shafts.Length(), tc.wantShafts)
			}
			shafts.Each(func(_ int, shaft *goquery.Selection) {
				checkPaint(shaft, "stroke", d2scene.Identity())
			})
			for _, end := range []struct {
				ref, shape, paint string
				angle             float64
			}{
				{"marker-start", "polygon", "fill", math.Pi},
				{"marker-end", "circle", "stroke", math.Atan2(-40, -30)},
			} {
				shaft := shafts.Filter("[" + end.ref + "]")
				marker := ref(shaft, end.ref)
				shape := marker.Find(end.shape)
				if end.shape == "circle" && strings.HasPrefix(shape.AttrOr("fill", ""), "url(") {
					t.Fatal("the hollow circle interior uses the connection gradient")
				}
				points := strings.Fields(shaft.AttrOr("d", ""))
				pos := 1 // M x y
				if end.ref == "marker-end" {
					pos = len(points) - 2
				}
				x := svgGradientNumber(t, points[pos])
				y := svgGradientNumber(t, points[pos+1])
				refX := svgGradientNumber(t, marker.AttrOr("refX", ""))
				refY := svgGradientNumber(t, marker.AttrOr("refY", ""))
				toDiagram := d2scene.Translate(x, y).Mul(d2scene.Rotate(end.angle)).Mul(d2scene.Translate(-refX, -refY))
				checkPaint(shape, end.paint, toDiagram)
			}
		})
	}
}

func svgGradientNumber(t *testing.T, s string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestConnectionsCanShareGradientDefinitions(t *testing.T) {
	forward := *d2target.BaseConnection()
	forward.ID = "forward"
	forward.Stroke = "linear-gradient(to right, red, blue)"
	forward.DstArrow = d2target.TriangleArrowhead
	forward.Route = []*geo.Point{{X: 0, Y: 20}, {X: 100, Y: 20}}
	reverse := forward
	reverse.ID = "reverse"
	reverse.Route = []*geo.Point{forward.Route[1], forward.Route[0]}

	d := d2target.NewDiagram()
	d.Connections = []d2target.Connection{forward, reverse}
	out, err := d2svg.Render(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertUniqueResolvableLocalIRIs(t, out)
}

func TestFractionalStrokesUseDistinctArrowheads(t *testing.T) {
	d := d2target.NewDiagram()
	for i, offset := range []float64{0.25, 0.5} {
		c := *d2target.BaseConnection()
		c.ID = strconv.Itoa(i)
		c.StrokeWidth, c.StrokeWidthOffset = 2, offset
		c.DstArrow = d2target.TriangleArrowhead
		y := float64(i * 25)
		c.Route = []*geo.Point{{Y: y}, {X: 100, Y: y}}
		d.Connections = append(d.Connections, c)
	}
	out, err := d2svg.Render(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range d.Connections {
		width := c.RenderedStrokeWidth()
		shaft := doc.Find(fmt.Sprintf(`path[style="stroke-width:%v;"][marker-end]`, width))
		ref := localIRIRegexp.FindStringSubmatch(shaft.AttrOr("marker-end", ""))
		if len(ref) != 2 {
			t.Fatalf("missing marker for %v px stroke", width)
		}
		marker := doc.Find("#" + ref[1])
		wantWidth, wantHeight := c.DstArrow.Dimensions(width)
		if gotWidth, gotHeight := marker.AttrOr("markerWidth", ""), marker.AttrOr("markerHeight", ""); gotWidth != fmt.Sprint(wantWidth) || gotHeight != fmt.Sprint(wantHeight) {
			t.Errorf("%v px stroke marker is %s x %s, want %v x %v", width, gotWidth, gotHeight, wantWidth, wantHeight)
		}
	}
}
