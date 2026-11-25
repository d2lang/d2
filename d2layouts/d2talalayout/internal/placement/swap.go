package placement

import (
	"context"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/placementcost"
	"github.com/d2lang/d2/lib/geo"
)

func swapPositions(nodeA *layoutgraph.Node, nodeB *layoutgraph.Node) {
	tmpX := nodeA.TopLeft.X
	tmpY := nodeA.TopLeft.Y

	nodeA.MoveWithChildren(
		nodeB.TopLeft.X-nodeA.TopLeft.X,
		nodeB.TopLeft.Y-nodeA.TopLeft.Y,
	)

	nodeB.MoveWithChildren(
		tmpX-nodeB.TopLeft.X,
		tmpY-nodeB.TopLeft.Y,
	)
}

func smartSwapPositions(nodeA *layoutgraph.Node, nodeB *layoutgraph.Node) {
	tmpX := nodeA.TopLeft.X
	tmpY := nodeA.TopLeft.Y

	o := nodeA.Orientation(nodeB)
	if o == geo.Left {
		nodeA.MoveAbsWithChildren(nodeB.TopLeft.X+nodeB.Width-nodeA.Width, nodeA.TopLeft.Y)
		nodeB.MoveAbsWithChildren(tmpX, nodeB.TopLeft.Y)
	} else if o == geo.Right {
		nodeA.MoveAbsWithChildren(nodeB.TopLeft.X, nodeA.TopLeft.Y)
		nodeB.MoveAbsWithChildren(tmpX+nodeA.Width-nodeB.Width, nodeB.TopLeft.Y)
	} else if o == geo.Top {
		nodeA.MoveAbsWithChildren(nodeA.TopLeft.X, nodeB.TopLeft.Y+nodeB.Height-nodeA.Height)
		nodeB.MoveAbsWithChildren(nodeB.TopLeft.X, tmpY)
	} else if o == geo.Bottom {
		nodeA.MoveAbsWithChildren(nodeA.TopLeft.X, nodeB.TopLeft.Y)
		nodeB.MoveAbsWithChildren(nodeB.TopLeft.X, tmpY+nodeA.Height-nodeB.Height)
	} else if o == geo.TopLeft {
		nodeA.MoveAbsWithChildren(nodeB.TopLeft.X+nodeB.Width-nodeA.Width, nodeB.TopLeft.Y+nodeB.Height-nodeA.Height)
		nodeB.MoveAbsWithChildren(tmpX, tmpY)
	} else if o == geo.TopRight {
		nodeA.MoveAbsWithChildren(nodeB.TopLeft.X, nodeB.TopLeft.Y+nodeB.Height-nodeA.Height)
		nodeB.MoveAbsWithChildren(tmpX+nodeA.Width-nodeB.Width, tmpY)
	} else if o == geo.BottomLeft {
		nodeA.MoveAbsWithChildren(nodeB.TopLeft.X+nodeB.Width-nodeA.Width, nodeB.TopLeft.Y)
		nodeB.MoveAbsWithChildren(tmpX, tmpY+nodeA.Height-nodeB.Height)
	} else if o == geo.BottomRight {
		nodeA.MoveAbsWithChildren(nodeB.TopLeft.X, nodeB.TopLeft.Y)
		nodeB.MoveAbsWithChildren(tmpX+nodeA.Width-nodeB.Width, tmpY+nodeA.Height-nodeB.Height)
	}
}

// swapNeighbourhood is how many of a node's siblings swapOptimize tries: the
// ones whose centres are nearest to the node's centre.
const swapNeighbourhood = 6

