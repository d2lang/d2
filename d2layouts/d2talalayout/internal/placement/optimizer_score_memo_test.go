package placement

import (
	"context"
	"fmt"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/placementcost"
	"github.com/d2lang/d2/lib/geo"
)

func init() {
	scoreMemoHitCheck = checkScoreMemoHit
}

// checkScoreMemoHit compares a memo hit for node at its current position
// with a fresh evaluation, when stats ask for it: stored scores must be
// bit-identical, and a lower bound must not exceed the score nor discard
// where the score does not.
func checkScoreMemoHit(stats *scoreMemoStats, node *layoutgraph.Node, edgeAbductions []*layoutgraph.EdgeAbduction, known optimizerScoreSlot, bound, offset float64) {
	if stats == nil || !stats.verify || known.level == scoreMemoEmpty {
		return
	}
	ctx := context.Background()
	full, err := placementcost.NodeEdgeLength(ctx, node, placementcost.EdgeLengthOptions{EdgeAbductions: edgeAbductions, IncludeNodeSizes: true, PenalizeDirection: true})
	mismatch := ""
	switch {
	case err != nil:
		mismatch = err.Error()
	case known.level == scoreMemoLowerBound:
		if known.score > full {
			mismatch = fmt.Sprintf("lower bound %v above score %v", known.score, full)
		} else if !math.IsInf(bound, 1) && geo.PrecisionCompare(known.score-offset, bound, geo.PRECISION) == 1 &&
			geo.PrecisionCompare(full-offset, bound, geo.PRECISION) != 1 {
			mismatch = fmt.Sprintf("lower bound %v discards, score %v does not", known.score, full)
		}
	case math.Float64bits(known.score) != math.Float64bits(full):
		mismatch = fmt.Sprintf("stored score %v, fresh %v", known.score, full)
	case known.level == scoreMemoAll:
		columnCrossing, err1 := placementcost.ColumnCrossingCost(ctx, node, edgeAbductions)
		symmetry, err2 := placementcost.NodeSymmetry(ctx, node, edgeAbductions)
		if err1 != nil || err2 != nil || math.Float64bits(columnCrossing) != math.Float64bits(known.columnCrossing) ||
			math.Float64bits(symmetry) != math.Float64bits(known.symmetry) {
			mismatch = fmt.Sprintf("stored (%v, %v), fresh (%v, %v)", known.columnCrossing, known.symmetry, columnCrossing, symmetry)
		}
	}
	if mismatch != "" {
		if stats.mismatches == 0 {
			stats.firstMismatch = fmt.Sprintf("node %d at (%v, %v): %s", node.ID, node.TopLeft.X, node.TopLeft.Y, mismatch)
		}
		stats.mismatches++
	}
}
