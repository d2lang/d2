package d2lib

import (
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/label"
)

func TestTweenCaptionExcursions(t *testing.T) {
	center, left := label.InsideTopCenter.String(), label.InsideTopLeft.String()
	for _, tc := range []struct {
		name        string
		blockSecond bool
		budget      int
		want        string
	}{
		{"follows moving container", false, 100, center},
		{"later child blocks the whole excursion", true, 100, left},
		{"budget runs out between samples", false, 1, left},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newGroup := func(x int, anchor string) d2target.Shape {
				return d2target.Shape{ID: "group", Type: d2target.ShapeRectangle, Pos: d2target.Point{X: x}, Width: 300, Height: 150, Opacity: 1,
					Text: d2target.Text{Label: "Title", LabelWidth: 60, LabelHeight: 18}, LabelPosition: anchor}
			}
			start, end := newGroup(0, center), newGroup(90, center)
			track := []*d2target.Shape{&start}
			var frames []tweenCaptionFrame
			for i, x := range []int{30, 60} {
				board := &d2target.Diagram{Shapes: []d2target.Shape{newGroup(x, left)}}
				if i == 1 && tc.blockSecond {
					board.Shapes = append(board.Shapes, d2target.Shape{ID: "group.child", Type: d2target.ShapeRectangle,
						Pos: d2target.Point{X: x + 140, Y: 8}, Width: 20, Height: 20, Opacity: 1})
				}
				track = append(track, &board.Shapes[0])
				frames = append(frames, tweenCaptionFrame{board: board, shapes: gridEdgeShapeMap(board.Shapes)})
			}
			budget := tc.budget
			removeCaptionExcursions(append(track, &end), frames, &budget)
			for i := range frames {
				s := &frames[i].board.Shapes[0]
				if s.LabelPosition != tc.want {
					t.Fatalf("sample %d: got %s, want %s", i, s.LabelPosition, tc.want)
				}
				if tc.want == center && tweenCaptionBox(s, label.InsideTopCenter).Min.X != s.Pos.X+120 {
					t.Fatalf("sample %d: restored title does not follow the container", i)
				}
			}
		})
	}
}
