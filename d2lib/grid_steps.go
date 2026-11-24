package d2lib

import (
	"context"
	"fmt"

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
	count := d2target.TweenFrameCount(duration)
	cost := gridFrameElements(prev) + gridFrameElements(next)
	if cost > *remaining/count {
		return fmt.Errorf("grid animation exceeds the limit of %d sampled geometry elements", maxGridSampleElements)
	}
	*remaining -= cost * count
	prev.IsFolderOnly, next.IsFolderOnly = false, false
	frames := make([]*d2target.Diagram, count)
	for i := range frames {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Reuse intermediate paint and variable values when the same step
		// also has an authored property tween.
		base := next
		if len(next.TweenFrames) != 0 {
			base = next.TweenFrames[i]
		}
		frames[i] = motion.sample(base, float64(i)/float64(count))
	}
	next.TweenFrames, next.TweenDurationMS, next.GridMotion = frames, duration, true
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
