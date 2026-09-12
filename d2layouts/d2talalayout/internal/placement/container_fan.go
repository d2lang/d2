package placement

import (
	"context"
	"math"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

// orientExplicitFan considers two ranks only for a complete local directed star.
// Arms remain separate nodes: their distinct external edges and semantic owners
// are retained. The caller has excluded fixed geometry and unsupported groups.
func orientExplicitFan(ctx context.Context, g, positionGraph *layoutgraph.Graph, root *layoutgraph.Node, local map[*layoutgraph.Node]bool, direction geo.Orientation, guard *limits.WorkGuard) (bool, error) {
	if len(positionGraph.Nodes) < 3 {
		return false, nil
	}
	for _, n := range positionGraph.Nodes {
		if n.OwningContainer() != root || n.IsClusterVessel() || g.NodeToTree[n] != nil {
			return false, nil
		}
	}
	pairs := make(map[[2]*layoutgraph.Node]struct{})
	var commonFrom, commonTo *layoutgraph.Node
	first := true
	needsDirection := false
	for _, edge := range g.Edges {
		if err := guard.Step(); err != nil {
			return false, err
		}
		// Establish scope before reading arrow semantics: undirected and
		// bidirectional edges have no directed endpoints, but an in-scope
		// connection between distinct blocks still rules out a directed star.
		fromBlock, toBlock := orientationBlock(edge.From, root, local), orientationBlock(edge.To, root, local)
		if fromBlock == nil || toBlock == nil || fromBlock == toBlock {
			continue
		}
		from, to, directed := edge.DirectedEndpoints()
		if !directed {
			return false, nil
		}
		from, to = orientationBlock(from, root, local), orientationBlock(to, root, local)
		pair := [2]*layoutgraph.Node{from, to}
		if _, seen := pairs[pair]; seen {
			continue
		}
		pairs[pair] = struct{}{}
		if first {
			commonFrom, commonTo = from, to
			first = false
		} else {
			if commonFrom != from {
				commonFrom = nil
			}
			if commonTo != to {
				commonTo = nil
			}
		}
		needsDirection = needsDirection || !fanForwardSeparated(from, to, direction)
	}
	if !needsDirection || len(pairs) != len(positionGraph.Nodes)-1 || (commonFrom == nil && commonTo == nil) {
		return false, nil
	}
	hub, fanIn := commonFrom, false
	if commonTo != nil {
		hub, fanIn = commonTo, true
	}
	arms := make([]*layoutgraph.Node, 0, len(positionGraph.Nodes)-1)
	for _, n := range positionGraph.Nodes {
		if n == hub {
			continue
		}
		pair := [2]*layoutgraph.Node{hub, n}
		if fanIn {
			pair = [2]*layoutgraph.Node{n, hub}
		}
		if _, connected := pairs[pair]; !connected {
			return false, nil
		}
		arms = append(arms, n)
	}
	// Preserve the transverse order already found by placement. This avoids
	// introducing another randomized ordering or merging equivalent-looking arms.
	slices.SortStableFunc(arms, func(a, b *layoutgraph.Node) int {
		ca, cb := a.Center().X, b.Center().X
		if direction.IsHorizontal() {
			ca, cb = a.Center().Y, b.Center().Y
		}
		if ca < cb {
			return -1
		}
		if ca > cb {
			return 1
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	beforeWidth, beforeHeight := orientationFootprintSize(positionGraph, root)
	txn, err := g.NewRequestTransaction(ctx, layoutgraph.TransactionOptions{IgnoreContainerEscape: true})
	if err != nil {
		return false, err
	}
	txn.AddOp(func() error {
		rankGap, armGap := float64(layoutgraph.ConnectedNodeGap), float64(layoutgraph.NodeGap)
		arrange := func() error {
			hubAlong, hubCross := hub.Height, hub.Width
			if direction.IsHorizontal() {
				hubAlong, hubCross = hub.Width, hub.Height
			}
			armAlong, totalCross := 0.0, 0.0
			for _, arm := range arms {
				along, cross := arm.Height, arm.Width
				if direction.IsHorizontal() {
					along, cross = arm.Width, arm.Height
				}
				armAlong = math.Max(armAlong, along)
				totalCross += cross
			}
			totalCross += float64(len(arms)-1) * armGap
			hubPosition, armsPosition := 0.0, hubAlong+rankGap
			if fanIn {
				hubPosition, armsPosition = armAlong+rankGap, 0
			}
			placeFanBox(hub, hubPosition, (totalCross-hubCross)/2, direction)
			crossPosition := 0.0
			for _, arm := range arms {
				if err := guard.Step(); err != nil {
					return err
				}
				placeFanBox(arm, armsPosition, crossPosition, direction)
				cross := arm.Width
				if direction.IsHorizontal() {
					cross = arm.Height
				}
				crossPosition += cross + armGap
			}
			return nil
		}
		if err := arrange(); err != nil {
			return err
		}
		// Reserve the engine's actual spacing, including edge labels and
		// multi-shape/outside-label margins, without compacting across ranks.
		for pair := range pairs {
			rankGap = math.Max(rankGap, float64(pair[0].DeltaTo(pair[1], pair[0].TopLeft))+1)
		}
		for i, arm := range arms {
			for _, other := range arms[i+1:] {
				armGap = math.Max(armGap, float64(arm.DeltaTo(other, arm.TopLeft))+1)
			}
		}
		if err := arrange(); err != nil {
			return err
		}

		positionGraph.SyncNestedGeometry()
		width, height := orientationFootprintSize(positionGraph, root)
		if !(width >= 0 && height >= 0 && width <= limits.MaxGraphSize && height <= limits.MaxGraphSize) {
			return layoutgraph.ErrInvalidCandidate
		}
		if width*height > 2*beforeWidth*beforeHeight {
			return layoutgraph.ErrNonImprovingCandidate
		}
		for pair := range pairs {
			if !fanForwardSeparated(pair[0], pair[1], direction) {
				return layoutgraph.ErrNonImprovingCandidate
			}
		}
		return guard.Finish()
	})
	if err := txn.Commit(ctx); err != nil {
		if layoutgraph.IsCandidateRejection(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func placeFanBox(node *layoutgraph.Node, along, cross float64, direction geo.Orientation) {
	switch direction {
	case geo.Right:
		node.MoveAbsWithChildren(math.Round(along), math.Round(cross))
	case geo.Left:
		node.MoveAbsWithChildren(math.Round(-along-node.Width), math.Round(cross))
	case geo.Bottom:
		node.MoveAbsWithChildren(math.Round(cross), math.Round(along))
	case geo.Top:
		node.MoveAbsWithChildren(math.Round(cross), math.Round(-along-node.Height))
	}
}

func fanForwardSeparated(from, to *layoutgraph.Node, direction geo.Orientation) bool {
	switch direction {
	case geo.Right:
		return to.TopLeft.X >= from.TopLeft.X+from.Width
	case geo.Left:
		return from.TopLeft.X >= to.TopLeft.X+to.Width
	case geo.Bottom:
		return to.TopLeft.Y >= from.TopLeft.Y+from.Height
	case geo.Top:
		return from.TopLeft.Y >= to.TopLeft.Y+to.Height
	default:
		return false
	}
}
