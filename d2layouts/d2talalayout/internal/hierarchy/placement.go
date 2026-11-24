package hierarchy

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"math/rand"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"

	"github.com/d2lang/d2/lib/geo"
)

const (
	crossingSpacing     = 50.0
	parentSpacing       = 300.0
	siblingDummySpacing = 50.0
	siblingSpacing      = 60.0
)

type placementNode struct {
	graphNode *layoutgraph.Node
	level     int
	// this is the level index considering the order left to right as if it was a flatten array, no container depth considered
	rank      int
	aboves    map[*placementNode]struct{}
	belows    map[*placementNode]struct{}
	container *placementNode
	children  []*placementNode
	// flags whether or not we should recurse to optimize the children's position
	// currently only for SQL Tables as we need to consider their columns as `children`
	// but we don't want to change their position
	// also, tables are simple nodes and we create children as dummy nodes so that we can optimize
	// the tables position based on the columns (as routing from column to column can change crossings)
	optimizeChildrenCrossings bool

	// a flag for intermediate nodes used to break long connections
	// a connection from level 1 to 3 is broken with a dummy node at level 2
	// (so that all `aboves`/`belows` are 1 level apart)
	// this flags marks the node at level 2 as being part of the chain
	isChainningConnection bool

	// isContainer quickly checks if a node is a container or not
	// Table nodes have children, but aren't containers
	isContainer bool

	isDummy bool
}

func newPlacementNode(level int, node *layoutgraph.Node) *placementNode {
	return &placementNode{
		graphNode:                 node,
		level:                     level,
		rank:                      0,
		aboves:                    make(map[*placementNode]struct{}),
		belows:                    make(map[*placementNode]struct{}),
		container:                 nil,
		children:                  nil,
		optimizeChildrenCrossings: true,
		isChainningConnection:     false,
		isContainer:               false,
		isDummy:                   false,
	}
}

func (pn *placementNode) degree() int {
	if pn.isDummy {
		// dummy nodes have only 2 edges
		return 2
	}
	return countHierarchyStructuralEdges(pn.graphNode)
}

func (pn *placementNode) findNonDummyNode(above bool) *placementNode {
	firstNeighbor := func(n *placementNode, isAbove bool) *placementNode {
		nodes := n.aboves
		if !isAbove {
			nodes = n.belows
		}
		for connected := range nodes {
			return connected
		}
		return nil
	}
	n := pn
	for n.isDummy && n.isChainningConnection {
		n = firstNeighbor(n, above)
	}
	return n
}

func (below *placementNode) connect(above *placementNode) {
	if above.level == below.level {
		return
	}
	if above.level > below.level {
		below, above = above, below
	}
	below.aboves[above] = struct{}{}
	above.belows[below] = struct{}{}
}

// Placement configures coordinate assignment for one hierarchy attempt.
// CompactSpacing removes the outer rank margin and shortens boundaries with
// one unlabeled edge. Both placements preserve padding around real containers.
type Placement struct {
	Random         *rand.Rand
	CompactSpacing bool
	ClearLoopRanks bool
}

func Place(ctx context.Context, g *layoutgraph.Graph, root *layoutgraph.Node, random *rand.Rand) error {
	return (Placement{Random: random}).Place(ctx, g, root)
}

func (p Placement) Place(ctx context.Context, g *layoutgraph.Graph, root *layoutgraph.Node) (err error) {
	if err := layoutgraph.Validate(ctx, "PlaceHierarchies", g); err != nil {
		return err
	}
	if len(g.Containers[root]) == 0 {
		return nil
	}
	ctx, guard, err := layoutgraph.EnsureTransactionWorkGuard(ctx, "PlaceHierarchiesTransactions")
	if err != nil {
		return err
	}
	state := layoutgraph.NewGraphStateSnapshot(layoutgraph.GraphStateSnapshotOptions{
		CaptureTopology:   true,
		CaptureEdgeRoutes: true,
	})
	if err := state.UpdateWithWorkGuard(g, guard); err != nil {
		return err
	}
	rollback := &layoutgraph.Transaction{Graph: g, PriorGraphState: state}
	complete := false
	defer func() {
		if !complete {
			rollback.Rollback()
		}
	}()

	// The hierarchy stage places each ranked component independently. Keep
	// cross-component proximity hints for packing the completed components.
	restoreNears, err := layoutgraph.Nodes(g.Nodes).DetachHierarchyNears(ctx)
	if err != nil {
		return err
	}
	defer restoreNears()

	if err := p.place(ctx, g, root); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("PlaceHierarchies: %w", err)
	}
	complete = true
	return nil
}

