package d2lib

import (
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func TestReorderCaptionStaysOnClearSegment(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		a, b := tweenTestBox(0, 25, 50, 50), tweenTestBox(950, 25, 50, 50)
		a.ID, b.ID = "a", "b"
		a.Opacity, b.Opacity = 1, 1
		var frames []tweenCaptionFrame
		var track []*d2target.Connection
		for i, p := range []float64{.5, .5, .1, .5, .5} {
			edge := d2target.BaseConnection()
			edge.ID, edge.Src, edge.Dst = "wire", "a", "b"
			edge.Route = []*geo.Point{{X: 50, Y: 50}, {X: 950, Y: 50}}
			edge.Label, edge.LabelWidth, edge.LabelHeight = "keyboard", 100, 20
			edge.LabelPosition, edge.LabelPercentage = label.UnlockedTop.String(), p
			panel := &d2target.Diagram{Shapes: []d2target.Shape{*a, *b}, Connections: []d2target.Connection{*edge}}
			if i == 2 && blocked {
				bounds := tweenTestBox(400, 0, 200, 100)
				bounds.ID, bounds.Opacity = "obstacle", 1
				panel.Shapes = append(panel.Shapes, *bounds)
			}
			track = append(track, &panel.Connections[0])
			frames = append(frames, tweenCaptionFrame{board: panel, shapes: gridEdgeShapeMap(panel.Shapes)})
		}
		before, after := *track[0], *track[4]
		budget := maxBoardSampleElements
		chooseEdgeCaptionTrack(track, frames, &budget)
		if *track[0].GetLabelTopLeft() != *before.GetLabelTopLeft() || *track[4].GetLabelTopLeft() != *after.GetLabelTopLeft() {
			t.Fatal("changed a held caption")
		}
		for i, e := range track {
			atCenter := *e.GetLabelTopLeft() == *before.GetLabelTopLeft()
			if !blocked && !atCenter || blocked && i == 2 && atCenter {
				t.Fatalf("sample %d, obstacle present %t: caption at %v", i, blocked, e.GetLabelTopLeft())
			}
		}
	}
}

func TestReorderCaptionMakesRoomForPassingWire(t *testing.T) {
	a, b := tweenTestBox(0, 75, 50, 50), tweenTestBox(950, 75, 50, 50)
	c, d := tweenTestBox(475, -100, 50, 50), tweenTestBox(475, 190, 50, 50)
	a.ID, b.ID, c.ID, d.ID = "a", "b", "c", "d"
	a.Opacity, b.Opacity, c.Opacity, d.Opacity = 1, 1, 1, 1
	var track []*d2target.Connection
	var frames []tweenCaptionFrame
	for i := 0; i < 5; i++ {
		edge := d2target.BaseConnection()
		edge.ID, edge.Src, edge.Dst = "captioned", a.ID, b.ID
		edge.Route = []*geo.Point{{X: 50, Y: 100}, {X: 950, Y: 100}}
		edge.Label, edge.LabelWidth, edge.LabelHeight = "response", 100, 20
		edge.LabelPosition = label.OutsideTopCenter.String()
		panel := &d2target.Diagram{Shapes: []d2target.Shape{*a, *b, *c, *d}, Connections: []d2target.Connection{*edge}}
		if i == 2 {
			other := d2target.BaseConnection()
			other.ID, other.Src, other.Dst = "passing", c.ID, d.ID
			other.Route = []*geo.Point{{X: 500, Y: -50}, {X: 500, Y: 190}}
			panel.Connections = append(panel.Connections, *other)
		}
		track = append(track, &panel.Connections[0])
		frames = append(frames, tweenCaptionFrame{board: panel, shapes: gridEdgeShapeMap(panel.Shapes)})
	}
	budget := maxBoardSampleElements
	start := *track[0].GetLabelTopLeft()
	chooseEdgeCaptionTrack(track, frames, &budget)
	p := track[2].GetLabelTopLeft()
	if p.X < 501 && p.X+100 > 499 {
		t.Fatalf("passing wire still bisects caption at %v", p)
	}
	if *track[0].GetLabelTopLeft() != start || *track[4].GetLabelTopLeft() != start {
		t.Fatal("changed a held caption")
	}
}
