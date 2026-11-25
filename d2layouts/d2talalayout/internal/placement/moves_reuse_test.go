package placement

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"slices"
	"sort"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/placementcost"
	"github.com/d2lang/d2/lib/geo"
)

// moveNodeToBestBaseline is moveNodeToBest as it was before it reused the
// scores of a candidate that moves nothing.
func moveNodeToBestBaseline(ctx context.Context, graph *layoutgraph.Graph, node *layoutgraph.Node, points []*geo.Point, edgeAbductions []*layoutgraph.EdgeAbduction, includeSizes bool, guard *limits.OptimizationWorkGuard) (bool, error) {
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

	for _, point := range points {
		if err := guard.Step(); err != nil {
			return false, err
		}
		if point == nil {
			return false, fmt.Errorf("TALA %s found a nil placement point", guard.Location())
		}
		x, y := point.X, point.Y
		if fixedOrigin != nil && (x < fixedOrigin.X || y < fixedOrigin.Y) {
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
			continue
		}
		if err := optimizerMoveNodeAbs(node, x, y, guard); err != nil {
			return false, err
		}
		if err := chargeOptimizerScoring(node, edgeAbductions, includeSizes, guard); err != nil {
			return false, err
		}
		edgeLength, err := placementcost.NodeEdgeLength(ctx, node, placementcost.EdgeLengthOptions{EdgeAbductions: edgeAbductions, IncludeNodeSizes: includeSizes, EnforceMinimumGap: false, PenalizeDirection: true})
		if err != nil {
			return false, err
		}
		if includeSizes {
			if !math.IsInf(leastDistance, 1) && geo.PrecisionCompare(edgeLength-symmetryCost, leastDistance, geo.PRECISION) == 1 {
				continue
			}
			crossingCost, err := placementcost.ColumnCrossingCost(ctx, node, edgeAbductions)
			if err != nil {
				return false, err
			}
			symmetry, err := placementcost.NodeSymmetry(ctx, node, edgeAbductions)
			if err != nil {
				return false, err
			}
			edgeLength += crossingCost
			edgeLength -= symmetry * symmetryCost
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

// shiftSubgraphsBaseline is shiftSubgraphs as it was before it reused the
// cost of a repeated zero-delta move.
func shiftSubgraphsBaseline(ctx context.Context, g *layoutgraph.Graph, isHorizontal, includeSizes bool, factor float64, edgeAbductions []*layoutgraph.EdgeAbduction, vEdges layoutgraph.Edges) (bool, error) {
	changed := false
	subgraphSet := make(map[*layoutgraph.Node][]*layoutgraph.Node)
	orderedSubgraphRoots := []*layoutgraph.Node{}

	orderedNodes := orderedAlongAxis(g, isHorizontal)

	for _, node := range orderedNodes {
		nearestFrom := nearestFrom(vEdges, node, isHorizontal, includeSizes)
		if nearestFrom == nil {
			subgraphSet[node] = []*layoutgraph.Node{node}
			orderedSubgraphRoots = append(orderedSubgraphRoots, node)
		} else {
			subgraphSet[nearestFrom] = append(subgraphSet[nearestFrom], node)
		}
	}

	filtered := []*layoutgraph.Node{}
	for _, root := range orderedSubgraphRoots {
		sgHasFixedNode := false
		for _, n := range subgraphSet[root] {
			if n.FixedTopLeft != nil {
				sgHasFixedNode = true
				break
			}
		}
		if sgHasFixedNode {
			delete(subgraphSet, root)
		} else {
			filtered = append(filtered, root)
		}
	}
	orderedSubgraphRoots = filtered
	fixedNodes := g.FixedNodes()

	sort.Slice(orderedSubgraphRoots, func(i, j int) bool {
		if isHorizontal {
			if orderedSubgraphRoots[i].TopLeft.X == orderedSubgraphRoots[j].TopLeft.X {
				return orderedSubgraphRoots[i].ID < orderedSubgraphRoots[j].ID
			}
			return orderedSubgraphRoots[i].TopLeft.X < orderedSubgraphRoots[j].TopLeft.X
		} else {
			if orderedSubgraphRoots[i].TopLeft.Y == orderedSubgraphRoots[j].TopLeft.Y {
				return orderedSubgraphRoots[i].ID < orderedSubgraphRoots[j].ID
			}
			return orderedSubgraphRoots[i].TopLeft.Y < orderedSubgraphRoots[j].TopLeft.Y
		}
	})

	globallyFurthestBehind := globallyFurthestBehind(g, isHorizontal)
	for _, furthestBehind := range orderedSubgraphRoots {
		subgraph := subgraphSet[furthestBehind]
		var possibleMoves []*geo.Point
		var err error
		if isHorizontal {
			if furthestBehind.TopLeft.X == globallyFurthestBehind.TopLeft.X {
				possibleMoves, err = candidateMoves(ctx, g, furthestBehind, factor, isHorizontal, includeSizes, 2, vEdges)
			} else {
				possibleMoves, err = candidateMoves(ctx, g, furthestBehind, factor, isHorizontal, includeSizes, 0, vEdges)
			}
		} else {
			if furthestBehind.TopLeft.Y == globallyFurthestBehind.TopLeft.Y {
				possibleMoves, err = candidateMoves(ctx, g, furthestBehind, factor, isHorizontal, includeSizes, 2, vEdges)
			} else {
				possibleMoves, err = candidateMoves(ctx, g, furthestBehind, factor, isHorizontal, includeSizes, 0, vEdges)
			}
		}
		if err != nil {
			return false, err
		}
		possibleMoves = append(possibleMoves, furthestBehind.TopLeft.Copy())

		// shifting subgraphs shouldn't introduce any overlaps between subgraph nodes, but we still need to check for overlaps with nodes not in this subgraph
		nonSubgraphNodes := []*layoutgraph.Node{}
		nonSubgraphNodes = append(nonSubgraphNodes, fixedNodes...)
		for _, otherN := range g.Nodes {
			in := slices.Contains(subgraph, otherN)
			if !in {
				nonSubgraphNodes = append(nonSubgraphNodes, otherN)
			}
		}

		symmetryCost := 1.0
		if includeSizes {
			symmetryCost = g.CellSize
		}
		symmetryCost *= float64(layoutgraph.Nodes(subgraph).NumAdjacent())

		bestEdgeDistance, err := placementcost.NodesEdgeLength(ctx, layoutgraph.Nodes(subgraph), placementcost.EdgeLengthOptions{EdgeAbductions: edgeAbductions, IncludeNodeSizes: includeSizes, EnforceMinimumGap: false, PenalizeDirection: true})
		if err != nil {
			return false, err
		}
		bestDelta := 0.0

		originalPositions, err := snapshotNodePositionsContext(ctx, "Compaction", subgraph)
		if err != nil {
			return false, err
		}
		for _, move := range possibleMoves {
			var delta float64
			if isHorizontal {
				delta = furthestBehind.TopLeft.X - move.X
			} else {
				delta = furthestBehind.TopLeft.Y - move.Y
			}

			overlaps := false
			for _, n := range subgraph {
				p := n.TopLeft.Copy()
				if isHorizontal {
					p.X -= delta
				} else {
					p.Y -= delta
				}
				if len(fixedNodes) > 0 {
					if n.PointPastFixedOrigin(p.X, p.Y, includeSizes) {
						overlaps = true
						break
					}
				}
				for _, otherN := range nonSubgraphNodes {
					if includeSizes {
						if n.DoesOverlapAt(otherN, p) {
							overlaps = true
							break
						}
					} else {
						if nonNilEquals(otherN.TopLeft, p) {
							overlaps = true
							break
						}
					}
				}
				if overlaps {
					break
				}
			}
			if overlaps {
				continue
			}

			for _, node := range subgraph {
				if isHorizontal {
					node.MoveWithChildren(-delta, 0)
				} else {
					node.MoveWithChildren(0, -delta)
				}
			}

			edgeDistanceAfter, err := placementcost.NodesEdgeLength(ctx, layoutgraph.Nodes(subgraph), placementcost.EdgeLengthOptions{EdgeAbductions: edgeAbductions, IncludeNodeSizes: includeSizes, EnforceMinimumGap: false, PenalizeDirection: true})
			if err != nil {
				restoreNodePositions(originalPositions)
				return false, err
			}
			if includeSizes {
				symmetry, err := placementcost.NodesSymmetry(ctx, layoutgraph.Nodes(subgraph), edgeAbductions)
				if err != nil {
					restoreNodePositions(originalPositions)
					return false, err
				}
				edgeDistanceAfter -= symmetry * symmetryCost
			}

			restoreNodePositions(originalPositions)

			if edgeDistanceAfter < bestEdgeDistance {
				bestEdgeDistance = edgeDistanceAfter
				bestDelta = delta
			}
		}
		if bestDelta != 0 {
			changed = true
			for _, node := range subgraph {
				if isHorizontal {
					node.MoveWithChildren(-bestDelta, 0)
				} else {
					node.MoveWithChildren(0, -bestDelta)
				}
			}
		}
	}
	return changed, nil
}

var reuseTestFractions = []float64{0, 0, 0, 0.1, 0.3, 1.0 / 3, 0.7}

func reuseTestCoordinate(random *rand.Rand, cells int, step float64) float64 {
	return float64(random.Intn(cells))*step + reuseTestFractions[random.Intn(len(reuseTestFractions))]
}

// moveNodeToBest reuses the scores of a candidate that moves nothing;
// positions, results and work units must equal the baseline's for candidate
// lists with and without repeats, on integral and fractional coordinates.
func TestMoveNodeToBestMatchesBaseline(t *testing.T) {
	for seed := int64(1); seed <= 200; seed++ {
		for _, includeSizes := range []bool{false, true} {
			type outcome struct {
				moved bool
				err   string
				x, y  float64
				units uint64
			}
			run := func(move func(context.Context, *layoutgraph.Graph, *layoutgraph.Node, []*geo.Point, []*layoutgraph.EdgeAbduction, bool, *limits.OptimizationWorkGuard) (bool, error)) outcome {
				random := rand.New(rand.NewSource(seed))
				graph := layoutgraph.NewGraph()
				var nodes []*layoutgraph.Node
				for i := range 8 {
					node := layoutgraph.NewNode(layoutgraph.EntityID(i+1), 20, 20)
					node.TopLeft = geo.NewPoint(reuseTestCoordinate(random, 6, 40), reuseTestCoordinate(random, 6, 40))
					graph.AddNewNodeToContainer(nil, node)
					nodes = append(nodes, node)
				}
				for range 10 {
					a, b := nodes[random.Intn(len(nodes))], nodes[random.Intn(len(nodes))]
					if a != b {
						graph.Connect(a, b)
					}
				}
				graph.ComputeCellSize()
				node := nodes[0]
				var points []*geo.Point
				for range 3 + random.Intn(6) {
					p := geo.NewPoint(reuseTestCoordinate(random, 8, 20), reuseTestCoordinate(random, 8, 20))
					points = append(points, p)
					for random.Intn(3) == 0 {
						points = append(points, p.Copy())
					}
				}
				points = append(points, node.TopLeft.Copy(), node.TopLeft.Copy())
				guard, err := limits.NewOptimizationWorkGuard(context.Background(), "MoveTest", limits.MaxOptimizationWorkUnits)
				if err != nil {
					t.Fatal(err)
				}
				moved, err := move(context.Background(), graph, node, points, nil, includeSizes, guard)
				result := outcome{moved: moved, x: node.TopLeft.X, y: node.TopLeft.Y, units: guard.Used()}
				if err != nil {
					result.err = err.Error()
				}
				return result
			}
			want := run(moveNodeToBestBaseline)
			got := run(moveNodeToBest)
			if math.Float64bits(got.x) != math.Float64bits(want.x) || math.Float64bits(got.y) != math.Float64bits(want.y) ||
				got.moved != want.moved || got.err != want.err || got.units != want.units {
				t.Fatalf("seed %d sizes %v: got %+v, want %+v", seed, includeSizes, got, want)
			}
		}
	}
}

// shiftSubgraphs reuses the cost of a repeated zero-delta move; every pass
// must report the same change and leave every node at the same position as
// the baseline.
func TestShiftSubgraphsMatchesBaseline(t *testing.T) {
	type shiftFunc func(context.Context, *layoutgraph.Graph, bool, bool, float64, []*layoutgraph.EdgeAbduction, layoutgraph.Edges) (bool, error)
	for seed := int64(1); seed <= 120; seed++ {
		for _, isHorizontal := range []bool{false, true} {
			for _, includeSizes := range []bool{false, true} {
				run := func(shift shiftFunc) ([]float64, []bool) {
					random := rand.New(rand.NewSource(seed))
					graph := layoutgraph.NewGraph()
					cells := random.Perm(36)
					var nodes []*layoutgraph.Node
					for i := range 6 + random.Intn(8) {
						node := layoutgraph.NewNode(layoutgraph.EntityID(i+1), float64(20+random.Intn(40)), float64(20+random.Intn(40)))
						x := float64(cells[i]%6)*120 + reuseTestFractions[random.Intn(len(reuseTestFractions))]
						y := float64(cells[i]/6)*120 + reuseTestFractions[random.Intn(len(reuseTestFractions))]
						node.TopLeft = geo.NewPoint(x, y)
						graph.AddNewNodeToContainer(nil, node)
						nodes = append(nodes, node)
					}
					if random.Intn(2) == 0 {
						parent := nodes[random.Intn(len(nodes))]
						child := layoutgraph.NewNode(layoutgraph.EntityID(100), 4, 4)
						child.TopLeft = geo.NewPoint(parent.TopLeft.X+2, parent.TopLeft.Y+2)
						graph.AddNewNodeToContainer(parent, child)
					}
					for range len(nodes) + random.Intn(len(nodes)) {
						a, b := nodes[random.Intn(len(nodes))], nodes[random.Intn(len(nodes))]
						if a != b {
							graph.Connect(a, b)
						}
					}
					graph.ComputeCellSize()
					ctx := context.Background()
					edges := mustVisibilityEdges(t, ctx, graph, isHorizontal, includeSizes)
					factor := 1 + float64(random.Intn(3))*0.7
					inflateAlongAxis(graph, isHorizontal, includeSizes, factor, edges, false)
					var changes []bool
					for range 6 {
						changed, err := shift(ctx, graph, isHorizontal, includeSizes, factor, nil, edges)
						if err != nil {
							t.Fatal(err)
						}
						changes = append(changes, changed)
						if !changed {
							break
						}
					}
					var positions []float64
					for _, node := range graph.Nodes {
						positions = append(positions, node.TopLeft.X, node.TopLeft.Y)
					}
					return positions, changes
				}
				wantPositions, wantChanges := run(shiftSubgraphsBaseline)
				gotPositions, gotChanges := run(shiftSubgraphs)
				if !slices.Equal(gotChanges, wantChanges) {
					t.Fatalf("seed %d horizontal %v sizes %v: changes %v, want %v", seed, isHorizontal, includeSizes, gotChanges, wantChanges)
				}
				for i := range wantPositions {
					if math.Float64bits(gotPositions[i]) != math.Float64bits(wantPositions[i]) {
						t.Fatalf("seed %d horizontal %v sizes %v: positions %v, want %v", seed, isHorizontal, includeSizes, gotPositions, wantPositions)
					}
				}
			}
		}
	}
	_ = sort.Slice
	_ = placementcost.IdealGapSize
}