func (p Placement) place(ctx context.Context, g *layoutgraph.Graph, root *layoutgraph.Node) error {

	hGraph := layoutgraph.NewGraph()
	hGraph.CopyEntitiesFrom(g)
	for _, child := range g.Containers[root] {
		hGraph.AddNodeUnchecked(child)
	}

	edgeAbductions := g.AbductEdges(root, hGraph)
	edgesRestored := false
	var splitOwnership layoutgraph.NodeGraphOwnershipJournal
	defer func() {
		if !edgesRestored {
			g.RestoreEdgeAbductions(edgeAbductions)
		}
		// The split follows Nears outside hGraph.Nodes and temporarily redirects
		// their owners too. Restore every exact pre-split owner before retaining the
		// hierarchy stage's existing successful owner state for nodes in g.
		splitOwnership.Restore()
		layoutgraph.Nodes(g.Nodes).SetGraphReference(g)
	}()

	for _, child := range g.Containers[root] {
		if child.Hierarchy == nil && child.IsContainer() {
			if err := p.place(ctx, g, child); err != nil {
				return err
			}
		}
	}

	subgraphs, splitOwnership, err := hGraph.SplitSubgraphsTracked(ctx, layoutgraph.SplitOptions{IncludeNears: true}, nil)
	if err != nil {
		return err
	}
	for _, subgraph := range subgraphs {
		if subgraph.Nodes[0].Hierarchy == nil {
			continue
		}
		subgraph.ComputeCellSize()
		layoutgraph.Nodes(g.Nodes).SetGraphReference(subgraph)
		edgeAbductions = subgraph.RestoreEdgeAbductions(edgeAbductions)
		if err := p.placeNodesInHierarchy(ctx, subgraph); err != nil {
			return err
		}
	}

	g.RestoreEdgeAbductions(edgeAbductions)
	edgesRestored = true
	return nil
}

func (p Placement) placeNodesInHierarchy(ctx context.Context, g *layoutgraph.Graph) (err error) {
	// A hierarchy may already be attached by an earlier stage. Never let derived
	// state override an absolute constraint on a member, including a nested
	// container member.
	if hierarchy := g.Nodes[0].Hierarchy; hierarchy != nil {
		for node := range hierarchy.Levels() {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("PlaceHierarchies: %w", err)
			}
			if node.FixedTopLeft != nil {
				return nil
			}
		}
	}
	ctx, guard, err := layoutgraph.EnsureTransactionWorkGuard(ctx, "PlaceHierarchiesTransactions")
	if err != nil {
		return err
	}
	state := layoutgraph.NewGraphStateSnapshot(layoutgraph.GraphStateSnapshotOptions{
		CaptureEdgeRoutes: true,
	})
	if err := state.UpdateWithWorkGuard(g, guard); err != nil {
		return err
	}
	rollback := &layoutgraph.Transaction{Graph: g, PriorGraphState: state}
	complete := false
	defer func() {
		if !complete {
			rollback.Rollback()
		}
	}()

	placementNodes := createPlacementNodes(g, g.Nodes, p.Random)
	connectPlacementNodes(g, placementNodes)
	byLevel := groupPlacementNodesByLevel(placementNodes)
	initializeRanks(byLevel)
	breakLongConnections(placementNodes, byLevel)
	minimizeHierarchyCrossings(byLevel)
	if err := globalSifting(byLevel); err != nil {
		return err
	}
	if err := p.placeAndAlignLevels(ctx, g, byLevel); err != nil {
		return err
	}
	direction := g.Nodes[0].ContainerDirection()
	if direction == geo.Left || direction == geo.Top {
		mirrorRankBlocks(g, direction)
	}
	if p.ClearLoopRanks {
		if err := clearLoopRanks(g, byLevel, direction, guard); err != nil {
			return err
		}
	}
	complete = true
	return nil
}

