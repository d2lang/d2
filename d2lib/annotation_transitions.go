package d2lib

import (
	"context"
	"math"
	"slices"

	"github.com/d2lang/d2/d2target"
)

// boardAnnotations is everything one board points at, lifted off its shapes.
type boardAnnotations struct {
	shapes, edges map[string]*d2target.Annotation
	caption       *d2target.Annotation
}

// liftAnnotations takes every annotation off the board tree and returns them
// by board. The transition planners compare consecutive boards shape by
// shape to decide what moved; an annotation lasts one board, so leaving it on
// would make every annotated shape look changed. The marks are put back, and
// given their own transition, by sampleAnnotations.
func liftAnnotations(root *d2target.Diagram) map[*d2target.Diagram]*boardAnnotations {
	lifted := make(map[*d2target.Diagram]*boardAnnotations)
	strip := func(board *d2target.Diagram) *boardAnnotations {
		var a boardAnnotations
		for i := range board.Shapes {
			s := &board.Shapes[i]
			if s.Annotation != nil {
				if a.shapes == nil {
					a.shapes = make(map[string]*d2target.Annotation)
				}
				a.shapes[s.ID], s.Annotation = s.Annotation, nil
			}
		}
		for i := range board.Connections {
			c := &board.Connections[i]
			if c.Annotation != nil {
				if a.edges == nil {
					a.edges = make(map[string]*d2target.Annotation)
				}
				a.edges[c.ID], c.Annotation = c.Annotation, nil
			}
		}
		a.caption, board.Root.Annotation = board.Root.Annotation, nil
		if a.shapes == nil && a.edges == nil && a.caption == nil {
			return nil
		}
		return &a
	}
	var walk func(*d2target.Diagram)
	walk = func(board *d2target.Diagram) {
		if a := strip(board); a != nil {
			lifted[board] = a
		}
		// A step's style samples carry copies of its annotations.
		for _, frame := range board.TweenFrames {
			strip(frame)
		}
		for _, children := range [][]*d2target.Diagram{board.Layers, board.Scenarios, board.Steps} {
			for _, child := range children {
				walk(child)
			}
		}
	}
	walk(root)
	if len(lifted) == 0 {
		return nil
	}
	return lifted
}

// boardMarks plans how a board's annotations are drawn while the previous
// board's are erased.
type boardMarks struct {
	now *boardAnnotations
	// arriving holds each new mark's place in drawing order, keyed by the ID
	// of the shape or connection it points at.
	shapes, edges map[string]int
	count         int
	// leaving holds the previous board's marks that this one does not keep.
	leavingShapes, leavingEdges map[string]*d2target.Annotation
	captionChanged              bool
	leavingCaption              *d2target.Annotation
}

func sameAnnotation(a, b *d2target.Annotation) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Label == b.Label && a.Shape == b.Shape && a.Position == b.Position && a.Stroke == b.Stroke &&
		a.FontColor == b.FontColor && slices.Equal(a.Members, b.Members)
}

// showsOn reports whether the plan changes anything the board can draw. A
// mark whose target the board no longer has cannot be erased on it.
func (m *boardMarks) showsOn(board *d2target.Diagram) bool {
	if m.count > 0 || m.captionChanged {
		return true
	}
	for i := range board.Shapes {
		if _, leaving := m.leavingShapes[board.Shapes[i].ID]; leaving {
			return true
		}
	}
	for i := range board.Connections {
		if _, leaving := m.leavingEdges[board.Connections[i].ID]; leaving {
			return true
		}
	}
	return false
}