// nearestSwapCandidates returns the at most k siblings of node that can be
// swapped with it (placed, not node, not in a tree, not in a hierarchy, not
// fixed) whose centres are nearest to node's centre (squared distance, ties
// in sibling order), in sibling order. With at most k such siblings it
// returns them all. out is reused storage.
func nearestSwapCandidates(g *layoutgraph.Graph, node *layoutgraph.Node, siblings []*layoutgraph.Node, k int, out []*layoutgraph.Node) []*layoutgraph.Node {
	out = out[:0]
	if node.TopLeft == nil || k <= 0 {
		return out
	}
	type candidate struct {
		index    int
		distance float64
	}
	var nearest [swapNeighbourhood]candidate
	k = min(k, len(nearest))
	found := 0
	cx, cy := node.TopLeft.X+node.Width/2, node.TopLeft.Y+node.Height/2
	for i, sibling := range siblings {
		if sibling == node || sibling == nil || sibling.TopLeft == nil || sibling.Hierarchy != nil || sibling.FixedTopLeft != nil {
			continue
		}
		if _, has := g.NodeToTree[sibling]; has {
			continue
		}
		dx := sibling.TopLeft.X + sibling.Width/2 - cx
		dy := sibling.TopLeft.Y + sibling.Height/2 - cy
		c := candidate{index: i, distance: dx*dx + dy*dy}
		if math.IsNaN(c.distance) {
			c.distance = math.Inf(1)
		}
		// Insert into the sorted list of the nearest; a later sibling at an
		// equal distance goes after the earlier ones.
		j := found
		if found < k {
			found++
		} else if !(c.distance < nearest[k-1].distance) {
			continue
		} else {
			j = k - 1
		}
		for j > 0 && c.distance < nearest[j-1].distance {
			nearest[j] = nearest[j-1]
			j--
		}
		nearest[j] = c
	}
	chosen := nearest[:found]
	// Back to sibling order.
	for i := 1; i < len(chosen); i++ {
		for j := i; j > 0 && chosen[j].index < chosen[j-1].index; j-- {
			chosen[j], chosen[j-1] = chosen[j-1], chosen[j]
		}
	}
	for _, c := range chosen {
		out = append(out, siblings[c.index])
	}
	return out
}

