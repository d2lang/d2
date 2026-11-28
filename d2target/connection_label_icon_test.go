package d2target

import (
	"math"
	"net/url"
	"testing"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func TestOutsideConnectionLabelClearsTravelingIcon(t *testing.T) {
	for _, tc := range []struct {
		name  string
		route geo.Route
		pos   label.Position
		curve bool
	}{
		{"horizontal", geo.Route{{X: 0, Y: 0}, {X: 180, Y: 0}}, label.OutsideTopCenter, false},
		{"vertical", geo.Route{{X: 0, Y: 0}, {X: 0, Y: 180}}, label.OutsideBottomCenter, false},
		{"diagonal", geo.Route{{X: 0, Y: 0}, {X: 180, Y: 135}}, label.UnlockedTop, false},
		{"reverse diagonal", geo.Route{{X: 180, Y: 135}, {X: 0, Y: 0}}, label.UnlockedBottom, false},
		{"study elbow", geo.Route{{X: 403, Y: 188}, {X: 403, Y: 233}, {X: 548, Y: 233}}, label.UnlockedTop, false},
		{"cubic", geo.Route{{X: 0, Y: 100}, {X: 30, Y: 60}, {X: 180, Y: 110}, {X: 210, Y: 30}}, label.UnlockedBottom, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := BaseConnection()
			c.Route, c.IsCurve = tc.route, tc.curve
			c.LabelPosition, c.LabelPercentage = tc.pos.String(), .5
			c.LabelWidth, c.LabelHeight = 76, 22
			static := c.GetLabelTopLeft()
			c.IconAnimation = &ConnectionIconAnimation{}
			dot := c.GetLabelTopLeft()
			for _, image := range []bool{false, true} {
				if image {
					c.Icon = &url.URL{Path: "art.svg"}
				}
				topLeft := c.GetLabelTopLeft()
				if image && (dot.Equals(topLeft) || static.Equals(topLeft)) {
					t.Fatalf("artwork, dot and ordinary caption should reserve different footprints: %v %v %v", topLeft, dot, static)
				}
				// Match both renderers' rounded text mask, including its side padding.
				left, top := math.Round(topLeft.X)-2, math.Round(topLeft.Y)
				right, bottom := left+float64(c.LabelWidth)+4, top+float64(c.LabelHeight)
				path := c.IconMotionPath(2, 2)
				for distance := 0.; distance <= path.Length(); distance += .2 {
					p := path.PointAt(distance)
					dx := max(left-p.X, p.X-right, 0.)
					dy := max(top-p.Y, p.Y-bottom, 0.)
					gap := math.Hypot(dx, dy)
					if image {
						gap = max(dx, dy)
					}
					if gap < float64(c.IconSize())/2+3 {
						t.Fatalf("icon overlaps the caption clearance at %v (distance %g, mask %.1f %.1f %.1f %.1f)", p, gap, left, top, right, bottom)
					}
				}
				if image && tc.name == "study elbow" && (topLeft.Y < 180 || topLeft.X > 440) {
					t.Fatalf("the caption should slide just beyond the elbow, got %v", topLeft)
				}
			}
			c.IconAnimation = &ConnectionIconAnimation{Rate: new(0.)}
			if stopped := c.GetLabelTopLeft(); stopped.Equals(dot) {
				t.Fatal("stopping departures discarded the artwork's clearance")
			}
		})
	}
}

func TestConnectionLabelRetainsInlineStaticAndAnchoredPositions(t *testing.T) {
	c := BaseConnection()
	c.Route = geo.Route{{X: 0, Y: 0}, {X: 200, Y: 0}}
	c.LabelWidth, c.LabelHeight = 80, 20
	c.LabelPosition = label.UnlockedTop.String()
	c.LabelPercentage = .5
	want := c.GetLabelTopLeft()
	c.Icon = &url.URL{Path: "art.svg"}
	if got := c.GetLabelTopLeft(); !got.Equals(want) {
		t.Fatalf("static artwork moved the caption, got %v, want %v", got, want)
	}
	c.LabelPosition = label.InsideMiddleCenter.String()
	want = c.GetLabelTopLeft()
	c.IconAnimation = &ConnectionIconAnimation{}
	if got := c.GetLabelTopLeft(); !got.Equals(want) {
		t.Fatalf("moving artwork moved an inline caption, got %v, want %v", got, want)
	}
	c.LabelPosition = label.UnlockedTop.String()
	c.MotionLabel = &MotionLabel{Point: Point{X: 3, Y: 7}}
	if got := c.GetLabelTopLeft(); *got != (geo.Point{X: 3, Y: 7}) {
		t.Fatalf("moved an already positioned sampled caption: %v", got)
	}
}
