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
	plan, err := planSVGFrames(root, boardPath, intervalMS, zoomMS)
	if err != nil {
		return nil, err
	}
	if len(plan.boundaries) == 0 {
		return plan.frames, nil
	}
	icons := newIconTimeline(root, intervalMS)
	links := layerLinks(plan.nav, plan.frames, zoomMS)
	var fixedLayers map[int]bool
	if len(links) != 0 {
		slots := layerSlots(plan.nav, plan.frames, links)
		fixedLayers = make(map[int]bool)
		for _, link := range links {
			if len(link.legs) == 0 || slots[link.to].count == 1 {
				fixedLayers[link.to] = true
			}
		}
	}
	total := plan.boundaries[len(plan.frames)]
	for _, span := range plan.iconBoards {
		if !icons.Active(span.board) {
			continue
		}
		for i := span.start; i < span.end; i++ {
			actual := plan.frames[i]
			start := plan.boundaries[i]
			actual.DurationMS = plan.boundaries[i+1] - start
			plan.frames[i].picture = icons.scheduledPicture(span.board, actual, start, total, fixedLayers[i])
		}
	}
	return plan.frames, nil
}

// ValidateSVGIconLoop checks an explicitly selected animation without creating
// per-frame pictures. A single drawable SVG runs its icon clocks continuously.
func ValidateSVGIconLoop(root *d2target.Diagram, boardPath []string, intervalMS, zoomMS int) error {
	_, err := planSVGFrames(root, boardPath, intervalMS, zoomMS)
	return err
}

type iconBoardSpan struct {
	board      *d2target.Diagram
	start, end int
}

type svgFramePlan struct {
	frames     []Frame
	iconBoards []iconBoardSpan
	boundaries []int // millisecond offsets, including the end of the film
	nav        *LayerNavigation
}

func planSVGFrames(root *d2target.Diagram, boardPath []string, intervalMS, zoomMS int) (*svgFramePlan, error) {
	if zoomMS < 0 || zoomMS > d2target.MaxTweenDurationMS {
		return nil, fmt.Errorf("invalid layer transition duration %dms", zoomMS)
	}
	if err := d2target.ValidateRenderTarget(root); err != nil {
		return nil, err
	}
	if intervalMS <= 0 {
		return nil, fmt.Errorf("animation interval must be positive")
	}
	var p svgFramePlan
	var walk func(*d2target.Diagram)
	walk = func(board *d2target.Diagram) {
		if !board.IsFolderOnly {
			start := len(p.frames)
			p.frames = AppendBoardFrames(p.frames, board, intervalMS)
			for i := range board.Connections {
				if board.Connections[i].IconAnimation != nil {
					p.iconBoards = append(p.iconBoards, iconBoardSpan{board, start, len(p.frames)})
					break
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
	if len(p.frames) < 2 || len(p.iconBoards) == 0 {
		return &p, nil
	}
	p.nav = NewLayerNavigation(root, boardPath)
	var zooms map[int][]frameLeg
	if zoomMS > 0 && len(p.nav.focus) > 0 {
		zooms = playbackZooms(p.nav, p.frames)
	}
	p.boundaries = make([]int, len(p.frames)+1)
	for i, frame := range p.frames {
		hold := frame.DurationMS
		if zooms[i] != nil {
			hold = max(hold, zoomMS)
		}
		if hold > math.MaxInt-p.boundaries[i] {
			return nil, fmt.Errorf("animation duration exceeds the supported range")
		}
		p.boundaries[i+1] = p.boundaries[i] + hold
	}
	for _, span := range p.iconBoards {
		if err := d2target.ValidateIconLoopDuration(span.board, p.boundaries[len(p.frames)]); err != nil {
			return nil, err
		}
	}
	return &p, nil
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
