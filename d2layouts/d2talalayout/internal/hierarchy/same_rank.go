package hierarchy

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

// PlaceSameRanks runs on restored authored geometry, before building the
// routing visibility graph. A rank is a board-wide coordinate. Enclosing
// shapes resize around the final positions of their ranked descendants.
func PlaceSameRanks(ctx context.Context, g *layoutgraph.Graph) error {
	if len(g.SameRankGroups) == 0 {
		return nil
	}
	guard, err := limits.NewWorkGuard(ctx, "SameRank", limits.MaxEngineWorkUnits)
	if err != nil {
		return err
	}
	byID := make(map[layoutgraph.EntityID]*layoutgraph.Node, len(g.Nodes))
	fixed := make(map[*layoutgraph.Node]geo.Point)
	for _, n := range g.Nodes {
		byID[n.ID] = n
		if n.FixedTopLeft != nil {
			fixed[n] = relativeTopLeft(n)
		}
	}
	representative := make(map[*layoutgraph.Node]*layoutgraph.Node)
	expanded := make(map[*layoutgraph.Node]bool)
	for _, group := range g.SameRankGroups {
		for _, id := range group {
			if err := guard.Step(); err != nil {
				return err
			}
			n := byID[id]
			if n == nil || n.IsContainer() {
				return fmt.Errorf("TALA rank requires leaf shapes, invalid member %d", id)
			}
			representative[n] = byID[group[0]]
			for p := n.Container; p != nil && !expanded[p]; p = p.Container {
				expanded[p] = true
			}
		}
	}

	// An unrelated container retains its interior placement as a rigid block.
	// Only the ancestor chains of actual rank members are transparent.
	q := layoutgraph.NewGraph()
	proxies := make(map[*layoutgraph.Node]*layoutgraph.Node)
	owner := make(map[*layoutgraph.Node]*layoutgraph.Node, len(g.Nodes))
	blocks := make(map[layoutgraph.EntityID][]*layoutgraph.Node)
	constrained := make(map[layoutgraph.EntityID]bool)
	for _, n := range g.Nodes {
		if expanded[n] {
			continue
		}
		block := n
		for block.Container != nil && !expanded[block.Container] {
			if err := guard.Step(); err != nil {
				return err
			}
			block = block.Container
		}
		rep := block
		if r := representative[block]; r != nil {
			rep = r
			constrained[rep.ID] = true
		}
		proxy := proxies[rep]
		if proxy == nil {
			proxy = layoutgraph.NewNode(rep.ID, 1, 1)
			q.AddNewNodeToContainer(nil, proxy)
			proxies[rep] = proxy
		}
		owner[n] = proxy
		if n == block {
			blocks[rep.ID] = append(blocks[rep.ID], block)
		}
	}

	// When an authored edge ends on an expanded container, its incoming
	// boundary precedes each child and its outgoing boundary follows them.
	// Share boundary vertices across edges; even a container-to-container
	// connection adds no Cartesian product of descendant pairs.
	type boundaryKey struct {
		container *layoutgraph.Node
		outgoing  bool
	}
	boundaries := make(map[boundaryKey]*layoutgraph.Node)
	var boundary func(*layoutgraph.Node, bool) (*layoutgraph.Node, error)
	boundary = func(n *layoutgraph.Node, outgoing bool) (*layoutgraph.Node, error) {
		if !expanded[n] {
			return owner[n], nil
		}
		key := boundaryKey{n, outgoing}
		if b := boundaries[key]; b != nil {
			return b, nil
		}
		// The D2 adapter allocates positive IDs to authored shapes and edges.
		b := layoutgraph.NewNode(-int64(len(boundaries)+1), 1, 1)
		q.AddNewNodeToContainer(nil, b)
		boundaries[key] = b
		for _, child := range g.Containers[n] {
			if err := guard.Step(); err != nil {
				return nil, err
			}
			c, err := boundary(child, outgoing)
			if err != nil {
				return nil, err
			}
			from, to := b, c
			if outgoing {
				from, to = c, b
			}
			e := q.Connect(from, to)
			e.ID = -int64(len(q.Edges))
			e.SourceArrowhead, e.TargetArrowhead = layoutgraph.NoArrowhead, layoutgraph.TriangleArrowhead
		}
		return b, nil
	}

	flow := g.Direction(nil)
	if flow == geo.NONE {
		flow = geo.Bottom
	}
	horizontal := flow.IsHorizontal()
	localFlow := func(parent *layoutgraph.Node) geo.Orientation {
		for p := parent; p != nil; p = p.Container {
			if d := g.Direction(p); d != geo.NONE {
				return d
			}
		}
		return flow
	}
	gaps := make(map[*layoutgraph.Node]float64)
	flatLabels := make(map[[2]*layoutgraph.Node]float64)
	flatPaths := make(flatRankPaths)
	for _, edge := range g.Edges {
		if err := guard.Step(); err != nil {
			return err
		}
		if edge.IsLoop() || edge.From.IsDescendantOf(edge.To) || edge.To.IsDescendantOf(edge.From) {
			continue
		}
		from, err := boundary(edge.From, true)
		if err != nil {
			return err
		}
		to, err := boundary(edge.To, false)
		if err != nil {
			return err
		}
		if from == to {
			if !constrained[from.ID] || edge.IsInvisible {
				continue
			}
			p := edge.From.NearestSharedAncestor(edge.To)
			a, b := edge.From, edge.To
			for a.Container != p {
				a = a.Container
			}
			for b.Container != p {
				b = b.Container
			}
			flatPaths.link(a, b, from.ID)
			flatPaths.link(b, a, from.ID)
			if edge.Label != nil {
				// Reverse transitions get separate facing ports. Their labels can
				// share the same transverse interval; same-direction parallel
				// edges still reserve room for their full bundle.
				pair := [2]*layoutgraph.Node{edge.From, edge.To}
				flatLabels[pair] += axisValue(!horizontal, edge.Label.Width, edge.Label.Height) + crossingSpacing
				gaps[p] = max(gaps[p], flatLabels[pair])
			}
			continue
		}
		// Nested containers may specify their own flow. Perpendicular edges
		// belong to the transverse placement; opposite flows reverse the
		// span inequality without changing the authored edge or its route.
		local := localFlow(edge.From.NearestSharedAncestor(edge.To))
		if local.IsHorizontal() != horizontal {
			continue
		}
		if local != flow {
			from, to = to, from
		}
		e := q.Connect(from, to)
		e.ID, e.SourceArrowhead, e.TargetArrowhead = edge.ID, edge.SourceArrowhead, edge.TargetArrowhead
	}
	components, err := q.SplitSubgraphs(ctx, layoutgraph.SplitOptions{})
	if err != nil {
		return err
	}
	placedLevels := make(map[*layoutgraph.Node]int)
	for _, component := range components {
		if !slices.ContainsFunc(component.Nodes, func(n *layoutgraph.Node) bool { return constrained[n.ID] }) {
			continue
		}
		// Collapse equality before breaking cycles. Paths that leave and
		// re-enter a class may require feedback edges in the quotient DAG.
		dag, err := makeSimpleDAG(ctx, component)
		if err != nil {
			return err
		}
		ranks, err := rankDAG(ctx, dag)
		if err != nil {
			return err
		}
		if err := placeRankCenters(g, component, blocks, ranks, horizontal, placedLevels); err != nil {
			return err
		}
	}
	// Descendants already have world-space rank coordinates. Compact siblings
	// only on the transverse axis, then fit each enclosing shape around them.
	containers, err := g.ContainerRDFSOrder(nil, guard)
	if err != nil {
		return err
	}
	for _, p := range append(containers, nil) {
		if p != nil && !expanded[p] {
			continue
		}
		children := g.Containers[p]
		gap := max(siblingSpacing, gaps[p])
		if err := separateRankSiblings(children, horizontal, gap, flatPaths, guard); err != nil {
			return err
		}
		if localFlow(p).IsHorizontal() == horizontal {
			if err := alignRankChains(children, placedLevels, horizontal, gap, guard); err != nil {
				return err
			}
		}
		if p != nil {
			p.WrapChildren()
		}
	}
	for _, n := range g.Nodes {
		before, pinned := fixed[n]
		if !pinned {
			continue
		}
		after := relativeTopLeft(n)
		if math.Abs(before.X-after.X) > 1e-6 || math.Abs(before.Y-after.Y) > 1e-6 {
			return fmt.Errorf("TALA rank conflicts with the fixed top/left of %s", n.DebugID())
		}
	}
	return guard.Finish()
}

