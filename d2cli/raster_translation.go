package d2cli

import (
	"image"

	"github.com/d2lang/d2/d2renderers/d2animate"
	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/xgif"
)

func rasterTranslations(board *d2target.Diagram, viewport *gifViewport) map[string]d2scene.Track {
	if viewport == nil {
		return nil
	}
	tracks := d2animate.TranslationTracks(board)
	if tracks != nil {
		for _, sample := range board.TweenFrames {
			tl, br := sample.BoundingBox()
			viewport.bounds = viewport.bounds.Union(image.Rect(tl.X, tl.Y, br.X, br.Y))
		}
	}
	return tracks
}

// Use spare GIF frame and pixel capacity for moving shapes. Authored holds
// and pre-existing diagrams retain their duration and baseline sample budget.
func densifyGIFTranslations(timeline []gifSegment, delays []int, spare int) []int {
	wanted := 0
	for _, s := range timeline {
		if s.translations != nil {
			wanted += max(0, gifStepDelay(s.Frame)/2-s.count)
		}
	}
	extra := min(spare, wanted)
	if extra <= 0 {
		return delays
	}
	result := make([]int, 0, len(delays)+extra)
	start, allocated, cumulative := 0, 0, 0
	for i := range timeline {
		s := &timeline[i]
		end := start + s.count
		if s.translations != nil {
			delay := gifStepDelay(s.Frame)
			cumulative += max(0, delay/2-s.count)
			newAllocation := int(int64(extra) * int64(cumulative) / int64(wanted))
			s.count += newAllocation - allocated
			allocated = newAllocation
			result = xgif.AppendFrameDelays(result, delay, s.count)
		} else {
			result = append(result, delays[start:end]...)
		}
		start = end
	}
	return result
}
