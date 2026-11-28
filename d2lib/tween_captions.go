package d2lib

import (
	"context"
	"image"
	"math"
	"strings"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/label"
)

// A static layout can move a container title out of a wire's way for a single
// sample. Once transition routing has chosen the actual wires, remove excursions
// from an anchor when it is usable for the whole interval until the title returns.
func stabilizeTweenCaptions(ctx context.Context, previous, board *d2target.Diagram, graph *d2graph.Graph, budget *int) error {
	if len(board.TweenFrames) == 0 || graph == nil || len(graph.TweenFrames) != len(board.TweenFrames) || *budget <= 0 {
		return nil
	}
	var containers []string
	for _, obj := range graph.Objects {
		if obj.IsContainer() && obj.Attributes.LabelPosition == nil && obj.Label.Value != "" {
			containers = append(containers, obj.AbsID())
		}
	}
	if len(containers) == 0 {
		return nil
	}
	cost := len(board.Shapes)
	for _, f := range board.TweenFrames {
		cost += gridFrameElements(f)
	}
	if previous != nil {
		cost += len(previous.Shapes)
	}
	if cost > *budget {
		return nil
	}
	*budget -= cost
	frames := make([]tweenCaptionFrame, len(board.TweenFrames))
	for i, f := range board.TweenFrames {
		frames[i] = tweenCaptionFrame{board: f, shapes: gridEdgeShapeMap(f.Shapes)}
		// A sampled override (including an inherited label.near) owns its
		// anchor. Exclude non-containers too if a step changed the hierarchy.
		frames[i].automatic = make(map[string]bool, len(containers))
		for _, obj := range graph.TweenFrames[i].Objects {
			if obj.IsContainer() && obj.Attributes.LabelPosition == nil {
				frames[i].automatic[obj.AbsID()] = true
			}
		}
	}
	last := gridEdgeShapeMap(board.Shapes)
	var first map[string]*d2target.Shape
	if previous != nil {
		first = gridEdgeShapeMap(previous.Shapes)
	}
	for _, id := range containers {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := first[id]
		if start == nil {
			start = frames[0].shapes[id]
		}
		end := last[id]
		if !tweenCaptionEligible(end, end) || !tweenCaptionEligible(start, end) {
			continue
		}
		// Include the two held layouts as immutable sentinels.
		track := []*d2target.Shape{start}
		for i := range frames {
			s := frames[i].shapes[id]
			if !frames[i].automatic[id] || !tweenCaptionEligible(s, end) {
				break
			}
			track = append(track, s)
		}
		if len(track) != len(frames)+1 {
			continue
		}
		removeCaptionExcursions(append(track, end), frames, budget)
		if *budget <= 0 {
			break
		}
	}
	return ctx.Err()
}

func tweenCaptionEligible(s, end *d2target.Shape) bool {
	if s == nil || end == nil || s.Type != end.Type || s.Label == "" || s.Label != end.Label ||
		s.Language != "" || end.Language != "" || s.MotionLabelTopLeft != nil || s.MotionLabelBox != nil ||
		s.RowReveal != nil || s.MotionScale != 0 || s.MotionClip != nil {
		return false
	}
	// Border captions cut a mask into the outline; outside captions can
	// affect the canvas. Keep this pass to ordinary headings inside shapes.
	p := label.FromString(s.LabelPosition)
	return p >= label.InsideTopLeft && p <= label.InsideBottomRight
}

func removeCaptionExcursions(track []*d2target.Shape, frames []tweenCaptionFrame, budget *int) {
	// For A B ... A, try A wherever the native caption left it. Commit as
	// a unit; a real obstruction anywhere in the detour keeps it intact.
	for i := 0; i+2 < len(track) && *budget > 0; i++ {
		anchor := track[i].LabelPosition
		if track[i+1].LabelPosition == anchor {
			continue
		}
		j := i + 2
		for ; j < len(track) && track[j].LabelPosition != anchor; j++ {
		}
		if j == len(track) {
			continue
		}
		clear := true
		for k := i + 1; k < j; k++ {
			if !frames[k-1].clear(track[k], anchor, budget) {
				clear = false
				break
			}
		}
		if clear {
			for _, s := range track[i+1 : j] {
				s.LabelPosition = anchor
			}
			i = j - 1
		}
	}
}

