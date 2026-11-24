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
const maxGridSampleElements = 1_000_000

type gridStepPlan struct {
	board  *d2target.Diagram
	motion *gridMotion
	bounds []int
	cost   int // Geometry elements per sample.
}

// Grid placement is only known after both authored endpoints have been laid out.
func sampleGridSteps(ctx context.Context, root *d2target.Diagram, graph *d2graph.Graph, duration, boardLimit int) error {
	var plans []gridStepPlan
	available := boardLimit
	var walk func(*d2target.Diagram, *d2graph.Graph) error
	walk = func(parent *d2target.Diagram, g *d2graph.Graph) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		available -= 1 + len(parent.TweenFrames)
		for i, layer := range parent.Layers {
			if err := walk(layer, g.Layers[i]); err != nil {
				return err
			}
		}
		for i, scenario := range parent.Scenarios {
			if err := walk(scenario, g.Scenarios[i]); err != nil {
				return err
			}
		}
		prev, prevGraph := parent, g
		for i, next := range parent.Steps {
			nextGraph := g.Steps[i]
			if plan, ok := planGridStep(prev, next, prevGraph, nextGraph, duration); ok {
				plans = append(plans, plan)
				// These samples supply the paint for the replacement grid frames.
				available += len(next.TweenFrames)
				prev.IsFolderOnly, next.IsFolderOnly = false, false
			}
			if err := walk(next, nextGraph); err != nil {
				return err
			}
			prev, prevGraph = next, nextGraph
		}
		return nil
	}
	if err := walk(root, graph); err != nil {
		return err
	}
	if len(plans) == 0 {
		return nil
	}
	if available < len(plans) {
		return fmt.Errorf("grid animation needs at least one sample per transition within the limit of %d total boards", boardLimit)
	}
	if err := fitGridSamples(plans, available, maxGridSampleElements); err != nil {
		return err
	}
	for i := range plans {
		if err := plans[i].sample(ctx, duration); err != nil {
			return err
		}
		plans[i] = gridStepPlan{}
	}
	return nil
}

// Reserve one starting sample per transition. Share the remaining slots in
// proportion to the desired schedules, accounting for the geometry each
// sample copies before allocating frames.
func fitGridSamples(plans []gridStepPlan, available, geometryLimit int) error {
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
		return fmt.Errorf("grid animation exceeds the limit of %d sampled geometry elements", geometryLimit)
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

func planGridStep(prev, next *d2target.Diagram, prevGraph, nextGraph *d2graph.Graph, duration int) (gridStepPlan, bool) {
	// A folder contributes no playback slot. An explicitly empty grid, or a
	// step removing the last cell, does have an observable animation state.
	if prev.IsFolderOnly && (len(prev.Shapes) != 0 || !prevGraph.Root.IsGridDiagram()) {
		return gridStepPlan{}, false
	}
	if next.IsFolderOnly && len(next.Shapes) != 0 {
		return gridStepPlan{}, false
	}
	motion := newGridMotion(prev, next, prevGraph, nextGraph)
	if motion == nil {
		return gridStepPlan{}, false
	}
	cost := gridFrameElements(prev) + gridFrameElements(next)
	if motion.handoffs != nil {
		cost += 3 * len(motion.handoffs.pairs)
	}
	return gridStepPlan{board: next, motion: motion, bounds: motion.sampleBoundaries(duration, len(next.TweenFrames)), cost: cost}, true
}

func (p gridStepPlan) sample(ctx context.Context, duration int) error {
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
			j := ((ms+1)*len(base.TweenFrames) - 1) / duration
			base = base.TweenFrames[j]
		}
		frames[i] = p.motion.sample(base, ms, duration)
	}
	p.board.TweenFrames, p.board.TweenDurationMS, p.board.GridMotion = frames, duration, true
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
