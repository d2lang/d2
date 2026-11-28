package d2animate

import (
	"fmt"
	"math"

	"github.com/d2lang/d2/d2target"
)

// Frame is a diagram held for a duration on the playback timeline.
type Frame struct {
	Diagram    *d2target.Diagram
	DurationMS int
	picture    *d2target.Diagram // render-only clock, authored Diagram stays the navigation identity
	// TweenSample distinguishes an already sampled transition from a board
	// whose independent shape and connection animations still need sampling.
	TweenSample bool
	// ElapsedMS is the local time of a sampled frame within its step.
	ElapsedMS int
}

// Picture returns the artwork for this frame, including any render-time icon schedule.
func (f Frame) Picture() *d2target.Diagram {
	if f.picture != nil {
		return f.picture
	}
	return f.Diagram
}

// Timeline returns the same root, layer, scenario and step order as a normal
// multiboard export, inserting each step's samples immediately before it.
func Timeline(root *d2target.Diagram, intervalMS int) ([]Frame, error) {
	return SVGTimeline(root, nil, intervalMS, 0)
}

// SVGTimeline also accounts for the travel time of linked-layer zooms when
// rendering a selected subtree of the document.
func SVGTimeline(root *d2target.Diagram, boardPath []string, intervalMS, zoomMS int) ([]Frame, error) {
	if zoomMS < 0 || zoomMS > d2target.MaxTweenDurationMS {
		return nil, fmt.Errorf("invalid layer transition duration %dms", zoomMS)
	}
	if err := d2target.ValidateRenderTarget(root); err != nil {
		return nil, err
	}
	if intervalMS <= 0 {
		return nil, fmt.Errorf("animation interval must be positive")
	}
	icons := newIconTimeline(root, intervalMS)
	// Direct single-board SVG uses its continuously looping native track.
	standalone := len(root.Layers)+len(root.Scenarios)+len(root.Steps)+len(root.TweenFrames) == 0
	var frames []Frame
	var owners []*d2target.Diagram
	var walk func(*d2target.Diagram)
	walk = func(board *d2target.Diagram) {
		if !board.IsFolderOnly {
			start := len(frames)
			frames = AppendBoardFrames(frames, board, intervalMS)
			if icons != nil && !standalone {
				for range len(frames) - start {
					owners = append(owners, board)
				}
			}
		}
		for _, group := range [][]*d2target.Diagram{board.Layers, board.Scenarios, board.Steps} {
			for _, child := range group {
				walk(child)
			}
		}
	}
	walk(root)
	if len(owners) != 0 {
		nav := NewLayerNavigation(root, boardPath)
		links := layerLinks(nav, frames, zoomMS)
		var fixedLayers map[int]bool
		if len(links) != 0 {
			slots := layerSlots(nav, frames, links)
			fixedLayers = make(map[int]bool)
			for _, link := range links {
				if len(link.legs) == 0 || slots[link.to].count == 1 {
					fixedLayers[link.to] = true
				}
			}
		}
		var zooms map[int][]frameLeg
		if zoomMS > 0 && len(nav.focus) > 0 {
			zooms = playbackZooms(nav, frames)
		}
		hold := func(i int) int {
			duration := frames[i].DurationMS
			if zooms[i] != nil {
				return max(duration, zoomMS)
			}
			return duration
		}
		total := 0
		for i := range frames {
			duration := hold(i)
			if duration > math.MaxInt-total {
				return nil, fmt.Errorf("animation duration exceeds the supported range")
			}
			total += duration
		}
		start := 0
		for i, owner := range owners {
			f := &frames[i]
			if icons.Active(owner) {
				actual := *f
				actual.DurationMS = hold(i)
				f.picture = icons.scheduledPicture(owner, actual, start, total, fixedLayers[i])
			}
			start += hold(i)
		}
	}
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
