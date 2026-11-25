package d2cli

import (
	"image"
	"slices"
	"strconv"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
)

type gifViewport struct {
	bounds image.Rectangle // unpadded world coordinates
	canvas image.Rectangle // actual rendered extents in pixels, including decorations
}

// TALA leaves explicitly pinned boards in the author's coordinate system.
// Record that relationship after layout and transition planning, before --target
// selects a subtree. Independent layers always start with their own camera.
func markPinnedGIFBoards(board *d2target.Diagram, graph *d2graph.Graph) {
	for i, layer := range board.Layers {
		markPinnedGIFBoards(layer, graph.Layers[i])
	}
	for i, scenario := range board.Scenarios {
		child := graph.Scenarios[i]
		scenario.LayoutContinuity = scenario.LayoutContinuity || sharePinnedCoordinates(graph, child)
		markPinnedGIFBoards(scenario, child)
	}
	previous := graph
	for i, step := range board.Steps {
		child := graph.Steps[i]
		step.LayoutContinuity = step.LayoutContinuity || sharePinnedCoordinates(previous, child)
		markPinnedGIFBoards(step, child)
		previous = child
	}
}

func sharePinnedCoordinates(from, to *d2graph.Graph) bool {
	// One honored top-level pin establishes the author's coordinate system,
	// even if successive boards replace every object, including the pin.
	hasAbsolutePin := func(g *d2graph.Graph) bool {
		for _, obj := range g.Root.Children {
			if obj.Left == nil || obj.Top == nil || obj.Box == nil || obj.TopLeft == nil {
				continue
			}
			x, errX := strconv.Atoi(obj.Left.Value)
			y, errY := strconv.Atoi(obj.Top.Value)
			if errX == nil && errY == nil && obj.TopLeft.X == float64(x) && obj.TopLeft.Y == float64(y) {
				return true
			}
		}
		return false
	}
	if !hasAbsolutePin(from) || !hasAbsolutePin(to) {
		return false
	}
	for _, sample := range to.TweenFrames {
		if !hasAbsolutePin(sample) {
			return false
		}
	}
	return true
}

// Related frames must agree on the screen position of a world coordinate.
// Independent layers and re-laid-out steps can fit on their own.
func gifViewports(root *d2target.Diagram, boards []*d2target.Diagram) map[*d2target.Diagram]*gifViewport {
	if !slices.ContainsFunc(boards, func(b *d2target.Diagram) bool { return b.GridMotion || b.LayoutContinuity }) {
		return nil
	}
	viewports := make(map[*d2target.Diagram]*gifViewport)
	share := func(board *d2target.Diagram, v *gifViewport) {
		viewports[board] = v
		for _, sample := range board.TweenFrames {
			viewports[sample] = v
		}
	}
	// A selected subtree can start at the incoming transition itself.
	if (root.LayoutContinuity || root.GridMotion) && len(root.TweenFrames) > 0 {
		share(root, new(gifViewport))
	}
	link := func(base, child *d2target.Diagram, gridSteps bool) {
		if !base.IsFolderOnly && (child.LayoutContinuity || child.GridMotion || gridSteps && sameGIFPlacements(base, child)) {
			if viewports[base] == nil {
				viewports[base] = new(gifViewport)
			}
			share(child, viewports[base])
		}
	}
	var visit func(*d2target.Diagram)
	visit = func(b *d2target.Diagram) {
		for _, layer := range b.Layers {
			visit(layer)
		}
		for _, scenario := range b.Scenarios {
			link(b, scenario, false)
			visit(scenario)
		}
		// Paint-only steps and holds in a grid sequence keep the same camera
		// after a reflow (including one that removed the largest cell).
		gridSteps := slices.ContainsFunc(b.Steps, func(s *d2target.Diagram) bool { return s.GridMotion })
		base := b
		for _, step := range b.Steps {
			link(base, step, gridSteps)
			visit(step)
			base = step
		}
	}
	visit(root)
	return viewports
}

// A stationary grid may have an incoming style tween. Verify each sample
// before using its endpoint's world coordinates for the whole step.
func sameGIFPlacements(base, step *d2target.Diagram) bool {
	matches := func(board *d2target.Diagram) bool {
		if len(base.Shapes) != len(board.Shapes) {
			return false
		}
		for i := range base.Shapes {
			a, b := &base.Shapes[i], &board.Shapes[i]
			if a.ID != b.ID || a.Pos != b.Pos || a.Width != b.Width || a.Height != b.Height {
				return false
			}
		}
		return true
	}
	return matches(step) && !slices.ContainsFunc(step.TweenFrames, func(b *d2target.Diagram) bool { return !matches(b) })
}
