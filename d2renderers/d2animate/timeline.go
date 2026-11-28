package d2animate

import (
	"fmt"

	"github.com/d2lang/d2/d2target"
)

// Frame is a diagram held for a duration on the playback timeline.
type Frame struct {
	Diagram    *d2target.Diagram
	DurationMS int
	// TweenSample distinguishes an already sampled transition from a board
	// whose independent shape and connection animations still need sampling.
	TweenSample bool
	// ElapsedMS is the local time of a sampled frame within its step.
	ElapsedMS int
}

// Timeline returns the same root, layer, scenario and step order as a normal
// multiboard export, inserting each step's samples immediately before it.
func Timeline(root *d2target.Diagram, intervalMS int) ([]Frame, error) {
	if err := d2target.ValidateRenderTarget(root); err != nil {
		return nil, err
	}
	if intervalMS <= 0 {
		return nil, fmt.Errorf("animation interval must be positive")
	}
	var frames []Frame
	var walk func(*d2target.Diagram)
	walk = func(board *d2target.Diagram) {
		if !board.IsFolderOnly {
			frames = AppendBoardFrames(frames, board, intervalMS)
		}
		for _, group := range [][]*d2target.Diagram{board.Layers, board.Scenarios, board.Steps} {
			for _, child := range group {
				walk(child)
			}
		}
	}
	walk(root)
	return frames, nil
}

// AppendBoardFrames expands one validated, drawable board. Raster exporters
// impose their own traversal limits before expanding its samples.
func AppendBoardFrames(frames []Frame, board *d2target.Diagram, intervalMS int) []Frame {
	// The interval is a floor. A board with a caption or notes is held for as
	// long as they take to read.
	intervalMS = max(intervalMS, board.ReadingMS())
	if len(board.TweenFrames) == 0 {
		return append(frames, Frame{Diagram: board, DurationMS: intervalMS})
	}
	duration := board.TweenDurationMS
	slot := max(intervalMS, duration)
	start := 0
	for i, sample := range board.TweenFrames {
		end := int(int64(i+1) * int64(duration) / int64(len(board.TweenFrames)))
		if len(board.TweenFrameEndsMS) != 0 {
			end = board.TweenFrameEndsMS[i]
		}
		if i == len(board.TweenFrames)-1 && end == slot {
			// Without a hold, split the last sampling period between its
			// interpolated value and the exact endpoint before the next board.
			end = start + (end-start)/2
		}
		if end > start {
			frames = append(frames, Frame{Diagram: sample, DurationMS: end - start, ElapsedMS: start, TweenSample: true})
		}
		start = end
	}
	return append(frames, Frame{Diagram: board, DurationMS: slot - start, ElapsedMS: start})
}