// swapOptimize swaps node positions when doing so lowers placement cost.
func swapOptimize(ctx context.Context, nodes layoutgraph.Nodes, g *layoutgraph.Graph) (bool, error) {
	ctx, _, err := layoutgraph.EnsureTransactionWorkGuard(ctx, "SwapOptimizeTransactions")
	if err != nil {
		return false, err
	}
	symmetryCost := g.CellSize
	swapMade := false

	measure := func(n *layoutgraph.Node) (float64, error) {
		l, err := placementcost.NodeEdgeLength(ctx, n, placementcost.EdgeLengthOptions{EdgeAbductions: nil, IncludeNodeSizes: true, EnforceMinimumGap: false, PenalizeDirection: true})
		if err != nil {
			return 0, err
		}
		columnCrossingCost, err := placementcost.ColumnCrossingCost(ctx, n, nil)
		if err != nil {
			return 0, err
		}
		symmetry, err := placementcost.NodeSymmetry(ctx, n, nil)
		if err != nil {
			return 0, err
		}
		l += columnCrossingCost
		l -= symmetry * symmetryCost * float64(len(n.Edges))
		return l, nil
	}

	txn, err := g.NewRequestTransaction(ctx, layoutgraph.TransactionOptions{AffectContainers: true, StableStructure: true})
	if err != nil {
		return false, err
	}
	defer txn.Release()
	// Only positions change while the swaps are tried (transactions move
	// nodes and re-wrap containers, rollbacks restore them), so the session
	// re-evaluates only the terms a trial can change. Its results are those
	// of placementcost.EdgeLength and GraphEdgeCrossings.
	scores := placementcost.NewEdgeLengthSession(g, placementcost.EdgeLengthOptions{EdgeAbductions: nil, IncludeNodeSizes: true, EnforceMinimumGap: false, PenalizeDirection: true})
	defer scores.Close()
	var candidates []*layoutgraph.Node
	for _, node := range nodes {
		if _, has := g.NodeToTree[node]; has {
			continue
		}
		if node.Hierarchy != nil {
			continue
		}
		if node.FixedTopLeft != nil {
			continue
		}
		if len(node.Edges) == 0 && !node.HasLeakyEdge() {
			continue
		}

		bestSmartSwapped := false
		var bestSwapCandidate *layoutgraph.Node
		bestSwapEdgeLength := math.Inf(1)

		currentGlobalL, err := scores.EdgeLengthBase(ctx)
		if err != nil {
			return false, err
		}
		currentL1, err := measure(node)
		if err != nil {
			return false, err
		}
		currentCrossings, err := scores.GraphEdgeCrossings(ctx)
		if err != nil {
			return false, err
		}

		// Only the nearest siblings are tried: a swap with a distant node
		// rarely shortens the edges, and each trial validates the graph twice.
		candidates = nearestSwapCandidates(g, node, g.Containers[node.Container], swapNeighbourhood, candidates)
		for _, swapCandidate := range candidates {
			if swapCandidate == node {
				continue
			}
			if _, has := g.NodeToTree[swapCandidate]; has {
				continue
			}
			if swapCandidate.Hierarchy != nil {
				continue
			}
			if swapCandidate.FixedTopLeft != nil {
				continue
			}

			txn.AddOp(func() error {
				swapPositions(node, swapCandidate)

				return nil
			})

			var swappedL1 float64
			var swappedGlobalL float64
			var swappedCrossings int64
			err = txn.Commit(ctx)
			if err == nil {
				swappedL1, err = measure(node)
				if err == nil {
					swappedGlobalL, err = scores.EdgeLength(ctx)
				}
				if err == nil {
					swappedCrossings, err = scores.GraphEdgeCrossings(ctx)
				}
				if err != nil {
					txn.Rollback()
					txn.Clear()
					return false, err
				}
			} else if !layoutgraph.IsCandidateRejection(err) {
				return false, err
			}

			txn.Rollback()
			txn.Clear()

			txn.AddOp(func() error {
				smartSwapPositions(node, swapCandidate)

				return nil
			})

			err = txn.Commit(ctx)

			var smartSwappedL1 float64
			var smartSwappedGlobalL float64
			var smartSwappedCrossings int64
			if err == nil {
				smartSwappedL1, err = measure(node)
				if err == nil {
					smartSwappedGlobalL, err = scores.EdgeLength(ctx)
				}
				if err == nil {
					smartSwappedCrossings, err = scores.GraphEdgeCrossings(ctx)
				}
				if err != nil {
					txn.Rollback()
					txn.Clear()
					return false, err
				}
			} else if !layoutgraph.IsCandidateRejection(err) {
				return false, err
			}

			txn.Rollback()
			txn.Clear()

			if swappedGlobalL == 0 && smartSwappedGlobalL == 0 {
				continue
			}
			if swappedL1 != 0 && smartSwappedL1 == 0 {
				if swappedCrossings <= currentCrossings && geo.PrecisionCompare(swappedL1, currentL1, geo.PRECISION) < 0 && geo.PrecisionCompare(swappedGlobalL, currentGlobalL, geo.PRECISION) < 0 && geo.PrecisionCompare(swappedGlobalL, bestSwapEdgeLength, geo.PRECISION) < 0 {
					bestSwapEdgeLength = swappedGlobalL
					bestSmartSwapped = false
					bestSwapCandidate = swapCandidate
				}
			} else if smartSwappedL1 != 0 && swappedL1 == 0 {
				if smartSwappedCrossings <= currentCrossings && geo.PrecisionCompare(smartSwappedL1, currentL1, geo.PRECISION) < 0 && geo.PrecisionCompare(smartSwappedGlobalL, currentGlobalL, geo.PRECISION) < 0 && geo.PrecisionCompare(smartSwappedGlobalL, bestSwapEdgeLength, geo.PRECISION) < 0 {
					bestSwapEdgeLength = smartSwappedGlobalL
					bestSmartSwapped = true
					bestSwapCandidate = swapCandidate
				}
			} else if swappedL1 != 0 && smartSwappedL1 != 0 {
				if geo.PrecisionCompare(smartSwappedL1, swappedL1, geo.PRECISION) < 0 {
					if smartSwappedCrossings <= currentCrossings && geo.PrecisionCompare(smartSwappedL1, currentL1, geo.PRECISION) < 0 && geo.PrecisionCompare(smartSwappedGlobalL, currentGlobalL, geo.PRECISION) < 0 && geo.PrecisionCompare(smartSwappedGlobalL, bestSwapEdgeLength, geo.PRECISION) < 0 {
						bestSwapEdgeLength = smartSwappedGlobalL
						bestSmartSwapped = true
						bestSwapCandidate = swapCandidate
					}
				} else {
					if swappedCrossings <= currentCrossings && geo.PrecisionCompare(swappedL1, currentL1, geo.PRECISION) < 0 && geo.PrecisionCompare(swappedGlobalL, currentGlobalL, geo.PRECISION) < 0 && geo.PrecisionCompare(swappedGlobalL, bestSwapEdgeLength, geo.PRECISION) < 0 {
						bestSwapEdgeLength = swappedGlobalL
						bestSmartSwapped = false
						bestSwapCandidate = swapCandidate
					}
				}
			} else {
				// They can both be 0 if the node is just a container, in which case we compare globals
				if swappedGlobalL != 0 && smartSwappedGlobalL == 0 {
					if swappedCrossings <= currentCrossings && geo.PrecisionCompare(swappedGlobalL, currentGlobalL, geo.PRECISION) < 0 && geo.PrecisionCompare(swappedGlobalL, bestSwapEdgeLength, geo.PRECISION) < 0 {
						bestSwapEdgeLength = swappedGlobalL
						bestSmartSwapped = false
						bestSwapCandidate = swapCandidate
					}
				} else if swappedGlobalL == 0 && smartSwappedGlobalL != 0 {
					if smartSwappedCrossings <= currentCrossings && geo.PrecisionCompare(smartSwappedGlobalL, currentGlobalL, geo.PRECISION) < 0 && geo.PrecisionCompare(smartSwappedGlobalL, bestSwapEdgeLength, geo.PRECISION) < 0 {
						bestSwapEdgeLength = smartSwappedGlobalL
						bestSmartSwapped = true
						bestSwapCandidate = swapCandidate
					}
				} else {
					if geo.PrecisionCompare(swappedGlobalL, smartSwappedGlobalL, geo.PRECISION) < 0 {
						if swappedCrossings <= currentCrossings && geo.PrecisionCompare(swappedGlobalL, currentGlobalL, geo.PRECISION) < 0 && geo.PrecisionCompare(swappedGlobalL, bestSwapEdgeLength, geo.PRECISION) < 0 {
							bestSwapEdgeLength = swappedGlobalL
							bestSmartSwapped = false
							bestSwapCandidate = swapCandidate
						}
					} else {
						if smartSwappedCrossings <= currentCrossings && geo.PrecisionCompare(smartSwappedGlobalL, currentGlobalL, geo.PRECISION) < 0 && geo.PrecisionCompare(smartSwappedGlobalL, bestSwapEdgeLength, geo.PRECISION) < 0 {
							bestSwapEdgeLength = smartSwappedGlobalL
							bestSmartSwapped = true
							bestSwapCandidate = swapCandidate
						}
					}
				}
			}
		}

		if bestSwapCandidate != nil {
			swapMade = true
			txn.AddOp(func() error {
				if bestSmartSwapped {
					smartSwapPositions(node, bestSwapCandidate)
				} else {
					swapPositions(node, bestSwapCandidate)
				}
				return nil
			})
			err := txn.Commit(ctx)
			if err != nil {
				return false, err
			}
			txn.Clear()
			if err := txn.UpdateState(); err != nil {
				return false, err
			}
		}
	}

	return swapMade, nil
}
