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

// moveNodeToBestAfterSearch is moveNodeToBestGuarded right after node's
// closest-unoccupied search, its candidate generation and shuffle, with
// nothing moved since: the search's point memo still describes the graph.
func (optim *sizedOptimizer) moveNodeToBestAfterSearch(ctx context.Context, node *layoutgraph.Node, points []geo.Point, mustImprove bool, guard *limits.OptimizationWorkGuard) (bool, error) {
	return optim.moveNodeToBest(ctx, node, points, mustImprove, true, guard)
}

func (optim *sizedOptimizer) moveNodeToBest(ctx context.Context, node *layoutgraph.Node, points []geo.Point, mustImprove, afterSearch bool, guard *limits.OptimizationWorkGuard) (bool, error) {
	search := &optim.unoccupied
	defer search.release()
	optim.current = optimizerCurrentScore{}
	// Set to true by a successful search that leaves every node it moved at
	// its exact starting bits.
	optim.moveExact = false
	switch {
	case node == nil || node.Graph == nil || node.TopLeft == nil:
		search = nil
	case !(afterSearch && search.preparedFor(optim, node)) && !search.prepare(optim, node):
		search = nil
	case optim.nodeMayMoveDescendants(node) && !optim.moveScratch.descendantsOutside(node, search.table.entries):
		// Containers, cluster and sequence vessels move their descendants
		// with them: the prepared answers hold only if none of those is a
		// node of the scan table (they are not graph nodes of this level in
		// valid graphs).
		search = nil
	}
	return optim.moveNodeToBestWith(ctx, node, points, mustImprove, search, guard)
}

