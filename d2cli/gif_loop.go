package d2cli

import (
	"fmt"
	"math"
	"slices"

	"github.com/d2lang/d2/d2renderers/d2scenebuild"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/xgif"
)

const defaultGIFBoardMS = 1000

func maxGIFLoopMS() int {
	limit, _ := xgif.AnimationFrameTime(gifMaxFrames)
	return int(limit.Milliseconds())
}

// A steady stream looks the same after every launch, even when several
// identical copies are already on the wire. Include pulses and dashes in the
// same clock so choosing an icon period doesn't introduce a seam elsewhere.
func automaticGIFLoopMS(board *d2target.Diagram) (int, error) {
	periods := make(map[float64]bool)
	var longestAuthoredPeriod float64
	for edge, seconds := range board.ConnectionIconPeriods() {
		periods[seconds] = true
		if edge.IconAnimation.HasExplicitPeriod() {
			longestAuthoredPeriod = max(longestAuthoredPeriod, seconds)
		}
	}
	for period := range d2scenebuild.PaintAnimationPeriods(board) {
		periods[period.Seconds()] = true
	}
	if len(periods) == 0 {
		return max(defaultGIFBoardMS, board.ReadingMS()), nil
	}
	sorted := make([]float64, 0, len(periods))
	for seconds := range periods {
		sorted = append(sorted, seconds)
	}
	slices.Sort(sorted)
	slices.Reverse(sorted)
	longest := sorted[0]
	limit := maxGIFLoopMS()
	if longest > float64(limit)/1000 {
		return 0, fmt.Errorf("GIF animation needs %g seconds per cycle, exceeding the %d-frame limit (%ds); use SVG for continuous playback", longest, gifMaxFrames, limit/1000)
	}
	// Round geometric periods to the nearest GIF tick. An authored period
	// needs time for one complete departure interval. Keep at least two
	// 30fps samples even when the authored effect is very fast.
	authoredFloorCS := int(math.Ceil(math.Nextafter(longestAuthoredPeriod, 0) * 100))
	minCS := max(4, authoredFloorCS, int(math.Round(longest*100)), (board.ReadingMS()+9)/10)
	firstMatch := 0
	aliasedGaps := make(map[int]bool)
	for cs := minCS; cs <= limit/10; cs++ {
		seconds := float64(cs) / 100
		if slices.ContainsFunc(sorted, func(period float64) bool {
			// When the authored minimum falls between ticks, its first
			// valid duration can round up by nearly a whole tick.
			if cs == authoredFloorCS && period == longestAuthoredPeriod {
				return false
			}
			return math.Abs(math.Remainder(seconds, period)) > .005+1e-12
		}) {
			continue
		}
		if firstMatch == 0 {
			firstMatch = cs
		}
		frames, _ := xgif.AnimationFrameCount(cs * 10)
		gap := cs / frames
		// AppendFrameDelays distributes the remainder, ending with a longer
		// delay. With at least two extra ticks, both gap and gap+1 occur
		// between painted frames, so only an unavoidable 10ms alias remains.
		if cs%frames <= 1 {
			aliased, known := aliasedGaps[gap]
			if !known {
				aliased = slices.ContainsFunc(sorted, func(period float64) bool {
					const tolerance = 1e-9
					return math.Abs(math.Remainder(float64(gap)/100, period)) <= tolerance &&
						math.Abs(math.Remainder(.01, period)) > tolerance
				})
				aliasedGaps[gap] = aliased
			}
			if aliased {
				continue
			}
		}
		return cs * 10, nil
	}
	if firstMatch != 0 {
		// The GIF frame/clock budget cannot show any more distinct phases.
		return firstMatch * 10, nil
	}
	return 0, fmt.Errorf("GIF animation periods have no shared loop within the %d-frame limit (%ds); set animate-interval to at least %dms to choose the capture duration, or use SVG", gifMaxFrames, limit/1000, minCS*10)
}
