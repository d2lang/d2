package hierarchy

import (
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

// clearLoopRanks runs in drawing coordinates after transposition and mirroring.
// Advance later ranks just enough for the full loop envelope to clear a
// neighboring rank, leaving the order and contents of each rank intact.
func clearLoopRanks(g *layoutgraph.Graph, levels map[int][]*placementNode, direction geo.Orientation, guard *limits.WorkGuard) error {
	horizontal := IsHorizontal(g.Nodes)
	reverse := horizontal && direction == geo.Left || !horizontal && direction == geo.Top
	previousEnd := math.Inf(-1)
	previousHadLoops := false
	shift := 0.0
	for level := 0; level < len(levels); level++ {
		start, end := math.Inf(1), math.Inf(-1)
		hasLoops := false
		for _, pn := range levels[level] {
			if err := guard.Step(); err != nil {
				return err
			}
			if pn.isDummy {
				continue
			}
			node := pn.graphNode
			tl, br := node.BoundingBox(nil)
			lo, hi := tl.Y, br.Y
			if horizontal {
				lo, hi = tl.X, br.X
			}
			if reverse {
				lo, hi = -hi, -lo
			}
			start, end = math.Min(start, lo), math.Max(end, hi)
			hasLoops = hasLoops || len(node.LoopOffsets) > 0
		}
		if math.IsInf(start, 1) {
			continue
		}
		if hasLoops || previousHadLoops {
			shift = math.Max(shift, previousEnd+layoutgraph.MinPortClearance-start)
		}
		previousEnd, previousHadLoops = end+shift, hasLoops
		if shift == 0 {
			continue
		}
		dx, dy := 0.0, shift
		if reverse {
			dy = -dy
		}
		if horizontal {
			dx, dy = dy, 0
		}
		for _, pn := range levels[level] {
			if err := guard.Step(); err != nil {
				return err
			}
			if pn.isDummy {
				continue
			}
			node := pn.graphNode
			node.Translate(dx, dy)
			if node.IsContainer() {
				children, err := g.AllDescendantNodesWithWorkGuard(node, true, guard)
				if err != nil {
					return err
				}
				for _, child := range children {
					if err := guard.Step(); err != nil {
						return err
					}
					child.Translate(dx, dy)
				}
			}
		}
	}
	return guard.Finish()
}