// moveNodeToBestWith moves node to the best of points. With search nil it
// is the stepped loop: every canMove check runs indexedCanMove and every
// unit is charged as it is taken.
//
// A search prepared for node (a leaf, or a node whose descendants, which move
// with it, are not in the scan table) answers the canMove checks instead.
// Only node and its descendants move while it runs, so a canMove answer and
// its units are a pure function of the point (and of node's current
// position, which is tested first exactly as indexedCanMove does). The units
// of the loop steps and canMove checks since the last observation are
// charged with one Add before anything else touches the guard or the graph;
// on a rejection replayMoveNodeSteps replays those points stepped from the
// identical guard state.
//
// Only leaves with a prepared search use the score memo and record their own
// scores for the swap search: a node with descendants is not fixed by its own
// position alone.
func (optim *sizedOptimizer) moveNodeToBestWith(ctx context.Context, node *layoutgraph.Node, points []geo.Point, mustImprove bool, search *optimizerUnoccupiedSearch, guard *limits.OptimizationWorkGuard) (bool, error) {
	counted := search != nil
	leaf := counted && !optim.nodeMayMoveDescendants(node)
	// Only node and its descendants move between the scorer's Score calls,
	// the sweep scorer's contract.
	scorer := placementcost.NewNodeEdgeLengthSweepScorer(node, placementcost.EdgeLengthOptions{
		EdgeAbductions: optim.edgeAbductions, IncludeNodeSizes: true,
		EnforceMinimumGap: false, PenalizeDirection: true,
	})
	defer scorer.Close()
	symmetryCost := optim.cellSize * float64(len(node.Edges))
	leastDistance := math.Inf(1)
	// Scores of node at a position computed in an earlier pass under the
	// same graph version are reused (see optimizerScoreMemo).
	memo := &optim.scores
	turnBits, crossingBits, memoOn := memo.usable(ctx, node)
	memoOn = memoOn && leaf
	if mustImprove {
		if err := chargeOptimizerScoring(node, optim.edgeAbductions, true, guard); err != nil {
			return false, err
		}
		var known optimizerScoreSlot
		var px, py uint64
		if memoOn {
			px, py = math.Float64bits(node.TopLeft.X), math.Float64bits(node.TopLeft.Y)
			known = memo.lookup(node, px, py, turnBits, crossingBits)
			if memo.stats != nil {
				memo.verifyHit(node, optim.edgeAbductions, known, math.Inf(1), symmetryCost)
			}
		}
		var columnCrossingCost, symmetry float64
		if known.level == scoreMemoAll {
			leastDistance, columnCrossingCost, symmetry = known.score, known.columnCrossing, known.symmetry
		} else {
			if known.level == scoreMemoEdgeLength {
				leastDistance = known.score
			} else {
				var err error
				if leastDistance, err = scorer.Score(ctx); err != nil {
					return false, err
				}
			}
			var err error
			if columnCrossingCost, err = placementcost.ColumnCrossingCost(ctx, node, optim.edgeAbductions); err != nil {
				return false, err
			}
			if symmetry, err = placementcost.NodeSymmetry(ctx, node, optim.edgeAbductions); err != nil {
				return false, err
			}
			if memoOn {
				memo.store(node, px, py, turnBits, crossingBits, scoreMemoAll, leastDistance, columnCrossingCost, symmetry)
			}
		}
		if leaf {
			optim.current.record(node, optimizerScoredAll, leastDistance, columnCrossingCost, symmetry)
		}
		leastDistance += columnCrossingCost
		leastDistance -= symmetry * symmetryCost
	}
	leastDistancePoint := node.TopLeft.Copy()
	currentNodeX := node.TopLeft.X
	currentNodeY := node.TopLeft.Y
	var movement optimizerCandidateMovement
	var err error
	if leaf {
		movement, err = captureLeafCandidateMovement(node, &optim.movement, guard)
	} else {
		movement, err = optim.moveScratch.candidateMovement(node, guard)
	}
	if err != nil {
		return false, err
	}
	originalPositions := movement.positions
	complete := false
	defer func() {
		if !complete {
			restoreNodePositions(originalPositions)
		}
		optim.movement[0] = nodePositionSnapshot{}
	}()

	// pending holds the units of points[segment:] not yet charged: each
	// point's loop step and, up to lastChecked, its canMove check. The sum is
	// bounded by len(points) times one full graph scan, far below overflow.
	var pending uint64
	segment := 0
	flush := func(end int, lastChecked bool) error {
		if pending == 0 {
			return nil
		}
		units, start := pending, segment
		pending = 0
		segment = end + 1
		_, err := chargeOrReplay(guard, units, func() error {
			return optim.replayMoveNodeSteps(node, points, start, end, lastChecked, currentNodeX, currentNodeY, guard)
		})
		return err
	}

	for i := range points {
		if counted {
			pending++ // the loop's guard.Step
		} else if err := guard.Step(); err != nil {
			restoreNodePositions(originalPositions)
			return false, err
		}
		point := &points[i]
		x := point.X
		y := point.Y
		if optim.fixedOrigin != nil && (x < optim.fixedOrigin.X || y < optim.fixedOrigin.Y) {
			continue
		}
		// If the candidate position is the original one, we should be able to move back to it (if we moved somewhere else in this routine)
		canMove := x == currentNodeX && y == currentNodeY
		if !canMove {
			var err error
			switch {
			case !counted:
				canMove, err = optim.indexedCanMove(node, point, guard)
			case node.TopLeft.X == x && node.TopLeft.Y == y:
				// indexedCanMove accepts the node's own position without work.
				canMove = true
			default:
				movable, units, ok := search.canMove(x, y, !leaf)
				if ok {
					canMove = movable
					pending += units
				} else if err = flush(i, false); err == nil {
					canMove, err = optim.indexedCanMove(node, point, guard)
				}
			}
			if err != nil {
				restoreNodePositions(originalPositions)
				return false, err
			}
		}
		if canMove {
			if err := flush(i, true); err != nil {
				restoreNodePositions(originalPositions)
				return false, err
			}
			if err := movement.moveAbs(x, y, guard); err != nil {
				restoreNodePositions(originalPositions)
				return false, err
			}
			if err := chargeOptimizerScoring(node, optim.edgeAbductions, true, guard); err != nil {
				restoreNodePositions(originalPositions)
				return false, err
			}

			atCurrent := x == currentNodeX && y == currentNodeY
			// optim.current.record below keeps the value at the node's own
			// position for the swap search, so there it must be the full
			// score even when the candidate is discarded, unless that record
			// would change nothing (holdsAll).
			needFull := leaf && atCurrent && !optim.current.holdsAll(node)
			discardable := !math.IsInf(leastDistance, 1)
			var known optimizerScoreSlot
			var px, py uint64
			if memoOn {
				px, py = math.Float64bits(node.TopLeft.X), math.Float64bits(node.TopLeft.Y)
				known = memo.lookup(node, px, py, turnBits, crossingBits)
				if memo.stats != nil {
					memo.verifyHit(node, optim.edgeAbductions, known, leastDistance, symmetryCost)
				}
			}
			var edgeLength float64
			switch {
			case known.level >= scoreMemoEdgeLength:
				edgeLength = known.score
			case known.level == scoreMemoLowerBound && !needFull && discardable &&
				geo.PrecisionCompare(known.score-symmetryCost, leastDistance, geo.PRECISION) == 1:
				// The full score is at least this lower bound, so it would be
				// discarded below as well.
				edgeLength = known.score
			default:
				var err error
				if needFull {
					edgeLength, err = scorer.Score(ctx)
				} else {
					// The value is only used to discard the candidate, or is
					// the full score (see ScoreUnlessAbove).
					edgeLength, err = scorer.ScoreUnlessAbove(ctx, symmetryCost, leastDistance)
				}
				if err != nil {
					restoreNodePositions(originalPositions)
					return false, err
				}
				if memoOn {
					level := scoreMemoEdgeLength
					if !needFull && discardable && geo.PrecisionCompare(edgeLength-symmetryCost, leastDistance, geo.PRECISION) == 1 {
						// Possibly a partial sum: only a lower bound.
						level = scoreMemoLowerBound
					}
					memo.store(node, px, py, turnBits, crossingBits, level, edgeLength, 0, 0)
				}
			}
			if discardable && geo.PrecisionCompare(edgeLength-symmetryCost, leastDistance, geo.PRECISION) == 1 {
				// no need to continue, this is clearly worse
				if leaf && atCurrent {
					optim.current.record(node, optimizerScoredEdgeLength, edgeLength, 0, 0)
				}
				continue
			}
			// Not discarded: edgeLength is the full score.
			var columnCrossingCost, symmetry float64
			if known.level == scoreMemoAll {
				columnCrossingCost, symmetry = known.columnCrossing, known.symmetry
			} else {
				var err error
				if columnCrossingCost, err = placementcost.ColumnCrossingCost(ctx, node, optim.edgeAbductions); err != nil {
					restoreNodePositions(originalPositions)
					return false, err
				}
				if symmetry, err = placementcost.NodeSymmetry(ctx, node, optim.edgeAbductions); err != nil {
					restoreNodePositions(originalPositions)
					return false, err
				}
				if memoOn {
					memo.store(node, px, py, turnBits, crossingBits, scoreMemoAll, edgeLength, columnCrossingCost, symmetry)
				}
			}
			if leaf && atCurrent {
				optim.current.record(node, optimizerScoredAll, edgeLength, columnCrossingCost, symmetry)
			}
			edgeLength += columnCrossingCost
			edgeLength -= symmetry * symmetryCost

			for _, o := range optim.obstacles {
				if err := guard.Step(); err != nil {
					restoreNodePositions(originalPositions)
					return false, err
				}
				if o.TopLeft.X == -layoutgraph.ContainerPadding && o.TopLeft.Y == -layoutgraph.ContainerPadding {
					continue
				}
				if node.Box.Overlaps(o) {
					edgeLength += 3 * optim.g.TurnCost()
				}
			}

			switch geo.PrecisionCompare(edgeLength, leastDistance, geo.PRECISION) {
			case -1:
				leastDistance = edgeLength
				leastDistancePoint = point
			case 0:
				if point.X == currentNodeX && point.Y == currentNodeY {
					leastDistance = edgeLength
					leastDistancePoint = point
				}
			}
		}
	}
	if err := flush(len(points)-1, true); err != nil {
		restoreNodePositions(originalPositions)
		return false, err
	}

	if err := ctx.Err(); err != nil {
		restoreNodePositions(originalPositions)
		return false, fmt.Errorf("EdgeLength: %w", err)
	}
	if math.IsInf(leastDistance, 1) {
		restoreNodePositions(originalPositions)
		return false, fmt.Errorf("sizedOptimizer: could not find any placement")
	}

	if err := movement.moveAbs(leastDistancePoint.X, leastDistancePoint.Y, guard); err != nil {
		restoreNodePositions(originalPositions)
		return false, err
	}

	complete = true
	optim.moveExact = positionsExact(originalPositions)
	moved := (node.TopLeft.X != currentNodeX) || (node.TopLeft.Y != currentNodeY)
	if moved {
		optim.current = optimizerCurrentScore{}
	}
	return moved, nil
}

