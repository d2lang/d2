package d2svg_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestHairlineBorderAndSharedRail(t *testing.T) {
	card := *d2target.BaseShape()
	card.ID, card.Type, card.Stroke, card.Fill = "card", d2target.ShapeRectangle, "black", "white"
	card.Classes = []string{"test-card"}
	card.Pos, card.Width, card.Height = d2target.NewPoint(10, 20), 40, 20
	card.StrokeWidth, card.BorderRadius = 1, 4

	straight := *d2target.BaseConnection()
	straight.ID, straight.Classes, straight.Stroke = "straight", []string{"test-straight"}, "black"
	straight.StrokeWidth, straight.BorderRadius = 1, 4
	straight.Route = []*geo.Point{{X: 60, Y: 30}, {X: 120, Y: 30}}
	branch := straight
	branch.ID, branch.Classes = "branch", []string{"test-branch"}
	branch.Route = []*geo.Point{{X: 60, Y: 30}, {X: 85, Y: 30}, {X: 85, Y: 15}, {X: 120, Y: 15}}

	d := d2target.NewDiagram()
	d.Shapes = []d2target.Shape{card}
	d.Connections = []d2target.Connection{straight, branch}
	for _, tc := range []struct {
		name  string
		opts  *d2svg.RenderOpts
		rectX string
		width string
		rx    string
		railY string
	}{
		{"standalone", nil, "10.5", "39", "3.5", "30.5"},
		{"animation frame", &d2svg.RenderOpts{MasterID: "animation"}, "10", "40", "4", "30"},
		{"scaled", &d2svg.RenderOpts{Scale: new(1.5)}, "10", "40", "4", "30"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := renderCrossingSVG(t, d, tc.opts)
			rect := doc.Find(".test-card rect").First()
			if rect.AttrOr("x", "") != tc.rectX || rect.AttrOr("width", "") != tc.width || rect.AttrOr("rx", "") != tc.rx {
				t.Fatalf("rounded rectangle x=%s, width=%s, rx=%s", rect.AttrOr("x", ""), rect.AttrOr("width", ""), rect.AttrOr("rx", ""))
			}
			for _, selector := range []string{".test-straight", ".test-branch"} {
				path := doc.Find(selector+" path.connection").AttrOr("d", "")
				fields := strings.Fields(path)
				if len(fields) < 6 || fields[0] != "M" || fields[3] != "L" || fields[2] != tc.railY || fields[5] != tc.railY {
					t.Fatalf("shared rail must stay on y=%s: %q", tc.railY, path)
				}
				if selector == ".test-branch" && !strings.Contains(path, " S ") {
					t.Fatalf("rounded bend was lost: %q", path)
				}
			}
		})
	}
	if straight.Route[0].Y != 30 || branch.Route[0].Y != 30 {
		t.Fatal("render modified the source route")
	}

	for _, masked := range []string{"crossing", "label"} {
		t.Run(masked, func(t *testing.T) {
			other := straight
			other.ID, other.Classes = "other", []string{"test-other"}
			if masked == "crossing" {
				other.Route = []*geo.Point{{X: 90, Y: 0}, {X: 90, Y: 60}}
			} else {
				other.Label, other.LabelWidth, other.LabelHeight = "rail", 20, 12
				other.LabelPosition = "INSIDE_MIDDLE_CENTER"
			}
			maskedDiagram := *d
			maskedDiagram.Connections = []d2target.Connection{straight, other}
			doc := renderCrossingSVG(t, &maskedDiagram, nil)
			path := doc.Find(".test-straight path.connection").AttrOr("d", "")
			if !strings.Contains(path, " 30.5 L ") || !strings.HasSuffix(path, " 30.5") {
				t.Fatalf("unlabeled rail lost its alignment: %s", path)
			}
			if masked == "label" {
				labelledPath := doc.Find(".test-other path.connection").AttrOr("d", "")
				if labelledPath != path || doc.Find(".test-other text").Text() != "rail" {
					t.Fatal("labelled and unlabelled strokes no longer share their rail")
				}
			}
			if masked == "crossing" && doc.Find(`mask circle[cx="90.5"][cy="30.5"]`).Length() != 1 {
				t.Fatal("expected a clearance gap at the intersection of the aligned strokes")
			}
		})
	}
}

func TestHairlineMarkerContacts(t *testing.T) {
	d := d2target.NewDiagram()
	card := *d2target.BaseShape()
	card.ID, card.Width, card.Height = "a", 20, 20
	card.Type, card.Stroke, card.Fill, card.StrokeWidth = d2target.ShapeRectangle, "black", "white", 1
	next := card
	next.ID, next.Pos.X = "b", 80
	d.Shapes = []d2target.Shape{card, next}
	edge := *d2target.BaseConnection()
	edge.ID, edge.Src, edge.Dst = "ab", "a", "b"
	edge.Classes = []string{"test-contact"}
	edge.Stroke, edge.StrokeWidth = "black", 1
	edge.SrcArrow, edge.DstArrow = d2target.TriangleArrowhead, d2target.TriangleArrowhead
	edge.Route = []*geo.Point{{X: 20, Y: 10}, {X: 80, Y: 10}}
	d.Connections = []d2target.Connection{edge}
	doc := renderCrossingSVG(t, d, nil)
	path := doc.Find(".test-contact path.connection")
	// A one-unit triangle extends 1.5 units past each end of the shaft.
	if path.AttrOr("d", "") != "M 21.5 10.5 L 78.5 10.5" || path.AttrOr("marker-start", "") == "" || path.AttrOr("marker-end", "") == "" {
		t.Fatalf("markers must touch the outside borders at x=20 and x=80: %v", path.Nodes)
	}
}
