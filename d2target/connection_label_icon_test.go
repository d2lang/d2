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
			path := c.IconMotionPath(2, 2)
			for _, shape := range []string{ShapeCircle, ShapeSquare, ConnectionIconTriangle, ShapeDiamond, ""} {
				c.IconShape = shape
				if shape == "" {
					c.Icon = &url.URL{Path: "art.svg"}
				}
				topLeft := c.GetLabelTopLeft()
				if shape == "" && (dot.Equals(topLeft) || static.Equals(topLeft)) {
					t.Fatalf("artwork, dot and ordinary caption should reserve different footprints: %v %v %v", topLeft, dot, static)
				}
				// Match both renderers' rounded text mask, including its side padding.
				left, top := math.Round(topLeft.X)-2, math.Round(topLeft.Y)
				right, bottom := left+float64(c.LabelWidth)+4, top+float64(c.LabelHeight)
				for distance := 0.; distance <= path.Length(); distance += .2 {
					p := path.PointAt(distance)
					dx := max(left-p.X, p.X-right, 0.)
					dy := max(top-p.Y, p.Y-bottom, 0.)
					gap := math.Hypot(dx, dy)
					if shape != ShapeCircle {
						gap = max(dx, dy)
					}
					if gap < float64(c.IconSize())/2+3 {
						t.Fatalf("icon %q overlaps the caption clearance at %v (distance %g, mask %.1f %.1f %.1f %.1f)", shape, p, gap, left, top, right, bottom)
					}
				}
				if shape == "" && tc.name == "study elbow" && (topLeft.Y < 180 || topLeft.X > 440) {
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
	c.Label, c.LabelWidth, c.LabelHeight = "caption", 80, 20
	c.LabelPosition = label.UnlockedTop.String()
	c.LabelPercentage = .5
	want := c.GetLabelTopLeft()
	for _, shape := range ConnectionIconShapes {
		c.IconShape = shape
		icon := c.GetIconPosition()
		size := float64(c.IconSize())
		wantIcon := geo.NewPoint(want.X-CONNECTION_ICON_LABEL_GAP-size, want.Y+(float64(c.LabelHeight)-size)/2)
		if !want.Equals(c.GetLabelTopLeft()) || icon == nil || !icon.Equals(wantIcon) {
			t.Fatalf("static %s at %v should sit beside the caption at %v", shape, icon, want)
		}
	}
	c.IconShape = ""
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

func TestOutsideCaptionClearsPaintedEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name                               string
		route                              geo.Route
		pos                                label.Position
		width, height, stroke, shapeStroke int
		fraction                           float64
		curve                              bool
	}{
		// The arrow's 30px inset extends beyond the last 19px leg.
		{"short TALA elbow", geo.Route{{X: 129, Y: 278}, {X: 202, Y: 278}, {X: 202, Y: 259}}, label.OutsideBottomCenter, 47, 21, 15, 15, 0, false},
		// Moving the endpoint also changes the interior of a cubic.
		{"cubic", geo.Route{{X: 0, Y: 0}, {X: 50.5, Y: 75.5}, {X: 119, Y: 1}, {X: 182.5, Y: 55.5}}, label.UnlockedTop, 58, 25, 13, 8, .2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDiagram()
			d.Shapes = []Shape{{ID: "src", StrokeWidth: tc.shapeStroke}, {ID: "dst", StrokeWidth: tc.shapeStroke}}
			c := BaseConnection()
			c.Src, c.Dst, c.DstArrow = "src", "dst", DefaultArrowhead
			c.Route, c.IsCurve, c.StrokeWidth = tc.route, tc.curve, tc.stroke
			c.Label, c.LabelWidth, c.LabelHeight = "Data in", tc.width, tc.height
			c.LabelPosition, c.LabelPercentage = tc.pos.String(), tc.fraction
			c.Icon, c.IconAnimation = &url.URL{Path: "icon.svg"}, &ConnectionIconAnimation{}
			d.Connections = []Connection{*c}
			resolved := d.WithIconLabelInsets()
			if d.Connections[0].IconLabelInsets != ([2]float64{}) {
				t.Fatal("resolving geometry mutated the input board")
			}
			topLeft := resolved.Connections[0].GetLabelTopLeft()
			x, y := math.Round(topLeft.X)-2, math.Round(topLeft.Y)
			path := c.IconMotionPath(float64(tc.shapeStroke), float64(tc.shapeStroke))
			for distance := 0.; distance <= path.Length(); distance += 0.25 {
				pt := path.PointAt(distance)
				dx := max(0., x-pt.X, pt.X-(x+float64(tc.width)+4))
				dy := max(0., y-pt.Y, pt.Y-(y+float64(tc.height)))
				if gap := max(dx, dy) - float64(c.IconSize())/2; gap < 3 {
					t.Fatalf("moving image at %v clears the caption by only %.2f", pt, gap)
				}
			}
		})
	}
}
