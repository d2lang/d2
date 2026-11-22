package placementcost

// The original (e22f4d566) symmetry implementation, kept verbatim except for
// the ref prefix, as a reference for the frame-based implementation.

import (
	"context"
	"fmt"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/typedpool"
	"github.com/d2lang/d2/lib/geo"
)

type refSymmetryBoolScratch struct {
	values []bool
}

type refSymmetryNodeScratch struct {
	nodes []*layoutgraph.Node
	added map[*layoutgraph.Node]struct{}
}

var refSymmetryBoolPool = typedpool.New(func() *refSymmetryBoolScratch {
	return &refSymmetryBoolScratch{values: make([]bool, 0, 16)}
})

var refSymmetryNodePool = typedpool.New(func() *refSymmetryNodeScratch {
	return &refSymmetryNodeScratch{
		nodes: make([]*layoutgraph.Node, 0, 16),
		added: make(map[*layoutgraph.Node]struct{}, 16),
	}
})

func refBorrowSymmetryBools(length int) *refSymmetryBoolScratch {
	scratch := refSymmetryBoolPool.Get()
	if cap(scratch.values) < length {
		scratch.values = make([]bool, length)
		return scratch
	}
	scratch.values = scratch.values[:length]
	clear(scratch.values)
	return scratch
}

func refReturnSymmetryBools(scratch *refSymmetryBoolScratch) {
	scratch.values = scratch.values[:0]
	refSymmetryBoolPool.Put(scratch)
}

func refBorrowSymmetryNodes(capacity int) *refSymmetryNodeScratch {
	scratch := refSymmetryNodePool.Get()
	if cap(scratch.nodes) < capacity {
		scratch.nodes = make([]*layoutgraph.Node, 0, capacity)
	} else {
		scratch.nodes = scratch.nodes[:0]
	}
	clear(scratch.added)
	return scratch
}

func refReturnSymmetryNodes(scratch *refSymmetryNodeScratch) {
	clear(scratch.nodes[:cap(scratch.nodes)])
	scratch.nodes = scratch.nodes[:0]
	clear(scratch.added)
	refSymmetryNodePool.Put(scratch)
}

// refNodesSymmetry sums local symmetry scores for nodes in order.
func refNodesSymmetry(ctx context.Context, nodes layoutgraph.Nodes, edgeAbductions []*layoutgraph.EdgeAbduction) (float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("EdgeLength: %w", err)
	}
	sum := 0.0
	for i, node := range nodes {
		if err := scoringCancellationError(ctx, i); err != nil {
			return 0, err
		}
		symmetry, err := refNodeSymmetry(ctx, node, edgeAbductions, true)
		if err != nil {
			return 0, err
		}
		sum += symmetry
	}
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("EdgeLength: %w", err)
	}
	return sum, nil
}

// refExportedNodeSymmetry evaluates a node and the neighboring symmetry it contributes to.
func refExportedNodeSymmetry(ctx context.Context, node *layoutgraph.Node, edgeAbductions []*layoutgraph.EdgeAbduction) (float64, error) {
	return refNodeSymmetry(ctx, node, edgeAbductions, true)
}

