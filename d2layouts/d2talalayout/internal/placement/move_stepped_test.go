package placement

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/placementcost"
	"github.com/d2lang/d2/lib/geo"
)

// moveNodeToBestStepped is moveNodeToBest without the closest-unoccupied
// search: every canMove check runs indexedCanMove and every unit is charged
// as it is taken. The tests compare moveNodeToBest with it.
func (optim *sizedOptimizer) moveNodeToBestStepped(ctx context.Context, node *layoutgraph.Node, points []geo.Point, mustImprove bool, guard *limits.OptimizationWorkGuard) (bool, error) {
	scorer := placementcost.NewNodeEdgeLengthSweepScorer(node, placementcost.EdgeLengthOptions{
		EdgeAbductions: optim.edgeAbductions, IncludeNodeSizes: true,
		EnforceMinimumGap: false, PenalizeDirection: true,
	})
	defer scorer.Close()
	symmetryCost := optim.cellSize * float64(len(node.Edges))
	leastDistance := math.Inf(1)
	if mustImprove {
		if err := chargeOptimizerScoring(node, optim.edgeAbductions, true, guard); err != nil {
			return false, err
		}
		var err error
		leastDistance, err = scorer.Score(ctx)
		if err != nil {
			return false, err
		}
		columnCrossingCost, err := placementcost.ColumnCrossingCost(ctx, node, optim.edgeAbductions)
		if err != nil {
			return false, err
		}
		symmetry, err := placementcost.NodeSymmetry(ctx, node, optim.edgeAbductions)
		if err != nil {
			return false, err
		}
		leastDistance += columnCrossingCost
		leastDistance -= symmetry * symmetryCost
	}
	leastDistancePoint := node.TopLeft.Copy()
	currentNodeX := node.TopLeft.X
	currentNodeY := node.TopLeft.Y
	movement, err := optim.moveScratch.candidateMovement(node, guard)
	if err != nil {
		return false, err
	}
	originalPositions := movement.positions
	complete := false
	defer func() {
		if !complete {
			restoreNodePositions(originalPositions)
		}
	}()

	for i := range points {
		if err := guard.Step(); err != nil {
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
			canMove, err = optim.indexedCanMove(node, point, guard)
			if err != nil {
				restoreNodePositions(originalPositions)
				return false, err
			}
		}
		if canMove {
			if err := movement.moveAbs(x, y, guard); err != nil {
				restoreNodePositions(originalPositions)
				return false, err
			}
			if err := chargeOptimizerScoring(node, optim.edgeAbductions, true, guard); err != nil {
				restoreNodePositions(originalPositions)
				return false, err
			}

			edgeLength, err := scorer.ScoreUnlessAbove(ctx, symmetryCost, leastDistance)
			if err != nil {
				restoreNodePositions(originalPositions)
				return false, err
			}
			if !math.IsInf(leastDistance, 1) && geo.PrecisionCompare(edgeLength-symmetryCost, leastDistance, geo.PRECISION) == 1 {
				// no need to continue, this is clearly worse
				continue
			}
			columnCrossingCost, err := placementcost.ColumnCrossingCost(ctx, node, optim.edgeAbductions)
			if err != nil {
				restoreNodePositions(originalPositions)
				return false, err
			}
			symmetry, err := placementcost.NodeSymmetry(ctx, node, optim.edgeAbductions)
			if err != nil {
				restoreNodePositions(originalPositions)
				return false, err
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
	return (node.TopLeft.X != currentNodeX) || (node.TopLeft.Y != currentNodeY), nil
}

// TestMoveNodeToBestWithoutSearchMatchesStepped runs moveNodeToBestWith with
// a nil search, the mode moveNodeToBest selects when no prepared
// closest-unoccupied search answers node's canMove checks, against the
// stepped reference on identical graphs: leaves (fixed origins, obstacles,
// -0, tight budgets) and the nodes of level graphs whose containers, cluster
// and sequence vessels carry descendants (some unplaced, some with a nil
// child), with and without a Done channel. The outcome, the error or panic,
// the guard units, moveExact, every position and the scores recorded for the
// swap search (none) must agree.
func TestMoveNodeToBestWithoutSearchMatchesStepped(t *testing.T) {
	cancelable, cancel := context.WithCancel(context.Background())
	defer cancel()
	var carriers, moved, failures, panics int
	for _, ctx := range []context.Context{context.Background(), cancelable} {
		for trial := 0; trial < 600; trial++ {
			seed := int64(15000 + trial)
			run := func(stepped bool) (result string, carrier bool) {
				optim, node, nodes, points, mustImprove, limit := nilSearchMoveCase(seed)
				carrier = optim.nodeMayMoveDescendants(node)
				setup, _ := limits.NewOptimizationWorkGuard(ctx, "LocalOptimize", limits.MaxOptimizationWorkUnits)
				if err := optim.rebuildSpatialIndex(setup); err != nil {
					return "index " + err.Error(), carrier
				}
				guard, _ := limits.NewOptimizationWorkGuard(ctx, "LocalOptimize", limit)
				defer func() {
					if r := recover(); r != nil {
						result = fmt.Sprintf("panic %v units %d positions %s", r, guard.Used(), allPositions(nodes))
					}
				}()
				var movedResult bool
				var err error
				if stepped {
					movedResult, err = optim.moveNodeToBestStepped(ctx, node, points, mustImprove, guard)
				} else {
					movedResult, err = optim.moveNodeToBestWith(ctx, node, points, mustImprove, nil, guard)
				}
				return fmt.Sprintf("moved %v err %v exact %v units %d positions %s recorded %d",
					movedResult, err, optim.moveExact, guard.Used(), allPositions(nodes), optim.current.level), carrier
			}
			got, carrier := run(false)
			want, _ := run(true)
			if got != want {
				t.Fatalf("trial %d:\nnil search %s\nstepped    %s", trial, got, want)
			}
			if carrier {
				carriers++
			}
			switch {
			case strings.HasPrefix(got, "moved true err <nil>"):
				moved++
			case strings.HasPrefix(got, "panic"):
				panics++
			case !strings.HasPrefix(got, "moved false err <nil>"):
				failures++
			}
		}
	}
	t.Logf("%d nodes with descendants, %d moved, %d failures, %d panics", carriers, moved, failures, panics)
	if carriers == 0 || moved == 0 || failures == 0 {
		t.Errorf("exercised %d nodes with descendants, %d moves, %d failures", carriers, moved, failures)
	}
}

// nilSearchMoveCase builds from seed a node to move, every node whose
// position the move can change, the candidate points, mustImprove and the
// work limit: half of the time a leaf of buildSearch3Case, otherwise a node
// of the level graph of nestedMoveGraph.
func nilSearchMoveCase(seed int64) (optim *sizedOptimizer, node *layoutgraph.Node, nodes []*layoutgraph.Node, points []geo.Point, mustImprove bool, limit uint64) {
	random := rand.New(rand.NewSource(seed))
	if random.Intn(2) == 0 {
		c := buildSearch3Case(seed)
		return c.optim, c.node, c.optim.g.Nodes, c.points, c.mustImprove, c.limit
	}
	full, all := nestedMoveGraph(random, random.Intn(6) == 0, random.Intn(8) == 0)
	g, top := levelGraph(full, all)
	node = top[random.Intn(len(top))]
	optim = &sizedOptimizer{g: g, cellSize: g.CellSize, symmetryCost: g.CellSize, randGenerator: rand.New(rand.NewSource(seed))}
	var x, y float64
	if node.TopLeft != nil {
		x, y = node.TopLeft.X, node.TopLeft.Y
	}
	for k := 0; k < 3+random.Intn(40); k++ {
		points = append(points, geo.Point{
			X: roundToNearestCellSize(x+float64(random.Intn(30)-15)*10, 10),
			Y: roundToNearestCellSize(y+float64(random.Intn(30)-15)*10, 10),
		})
		if random.Intn(8) == 0 && node.TopLeft != nil {
			points = append(points, *node.TopLeft)
		}
	}
	limit = math.MaxUint64
	switch random.Intn(3) {
	case 0:
		limit = uint64(random.Intn(1500))
	case 1:
		limit = uint64(random.Intn(30000))
	}
	return optim, node, all, points, random.Intn(2) == 0, limit
}