type tweenCaptionFrame struct {
	board     *d2target.Diagram
	shapes    map[string]*d2target.Shape
	automatic map[string]bool
	wires     []tweenCaptionWire
}

type tweenCaptionWire struct {
	ink  tweenRouteInk
	clip *d2target.MotionClip
}

func tweenCaptionBox(s *d2target.Shape, pos label.Position) image.Rectangle {
	if s.Label == "" || s.LabelWidth <= 0 || s.LabelHeight <= 0 {
		return image.Rectangle{}
	}
	if pos == label.Unset && (s.Language == "markdown" || s.Language == "latex") {
		pos = label.InsideMiddleCenter
	}
	outline := s.Geometry()
	box := outline.GetInnerBox()
	if pos.IsOutside() || pos.IsBorder() {
		box = s.OuterLabelBox(outline.GetBox())
	}
	return tweenRouteTextBox(s.LabelTopLeft(pos, box), s.LabelWidth, s.LabelHeight)
}

func (f *tweenCaptionFrame) clear(owner *d2target.Shape, anchor string, budget *int) bool {
	// Charge before generating paths. Exhaustion preserves the native anchor.
	cost := gridFrameElements(f.board)
	if cost > *budget {
		*budget = 0
		return false
	}
	*budget -= cost
	box := tweenCaptionBox(owner, label.FromString(anchor))
	if box.Empty() {
		return false
	}
	box = box.Inset(-label.PADDING)
	if !tweenCaptionFits(owner, box, -1) {
		return false
	}

	for i := range f.board.Shapes {
		s := &f.board.Shapes[i]
		// Resolving annotations can reposition notes on other objects. Keep
		// their authored composition when checking an ordinary caption.
		if s.Annotation != nil || s.LeavingAnnotation != nil {
			return false
		}
		if s.Opacity <= 0 {
			continue
		}
		ancestor := strings.HasPrefix(owner.ID, s.ID+".")
		if s.ID == owner.ID || ancestor {
			// Badges and positioned tooltips have independent renderer geometry.
			if s.Link != "" || s.Tooltip != "" {
				return false
			}
		}
		if s.ID != owner.ID {
			pad := int(math.Ceil(s.RenderedStrokeWidth() / 2))
			body := tweenShapeBody(f.board, i, pad, !ancestor)
			if tweenRouteClip(body, s.MotionClip).Overlaps(box) && (!ancestor || !tweenCaptionFits(s, box, pad)) {
				return false
			}
			if tweenRouteClip(tweenCaptionBox(s, label.FromString(s.LabelPosition)), s.MotionClip).Overlaps(box) {
				return false
			}
		}
		if tweenRouteClip(tweenShapeIconBox(s), s.MotionClip).Overlaps(box) {
			return false
		}
	}
	// The connection geometry is final at this point. Test the same rendered
	// strokes, rounded bends, markers, captions and icons as the route planner.
	if f.wires == nil {
		wires := make([]tweenCaptionWire, 0, len(f.board.Connections))
		for i := range f.board.Connections {
			e := &f.board.Connections[i]
			if e.Annotation != nil || e.LeavingAnnotation != nil {
				return false
			}
			if e.Opacity <= 0 {
				continue
			}
			src, dst := f.shapes[e.Src], f.shapes[e.Dst]
			if src == nil || dst == nil || !validTweenRoute(e.Route) || e.IsCurve && (len(e.Route)-1)%3 != 0 {
				return false
			}
			wires = append(wires, tweenCaptionWire{tweenRouteInkFor(e, tweenRouteShapePair{src, dst}), e.MotionClip})
		}
		f.wires = wires
	}
	for i := range f.wires {
		stroke, decoration := f.wires[i].ink.overlap(tweenRouteClip(box, f.wires[i].clip), -1)
		if stroke > 0 || decoration > 0 {
			return false
		}
	}
	return true
}

// A child can be pinned across its parent's edge. Only the fully enclosed
// part of an ancestor may serve as background for a restored title.
func tweenCaptionFits(s *d2target.Shape, box image.Rectangle, inset int) bool {
	inner := s.Geometry().GetInnerBox()
	pad := float64(inset)
	return float64(box.Min.X) >= inner.TopLeft.X+pad && float64(box.Min.Y) >= inner.TopLeft.Y+pad &&
		float64(box.Max.X) <= inner.TopLeft.X+inner.Width-pad && float64(box.Max.Y) <= inner.TopLeft.Y+inner.Height-pad
}