func planBoardMarks(before, now *boardAnnotations, board *d2target.Diagram) *boardMarks {
	if before == nil {
		before = &boardAnnotations{}
	}
	if now == nil {
		now = &boardAnnotations{}
	}
	m := boardMarks{now: now}
	arrive := func(ids map[string]int, id string) map[string]int {
		if ids == nil {
			ids = make(map[string]int)
		}
		if _, planned := ids[id]; !planned {
			ids[id] = m.count
			m.count++
		}
		return ids
	}
	fresh := func(id string) bool {
		a, ok := now.shapes[id]
		return ok && !sameAnnotation(before.shapes[id], a)
	}
	// The caption names things in the order the reader meets them. Draw
	// those marks first and in that order, then the rest as declared.
	if now.caption != nil {
		for _, id := range now.caption.Cues {
			if fresh(id) {
				m.shapes = arrive(m.shapes, id)
			}
		}
	}
	for i := range board.Shapes {
		if id := board.Shapes[i].ID; fresh(id) {
			m.shapes = arrive(m.shapes, id)
		}
	}
	for i := range board.Connections {
		id := board.Connections[i].ID
		if a, ok := now.edges[id]; ok && !sameAnnotation(before.edges[id], a) {
			m.edges = arrive(m.edges, id)
		}
	}

	// An annotation declared again unchanged stays on the page. Any other
	// one from the previous board is erased.
	for id, old := range before.shapes {
		if !sameAnnotation(old, now.shapes[id]) {
			if m.leavingShapes == nil {
				m.leavingShapes = make(map[string]*d2target.Annotation)
			}
			m.leavingShapes[id] = old
		}
	}
	for id, old := range before.edges {
		if !sameAnnotation(old, now.edges[id]) {
			if m.leavingEdges == nil {
				m.leavingEdges = make(map[string]*d2target.Annotation)
			}
			m.leavingEdges[id] = old
		}
	}
	if !sameAnnotation(before.caption, now.caption) {
		m.captionChanged, m.leavingCaption = true, before.caption
	}

	if m.count == 0 && m.leavingShapes == nil && m.leavingEdges == nil && !m.captionChanged {
		return nil
	}
	return &m
}

func unitRamp(progress, start, end float64) float64 {
	if progress <= start {
		return 0
	}
	if progress >= end {
		return 1
	}
	return (progress - start) / (end - start)
}

// drawn returns how much of mark i is on the page. Old ink comes off first.
// New marks then follow one another, each starting when the one before it is
// half done, so the eye is led from one to the next.
func (m *boardMarks) drawn(i int, progress float64) float64 {
	start := 0.
	if m.leavingShapes != nil || m.leavingEdges != nil {
		start = 0.2
	}
	slot := (1 - start) / (1 + 0.5*float64(m.count-1))
	begin := start + 0.5*slot*float64(i)
	return boardStroke(unitRamp(progress, begin, begin+slot))
}

// sampledAnnotation shows part of a mark on one frame. The fractions go into
// the diagram's hash, and with it into every ID in the SVG. Easing curves
// differ in their last bit between architectures, so the fractions are kept
// to four places, which is finer than a mark's ink can show.
func sampledAnnotation(a *d2target.Annotation, from, to float64) *d2target.Annotation {
	const places = 1e4
	sample := *a
	sample.Drawn = &[2]float64{
		math.Floor(from*places+0.5+1e-7) / places,
		math.Floor(to*places+0.5+1e-7) / places,
	}
	return &sample
}

