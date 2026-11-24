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

// Grid placement is only known after both authored endpoints have been laid out.
func sampleGridSteps(ctx context.Context, root *d2target.Diagram, graph *d2graph.Graph, duration int) error {
	remaining := maxGridSampleElements
	var walk func(*d2target.Diagram, *d2graph.Graph) error
	walk = func(parent *d2target.Diagram, g *d2graph.Graph) error {
		if err := ctx.Err(); err != nil {
			return err
		}
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
			if err := sampleGridStep(ctx, prev, next, prevGraph, nextGraph, duration, &remaining); err != nil {
				return err
			}
			if err := walk(next, nextGraph); err != nil {
				return err
			}
			prev, prevGraph = next, nextGraph
		}
		return nil
	}
	return walk(root, graph)
}

func sampleGridStep(ctx context.Context, prev, next *d2target.Diagram, prevGraph, nextGraph *d2graph.Graph, duration int, remaining *int) error {
	// A folder contributes no playback slot. An explicitly empty grid, or a
	// step removing the last cell, does have an observable animation state.
	if prev.IsFolderOnly && (len(prev.Shapes) != 0 || !prevGraph.Root.IsGridDiagram()) {
		return nil
	}
	if next.IsFolderOnly && len(next.Shapes) != 0 {
		return nil
	}
	motion := newGridMotion(prev, next, prevGraph, nextGraph)
	if motion == nil {
		return nil
	}
	bounds := motion.sampleBoundaries(duration, len(next.TweenFrames))
	count := len(bounds) - 1
	cost := gridFrameElements(prev) + gridFrameElements(next)
	if motion.handoffs != nil {
		cost += 3 * len(motion.handoffs.pairs)
	}
	if cost > *remaining/count {
		return fmt.Errorf("grid animation exceeds the limit of %d sampled geometry elements", maxGridSampleElements)
	}
	*remaining -= cost * count
	prev.IsFolderOnly, next.IsFolderOnly = false, false
	frames := make([]*d2target.Diagram, count)
	for i, ms := range bounds[:count] {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Reuse intermediate paint and variable values when the same step
		// also has an authored property tween.
		base := next
		if len(next.TweenFrames) != 0 {
			// Uniform style sample j starts at floor(j*duration/n).
			j := ((ms+1)*len(next.TweenFrames) - 1) / duration
			base = next.TweenFrames[j]
		}
		frames[i] = motion.sample(base, ms, duration)
	}
	next.TweenFrames, next.TweenDurationMS, next.GridMotion = frames, duration, true
	next.TweenFrameEndsMS = bounds[1:]
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
