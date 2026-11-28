package d2animate

import (
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestTranslationTracksPreserveHoldsAndChangingArtwork(t *testing.T) {
	frozen := func(x int, moving bool) *d2target.Diagram {
		board := d2target.NewDiagram()
		shape := d2target.Shape{ID: "box", Pos: d2target.Point{X: x}, Fill: "blue"}
		if moving {
			shape.MotionOffset = &geo.Point{}
		}
		board.Shapes = []d2target.Shape{shape}
		return board
	}
	step := frozen(4, false)
	step.TweenDurationMS = 1000
	step.TweenFrames = []*d2target.Diagram{frozen(0, false), frozen(2, true), frozen(2, true), frozen(3, true)}
	track := TranslationTracks(step)["box"]
	if len(track.Keyframes) != 4 || track.Keyframes[1].Offset != .25 || track.Keyframes[2].Offset != .5 ||
		track.Keyframes[1].Value.Transform.E != 2 || track.Keyframes[2].Value.Transform.E != 2 {
		t.Fatalf("lost the middle hold: %+v", track.Keyframes)
	}
	step.TweenFrames[2].Shapes[0].Fill = "red"
	if TranslationTracks(step) != nil {
		t.Fatal("discarded an intermediate fill")
	}
	step.TweenFrames[2].Shapes[0].Fill = "blue"
	step.TweenFrames[2].Shapes[0].MotionOffset = nil
	if TranslationTracks(step) != nil {
		t.Fatal("treated an unmarked interior pose as a continuous pin")
	}
}