func refNodeSymmetry(ctx context.Context, node *layoutgraph.Node, edgeAbductions []*layoutgraph.EdgeAbduction, checkNeighbors bool) (float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("EdgeLength: %w", err)
	}
	score := 0.0
	maxScore := 0

	usedEdgeAbductionsScratch := refBorrowSymmetryBools(len(edgeAbductions))
	defer refReturnSymmetryBools(usedEdgeAbductionsScratch)
	usedEdgeAbductions := usedEdgeAbductionsScratch.values
	symmetryNodesScratch := refBorrowSymmetryNodes(len(node.Edges))
	dedupedAdjacentNodes := symmetryNodesScratch.nodes
	defer func() {
		symmetryNodesScratch.nodes = dedupedAdjacentNodes
		refReturnSymmetryNodes(symmetryNodesScratch)
	}()
	added := symmetryNodesScratch.added

	// We consider container children with protruding edges like neighbors for symmetry, their score matters for the container score
	if node.IsContainer() {
		childNodes := make([]*layoutgraph.Node, 0)
		// Within node placement, we use edge abductions to discern children; otherwise, the AdjacentNodes are correct
		if edgeAbductions == nil {
			for i, child := range node.Graph.Containers[node] {
				if err := scoringCancellationError(ctx, i); err != nil {
					return 0, err
				}
				for j, e := range child.Edges {
					if err := scoringCancellationError(ctx, j); err != nil {
						return 0, err
					}
					adj := child.Adjacent(e)
					// We only want to consider symmetry of child nodes with protrutions
					if !adj.Container.IsDescendantOf(node) {
						added[child] = struct{}{}
						childNodes = append(childNodes, child)
						break
					}
				}
			}
		} else {
			for i, edgeAbduction := range edgeAbductions {
				if err := scoringCancellationError(ctx, i); err != nil {
					return 0, err
				}
				if usedEdgeAbductions[i] {
					continue
				}
				if edgeAbduction.CurrentFrom == node {
					if edgeAbduction.OriginallyFrom != nil {
						usedEdgeAbductions[i] = true
						if _, in := added[edgeAbduction.OriginallyFrom]; in {
							continue
						}
						added[edgeAbduction.OriginallyFrom] = struct{}{}
						childNodes = append(childNodes, edgeAbduction.OriginallyFrom)
					}
				}
				if edgeAbduction.CurrentTo == node {
					if edgeAbduction.OriginallyTo != nil {
						usedEdgeAbductions[i] = true
						if _, in := added[edgeAbduction.OriginallyTo]; in {
							continue
						}
						added[edgeAbduction.OriginallyTo] = struct{}{}
						childNodes = append(childNodes, edgeAbduction.OriginallyTo)
					}
				}
			}
		}

		for i, nodeReplacement := range childNodes {
			if err := scoringCancellationError(ctx, i); err != nil {
				return 0, err
			}
			symmetry, err := refNodeSymmetry(ctx, nodeReplacement, edgeAbductions, checkNeighbors)
			if err != nil {
				return 0, err
			}
			score += symmetry
			maxScore++
		}
	}

	// If a node has multiple edges to node A on one side, and only 1 edge to node B on another symmetric side, count that as symmetrical
	//   ┌───────────┐
	//   ▼           │
	// ┌──┐        ┌─┴┐       ┌──┐
	// │  │◄───────┤  ├──────►│  │
	// └──┘        └──┘       └──┘
	for edgeIndex, e := range node.Edges {
		if err := scoringCancellationError(ctx, edgeIndex); err != nil {
			return 0, err
		}
		adjacentNode := node.Adjacent(e)
		adjacentNodeReplacement := adjacentNode

		// Let's say A and A's container are both connected to B
		// ┌──────────┐
		// │          │
		// │   ┌───┐  │  ┌───┐
		// │   │ A ├──┼─►│B  │
		// │   └───┘  │  └───┘
		// │          │    ▲
		// └──────┬───┘    │
		//        │        │
		//        └────────┘
		// We've already considered A's symmetry above, so we should only count B as an adjacent node if it's not connected to a child
		isConnectedToChild := false
		if node.IsContainer() {
			for i, edgeAbduction := range edgeAbductions {
				if err := scoringCancellationError(ctx, i); err != nil {
					return 0, err
				}
				if (edgeAbduction.CurrentFrom == node) && (edgeAbduction.CurrentTo == adjacentNode) {
					if edgeAbduction.OriginallyFrom != nil {
						isConnectedToChild = true
						break
					}
				}
				if (edgeAbduction.CurrentFrom == adjacentNode) && (edgeAbduction.CurrentTo == node) {
					usedEdgeAbductions[i] = true
					if edgeAbduction.OriginallyTo != nil {
						isConnectedToChild = true
						break
					}
				}
			}
		}
		if isConnectedToChild {
			continue
		}

		// If we're considering symmetry for node B in the above diagram, we'll want to resolve to adjacent node to A, not container for A
		for i, edgeAbduction := range edgeAbductions {
			if err := scoringCancellationError(ctx, i); err != nil {
				return 0, err
			}
			if usedEdgeAbductions[i] {
				continue
			}
			// For symmetry, use cluster vessels
			if edgeAbduction.CurrentFrom != nil && edgeAbduction.CurrentFrom.IsClusterVessel() {
				continue
			}
			if edgeAbduction.CurrentTo != nil && edgeAbduction.CurrentTo.IsClusterVessel() {
				continue
			}
			if node.Graph.IsSequenceVessel(edgeAbduction.CurrentFrom) || node.Graph.IsSequenceVessel(edgeAbduction.CurrentTo) {
				continue
			}
			if (edgeAbduction.CurrentFrom == node) && (edgeAbduction.CurrentTo == adjacentNode) {
				usedEdgeAbductions[i] = true
				if edgeAbduction.OriginallyTo != nil {
					adjacentNodeReplacement = edgeAbduction.OriginallyTo
				}
				break
			}
			if (edgeAbduction.CurrentFrom == adjacentNode) && (edgeAbduction.CurrentTo == node) {
				usedEdgeAbductions[i] = true
				if edgeAbduction.OriginallyFrom != nil {
					adjacentNodeReplacement = edgeAbduction.OriginallyFrom
				}
				break
			}
		}

		// For clusters within containers, the Originally is pointed to the cluster node still
		//              ┌─────────┐
		//              │         │
		//              │         │
		//              │  ┌─┐    │
		// ┌──┐     ┌───┼─►│ │    │
		// │  ├─────┤   │  └─┘    │
		// └──┘     │   │         │
		//          │   │  ┌─┐    │
		//          └───┼─►│ │    │
		//              │  └─┘    │
		//              │         │
		//              └─────────┘
		if adjacentNodeReplacement.Cluster.IsActive() {
			adjacentNodeReplacement = adjacentNodeReplacement.Cluster.Vessel
		} else if adjacentNodeReplacement.Sequence.IsActive() {
			adjacentNodeReplacement = adjacentNodeReplacement.Sequence.Vessel
		}

		if _, in := added[adjacentNodeReplacement]; in {
			continue
		}

		dedupedAdjacentNodes = append(dedupedAdjacentNodes, adjacentNodeReplacement)
		added[adjacentNodeReplacement] = struct{}{}
	}

	// When we're resolving symmetry for children during node placement, all their edges were abducted to their containers, so here we find those
	for i, edgeAbduction := range edgeAbductions {
		if err := scoringCancellationError(ctx, i); err != nil {
			return 0, err
		}
		var connectedNode *layoutgraph.Node
		if edgeAbduction.OriginallyFrom == node {
			// When the adjacent node is also a container
			if edgeAbduction.OriginallyTo != nil &&
				edgeAbduction.CurrentTo != nil &&
				!edgeAbduction.CurrentTo.IsClusterVessel() &&
				!node.Graph.IsSequenceVessel(edgeAbduction.CurrentTo) {
				connectedNode = edgeAbduction.OriginallyTo
			} else {
				connectedNode = edgeAbduction.CurrentTo
			}
		}
		if edgeAbduction.OriginallyTo == node {
			if edgeAbduction.OriginallyFrom != nil &&
				edgeAbduction.CurrentFrom != nil &&
				!edgeAbduction.CurrentFrom.IsClusterVessel() &&
				!node.Graph.IsSequenceVessel(edgeAbduction.CurrentFrom) {
				connectedNode = edgeAbduction.OriginallyFrom
			} else {
				connectedNode = edgeAbduction.CurrentFrom
			}
		}
		if connectedNode != nil {
			if _, in := added[connectedNode]; in {
				continue
			}

			dedupedAdjacentNodes = append(dedupedAdjacentNodes, connectedNode)
			added[connectedNode] = struct{}{}
		}
	}

	for i := 0; i < len(dedupedAdjacentNodes); i++ {
		if err := scoringCancellationError(ctx, i); err != nil {
			return 0, err
		}
		d := node.DistanceTo(dedupedAdjacentNodes[i], true)
		if d > 1200 {
			dedupedAdjacentNodes = append(dedupedAdjacentNodes[:i], dedupedAdjacentNodes[i+1:]...)
			i--
		}
	}

	matchedScratch := refBorrowSymmetryBools(len(dedupedAdjacentNodes))
	defer refReturnSymmetryBools(matchedScratch)
	matched := matchedScratch.values
	s, err := refComputeSymmetryScoreInto(ctx, node, dedupedAdjacentNodes, matched)
	if err != nil {
		return 0, err
	}
	score += s
	maxScore += len(dedupedAdjacentNodes)

	// Node A may not have symmetry itself, but we still don't want it to move because it contributes to B's symmetry
	// So we check neighbors, see that B is symmetrical, which makes A also symmetrical
	// ┌──┐   ┌──┐   ┌──┐
	// │A │◄──┤B ├──►│C │
	// └──┘   └──┘   └──┘
	if checkNeighbors {
		for i, adjacentNode := range dedupedAdjacentNodes {
			if err := scoringCancellationError(ctx, i); err != nil {
				return 0, err
			}
			if matched[i] {
				continue
			}
			adjNodeSymm, err := refNodeSymmetry(ctx, adjacentNode, edgeAbductions, false)
			if err != nil {
				return 0, err
			}
			score += adjNodeSymm
		}
	}

	// No adjacent nodes, not a container of a child with protruding edges
	if maxScore == 0 {
		return 0.0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("EdgeLength: %w", err)
	}
	return score / float64(maxScore), nil
}