// attach puts the board's annotations on one of its frames. With a plan, the
// frame shows the marks as they stand at progress through the transition.
func (a *boardAnnotations) attach(frame *d2target.Diagram, m *boardMarks, progress float64) {
	if a == nil {
		a = &boardAnnotations{}
	}
	// Samples share their shapes and connections with the board they sample.
	frame.Shapes = slices.Clone(frame.Shapes)
	frame.Connections = slices.Clone(frame.Connections)
	frame.Root.Annotation = a.caption
	if m == nil {
		for i := range frame.Shapes {
			frame.Shapes[i].Annotation = a.shapes[frame.Shapes[i].ID]
		}
		for i := range frame.Connections {
			frame.Connections[i].Annotation = a.edges[frame.Connections[i].ID]
		}
		return
	}
	erased := boardStroke(unitRamp(progress, 0, 0.3))
	for i := range frame.Shapes {
		s := &frame.Shapes[i]
		s.Annotation = a.shapes[s.ID]
		if order, ok := m.shapes[s.ID]; ok && s.Annotation != nil {
			s.Annotation = sampledAnnotation(s.Annotation, 0, m.drawn(order, progress))
		}
		if old, ok := m.leavingShapes[s.ID]; ok && erased < 1 {
			s.LeavingAnnotation = sampledAnnotation(old, erased, 1)
		}
	}
	for i := range frame.Connections {
		c := &frame.Connections[i]
		c.Annotation = a.edges[c.ID]
		if order, ok := m.edges[c.ID]; ok && c.Annotation != nil {
			c.Annotation = sampledAnnotation(c.Annotation, 0, m.drawn(order, progress))
		}
		if old, ok := m.leavingEdges[c.ID]; ok && erased < 1 {
			c.LeavingAnnotation = sampledAnnotation(old, erased, 1)
		}
	}
	if m.captionChanged {
		if out := unitRamp(progress, 0, 0.25); m.leavingCaption != nil && out < 1 {
			frame.Root.LeavingAnnotation = sampledAnnotation(m.leavingCaption, out, 1)
		}
		if a.caption != nil {
			frame.Root.Annotation = sampledAnnotation(a.caption, 0, unitRamp(progress, 0.15, 0.5))
		}
	}
}

// restore puts the board's own annotations back, whole.
func (a *boardAnnotations) restore(board *d2target.Diagram) {
	if a == nil {
		return
	}
	for i := range board.Shapes {
		if annotation, ok := a.shapes[board.Shapes[i].ID]; ok {
			board.Shapes[i].Annotation = annotation
		}
	}
	for i := range board.Connections {
		if annotation, ok := a.edges[board.Connections[i].ID]; ok {
			board.Connections[i].Annotation = annotation
		}
	}
	board.Root.Annotation = a.caption
}

