package placement

import (
	"context"
	"fmt"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/placementcost"
	"github.com/d2lang/d2/lib/geo"
)

// moveNodeScores are the pure placement scores computed for the graph's
// current geometry.
type moveNodeScores struct {
	valid, hasEdgeLength, hasSymmetry  bool
	edgeLength, crossingCost, symmetry float64
}

// moveNodeToBest chooses the candidate with the lowest placement cost using
// the compaction stage's shared work guard.
func moveNodeToBest(ctx context.Context, graph *layoutgraph.Graph, node *layoutgraph.Node, points []*geo.Point, edgeAbductions []*layoutgraph.EdgeAbduction, includeSizes bool, guard *limits.OptimizationWorkGuard) (bool, error) {
	if guard == nil {
		return false, fmt.Errorf("TALA moveNodeToBest requires a work guard")
	}
	if err := guard.Finish(); err != nil {
		return false, err
	}

	symmetryCost := graph.CellSize * float64(len(node.Edges))
	leastDistance := math.Inf(1)
	leastDistancePoint := node.TopLeft.Copy()
	currentX, currentY := node.TopLeft.X, node.TopLeft.Y

	fixedOrigin, err := optimizerFixedOrigin(graph, node.EffectiveContainer(), guard)
	if err != nil {
		return false, err
	}
	if fixedOrigin != nil && !includeSizes {
		fixedOrigin.X = math.Round(fixedOrigin.X / graph.CellSize)
		fixedOrigin.Y = math.Round(fixedOrigin.Y / graph.CellSize)
	}

	// Candidate lists end with the current position, which is usually also
	// the last generated candidate. optimizerMoveNodeAbs leaves the graph
	// untouched when the node is already exactly at the point, so when the
	// previous iteration computed scores and this one moves nothing, the
	// geometry is the one those pure scores were computed for and they are
	// reused. Every work charge and comparison still runs.
	var previous moveNodeScores
	for _, point := range points {
		if err := guard.Step(); err != nil {
			return false, err
		}
		if point == nil {
			return false, fmt.Errorf("TALA %s found a nil placement point", guard.Location())
		}
		x, y := point.X, point.Y
		if fixedOrigin != nil && (x < fixedOrigin.X || y < fixedOrigin.Y) {
			previous.valid = false
			continue
		}
		canMove := x == currentX && y == currentY
		if !canMove {
			canMove, err = optimizerCanMove(node, point, includeSizes, guard)
			if err != nil {
				return false, err
			}
		}
		if !canMove {
			previous.valid = false
			continue
		}
		unmoved := node.TopLeft != nil && node.TopLeft.X == x && node.TopLeft.Y == y
		if err := optimizerMoveNodeAbs(node, x, y, guard); err != nil {
			return false, err
		}
		if err := chargeOptimizerScoring(node, edgeAbductions, includeSizes, guard); err != nil {
			return false, err
		}
		if !previous.valid || !unmoved {
			previous = moveNodeScores{valid: true}
		}
		if !previous.hasEdgeLength {
			previous.edgeLength, err = placementcost.NodeEdgeLength(ctx, node, placementcost.EdgeLengthOptions{EdgeAbductions: edgeAbductions, IncludeNodeSizes: includeSizes, EnforceMinimumGap: false, PenalizeDirection: true})
			if err != nil {
				return false, err
			}
			previous.hasEdgeLength = true
		}
		edgeLength := previous.edgeLength
		if includeSizes {
			if !math.IsInf(leastDistance, 1) && geo.PrecisionCompare(edgeLength-symmetryCost, leastDistance, geo.PRECISION) == 1 {
				continue
			}
			if !previous.hasSymmetry {
				previous.crossingCost, err = placementcost.ColumnCrossingCost(ctx, node, edgeAbductions)
				if err != nil {
					return false, err
				}
				previous.symmetry, err = placementcost.NodeSymmetry(ctx, node, edgeAbductions)
				if err != nil {
					return false, err
				}
				previous.hasSymmetry = true
			}
			edgeLength += previous.crossingCost
			edgeLength -= previous.symmetry * symmetryCost
		}
		switch geo.PrecisionCompare(edgeLength, leastDistance, geo.PRECISION) {
		case -1:
			leastDistance, leastDistancePoint = edgeLength, point
		case 0:
			if point.X == currentX && point.Y == currentY {
				leastDistance, leastDistancePoint = edgeLength, point
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("EdgeLength: %w", err)
	}
	if math.IsInf(leastDistance, 1) {
		return false, fmt.Errorf("sizedOptimizer.moveNodeToBest: could not find any placement")
	}
	if err := optimizerMoveNodeAbs(node, leastDistancePoint.X, leastDistancePoint.Y, guard); err != nil {
		return false, err
	}
	if err := guard.Finish(); err != nil {
		return false, err
	}
	return node.TopLeft.X != currentX || node.TopLeft.Y != currentY, nil
}
