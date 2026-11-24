package d2talalayout

import (
	"cmp"
	"context"
	"slices"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

type branchConnection struct {
	hub                      *layoutgraph.Node
	fromBranch               bool
	sourceArrow, targetArrow layoutgraph.Arrowhead
	curve                    bool
}

type branchSlotKey struct {
	hierarchy           *layoutgraph.Hierarchy
	container           *layoutgraph.Node
	level               int
	shape               string
	width, height       float64
	labelPosition       label.Position
	connections         [2]branchConnection // neighbors on earlier and later ranks
	hasLink, hasTooltip bool
	zIndex              int
}

type branchSlot struct {
	node     *layoutgraph.Node
	position *geo.Point
	edges    [2]*layoutgraph.Edge
	routes   [2][]*geo.Point
}

// Assign interchangeable routed branches to their existing slots in declaration
// order. Doing this after candidate selection preserves the physical routes of
// the winning seed, including routes in neighboring components.
func orderEquivalentBranchSlots(ctx context.Context, source *d2graph.Graph, result seedResult) error {
	members := make(map[layoutgraph.EntityID]*layoutgraph.Node)
	for _, n := range result.graph.Nodes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if n.Hierarchy != nil {
			members[n.ID] = n
		}
	}
	if len(members) < 3 {
		return nil
	}
	// Connection icons are rendered by D2 and do not appear in TALA's
	// routing graph. Keep their routes with the original slot.
	decorated := make(map[layoutgraph.EntityID]bool)
	for _, edge := range source.Edges {
		if err := ctx.Err(); err != nil {
			return err
		}
		if edge.Icon != nil || edge.ZIndex != 0 {
			decorated[result.bindings.edgeIDs[edge]] = true
		}
	}
	groups := make(map[branchSlotKey][]branchSlot)
	for _, obj := range source.Objects {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := members[result.bindings.objectIDs[obj]]
		if n == nil || len(obj.ChildrenArray) != 0 || obj.Attributes.TooltipPosition != nil ||
			obj.Top != nil || obj.Left != nil || obj.NearKey != nil ||
			obj.Style.StrokeWidth != nil || obj.Style.BorderRadius != nil ||
			obj.Style.DoubleBorder != nil || obj.Style.Shadow != nil {
			continue
		}
		key, slot, ok := equivalentBranchSlot(n, decorated)
		if ok {
			key.hasLink = obj.Link != nil && obj.Link.Value != ""
			key.hasTooltip = obj.Tooltip != nil && obj.Tooltip.Value != ""
			key.zIndex = obj.ZIndex
			groups[key] = append(groups[key], slot)
		}
	}
	for _, peers := range groups {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(peers) < 2 {
			continue
		}
		// Use the actual rank axis, including horizontal table hierarchies.
		// A staggered group has no single row of interchangeable slots.
		row, column := true, true
		for _, peer := range peers[1:] {
			row = row && peer.position.Y == peers[0].position.Y
			column = column && peer.position.X == peers[0].position.X
		}
		if !row && !column {
			continue
		}
		slots := slices.Clone(peers)
		slices.SortStableFunc(slots, func(a, b branchSlot) int {
			if row {
				return cmp.Compare(a.position.X, b.position.X)
			}
			return cmp.Compare(a.position.Y, b.position.Y)
		})
		for i, peer := range peers {
			if peer.node == slots[i].node {
				continue
			}
			peer.node.TopLeft = slots[i].position
			for j, edge := range peer.edges {
				if edge != nil {
					edge.Points = slots[i].routes[j]
				}
			}
		}
	}
	return ctx.Err()
}

func equivalentBranchSlot(n *layoutgraph.Node, decorated map[layoutgraph.EntityID]bool) (branchSlotKey, branchSlot, bool) {
	key := branchSlotKey{}
	slot := branchSlot{node: n, position: n.TopLeft}
	if len(n.Edges) < 1 || len(n.Edges) > 2 || n.IsContainer() || n.IsTable() || n.IsClass() ||
		n.Sequence != nil || n.Cluster != nil || n.HerdAssignment != nil ||
		n.IsInvisible || n.FixedTopLeft != nil || len(n.Nears) != 0 || n.Icon != nil || n.Is3D || n.IsMultiple {
		return key, slot, false
	}
	key = branchSlotKey{hierarchy: n.Hierarchy, level: n.HierarchyLevel(), container: n.Container,
		shape: n.ShapeType(), width: n.Width, height: n.Height}
	if l := n.Label; l != nil {
		if !l.Position.IsShapePosition() || l.Position.IsOutside() || l.Position.IsBorder() {
			return key, slot, false
		}
		p := n.LabelTopLeft(l.Position, l.Width, l.Height)
		if p.X < n.TopLeft.X || p.Y < n.TopLeft.Y || p.X+l.Width > n.TopLeft.X+n.Width || p.Y+l.Height > n.TopLeft.Y+n.Height {
			return key, slot, false
		}
		key.labelPosition = l.Position
	}
	for _, edge := range n.Edges {
		hub := n.Adjacent(edge)
		if decorated[edge.ID] || hub.Hierarchy != n.Hierarchy || edge.Label != nil || edge.HasTableColumn() ||
			edge.SourceArrowheadLabel != nil || edge.TargetArrowheadLabel != nil || edge.IsInvisible ||
			edge.Style.StrokeWidth != nil || edge.Style.BorderRadius != nil ||
			len(edge.Points) < 2 || hub.HierarchyLevel() == key.level {
			return key, slot, false
		}
		i := 0
		if hub.HierarchyLevel() > key.level {
			i = 1
		}
		if slot.edges[i] != nil {
			return key, slot, false
		}
		key.connections[i] = branchConnection{
			hub: hub, fromBranch: edge.From == n, sourceArrow: edge.SourceArrowhead,
			targetArrow: edge.TargetArrowhead, curve: edge.IsCurve,
		}
		slot.edges[i], slot.routes[i] = edge, edge.Points
	}
	return key, slot, true
}
