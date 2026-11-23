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
	// Continues marks an interior sequence hold. GIF delays round down at
	// interior boundaries so fractional centiseconds carry to the next frame.
	Continues bool
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
	var playback Playback
	var walk func(*d2target.Diagram) error
	walk = func(board *d2target.Diagram) error {
		if !board.IsFolderOnly {
			var err error
			frames, err = playback.AppendBoard(frames, board, intervalMS)
			if err != nil {
				return err
			}
		}
		for _, group := range [][]*d2target.Diagram{board.Layers, board.Scenarios, board.Steps} {
			for _, child := range group {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	return frames, nil
}

// Playback shares the sequence sampling budget across all boards in an export.
// Its zero value is ready to use.
type Playback struct {
	sequence sequenceBudget
}

// AppendBoard appends the incoming value transition and the message sequence
// for one validated board. Raster exporters can supply their own traversal.
func (p *Playback) AppendBoard(frames []Frame, board *d2target.Diagram, intervalMS int) ([]Frame, error) {
	if len(board.SequenceTimelines) == 0 || board.Config == nil || board.Config.AnimateSequence == "" || board.Config.AnimateSequence == "off" {
		return AppendBoardFrames(frames, board, intervalMS), nil
	}
	duration := min(intervalMS, 600)
	if configured := board.Config.AnimateDuration; configured != nil {
		if *configured < 0 || *configured > d2target.MaxTweenDurationMS {
			return nil, fmt.Errorf("sequence animation duration must be between 0 and %d milliseconds", d2target.MaxTweenDurationMS)
		}
		duration = int(*configured)
	}
	var incoming []Frame
	if len(board.TweenFrames) > 0 {
		incoming = AppendBoardFrames(nil, board, intervalMS)
	}
	sequence, err := sequenceFrames(board, board.Config.AnimateSequence, intervalMS, duration, &p.sequence, incoming)
	if err != nil {
		return nil, err
	}
	return append(frames, sequence...), nil
}

// AppendBoardFrames appends the incoming value transition and settled state of one board.
func AppendBoardFrames(frames []Frame, board *d2target.Diagram, intervalMS int) []Frame {
	if len(board.TweenFrames) == 0 {
		return append(frames, Frame{Diagram: board, DurationMS: intervalMS})
	}
	duration := board.TweenDurationMS
	slot := max(intervalMS, duration)
	start := 0
	for i, sample := range board.TweenFrames {
		end := int(int64(i+1) * int64(duration) / int64(len(board.TweenFrames)))
		if i == len(board.TweenFrames)-1 && slot == duration {
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