func (p Placement) placeAndAlignLevels(ctx context.Context, g *layoutgraph.Graph, byLevel map[int][]*placementNode) error {
	isHorizontal := IsHorizontal(g.Nodes)
	if isHorizontal {
		transpose(g)
		defer transpose(g)
	}
	p.placeNodesByLevel(g, byLevel, isHorizontal)
	for level := 0; level < len(byLevel); level++ {
		byLevel[level] = removeTableColumnNodes(byLevel[level])
		computeLevelRanks(byLevel[level])
	}
	return p.alignHierarchy(ctx, byLevel)
}

// create placement nodes recursively and set the container whenever required
func createPlacementNodes(g *layoutgraph.Graph, nodes []*layoutgraph.Node, rand *rand.Rand) []*placementNode {
	var placementNodes []*placementNode
	for _, node := range nodes {
		pn := newPlacementNode(node.HierarchyLevel(), node)
		placementNodes = append(placementNodes, pn)
		if node.IsTable() {
			pn.children = make([]*placementNode, node.NumColumns())
			pn.optimizeChildrenCrossings = false
			pn.isContainer = false
			for i := 0; i < node.NumColumns(); i++ {
				pn.children[i] = newPlacementNode(pn.level, nil)
				pn.children[i].container = pn
				pn.children[i].isDummy = true
			}
		} else {
			pn.optimizeChildrenCrossings = true
			pn.children = make([]*placementNode, 0, len(g.Containers[node]))
			for _, child := range createPlacementNodes(g, g.Containers[node], rand) {
				if child.container == nil {
					child.container = pn
					pn.children = append(pn.children, child)
				}
			}
			pn.isContainer = len(pn.children) > 0
		}
	}
	if rand != nil {
		rand.Shuffle(len(placementNodes), func(i, j int) {
			placementNodes[i], placementNodes[j] = placementNodes[j], placementNodes[i]
		})
	}
	return placementNodes
}

func connectPlacementNodes(g *layoutgraph.Graph, nodes []*placementNode) {
	nodeToPlacementNode := map[*layoutgraph.Node]*placementNode{}
	queue := make([]*placementNode, 0, len(nodes))
	queue = append(queue, nodes...)
	for len(queue) > 0 {
		pn := queue[0]
		queue = queue[1:]
		nodeToPlacementNode[pn.graphNode] = pn
		queue = append(queue, pn.children...)
	}

	for _, edge := range g.Edges {
		from := nodeToPlacementNode[edge.From]
		to := nodeToPlacementNode[edge.To]
		to.connect(from)

		if edge.IsBetweenTableColumns() {
			// connect the rows
			from = from.children[*edge.FromTableColumnIndex]
			to = to.children[*edge.ToTableColumnIndex]
			to.connect(from)
		}
	}
}

