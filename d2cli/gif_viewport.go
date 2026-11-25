package d2cli

import (
	"image"
	"slices"

	"github.com/d2lang/d2/d2target"
)

type gifViewport struct {
	bounds image.Rectangle // unpadded world coordinates
	canvas image.Rectangle // actual rendered extents in pixels, including decorations
}

// Related frames must agree on the screen position of a world coordinate.
// Layers and independently laid out steps can still fit on their own. Grid
// animations already share a viewport across the full playback sequence.
func gifViewports(root *d2target.Diagram, boards []*d2target.Diagram) map[*d2target.Diagram]*gifViewport {
	grid := slices.ContainsFunc(boards, func(b *d2target.Diagram) bool { return b.GridMotion })
	if !grid && !slices.ContainsFunc(boards, func(b *d2target.Diagram) bool { return b.LayoutContinuity }) {
		return nil
	}
	viewports := make(map[*d2target.Diagram]*gifViewport)
	if grid {
		v := new(gifViewport)
		for _, board := range boards {
			viewports[board] = v
			for _, sample := range board.TweenFrames {
				viewports[sample] = v
			}
		}
		return viewports
	}
	link := func(base, child *d2target.Diagram) {
		if child.LayoutContinuity && !base.IsFolderOnly {
			if viewports[base] == nil {
				viewports[base] = new(gifViewport)
			}
			viewports[child] = viewports[base]
			// Samples belong to the incoming transition. A board that only
			// anchors a later step can have independent incoming samples.
			for _, sample := range child.TweenFrames {
				viewports[sample] = viewports[base]
			}
		}
	}
	var visit func(*d2target.Diagram)
	visit = func(b *d2target.Diagram) {
		for _, layer := range b.Layers {
			visit(layer)
		}
		for _, scenario := range b.Scenarios {
			link(b, scenario)
			visit(scenario)
		}
		base := b
		for _, step := range b.Steps {
			link(base, step)
			visit(step)
			base = step
		}
	}
	visit(root)
	return viewports
}
