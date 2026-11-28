package d2svg_test

import (
	"encoding/xml"
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

func TestSVGIconTripDuration(t *testing.T) {
	c := *d2target.BaseConnection()
	c.ID, c.Stroke = "stream", "#f00"
	c.Route = []*geo.Point{{X: 0, Y: 0}, {X: 600, Y: 0}}
	d := d2target.NewDiagram()
	for _, tc := range []struct {
		animation       d2target.ConnectionIconAnimation
		count           int
		cycle, keyTimes string
	}{
		{d2target.ConnectionIconAnimation{DurationMS: 200}, 1, "0.2s", ""},
		{d2target.ConnectionIconAnimation{Rate: new(2.), DurationMS: 200}, 1, "0.5s", "0;0.4;1"},
		{d2target.ConnectionIconAnimation{Rate: new(2.), DurationMS: 1500}, 3, "1.5s", ""},
	} {
		c.IconAnimation = &tc.animation
		d.Connections = []d2target.Connection{c}
		data, err := d2svg.Render(d, nil)
		if err != nil {
			t.Fatal(err)
		}
		output := string(data)
		if n := strings.Count(output, `<animateMotion dur="`+tc.cycle+`"`); n != tc.count {
			t.Errorf("got %d icons looping every %s, want %d", n, tc.cycle, tc.count)
		}
		if tc.keyTimes != "" && !strings.Contains(output, `keyTimes="`+tc.keyTimes+`"`) {
			t.Errorf("expected travel and idle phases %s", tc.keyTimes)
		}
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
