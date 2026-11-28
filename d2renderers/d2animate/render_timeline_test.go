package d2animate

import (
	"bytes"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestRenderSVGFramesSharesStraightArtwork(t *testing.T) {
	board := func(x int, sample bool) *d2target.Diagram {
		d := d2target.NewDiagram()
		s := d2target.BaseShape()
		s.ID, s.Type, s.Width, s.Height = "box", d2target.ShapeRectangle, 12, 9
		s.Opacity, s.Pos.X = 1, x
		if sample {
			s.MotionOffset = &geo.Point{}
		}
		rail := *d2target.BaseShape()
		rail.ID, rail.Type, rail.Width, rail.Height = "rail", d2target.ShapeRectangle, 50, 10
		d.Shapes = []d2target.Shape{rail, *s}
		return d
	}
	root := board(0, false)
	end := board(10, false)
	root.Steps = []*d2target.Diagram{end}
	end.TweenDurationMS = 1000
	for i := range 10 {
		end.TweenFrames = append(end.TweenFrames, board(i, true))
	}
	frames, err := Timeline(root, 1250)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := root.HashID(nil)
	if err != nil {
		t.Fatal(err)
	}
	opts := d2svg.RenderOpts{MasterID: hash}
	for _, opaque := range []bool{false, true} {
		calls := 0
		projected, pictures, err := RenderSVGFrames(root, nil, frames, opts, 0, func(d *d2target.Diagram) ([]byte, error) {
			calls++
			b, err := d2svg.Render(d, &opts)
			if opaque {
				b = bytes.Replace(b, []byte("</g>"), []byte("<style>.box{fill:red}</style></g>"), 1)
			}
			return b, err
		})
		if err != nil {
			t.Fatal(err)
		}
		want := 4
		if opaque {
			want = len(frames)
		}
		if len(projected) != want || calls != want || len(pictures) != want {
			t.Fatalf("opaque=%t: %d renders, %d frames, %d pictures; want %d", opaque, calls, len(projected), len(pictures), want)
		}
		var duration int
		for _, f := range projected {
			duration += f.DurationMS
		}
		if duration != 2500 {
			t.Fatalf("playback duration changed to %dms", duration)
		}
	}
}