// optimizerCurrentScore holds the scores of a leaf node at its own position
// that its last moveNodeToBest computed (the mustImprove baseline, or the
// candidate at the node's position), for the swap search that follows when
// the node did not move. Only the node moves during moveNodeToBest, so the
// graph is the one the scores saw as long as the node is back at the bits it
// had when they were recorded.
type optimizerCurrentScore struct {
	node           *layoutgraph.Node
	x, y           uint64 // math.Float64bits of node.TopLeft when scored
	width, height  uint64 // math.Float64bits of the node's size
	edges          int
	level          uint8
	edgeLength     float64 // Score, equal to NodeEdgeLength with the scorer's options
	columnCrossing float64
	symmetry       float64
}

const (
	optimizerScoredNothing uint8 = iota
	optimizerScoredEdgeLength
	optimizerScoredAll
)

// record keeps scores computed with node at its current position unless
// more of them are already kept (the values at one position are equal).
func (current *optimizerCurrentScore) record(node *layoutgraph.Node, level uint8, edgeLength, columnCrossing, symmetry float64) {
	if current.node == node && current.level >= level && current.matches(node) {
		return
	}
	*current = optimizerCurrentScore{
		node:           node,
		x:              math.Float64bits(node.TopLeft.X),
		y:              math.Float64bits(node.TopLeft.Y),
		width:          math.Float64bits(node.Width),
		height:         math.Float64bits(node.Height),
		edges:          len(node.Edges),
		level:          level,
		edgeLength:     edgeLength,
		columnCrossing: columnCrossing,
		symmetry:       symmetry,
	}
}

