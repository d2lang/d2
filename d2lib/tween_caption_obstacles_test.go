package d2lib

import (
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/label"
)

func TestTweenCaptionClearance(t *testing.T) {
	parent := d2target.Shape{ID: "outer", Type: d2target.ShapeRectangle,
		Pos: d2target.Point{Y: 16}, Width: 184, Height: 200, Opacity: 1}
	group := d2target.Shape{ID: "outer.group", Type: d2target.ShapeRectangle,
		Pos: d2target.Point{X: 20, Y: 20}, Width: 160, Height: 120, Opacity: 1,
		Text: d2target.Text{Label: "Group", LabelWidth: 46, LabelHeight: 18}, LabelPosition: label.InsideBottomCenter.String()}
	neighbor := d2target.Shape{ID: "peer", Type: d2target.ShapeRectangle,
		Pos: d2target.Point{X: 200, Y: 16}, Width: 28, Height: 36, Opacity: 1}
	candidate := label.InsideTopRight
	clear := func(parent, group, neighbor d2target.Shape) bool {
		d := &d2target.Diagram{Shapes: []d2target.Shape{parent, group, neighbor}}
		f := tweenCaptionFrame{board: d, shapes: gridEdgeShapeMap(d.Shapes)}
		budget := maxBoardSampleElements
		return f.clear(&d.Shapes[1], candidate.String(), &budget)
	}
	if !clear(parent, group, neighbor) {
		t.Fatal("expected clearance inside both the owner and the ancestor")
	}
	t.Run("ancestor boundary crosses title", func(t *testing.T) {
		narrow := parent
		narrow.Width = 149
		if clear(narrow, group, neighbor) {
			t.Fatal("restored a title across its ancestor's right border")
		}
	})
	t.Run("owner link badge", func(t *testing.T) {
		linked := group
		linked.Link = "https://example.org"
		if clear(parent, linked, neighbor) {
			t.Fatal("restored a title under its own top-right link badge")
		}
	})
	t.Run("ancestor link badge", func(t *testing.T) {
		linked := parent
		linked.Link = "https://example.org"
		if clear(linked, group, neighbor) {
			t.Fatal("restored a title under its ancestor's top-right link badge")
		}
	})
	t.Run("neighbor's note", func(t *testing.T) {
		marked := neighbor
		marked.Annotation = &d2target.Annotation{Shape: d2target.AnnotationHighlight, Position: "center-left",
			Label: "Side note", LabelWidth: 64, LabelHeight: 20}
		d := &d2target.Diagram{Shapes: []d2target.Shape{parent, group, marked}}
		marks := d.AnnotationMarks()
		if len(marks) != 1 || marks[0].LabelBox == nil {
			t.Fatal("expected the neighbor's note to be rendered")
		}
		note := marks[0].LabelBox
		if !tweenRouteTextBox(note.TopLeft, int(note.Width), int(note.Height)).Overlaps(tweenCaptionBox(&group, candidate)) {
			t.Fatal("fixture must place note text across the candidate title")
		}
		if clear(parent, group, marked) {
			t.Fatal("restored a title on top of the neighbor's note")
		}
	})
}