// breakLongConnections creates chains of dummy nodes when two nodes are connected but their level distance is greater than 1
// . ┌──────────┐           ┌──────────┐
// . │          │           │          │
// . └────┬─────┘           └────┬─┬───┘
// .      │                      │ │
// . ┌────▼─────┐                │ │
// . │          │                │ │
// . │          ◄────────────────┘ │
// . │          │              #########
// . │          │              # dummy #
// . │          │              #########
// . └─────┬────┘                  │
// .       │                       │
// .  ┌────▼─────┐         ┌───────▼──┐
// .  │          │         │          │
// .  └──────────┘         └──────────┘
func breakLongConnections(nodes []*placementNode, byLevel map[int][]*placementNode) []*placementNode {
	var dummies []*placementNode
	for _, pn := range nodes {
		belows := make([]*placementNode, 0, len(pn.belows))
		for below := range pn.belows {
			if below.level-pn.level > 1 {
				belows = append(belows, below)
			}
		}
		slices.SortStableFunc(belows, func(a, b *placementNode) int {
			if order := cmp.Compare(a.rank, b.rank); order != 0 {
				return order
			}
			return cmp.Compare(a.level, b.level)
		})
		for _, below := range belows {
			delete(pn.belows, below)
			delete(below.aboves, pn)

			above := pn
			for level := pn.level + 1; level < below.level; level++ {
				dummyID := -layoutgraph.EntityID(len(dummies) + 1)
				dummy := newPlacementNode(level, layoutgraph.NewNode(dummyID, 1, 1))
				dummy.isChainningConnection = true
				dummy.isDummy = true
				dummy.rank = len(byLevel[level])
				dummy.connect(above)
				dummies = append(dummies, dummy)
				byLevel[level] = append(byLevel[level], dummy)
				above = dummy
			}
			below.connect(above)
		}
		dummies = append(dummies, breakLongConnections(pn.children, byLevel)...)
	}
	return dummies
}

func groupPlacementNodesByLevel(nodes []*placementNode) map[int][]*placementNode {
	byLevel := map[int][]*placementNode{}
	for _, pn := range nodes {
		byLevel[pn.level] = append(byLevel[pn.level], pn)
	}
	return byLevel
}

// removeTableColumnNodes removes nodes where `graphNode == nil` and restores edge references
func removeTableColumnNodes(nodes []*placementNode) []*placementNode {
	newNodes := make([]*placementNode, 0, len(nodes))
	for _, node := range nodes {
		if node.isDummy {
			if node.graphNode == nil {
				// table column
				continue
			} else if node.isChainningConnection {
				above := node.findNonDummyNode(true)
				below := node.findNonDummyNode(false)
				if above.graphNode == nil || below.graphNode == nil {
					// dummy node chaining a long connection between two table columns
					continue
				}
			}
		}
		newNodes = append(newNodes, node)
		node.children = removeTableColumnNodes(node.children)
	}
	return newNodes
}

func (p Placement) placeNodesByLevel(g *layoutgraph.Graph, byLevel map[int][]*placementNode, isHorizontal bool) {
	yOffset := 0.0
	for i := 0; i < len(byLevel); i++ {
		var nodes []*layoutgraph.Node
		levelHeight := 0.0
		xOffset := 0.0
		for _, pn := range byLevel[i] {
			nodes = append(nodes, pn.graphNode)
			pn.graphNode.TopLeft = geo.NewPoint(xOffset, yOffset)
			if !pn.isDummy {
				placeDescendants(g, pn)
				levelHeight = math.Max(levelHeight, pn.graphNode.Height)
				xOffset += math.Ceil(pn.graphNode.Width) + siblingSpacing
			} else {
				xOffset += siblingSpacing
			}
		}
		distanceToNextLevel := p.minimumDistanceToNextLevel(byLevel[i], isHorizontal)
		yOffset += math.Ceil(levelHeight + distanceToNextLevel + 2*layoutgraph.MinPortClearance)

		// move to center
		center := layoutgraph.Nodes(nodes).Center()
		center.Y = math.Ceil(center.Y)
		for _, node := range nodes {
			yDiff := math.Ceil(center.Y - node.Center().Y)
			node.Translate(0, yDiff)
		}
	}
}

// recursively place all descendants of a given container
func placeDescendants(g *layoutgraph.Graph, root *placementNode) {
	if !root.isContainer {
		return
	}
	padding := g.ContainerPadding(root.graphNode, true)
	tl := root.graphNode.InsidePlacement(1, 1, padding)
	x := tl.X
	for _, child := range root.children {
		child.graphNode.TopLeft = geo.NewPoint(x, tl.Y)
		placeDescendants(g, child)
		x += math.Ceil(child.graphNode.Width) + siblingSpacing
	}
	childrenTL, childrenBR := layoutgraph.Nodes(g.Containers[root.graphNode]).BoundingBox()
	root.graphNode.FitToBoundingBox(childrenTL, childrenBR, padding)
}