// holdsAll reports whether every record of node at its current position is a
// no-op: all three scores are kept for exactly this position.
func (current *optimizerCurrentScore) holdsAll(node *layoutgraph.Node) bool {
	return current.level == optimizerScoredAll && current.matches(node)
}

// matches reports whether node is where and what it was when scored.
func (current *optimizerCurrentScore) matches(node *layoutgraph.Node) bool {
	return current.level != optimizerScoredNothing && node != nil && current.node == node && node.TopLeft != nil &&
		math.Float64bits(node.TopLeft.X) == current.x && math.Float64bits(node.TopLeft.Y) == current.y &&
		math.Float64bits(node.Width) == current.width && math.Float64bits(node.Height) == current.height &&
		len(node.Edges) == current.edges
}

// replayMoveNodeSteps charges, stepped, what the stepped loop charges for
// points[start..end]: each point's loop step and its canMove check (the
// last point's check only when lastChecked). Nothing moves in between, so the
// checks see the graph the counted checks saw.
func (optim *sizedOptimizer) replayMoveNodeSteps(node *layoutgraph.Node, points []geo.Point, start, end int, lastChecked bool, currentNodeX, currentNodeY float64, guard *limits.OptimizationWorkGuard) error {
	for k := start; k <= end; k++ {
		if err := guard.Step(); err != nil {
			return err
		}
		if k == end && !lastChecked {
			return nil
		}
		point := &points[k]
		if optim.fixedOrigin != nil && (point.X < optim.fixedOrigin.X || point.Y < optim.fixedOrigin.Y) {
			continue
		}
		if point.X == currentNodeX && point.Y == currentNodeY {
			continue
		}
		if _, err := optim.indexedCanMove(node, point, guard); err != nil {
			return err
		}
	}
	return nil
}