/*
*
// This is ideal. Symmetrical through the center
// ┌──┐   ┌──┐   ┌──┐
// │  │◄──┤  ├──►│  │
// └──┘   └──┘   └──┘
//
// But this is not bad and should still be rewarded
//
//	  ┌──┐
//	┌─┤  ├─┐
//	│ └──┘ │
//	│      │
// ┌▼┐    ┌▼─┐
// │ │    │  │
// └─┘    └──┘
// So we give it a small score
*/
// refComputeSymmetryScoreInto writes match state into caller-owned storage so the
// optimizer can score symmetry without allocating.
func refComputeSymmetryScoreInto(ctx context.Context, node *layoutgraph.Node, neigh []*layoutgraph.Node, matchedSlice []bool) (float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("EdgeLength: %w", err)
	}
	n := len(neigh)
	if n < 2 {
		return 0, nil
	}
	matchedSlice = matchedSlice[:n]
	clear(matchedSlice)

	score := 0.0

	ownSiblings := node.Graph.Containers[node.Container]
	var otherSiblings []*layoutgraph.Node

	for i, n1 := range neigh {
		if err := scoringCancellationError(ctx, i); err != nil {
			return 0, err
		}
		if matchedSlice[i] {
			continue
		}
		if n1.Container != node.Container {
			otherSiblings = node.Graph.Containers[n1.Container]
		} else {
			otherSiblings = nil
		}

		bestIdx := -1
		best := 0.0

		// When two nodes are considered mirrored for the sake of symmetry, we only want ones under the same container
		// This just doesn't look aesthetically symmetrical, even if it is (in a larger graph at least)
		//                    ┌──────────┐
		//                    │          │
		// ┌──┐        ┌─-┐   │   ┌──┐   │
		// │  │◄───────┤  ├───┼──►│  │   │
		// └──┘        └──┘   │   └──┘   │
		//                    │          │
		//                    └──────────┘
		for j := i + 1; j < n; j++ {
			if err := scoringCancellationError(ctx, j-i-1); err != nil {
				return 0, err
			}
			if matchedSlice[j] {
				continue
			}
			n2 := neigh[j]
			if n1.Container != n2.Container {
				continue
			}
			// When nodes vary by a lot in size, it also doesn't come off as symmetrical
			area1, area2 := n1.Area(), n2.Area()
			if area1 > 2*area2 || area2 > 2*area1 {
				continue
			}

			ms := math.Inf(-1)
			// The two nodes can be symmetrical across the x or y axis
			for _, isX := range [...]bool{true, false} {
				axis := node.TopLeft.X + node.Width/2
				if !isX {
					axis = node.TopLeft.Y + node.Height/2
				}
				if isMirrored(n1, n2, isX, axis) {
					if node.OverlapsAlongDimension(n1, isX, true) &&
						node.OverlapsAlongDimension(n2, isX, true) {
						ms = 2
					} else {
						ms = 0.5
					}
					// no need to continue, as there's no way n1-n2 is aligned both horizontally and vertically
					break
				}
			}
			// skip obstruction search if the nodes aren't aligned
			if ms < 0 {
				continue
			}

			// We only want to count mirrored objects when they have no obstructions from node to it
			// This won't look symmetrical
			//             ┌─────────┐
			//             │         │
			// ┌───┐     ┌─┴┐  ┌─┐  ┌▼─┐
			// │   ◄─────┤  │  │ │  │  │
			// └───┘     └──┘  │ │  └──┘
			//                 └─┘
			isObstructed, err := refObstructed(ctx, node, n1, n2, ownSiblings, otherSiblings)
			if err != nil {
				return 0, err
			}
			if isObstructed {
				continue
			}

			if ms > best {
				best, bestIdx = ms, j
				if ms == 2 {
					break
				}
			}
		}

		if bestIdx != -1 {
			matchedSlice[i], matchedSlice[bestIdx] = true, true
			score += best
		}
	}

	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("EdgeLength: %w", err)
	}
	return score, nil
}

func refObstructed(ctx context.Context, center, a, b *layoutgraph.Node, sib1, sib2 []*layoutgraph.Node) (bool, error) {
	bounds := scoringNodeBounds(center).including(scoringNodeBounds(a)).including(scoringNodeBounds(b))
	check := func(sibs []*layoutgraph.Node) (bool, error) {
		for i, s := range sibs {
			if err := scoringCancellationError(ctx, i); err != nil {
				return false, err
			}
			if s == nil || s.TopLeft == nil || s.Graph != center.Graph {
				continue
			}
			if s == center || s == a || s == b {
				continue
			}
			if bounds.excludes(s) {
				continue
			}
			if center.IsDescendantOf(s) || a.IsDescendantOf(s) || b.IsDescendantOf(s) {
				continue
			}
			if s.IsDescendantOf(center) || s.IsDescendantOf(a) || s.IsDescendantOf(b) {
				continue
			}
			if s.PassesThrough(center.Center(), a.Center()) ||
				s.PassesThrough(center.Center(), b.Center()) {
				return true, nil
			}
		}
		return false, nil
	}
	isObstructed, err := check(sib1)
	if err != nil || isObstructed {
		return isObstructed, err
	}
	return check(sib2)
}

var _ = geo.NewPoint
