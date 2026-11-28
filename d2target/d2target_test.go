package d2target

import (
	"bytes"
	"net/url"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestResolveBoardKeepsTargetPrecedence(t *testing.T) {
	step := &Diagram{Name: "ship.us"}
	layer := &Diagram{Name: "orders", Steps: []*Diagram{step}}
	scenario := &Diagram{Name: "orders"}
	root := &Diagram{Name: "root", Layers: []*Diagram{layer}, Scenarios: []*Diagram{scenario}}

	for _, tc := range []struct {
		path, qualified []string
		want            *Diagram
	}{
		{nil, nil, root},
		{[]string{"root"}, nil, root},
		{[]string{"orders"}, []string{"layers", "orders"}, layer},
		{[]string{"scenarios", "orders"}, []string{"scenarios", "orders"}, scenario},
		{[]string{"orders", "ship.us"}, []string{"layers", "orders", "steps", "ship.us"}, step},
		{[]string{"layers", "orders", "ship.us", "ship.us"}, []string{"layers", "orders", "steps", "ship.us"}, step},
		{[]string{"steps", "orders"}, nil, nil},
		{[]string{"layers"}, nil, nil},
	} {
		got, path := root.ResolveBoard(tc.path)
		if got != tc.want || !slices.Equal(path, tc.qualified) {
			t.Errorf("ResolveBoard(%q) = (%p, %q), want (%p, %q)", tc.path, got, path, tc.want, tc.qualified)
		}
	}
}

func TestCalculateTooltipBoundsUsesDiagramFonts(t *testing.T) {
	fontFamily := d2fonts.HandDrawn
	monoFontFamily := d2fonts.SourceCodePro
	shape := Shape{
		Pos:             Point{X: 100, Y: 200},
		Width:           120,
		Height:          60,
		Tooltip:         "**wide handwritten tooltip**",
		TooltipPosition: "top-left",
		Text:            Text{FontFamily: "DEFAULT"},
	}

	minX, minY, maxX, maxY := calculateTooltipBounds(shape, &fontFamily, &monoFontFamily)
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	width, height, err := textmeasure.MeasureMarkdown(shape.Tooltip, ruler, &fontFamily, &monoFontFamily, d2fonts.FONT_SIZE_M)
	if err != nil {
		t.Fatal(err)
	}
	if minX != shape.Pos.X || maxX-minX != width+20 {
		t.Fatalf("tooltip horizontal bounds = (%d,%d), want x=%d width=%d", minX, maxX, shape.Pos.X, width+20)
	}
	wantY := shape.Pos.Y - (height + 20) - 10
	if minY != wantY || maxY-minY != height+20 {
		t.Fatalf("tooltip vertical bounds = (%d,%d), want y=%d height=%d", minY, maxY, wantY, height+20)
	}
}

func TestOutsideIconBoundsFollowShapeType(t *testing.T) {
	const x, y, w, h = 100, 75, 96, 96
	for _, tc := range []struct {
		shape string
		left  int
	}{
		{ShapeImage, x},
		{ShapeRectangle, x - 5 - 48}, // 5 px gap and a 48 px icon
	} {
		t.Run(tc.shape, func(t *testing.T) {
			d := Diagram{Shapes: []Shape{{
				Type: tc.shape, Pos: Point{x, y}, Width: w, Height: h,
				Icon: &url.URL{Path: "photo.svg"}, IconPosition: "OUTSIDE_LEFT_MIDDLE",
			}}}
			tl, br := d.BoundingBox()
			if tl != (Point{tc.left, y}) || br != (Point{x + w, y + h}) {
				t.Fatalf("bounds = %v ... %v", tl, br)
			}
		})
	}
}

func TestConnectionIconBounds(t *testing.T) {
	c := BaseConnection()
	c.Route = []*geo.Point{{X: 100, Y: 100}, {X: 200, Y: 100}}
	c.IconPosition = label.InsideMiddleCenter.String()
	c.IconShape = ShapeCircle
	d := Diagram{Shapes: []Shape{{Pos: Point{150, 100}, Width: 1, Height: 1}}}
	check := func(tl, br Point) {
		t.Helper()
		d.Connections = []Connection{*c}
		gotTL, gotBR := d.BoundingBox()
		if gotTL != tl || gotBR != br {
			t.Fatalf("icon bounds = %v ... %v, want %v ... %v", gotTL, gotBR, tl, br)
		}
	}
	check(Point{99, 95}, Point{201, 105})
	c.IconAnimation = &ConnectionIconAnimation{}
	check(Point{95, 95}, Point{205, 105})
	c.IconShape = ""
	c.Icon = &url.URL{Path: "image.png"}
	check(Point{84, 84}, Point{216, 116})
	c.IconAnimation = nil
	check(Point{99, 84}, Point{201, 116})
}

func TestHashIDStableAcrossURLFieldOrder(t *testing.T) {
	icon := &url.URL{
		Scheme:      "https",
		Opaque:      "opaque",
		User:        url.UserPassword("user", "password"),
		Host:        "example.com",
		Path:        "/icon.svg",
		RawPath:     "/icon%2Esvg",
		OmitHost:    true,
		ForceQuery:  true,
		RawQuery:    "x=1",
		Fragment:    "fragment",
		RawFragment: "frag%6Dent",
	}
	d := Diagram{
		Shapes: []Shape{{
			ID:   "shape",
			Icon: icon,
			Text: Text{Label: `quoted "icon":{"Scheme":"fake"}`},
		}},
		Connections: []Connection{{ID: "connection", Icon: icon}},
		Root:        Shape{ID: "root", Icon: icon},
		Layers: []*Diagram{{
			Shapes: []Shape{{ID: "layer-shape", Icon: icon}},
		}},
		Scenarios: []*Diagram{{
			Connections: []Connection{{ID: "scenario-connection", Icon: icon}},
		}},
		Steps: []*Diagram{{
			Root: Shape{ID: "step-root", Icon: icon},
		}},
	}

	b, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	legacyURLTail := []byte(`"Path":"/icon.svg","RawPath":"/icon%2Esvg","OmitHost":true,"ForceQuery":true,"RawQuery":"x=1","Fragment":"fragment","RawFragment":"frag%6Dent"`)
	if got, want := bytes.Count(b, legacyURLTail), 6; got != want {
		t.Fatalf("legacy URL field sequence count = %d, want %d", got, want)
	}

	got, err := d.HashID(nil)
	if err != nil {
		t.Fatal(err)
	}
	// This is the hash produced by Go 1.25.12 before net/url.URL fields were
	// reorganized in Go 1.26.
	const want = "d2-887368360"
	if got != want {
		t.Fatalf("HashID = %q, want %q", got, want)
	}
}

func TestBorderDecorationBounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		s      Shape
		tl, br Point
	}{
		{
			"border icon", Shape{Type: ShapeRectangle, Width: 120, Height: 120,
				Icon: &url.URL{Path: "tile.svg"}, IconPosition: "BORDER_TOP_CENTER"},
			Point{0, -30}, Point{120, 120},
		},
		{
			"3D border label", Shape{Type: ShapeRectangle, Width: 120, Height: 100, ThreeDee: true,
				Text: Text{Label: "right caption", LabelWidth: 121, LabelHeight: 19}, LabelPosition: "BORDER_RIGHT_MIDDLE"},
			Point{0, -15}, Point{196, 100},
		},
		{
			"multiple border label", Shape{Type: ShapeRectangle, Width: 120, Height: 100, Multiple: true,
				Text: Text{Label: "top caption", LabelWidth: 70, LabelHeight: 19}, LabelPosition: "BORDER_TOP_CENTER"},
			Point{0, -20}, Point{130, 100},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := Diagram{Shapes: []Shape{tc.s}}
			tl, br := d.BoundingBox()
			if tl != tc.tl || br != tc.br {
				t.Fatalf("bounds %v..%v, want %v..%v", tl, br, tc.tl, tc.br)
			}
		})
	}
}