// minimumDistanceToNextLevel reserves room for crossings and labels between adjacent ranks.
func (p Placement) minimumDistanceToNextLevel(levelNodes []*placementNode, isHorizontal bool) float64 {
	var scratch crossingScratch
	segments := scratch.crossLevelSegments(levelNodes, false, true)
	crossings := scratch.countCrossings(segments)
	crossingsDistance := float64(crossings) * crossingSpacing

	nextLevelEdges := 0
	maxLabelSize := 0.
	iterAllDescendants(levelNodes, func(node *placementNode) {
		if node.isDummy {
			return
		}
		for _, edge := range node.graphNode.Edges {
			if edge.Label == nil && !p.CompactSpacing {
				continue
			}
			adjacent := node.graphNode.Adjacent(edge)
			if adjacent.Hierarchy == nil || adjacent.HierarchyLevel() != node.level+1 {
				continue
			}
			nextLevelEdges++
			if edge.Label == nil {
				continue
			}
			if isHorizontal {
				maxLabelSize = math.Max(maxLabelSize, edge.Label.Width)
			} else {
				maxLabelSize = math.Max(maxLabelSize, edge.Label.Height)
			}
		}
	})
	// Parallel edges share a crossing segment, but still need separate routing lanes.
	if p.CompactSpacing && len(segments) == 1 && nextLevelEdges == 1 && maxLabelSize == 0 {
		return layoutgraph.ConnectedNodeGap - 2*layoutgraph.MinPortClearance
	}
	// Routes can share space at crossings, but the label needs its full extent.
	return math.Round(math.Max(math.Min(crossingsDistance, parentSpacing), maxLabelSize+crossingSpacing))
}

func isSource(n *layoutgraph.Node) bool {
	for _, e := range n.Edges {
		if !isHierarchyStructuralEdge(e) {
			continue
		}
		if e.HasSourceArrow() == e.HasTargetArrow() || (e.HasSourceArrow() && e.From == n) || (e.HasTargetArrow() && e.To == n) {
			return false
		}
	}
	return true
}

func isSink(n *layoutgraph.Node) bool {
	for _, e := range n.Edges {
		if !isHierarchyStructuralEdge(e) {
			continue
		}
		if e.HasSourceArrow() == e.HasTargetArrow() || (e.HasTargetArrow() && e.From == n) || (e.HasSourceArrow() && e.To == n) {
			return false
		}
	}
	return true
}

func iterContainersBFS(g *layoutgraph.Graph, apply func(n *layoutgraph.Node)) {
	queue := make([]*layoutgraph.Node, 0, len(g.Nodes))
	queue = append(queue, g.Nodes...)
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		if curr.IsContainer() {
			queue = append(queue, g.Containers[curr]...)
		}
		apply(curr)
	}
}

func transpose(g *layoutgraph.Graph) {
	iterContainersBFS(g, func(n *layoutgraph.Node) {
		n.Transpose()
		// Container fitting runs in the transposed coordinate system too.
		n.DesiredWidth, n.DesiredHeight = n.DesiredHeight, n.DesiredWidth
		if n.Label != nil {
			n.Label.Width, n.Label.Height = n.Label.Height, n.Label.Width
			n.Label.Position = n.Label.Position.Transposed()
		}
		if n.Icon != nil {
			n.Icon.Position = n.Icon.Position.Transposed()
		}
	})
}

// Reversing rank order moves each block with its contents. Its labels and
// icons still occupy the same authored sides of the block after the move.
func mirrorRankBlocks(g *layoutgraph.Graph, direction geo.Orientation) {
	for _, block := range g.Nodes {
		before := *block.TopLeft
		block.Mirror(direction == geo.Left, direction == geo.Top)
		dx, dy := block.TopLeft.X-before.X, block.TopLeft.Y-before.Y
		for _, child := range g.Containers[block] {
			child.MoveWithChildren(dx, dy)
		}
	}
}

func (pn *placementNode) DebugID() string {
	return fmt.Sprintf("%s[%d, %d]", pn.graphNode.DebugID(), pn.level, pn.rank)
}