func relativeTopLeft(n *layoutgraph.Node) geo.Point {
	topLeft := *n.TopLeft
	if n.Container != nil {
		topLeft.X -= n.Container.TopLeft.X
		topLeft.Y -= n.Container.TopLeft.Y
	}
	return topLeft
}

func axisValue(horizontal bool, x, y float64) float64 {
	if horizontal {
		return x
	}
	return y
}

func rankCenter(n *layoutgraph.Node, horizontal bool) float64 {
	if horizontal {
		return n.TopLeft.X + n.Width/2
	}
	return n.TopLeft.Y + n.Height/2
}

func placeRankCenters(g, component *layoutgraph.Graph, blocks map[layoutgraph.EntityID][]*layoutgraph.Node, ranks rankResult, horizontal bool, placedLevels map[*layoutgraph.Node]int) error {
	levels := mapDAGToGraphLevels(component, ranks.nodeToLevel)
	reverse := g.Direction(nil) == geo.Top || g.Direction(nil) == geo.Left
	physicalLevel := func(proxy *layoutgraph.Node) int {
		level := levels[proxy]
		if reverse {
			return ranks.levelCount - 1 - level
		}
		return level
	}
	spans := make([]float64, ranks.levelCount)
	first := math.Inf(1)
	pins := make([]*layoutgraph.Node, ranks.levelCount)
	for _, proxy := range component.Nodes {
		for _, n := range blocks[proxy.ID] {
			level := physicalLevel(proxy)
			spans[level] = max(spans[level], axisValue(horizontal, n.Width, n.Height))
			first = min(first, axisValue(horizontal, n.TopLeft.X, n.TopLeft.Y))
			if n.FixedTopLeft != nil && n.Container == nil {
				if prev := pins[level]; prev != nil && rankCenter(prev, horizontal) != rankCenter(n, horizontal) {
					return fmt.Errorf("TALA rank conflicts with the fixed top/left of %s and %s", prev.DebugID(), n.DebugID())
				}
				pins[level] = n
			}
		}
	}
	centers := make([]float64, len(spans))
	for i, span := range spans {
		centers[i] = first + span/2
		first += span + siblingSpacing
	}
	// Start at the first absolute pin. Later pins may increase spacing
	// without disturbing earlier hard coordinates.
	firstPinned := true
	offset := 0.
	for level, n := range pins {
		if n == nil {
			continue
		}
		at := rankCenter(n, horizontal)
		shift := at - centers[level]
		if !firstPinned && shift < offset {
			return fmt.Errorf("TALA rank conflicts with the fixed top/left of %s", n.DebugID())
		}
		if firstPinned {
			for i := 0; i < level; i++ {
				centers[i] += shift
			}
			firstPinned = false
		}
		offset = shift
		centers[level] = at
		for i := level + 1; i < len(pins) && pins[i] == nil; i++ {
			centers[i] += shift
		}
	}
	lanes := make(map[*layoutgraph.Node]*layoutgraph.Hierarchy)
	for _, proxy := range component.Nodes {
		level := physicalLevel(proxy)
		for _, n := range blocks[proxy.ID] {
			placedLevels[n] = level
			// Only siblings with the matching routing orientation can use this
			// hierarchy's local OVG lanes. Interior block geometry is still rigid.
			if n.Hierarchy != nil {
				delete(n.Hierarchy.Levels(), n)
				n.Hierarchy = nil
			}
			if n.ContainerDirection() == g.Direction(nil) && !n.IsContainer() && (!n.IsTable() || horizontal) {
				h := lanes[n.Container]
				if h == nil {
					h = layoutgraph.NewHierarchy()
					h.LevelCount = ranks.levelCount
					lanes[n.Container] = h
				}
				h.Levels()[n] = levels[proxy]
				n.Hierarchy = h
			}
			delta := centers[level] - rankCenter(n, horizontal)
			if horizontal {
				n.MoveWithChildren(delta, 0)
			} else {
				n.MoveWithChildren(0, delta)
			}
		}
	}
	return nil
}

