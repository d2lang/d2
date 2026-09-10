package placement

import (
	"context"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/grouping"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

type containerOrientationDisabled struct{}

// A container footprint can change the later placement of its siblings. Until
// those later operations validate self-loop envelopes, leave loop-bearing
// layouts on their existing placement path. This scan happens once per attempt.
func orientationContext(ctx context.Context, g *layoutgraph.Graph) (context.Context, error) {
	guard, err := limits.NewWorkGuard(ctx, "ContainerOrientationPreflight", limits.MaxEngineWorkUnits)
	if err != nil {
		return ctx, err
	}
	for _, e := range g.Edges {
		if err := guard.Step(); err != nil {
			return ctx, err
		}
		if e.IsLoop() {
			return context.WithValue(ctx, containerOrientationDisabled{}, true), nil
		}
	}
	return ctx, guard.Finish()
}

// orientSourceInterior aligns a small container with its explicit direction, or
// aligns an otherwise unconstrained source with its surrounding vertical flow.
// Nested containers remain upright and move as rigid boxes.
// The optional move is local: it does not add another complete layout attempt.
func orientSourceInterior(ctx context.Context, g *layoutgraph.Graph, root *layoutgraph.Node, obstacles []geo.Box) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ctx.Value(containerOrientationDisabled{}) == true || root == nil || len(obstacles) != 0 || root.FixedTopLeft != nil || root.Cluster != nil || root.Sequence != nil || root.ForceHierarchy {
		return nil
	}
	direction := root.Graph.Direction(root)
	explicit := direction != geo.NONE
	positionGraph := g
	if explicit {
		// Combined graphs also list descendants and cluster members. A scope
		// orientation moves only its immediate blocks, preserving their interiors.
		positionGraph = layoutgraph.NewGraph()
		positionGraph.CopyEntitiesFrom(g)
		positionGraph.Nodes = append([]*layoutgraph.Node(nil), g.Containers[root]...)
		positionGraph.Edges = g.Edges
		positionGraph.CellSize = g.CellSize
	}
	if len(positionGraph.Nodes) < 2 || len(positionGraph.Nodes) > 16 {
		return nil
	}
	guard, err := limits.NewWorkGuard(ctx, "ContainerOrientation", limits.MaxEngineWorkUnits)
	if err != nil {
		return err
	}
	if !explicit {
		direction = g.Direction(root.OwningContainer())
		if !direction.IsVertical() || len(positionGraph.Nodes) < 3 || len(root.Edges) < 3 {
			return nil
		}
		destinations := make(map[*layoutgraph.Node]struct{})
		for _, e := range root.Edges {
			if err := guard.Step(); err != nil {
				return err
			}
			from, to, ok := e.DirectedEndpoints()
			if !ok || from != root || to == root {
				return nil
			}
			destinations[to] = struct{}{}
		}
		if len(destinations) < 2 {
			return nil
		}
	}
	local := make(map[*layoutgraph.Node]bool, len(positionGraph.Nodes))
	for _, n := range positionGraph.Nodes {
		if (!explicit && n.IsContainer()) || n.Hierarchy != nil || hasFixedDescendant(n) || n.Sequence != nil || g.IsTreeSentinel(n) || g.IsSequenceVessel(n) || n.HerdAssignment != nil {
			return nil
		}
		// Self-loop envelopes do not turn with the boxes. Keep their established
		// placement until orientation can preserve that extra route clearance.
		for _, offset := range n.LoopOffsets {
			if offset > 0 {
				return nil
			}
		}
		local[n] = true
	}
	for _, n := range positionGraph.Nodes {
		for near := range n.Nears {
			if !local[near] {
				return nil
			}
		}
	}
	if explicit {
		changed, err := orientExplicitFan(ctx, g, positionGraph, root, local, direction, guard)
		if err != nil || changed {
			return err
		}
	}
	flow, err := orientationFlow(g, root, local, explicit, guard)
	if err != nil {
		return err
	}
	// A quarter-turn needs a clear transverse flow to improve. Disconnected
	// contents, balanced cycles and already aligned compositions are left alone.
	transverse, aligned, cross := flow.across, flow.along, flow.x
	if direction.IsHorizontal() {
		transverse, aligned, cross = flow.along, flow.across, flow.y
	}
	if transverse <= aligned+1e-9 || math.Abs(cross) <= 1e-9 {
		return nil
	}
	turn := 1.0
	if direction.IsHorizontal() {
		if (cross < 0) == (direction == geo.Left) {
			turn = -1
		}
	} else if (cross < 0) != (direction == geo.Top) {
		turn = -1
	}

	beforeWidth, beforeHeight := orientationFootprintSize(positionGraph, root)
	txn, err := g.NewRequestTransaction(ctx, layoutgraph.TransactionOptions{IgnoreContainerEscape: true})
	if err != nil {
		return err
	}
	txn.AddOp(func() error {
		centers := make(map[*layoutgraph.Node]geo.Point, len(positionGraph.Nodes))
		for _, n := range positionGraph.Nodes {
			centers[n] = *n.Center()
		}
		for _, n := range positionGraph.Nodes {
			if err := guard.Step(); err != nil {
				return err
			}
			if c := g.Clusters[n]; c != nil {
				c.Arrangement = c.Arrangement.Flip()
				c.DesiredArrangement = c.Arrangement
				// Preserve the group's logical member order, but recalculate spacing for
				// its new axis, including icon and outside-label room.
				c.Padding = grouping.PaddingBetween(c, true)
				c.Resize(n)
			}
			p := centers[n]
			n.MoveAbsWithChildren(math.Round(-turn*p.Y-n.Width/2), math.Round(turn*p.X-n.Height/2))
		}
		positionGraph.SyncNestedGeometry()
		oldCell, oldGraphCell := positionGraph.CellSize, g.CellSize
		positionGraph.CellSize, g.CellSize = 1, 1
		defer func() { positionGraph.CellSize, g.CellSize = oldCell, oldGraphCell }()
		for _, axis := range []layoutAxis{horizontalAxis, verticalAxis} {
			if err := compaction(ctx, positionGraph, compactionOptions{axis: axis, includeSizes: true, factor: 1, transition: true}); err != nil {
				return err
			}
		}
		positionGraph.SyncNestedGeometry()
		tl, br := positionGraph.BoundingBox()
		width, height := orientationFootprintSize(positionGraph, root)
		if br.X-tl.X > limits.MaxGraphSize || br.Y-tl.Y > limits.MaxGraphSize || !(width >= 0 && height >= 0 && width <= limits.MaxGraphSize && height <= limits.MaxGraphSize) {
			return layoutgraph.ErrInvalidCandidate
		}
		// Upright boxes can expand substantially during a quarter-turn. Allow
		// at most one additional original footprint, including the containing
		// shape, label room, and authored minimum dimensions.
		rejected := width*height > 2*beforeWidth*beforeHeight
		if rejected {
			return layoutgraph.ErrNonImprovingCandidate
		}
		after, err := orientationFlow(g, root, local, explicit, guard)
		if err != nil {
			return err
		}
		sign := 1.0
		if direction == geo.Top || direction == geo.Left {
			sign = -1
		}
		beforeForward, afterForward := flow.y, after.y
		if direction.IsHorizontal() {
			beforeForward, afterForward = flow.x, after.x
		}
		if sign*afterForward <= sign*beforeForward+1e-9 {
			return layoutgraph.ErrNonImprovingCandidate
		}
		return guard.Finish()
	})
	// Commit validates node clearances and restores positions, dimensions and
	// cluster metadata on rejection or error. Root fitting happens after success.
	if err := txn.Commit(ctx); err != nil {
		if layoutgraph.IsCandidateRejection(err) {
			return nil
		}
		return err
	}
	return nil
}

