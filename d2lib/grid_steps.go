package d2lib

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
)

// A single source may have many large steps. Bound the total geometry copied
// into render targets before allocating it, including the points of routes.
const maxBoardSampleElements = 1_000_000

type boardTransitionPlan struct {
	board    *d2target.Diagram
	motion   *gridMotion
	ink      *boardInk
	from     *d2target.Diagram // logical predecessor required for structural effects
	reveal   *boardReveal
	outline  *boardOutline
	transfer *boardTransfer
	bounds   []int
	cost     int // Geometry elements per sample.
}

// Compare successive drawable boards for ink changes after layout. Grid moves
// are still determined by step inheritance, independently of playback order.
func sampleBoardTransitions(ctx context.Context, root *d2target.Diagram, graph *d2graph.Graph, duration, boardLimit int, sketch, structural bool) error {
	// Discover empty grid endpoints before walking playback order, since those
	// boards gain a visible slot when cells enter or leave them.
	stepPlans := make(map[*d2target.Diagram]boardTransitionPlan)
	var visit func(*d2target.Diagram, *d2graph.Graph) error
	visit = func(board *d2target.Diagram, g *d2graph.Graph) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		prev, prevGraph := board, g
		for i, next := range board.Steps {
			nextGraph := g.Steps[i]
			if motion, cost := planGridStep(prev, next, prevGraph, nextGraph); motion != nil {
				prev.IsFolderOnly, next.IsFolderOnly = false, false
				stepPlans[next] = boardTransitionPlan{motion: motion, cost: cost}
			} else if structural && next.LayoutContinuity && !prev.IsFolderOnly && !next.IsFolderOnly && len(next.TweenFrames) == 0 {
				p := boardTransitionPlan{from: prev, cost: gridFrameElements(prev) + gridFrameElements(next)}
				p.transfer = newBoardTransfer(prev, next, prevGraph, nextGraph)
				if p.transfer == nil {
					p.reveal = planBoardReveal(prev, next, nextGraph)
					if !sketch {
						p.outline = planBoardOutline(prev, next, prevGraph, nextGraph)
					}
					if p.outline != nil && p.outline.needsReveal && p.reveal == nil {
						p.outline = nil
					}
				}
				if p.reveal != nil || p.outline != nil || p.transfer != nil {
					stepPlans[next] = p
				}
			}
			prev, prevGraph = next, nextGraph
		}
		for i, children := range [][]*d2target.Diagram{board.Layers, board.Scenarios, board.Steps} {
			graphs := [][]*d2graph.Graph{g.Layers, g.Scenarios, g.Steps}[i]
			for j, child := range children {
				if err := visit(child, graphs[j]); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(root, graph); err != nil {
		return err
	}
	return samplePlannedTransitions(ctx, root, duration, boardLimit, sketch, false, stepPlans)
}

// PrepareSketchTransitions adds pencil samples to a compiled diagram when a
// caller enables sketch at render time. It modifies the supplied board tree.
func PrepareSketchTransitions(ctx context.Context, root *d2target.Diagram, duration int64) error {
	if duration < 0 || duration > d2target.MaxTweenDurationMS {
		return fmt.Errorf("unsupported animation duration %d", duration)
	}
	if duration == 0 {
		return nil
	}
	if err := d2target.ValidateRenderTarget(root); err != nil {
		return err
	}
	return samplePlannedTransitions(ctx, root, int(duration), d2target.MaxRenderTargetBoards, true, true, nil)
}

func samplePlannedTransitions(ctx context.Context, root *d2target.Diagram, duration, boardLimit int, sketch, pencilOnly bool, stepPlans map[*d2target.Diagram]boardTransitionPlan) error {
	var plans []boardTransitionPlan
	available := boardLimit
	var previous *d2target.Diagram
	var walk func(*d2target.Diagram) error
	walk = func(board *d2target.Diagram) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		available -= 1 + len(board.TweenFrames)
		if !board.IsFolderOnly {
			p := stepPlans[board]
			if p.from != nil && p.from != previous {
				// A nested layer/scenario can interrupt cumulative step playback.
				// Only draw a structural transition from the board on screen.
				p = boardTransitionPlan{}
			}
			p.ink = planBoardInk(previous, board, sketch, pencilOnly)
			if p.motion != nil || p.ink != nil || p.from != nil {
				p.board = board
				count := len(board.TweenFrames)
				if p.from != nil {
					// Give a short arrival or landing enough distinct samples to
					// show its acceleration. The existing per-board and total
					// geometry limits still apply when many steps play together.
					count = min(d2target.MaxTweenFrames, (60*duration+999)/1000)
				} else if p.ink != nil {
					count = d2target.TweenFrameCount(duration)
				}
				if p.motion != nil {
					p.bounds = p.motion.sampleBoundaries(duration, count)
				} else {
					p.cost = max(p.cost, gridFrameElements(board))
					for _, frame := range board.TweenFrames {
						p.cost = max(p.cost, gridFrameElements(frame))
					}
					p.bounds = inkBoundaries(board, duration, count)
				}
				plans = append(plans, p)
				available += len(board.TweenFrames)
			}
			previous = board
		}
		for _, children := range [][]*d2target.Diagram{board.Layers, board.Scenarios, board.Steps} {
			for _, child := range children {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return err
	}
	if len(plans) == 0 {
		return nil
	}
	if available < len(plans) {
		return fmt.Errorf("animation needs at least one sample per transition within the limit of %d total boards", boardLimit)
	}
	if err := fitBoardSamples(plans, available, maxBoardSampleElements); err != nil {
		return err
	}
	for i := range plans {
		if err := plans[i].sample(ctx, duration); err != nil {
			return err
		}
		plans[i] = boardTransitionPlan{}
	}
	return nil
}

// Keep both the pencil schedule and the timing of any precompiled grid moves.
// If their union exceeds the per-board budget, reserve half the sample slots
// for pencil strokes across the duration and spread the rest over grid events.
func inkBoundaries(board *d2target.Diagram, duration, count int) []int {
	uniform := make([]int, count+1)
	for i := range uniform {
		uniform[i] = int(int64(i) * int64(duration) / int64(count))
	}
	if len(board.TweenFrameEndsMS) == 0 {
		return uniform
	}

	bounds := slices.Clone(uniform)
	for _, end := range board.TweenFrameEndsMS {
		if end < duration {
			bounds = append(bounds, end)
		}
	}
	slices.Sort(bounds)
	bounds = slices.Compact(bounds)
	if len(bounds) <= d2target.MaxTweenFrames+1 {
		return bounds
	}

	anchors := make([]int, 0, d2target.MaxTweenFrames+1)
	for i, ms := range uniform {
		if i%2 == 0 || i == count {
			anchors = append(anchors, ms)
		}
	}
	addWithinBudget := func(times []int) {
		var missing []int
		for _, ms := range times {
			if _, exists := slices.BinarySearch(anchors, ms); ms < duration && !exists {
				missing = append(missing, ms)
			}
		}
		keep := min(len(missing), d2target.MaxTweenFrames+1-len(anchors))
		for i := 0; i < keep; i++ {
			anchors = append(anchors, missing[i*len(missing)/keep])
		}
		slices.Sort(anchors)
	}
	addWithinBudget(board.TweenFrameEndsMS)
	addWithinBudget(uniform)
	return anchors
}

// Reserve one starting sample per transition. Share the remaining slots in
// proportion to the desired schedules, accounting for the geometry each
// sample copies before allocating frames.
func fitBoardSamples(plans []boardTransitionPlan, available, geometryLimit int) error {
	extraNeeded := 0
	geometryLeft := int64(geometryLimit)
	var extraGeometry int64
	for _, p := range plans {
		extra := len(p.bounds) - 2
		extraNeeded += extra
		geometryLeft -= int64(p.cost)
		extraGeometry += int64(extra) * int64(p.cost)
	}
	if geometryLeft < 0 {
		return fmt.Errorf("animation exceeds the limit of %d sampled geometry elements", geometryLimit)
	}
	extraAvailable := min(extraNeeded, available-len(plans))
	var remainder int64
	for i := range plans {
		p := &plans[i]
		wanted := len(p.bounds) - 1
		count := wanted
		if extraAvailable < extraNeeded {
			share := int64(wanted-1)*int64(extraAvailable) + remainder
			count = 1 + int(share/int64(extraNeeded))
			remainder = share % int64(extraNeeded)
		}
		if geometryLeft < extraGeometry {
			count = min(count, 1+int(int64(wanted-1)*geometryLeft/extraGeometry))
		}
		geometryLeft -= int64(count-1) * int64(p.cost)
		extraGeometry -= int64(wanted-1) * int64(p.cost)
		// Thin the existing nonuniform schedule so quick pops still receive
		// more samples per second than the rest of a long step.
		if count < wanted {
			for i := 1; i <= count; i++ {
				p.bounds[i] = p.bounds[i*wanted/count]
			}
			p.bounds = p.bounds[:count+1]
		}
	}
	return nil
}

func planGridStep(prev, next *d2target.Diagram, prevGraph, nextGraph *d2graph.Graph) (*gridMotion, int) {
	// A folder contributes no playback slot. An explicitly empty grid, or a
	// step removing the last cell, does have an observable animation state.
	if prev.IsFolderOnly && (len(prev.Shapes) != 0 || !prevGraph.Root.IsGridDiagram()) {
		return nil, 0
	}
	if next.IsFolderOnly && len(next.Shapes) != 0 {
		return nil, 0
	}
	motion := newGridMotion(prev, next, prevGraph, nextGraph)
	if motion == nil {
		return nil, 0
	}
	cost := gridFrameElements(prev) + gridFrameElements(next)
	if motion.handoffs != nil {
		cost += 3 * len(motion.handoffs.pairs)
	}
	return motion, cost
}

func (p boardTransitionPlan) sample(ctx context.Context, duration int) error {
	frames := make([]*d2target.Diagram, len(p.bounds)-1)
	for i, ms := range p.bounds[:len(frames)] {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Reuse intermediate paint and variable values when the same step
		// also has an authored property tween.
		base := p.board
		if len(base.TweenFrames) != 0 {
			// Uniform style sample j starts at floor(j*duration/n).
			var j int
			if len(base.TweenFrameEndsMS) > 0 {
				j = sort.SearchInts(base.TweenFrameEndsMS, ms+1)
			} else {
				j = int((int64(ms+1)*int64(len(base.TweenFrames)) - 1) / int64(base.TweenDurationMS))
			}
			if j < len(base.TweenFrames) {
				base = base.TweenFrames[j]
			}
		}
		progress := float64(ms) / float64(duration)
		if p.from != nil && ms == 0 {
			// The first visible frame is exactly the board being left.
			frame := *p.from
			frame.Steps, frame.Layers, frame.Scenarios, frame.TweenFrames = nil, nil, nil, nil
			frame.TweenDurationMS, frame.TweenFrameEndsMS, frame.GridMotion = 0, nil, false
			frame.Shapes = slices.Clone(frame.Shapes)
			frame.Connections = slices.Clone(frame.Connections)
			frames[i] = &frame
			continue
		}
		if p.motion != nil {
			frames[i] = p.motion.sample(base, ms, duration)
		} else if p.transfer != nil {
			frames[i] = p.transfer.sample(base, progress)
		} else {
			sample := *base
			sample.Layers, sample.Scenarios, sample.Steps, sample.TweenFrames = nil, nil, nil, nil
			sample.TweenDurationMS, sample.TweenFrameEndsMS, sample.GridMotion = 0, nil, false
			if p.from != nil || p.ink != nil && len(p.ink.shapes) != 0 {
				sample.Shapes = slices.Clone(base.Shapes)
			}
			frames[i] = &sample
			if p.reveal != nil {
				p.reveal.apply(&sample, progress)
			}
			if p.outline != nil {
				p.outline.apply(&sample, progress)
			}
		}
		if p.ink != nil {
			// Grid motion may share an unchanged edge slice with its authored endpoint.
			if len(p.ink.edges) > 0 {
				frames[i].Connections = slices.Clone(frames[i].Connections)
			}
			p.ink.apply(frames[i], progress)
		}
	}
	p.board.TweenFrames, p.board.TweenDurationMS, p.board.GridMotion = frames, duration, p.motion != nil || p.board.GridMotion
	p.board.TweenFrameEndsMS = p.bounds[1:]
	return nil
}

func gridFrameElements(d *d2target.Diagram) int {
	n := len(d.Shapes) + len(d.Connections)
	for i := range d.Shapes {
		n += len(d.Shapes[i].Columns)
	}
	for i := range d.Connections {
		n += len(d.Connections[i].Route)
	}
	return n
}

// Sample quick membership changes about every display frame, even on long steps.
// Reserve the remaining frame budget for full-step paint and position changes.
func (m *gridMotion) sampleBoundaries(duration, paintCount int) []int {
	const period = 17 // milliseconds, approximately 60 fps
	enterStart, enterEnd, exitEnd := m.popWindows(duration)
	endMS := duration
	if !m.moves && paintCount == 0 {
		// Once a pure pop settles, return playback to the board so its own
		// independent animations can continue during the remaining hold.
		endMS = max(enterEnd, exitEnd)
	}
	// Preserve the fast label roll and each synchronized band pop on long steps.
	windows := [][2]int{{0, exitEnd}, {enterStart, enterEnd}}
	if m.handoffs != nil {
		end := min(gridHandoffMS, duration)
		endMS = max(endMS, end)
		windows = append(windows, [2]int{0, end})
	}
	var seenBands [3]bool
	for _, band := range m.bands {
		if !seenBands[band] {
			start, end := band.window(duration)
			windows = append(windows, [2]int{start, end})
			seenBands[band] = true
		}
	}
	events := []int{0, endMS}
	for _, w := range windows {
		events = append(events, w[0], w[1])
	}
	slices.Sort(events)
	events = slices.Compact(events)
	var times []int
	for i, start := range events[:len(events)-1] {
		end := events[i+1]
		count := 1
		for _, w := range windows {
			if start >= w[0] && start < w[1] {
				count = (end - start + period - 1) / period
				break
			}
		}
		for j := 0; j < count; j++ {
			times = append(times, start+j*(end-start)/count)
		}
	}

	// Retain the original paint boundaries where possible. On long transitions
	// spread the available samples over the entire paint tween.
	count := min(paintCount, d2target.MaxTweenFrames-len(times))
	for j := 0; j < count; j++ {
		paintIndex := j * paintCount / count
		times = append(times, paintIndex*duration/paintCount)
	}
	slices.Sort(times)
	times = slices.Compact(times)
	if !m.moves {
		return append(times, endMS)
	}

	// Subdivide the remaining gaps evenly. Increase spacing only when needed
	// to stay within the existing per-transition limit.
	anchors := append(times, duration)
	frameCount := func(spacing int) int {
		n := 0
		for i, t := range times {
			n += (anchors[i+1] - t + spacing - 1) / spacing
		}
		return n
	}
	spacing := period
	if frameCount(spacing) > d2target.MaxTweenFrames {
		spacing += sort.Search(duration, func(extra int) bool {
			return frameCount(period+extra) <= d2target.MaxTweenFrames
		})
	}
	samples := make([]int, 0, frameCount(spacing)+1)
	for i, start := range times {
		gap := anchors[i+1] - start
		n := (gap + spacing - 1) / spacing
		for j := 0; j < n; j++ {
			samples = append(samples, start+j*gap/n)
		}
	}
	return append(samples, duration)
}
