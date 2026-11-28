package d2target

import (
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

func TestSequenceFrameFollowsArrowAndSelfRoute(t *testing.T) {
	points := []*geo.Point{{X: 0, Y: 0}, {X: 60, Y: 0}, {X: 60, Y: 40}, {X: 0, Y: 40}}
	for _, tt := range []struct {
		name       string
		src, dst   Arrowhead
		start, end geo.Point
	}{
		{"request", NoArrowhead, TriangleArrowhead, *points[0], geo.Point{X: 60, Y: 20}},
		{"reply", TriangleArrowhead, NoArrowhead, geo.Point{X: 60, Y: 20}, *points[3]},
		{"both", TriangleArrowhead, TriangleArrowhead, *points[0], geo.Point{X: 60, Y: 20}},
		{"no tips", NoArrowhead, NoArrowhead, *points[0], geo.Point{X: 60, Y: 20}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := *BaseConnection()
			c.SequenceStep, c.BorderRadius, c.Route = 1, 0, points
			c.SrcArrow, c.DstArrow = tt.src, tt.dst
			board := &Diagram{Connections: []Connection{c}}
			frame := board.SequencePlaybackFrame(SequenceMessageRevealMS / 2)
			path := frame.Connections[0].Route
			if *path[0] != tt.start || *path[len(path)-1] != tt.end {
				t.Fatalf("got endpoints %v to %v", path[0], path[len(path)-1])
			}
			if board.Connections[0].MotionTip != "" || len(board.Connections[0].Route) != 4 {
				t.Fatal("sampling changed the authored board")
			}
		})
	}
}
