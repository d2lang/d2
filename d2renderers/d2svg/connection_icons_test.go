package d2svg_test

import (
	"encoding/xml"
	"math"
	"net/url"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestConnectionIconStreamSVG(t *testing.T) {
	asset, _ := url.Parse("https://example.com/mail.svg")
	c := *d2target.BaseConnection()
	c.ID, c.Stroke, c.Icon, c.IconAnimation = "messages", "#bd2020", asset, &d2target.ConnectionIconAnimation{}
	c.Route = []*geo.Point{{X: 0, Y: 0}, {X: 180, Y: 0}}
	d := d2target.NewDiagram()
	render := func() string {
		t.Helper()
		d.Connections = []d2target.Connection{c}
		out, err := d2svg.Render(d, nil)
		if err != nil {
			t.Fatal(err)
		}
		var root struct{ XMLName xml.Name }
		if err := xml.Unmarshal(out, &root); err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	rate := 3.
	c.IconAnimation.Rate = &rate
	out := render()
	if strings.Count(out, "<animateMotion ") != 3 || strings.Count(out, asset.String()) != 1 || !strings.Contains(out, `class="d2-still-icon"`) || !strings.Contains(out, "prefers-reduced-motion:reduce") {
		t.Fatal("stream needs three staggered copies of a shared image and a reduced-motion fallback")
	}
	c.IconPositions = []geo.Point{{X: 73, Y: 0}}
	out = render()
	if strings.Contains(out, "<animateMotion") || !strings.Contains(out, `x="73" y="0"`) {
		t.Fatal("scheduled particle needs its sampled position without an independent animation")
	}
	c.IconPositions = []geo.Point{}
	if strings.Contains(render(), asset.String()) {
		t.Fatal("empty sample painted an icon")
	}
	c.IconPositions = nil
	rate = 0
	if strings.Contains(render(), "<animateMotion") {
		t.Fatal("zero rate emitted a particle")
	}
	c.IconAnimation = nil
	c.Icon = nil
	c.IconShape = d2target.ShapeCircle
	c.IconPosition = "UNLOCKED_TOP"
	out = render()
	if !strings.Contains(out, `r="5"`) || strings.Contains(out, "d2-moving-icons") {
		t.Fatal("a static circle should be painted directly")
	}
}

func TestScheduledIconPathUsesWireCoordinates(t *testing.T) {
	c := *d2target.BaseConnection()
	c.ID, c.Stroke, c.IconShape = "stream", "#f00", d2target.ShapeCircle
	c.IconAnimation = &d2target.ConnectionIconAnimation{}
	c.IconSchedule = &d2target.ConnectionIconSchedule{DurationMS: 1000, FilmMS: 1000, Period: 1, Transit: 1}
	c.Route = []*geo.Point{{X: -64, Y: 30}, {X: 116, Y: 30}}
	d := d2target.NewDiagram()
	d.Connections = []d2target.Connection{c}
	doc := renderCrossingSVG(t, d, nil)
	style := doc.Find(".d2-icon-tour-clock").AttrOr("style", "")
	// Both endpoints are inset by half the two-unit stroke.
	if !strings.Contains(style, "--d2-icon-shape:shape(from -63px 30px,line to 115px 30px) view-box") ||
		!strings.Contains(style, "--d2-icon-route:path('M-63 30L115 30')") {
		t.Fatalf("scheduled motion must use the wire coordinates directly: %s", style)
	}
}

func TestConnectionIconPolygonSVG(t *testing.T) {
	for _, tc := range []struct{ shape, points string }{
		{d2target.ShapeSquare, "-5,-5 5,-5 5,5 -5,5"},
		{d2target.ConnectionIconTriangle, "0,-5 5,5 -5,5"},
		{d2target.ShapeDiamond, "0,-5 5,0 0,5 -5,0"},
	} {
		t.Run(tc.shape, func(t *testing.T) {
			c := d2target.BaseConnection()
			c.ID, c.Stroke, c.IconShape = "stream", "linear-gradient(to right, red, blue)", tc.shape
			c.Route = []*geo.Point{{X: 20, Y: 10}, {X: 80, Y: 10}}
			c.IconAnimation = &d2target.ConnectionIconAnimation{DurationMS: 200}
			d := d2target.NewDiagram()
			d.Connections = []d2target.Connection{*c}
			doc := renderCrossingSVG(t, d, nil)
			polygon := doc.Find("defs > g > polygon")
			if polygon.Length() != 1 || polygon.AttrOr("points", "") != tc.points || !strings.HasPrefix(polygon.AttrOr("fill", ""), "url(#") {
				t.Fatalf("expected one %s filled with the connection gradient", tc.shape)
			}
			href := "#" + polygon.Parent().AttrOr("id", "")
			for _, selector := range []string{".d2-moving-icons use", ".d2-still-icon use"} {
				if doc.Find(selector).AttrOr("href", "") != href {
					t.Fatalf("%s does not use the %s", selector, tc.shape)
				}
			}
		})
	}
}

func TestPinnedIconScheduleOnReducedMotion(t *testing.T) {
	c := *d2target.BaseConnection()
	c.ID, c.Stroke = "stream", "#f00"
	c.Route = []*geo.Point{{X: 0, Y: 0}, {X: 180, Y: 0}}
	c.IconAnimation = &d2target.ConnectionIconAnimation{}
	c.IconSchedule = &d2target.ConnectionIconSchedule{FixedLayer: true, DurationMS: 500, FilmMS: 500, Transit: 1}
	for _, tc := range []struct {
		name          string
		period, first float64
		carried       []d2target.ConnectionIconTrip
		wantFallback  int
	}{
		{"stopped before opening", math.Inf(1), math.Inf(1), nil, 0},
		{"draining a previous launch", math.Inf(1), math.Inf(1), []d2target.ConnectionIconTrip{{Transit: 1, Progress: .5}}, 1},
		{"first launch after the board hold", 2, 1, nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.IconSchedule.Period, c.IconSchedule.First, c.IconSchedule.Carried = tc.period, tc.first, tc.carried
			d := d2target.NewDiagram()
			d.Connections = []d2target.Connection{c}
			doc := renderCrossingSVG(t, d, nil)
			if got := doc.Find(".d2-still-icon").Length(); got != tc.wantFallback {
				t.Fatalf("got %d reduced-motion icons, want %d", got, tc.wantFallback)
			}
		})
	}
}

func TestSVGIconTripDuration(t *testing.T) {
	c := *d2target.BaseConnection()
	c.ID, c.Stroke = "stream", "#f00"
	c.Route = []*geo.Point{{X: 0, Y: 0}, {X: 600, Y: 0}}
	d := d2target.NewDiagram()
	c.IconAnimation = &d2target.ConnectionIconAnimation{DurationMS: 200}
	d.Connections = []d2target.Connection{c}
	data, err := d2svg.Render(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "<animateMotion ") != 1 || !strings.Contains(string(data), `<animateMotion dur="0.2s" repeatCount="indefinite">`) {
		t.Fatal("a fixed 200ms trip must repeat one icon as soon as it arrives")
	}
}

func TestConnectionIconStreamUsesNeighborLabelMask(t *testing.T) {
	c := *d2target.BaseConnection()
	c.ID, c.Stroke, c.IconShape = "stream", "#f00", d2target.ShapeCircle
	c.IconAnimation = &d2target.ConnectionIconAnimation{}
	c.Route = []*geo.Point{{X: 0, Y: 0}, {X: 180, Y: 0}}
	label := c
	label.ID, label.IconShape, label.IconAnimation = "caption", "", nil
	label.Text = d2target.Text{Label: "95%", FontSize: 14, LabelWidth: 30, LabelHeight: 18}
	label.LabelPosition = "INSIDE_MIDDLE_CENTER"
	d := d2target.NewDiagram()
	d.Connections = []d2target.Connection{label, c}
	doc := renderCrossingSVG(t, d, nil)
	mask := doc.Find("path.connection").First().AttrOr("mask", "")
	if mask == "" || doc.Find("mask rect").Length() < 2 {
		t.Fatal("missing on-edge caption cutout")
	}
	for _, class := range []string{".d2-moving-icons", ".d2-still-icon"} {
		if got := doc.Find(class).Parent().AttrOr("mask", ""); got != mask {
			t.Fatalf("%s mask = %q, want stationary board mask %q", class, got, mask)
		}
	}
}