// interiorFlow gives each distinct directed adjacency one vote, normalized by
// its Manhattan length so a single long edge cannot dominate the orientation.
type containerFlow struct{ x, y, across, along float64 }

func interiorFlow(g *layoutgraph.Graph, local map[*layoutgraph.Node]bool, guard *limits.WorkGuard) (flow containerFlow, err error) {
	return orientationFlow(g, nil, local, false, guard)
}

func orientationFlow(g *layoutgraph.Graph, root *layoutgraph.Node, local map[*layoutgraph.Node]bool, project bool, guard *limits.WorkGuard) (flow containerFlow, err error) {
	seen := make(map[[2]*layoutgraph.Node]bool)
	for _, e := range g.Edges {
		if err := guard.Step(); err != nil {
			return containerFlow{}, err
		}
		from, to, ok := e.DirectedEndpoints()
		if project {
			from, to = orientationBlock(from, root, local), orientationBlock(to, root, local)
		}
		if !ok || from == to || !local[from] || !local[to] {
			continue
		}
		key := [2]*layoutgraph.Node{from, to}
		if seen[key] {
			continue
		}
		seen[key] = true
		a, b := from.Center(), to.Center()
		dx, dy := b.X-a.X, b.Y-a.Y
		length := math.Abs(dx) + math.Abs(dy)
		if length == 0 {
			continue
		}
		flow.x += dx / length
		flow.across += math.Abs(dx) / length
		flow.y += dy / length
		flow.along += math.Abs(dy) / length
	}
	return flow, guard.Finish()
}

// Use the actual fitting path, including label room, shape geometry and authored
// minimum dimensions. Its only mutations are Width/Height; restore both before
// returning so optional admission does not resize the parent early.
func orientationFitsContainer(g *layoutgraph.Graph, root *layoutgraph.Node) bool {
	width, height := orientationFootprintSize(g, root)
	return width >= 0 && height >= 0 && width <= limits.MaxGraphSize && height <= limits.MaxGraphSize
}

// Fitting mutates only the containing node's dimensions. Restore those values
// so observing the original or optional footprint never resizes the parent early.
func orientationFootprintSize(g *layoutgraph.Graph, root *layoutgraph.Node) (width, height float64) {
	oldWidth, oldHeight := root.Width, root.Height
	defer func() { root.Width, root.Height = oldWidth, oldHeight }()
	root.FitToGraph(g, g.ContainerPadding(root, false))
	return root.Width, root.Height
}

// orientationBlock projects a semantic endpoint to its immediate placement
// block without altering the edge. Edges wholly inside a nested box project
// to one block and do not vote on their parent's orientation.
func orientationBlock(node, root *layoutgraph.Node, local map[*layoutgraph.Node]bool) *layoutgraph.Node {
	for node != nil && node != root {
		if local[node] {
			return node
		}
		if node.Cluster != nil && local[node.Cluster.Vessel] {
			return node.Cluster.Vessel
		}
		node = node.OwningContainer()
	}
	return nil
}