func separateRankSiblings(children []*layoutgraph.Node, horizontal bool, gap float64, flatPaths flatRankPaths, guard *limits.WorkGuard) error {
	ordered := slices.Clone(children)
	// The seed's transverse order preserves distinct branches of a workflow.
	// When the seed stacked peers down a single lane, declaration order breaks ties.
	slices.SortStableFunc(ordered, func(a, b *layoutgraph.Node) int {
		if (a.FixedTopLeft != nil) != (b.FixedTopLeft != nil) {
			if a.FixedTopLeft != nil {
				return -1
			}
			return 1
		}
		return cmp.Compare(math.Round(rankCenter(a, !horizontal)), math.Round(rankCenter(b, !horizontal)))
	})
	seedCenters := make([]float64, len(ordered))
	for i, n := range ordered {
		seedCenters[i] = rankCenter(n, !horizontal)
	}
	flatPaths.order(ordered)
	type interval struct{ low, high, far float64 }
	placed := make([]interval, 0, len(ordered))
	type fixedInterval struct{ low, high, start, end float64 }
	var fixed []fixedInterval
	fixedSorted := false
	for i, n := range ordered {
		tl, br := n.BoundingBox(layoutgraph.Nodes{n})
		start := axisValue(!horizontal, tl.X, tl.Y)
		end := axisValue(!horizontal, br.X, br.Y)
		low, high := axisValue(horizontal, tl.X, tl.Y), axisValue(horizontal, br.X, br.Y)
		if n.FixedTopLeft != nil {
			fixed = append(fixed, fixedInterval{low, high, start, end})
			continue
		}
		if !fixedSorted {
			slices.SortFunc(fixed, func(a, b fixedInterval) int { return cmp.Compare(a.start, b.start) })
			fixedSorted = true
		}
		pos := start + (seedCenters[i] - rankCenter(n, !horizontal))
		for _, b := range placed {
			if err := guard.Step(); err != nil {
				return err
			}
			if b.high+crossingSpacing > low && high+crossingSpacing > b.low {
				pos = max(pos, b.far+gap)
			}
		}
		// Anchored siblings can lie on either side of this node. They are
		// sorted by their leading edge, so moving past one can be checked
		// against subsequent obstacles in the same pass.
		for _, b := range fixed {
			if err := guard.Step(); err != nil {
				return err
			}
			if b.high+crossingSpacing > low && high+crossingSpacing > b.low &&
				pos+end-start+gap > b.start && pos < b.end+gap {
				pos = b.end + gap
			}
		}
		if horizontal {
			n.MoveWithChildren(0, pos-start)
		} else {
			n.MoveWithChildren(pos-start, 0)
		}
		placed = append(placed, interval{low, high, pos + end - start})
	}
	return nil
}