// sampleAnnotations returns the lifted annotations to their boards. When the
// animation has a transition duration it also draws them: each board's marks
// are sampled onto the frames its other transitions produced, and a board
// with no other transition gets frames of its own.
func sampleAnnotations(ctx context.Context, root *d2target.Diagram, lifted map[*d2target.Diagram]*boardAnnotations, duration, boardLimit int) error {
	if lifted == nil {
		return nil
	}
	boards := 0
	var count func(*d2target.Diagram)
	count = func(board *d2target.Diagram) {
		boards += 1 + len(board.TweenFrames)
		for _, children := range [][]*d2target.Diagram{board.Layers, board.Scenarios, board.Steps} {
			for _, child := range children {
				count(child)
			}
		}
	}
	count(root)
	framesLeft := boardLimit - boards
	elementsLeft := maxBoardSampleElements

	var previous *d2target.Diagram
	var walk func(board *d2target.Diagram, clean bool) error
	walk = func(board *d2target.Diagram, clean bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !board.IsFolderOnly {
			now := lifted[board]
			var plan *boardMarks
			if duration > 0 && previous != nil {
				// A layer is a page of its own. Nothing from the board shown
				// before it is erased on it.
				var before *boardAnnotations
				if !clean {
					before = lifted[previous]
				}
				plan = planBoardMarks(before, now, board)
			}
			if plan != nil && len(board.TweenFrames) == 0 && plan.showsOn(board) {
				// Nothing else changes on this step, so the marks set its frames.
				cost := max(1, len(board.Shapes)+len(board.Connections))
				n := min(d2target.TweenFrameCount(duration), framesLeft, elementsLeft/cost)
				if n > 0 {
					frames := make([]*d2target.Diagram, n)
					for i := range frames {
						frame := *board
						frame.Layers, frame.Scenarios, frame.Steps, frame.TweenFrames = nil, nil, nil, nil
						frame.TweenDurationMS, frame.TweenFrameEndsMS, frame.GridMotion = 0, nil, false
						frames[i] = &frame
					}
					board.TweenFrames, board.TweenDurationMS = frames, duration
					framesLeft -= n
					elementsLeft -= n * cost
				}
			}
			// The frames can end before the transition's time is up, and the
			// board then shows whole. The marks are paced to finish with the
			// frames, so that nothing is left to pop in.
			span := board.TweenDurationMS
			if ends := board.TweenFrameEndsMS; len(ends) != 0 {
				span = ends[len(ends)-1]
			}
			start := 0
			for i, frame := range board.TweenFrames {
				now.attach(frame, plan, float64(start)/float64(span))
				start = board.TweenFrameEndMS(i)
			}
			now.restore(board)
			previous = board
		}
		for kind, children := range [][]*d2target.Diagram{board.Layers, board.Scenarios, board.Steps} {
			for _, child := range children {
				if err := walk(child, kind == 0); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(root, true)
}

// alignCaptions pins every board's caption to one lower third. An animation
// shows all of its boards in a single viewport; without a shared anchor the
// caption would jump whenever a step changed the diagram's extent.
func alignCaptions(root *d2target.Diagram) {
	boards := animationBoards(root)
	captioned := false
	for _, board := range boards {
		captioned = captioned || board.Root.Annotation != nil
	}
	if !captioned {
		return
	}
	first := true
	var left, right, bottom int
	for _, board := range boards {
		if len(board.Shapes) == 0 {
			continue
		}
		tl, br := board.InkBoundingBox()
		if first {
			left, right, bottom, first = tl.X, br.X, br.Y, false
			continue
		}
		left, right, bottom = min(left, tl.X), max(right, br.X), max(bottom, br.Y)
	}
	if first {
		return
	}
	anchor := &d2target.Point{X: (left + right) / 2, Y: bottom + d2target.CaptionGap}
	for _, board := range boards {
		for _, caption := range []**d2target.Annotation{&board.Root.Annotation, &board.Root.LeavingAnnotation} {
			if *caption != nil {
				pinned := **caption
				pinned.Anchor = anchor
				*caption = &pinned
			}
		}
	}
}

// animationBoards lists every board an animation shows, each followed by the
// frames of its incoming transition.
func animationBoards(root *d2target.Diagram) []*d2target.Diagram {
	var boards []*d2target.Diagram
	var walk func(*d2target.Diagram)
	walk = func(board *d2target.Diagram) {
		if !board.IsFolderOnly {
			boards = append(boards, board)
			boards = append(boards, board.TweenFrames...)
		}
		for _, children := range [][]*d2target.Diagram{board.Layers, board.Scenarios, board.Steps} {
			for _, child := range children {
				walk(child)
			}
		}
	}
	walk(root)
	return boards
}

// shareCanvas gives every board and frame of an annotated animation the same
// canvas. A renderer paints a board's background over that board's own
// bounds, and a note that sticks out changes those bounds, so without a
// shared canvas the background would resize on almost every step. It shows
// on any theme whose canvas is not the color of the page behind it.
func shareCanvas(root *d2target.Diagram) {
	boards := animationBoards(root)
	annotated := false
	for _, board := range boards {
		if boardIsAnnotated(board) {
			annotated = true
			break
		}
	}
	if !annotated {
		return
	}
	first := true
	var tl, br d2target.Point
	for _, board := range boards {
		if len(board.Shapes) == 0 {
			continue
		}
		btl, bbr := board.BoundingBox()
		if first {
			tl, br, first = btl, bbr, false
			continue
		}
		tl.X, tl.Y = min(tl.X, btl.X), min(tl.Y, btl.Y)
		br.X, br.Y = max(br.X, bbr.X), max(br.Y, bbr.Y)
	}
	if first || br.X <= tl.X || br.Y <= tl.Y {
		return
	}
	for _, board := range boards {
		board.Canvas = &d2target.MotionBox{Point: tl, Width: br.X - tl.X, Height: br.Y - tl.Y}
	}
}

func boardIsAnnotated(board *d2target.Diagram) bool {
	if board.Root.Annotation != nil || board.Root.LeavingAnnotation != nil {
		return true
	}
	for i := range board.Shapes {
		if board.Shapes[i].Annotation != nil || board.Shapes[i].LeavingAnnotation != nil {
			return true
		}
	}
	for i := range board.Connections {
		if board.Connections[i].Annotation != nil || board.Connections[i].LeavingAnnotation != nil {
			return true
		}
	}
	return false
}
