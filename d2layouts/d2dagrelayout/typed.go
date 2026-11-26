package d2dagrelayout

import (
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/dagro"
)

// dagroInput returns the dagro.LayoutTyped input for g. It describes the
// graph the original adapter built with dagro's Graph API, with the same calls
// in the same order (dagro.LayoutTyped produces what dagre produces for that
// graph): rankdir, ranksep, edgesep and nodesep; a node for each object with,
// unless the parent is g.Root, its parent; an edge for each edge, between its
// resolved endpoints. So node i is g.Objects[i], with the original's node id:
// the position of the object's last occurrence in g.Objects (as objectMapper
// numbered them), and edge i is g.Edges[i], with the same label values.
//
// Only a Go caller can break g so that dagro.LayoutTyped rejects the input
// (with an error wrapping dagro.ErrUnsupportedInput) before laying anything
// out:
//   - an object that appears twice in g.Objects gives two nodes with the
//     same id;
//   - a parent other than g.Root or a resolved endpoint that is not in
//     g.Objects becomes one more node, in.Nodes[len(g.Objects)], with the
//     empty id (the node "" of the original);
//   - two edges with the same resolved endpoints and AbsID repeat an edge;
//   - parents that form a cycle (the input then has no edges).
func dagroInput(g *d2graph.Graph, ix *objectIndex, rootAttrs dagreOpts, resolvedSrc, resolvedDst []*d2graph.Object, sameWay, bothWays []int) (in dagro.LayoutInput) {
	// Every graph attribute is set: zero values are not dagro's defaults. The
	// original set rankdir, ranksep, edgesep and nodesep; a zero MarginX or
	// MarginY and empty Acyclicer, Ranker and Align mean "not set".
	in.RankDir = rootAttrs.rankdir
	in.RankSep = float64(rootAttrs.ranksep)
	in.EdgeSep = float64(rootAttrs.EdgeSep)
	in.NodeSep = float64(rootAttrs.NodeSep)

	// Every element is written before in is used (an early return discards
	// it). The node "", when it is needed, is in.Nodes[n].
	n := len(g.Objects)
	empty := dagro.LayoutNode{Parent: -1}
	in.Nodes = grow(ix.sc.in.Nodes, n)
	for i, obj := range g.Objects {
		parent := -1
		if obj.Parent != g.Root {
			parent = ix.node(obj.Parent, n)
		}
		in.Nodes[i] = dagro.LayoutNode{
			ID:     nodeID(int(ix.lastAt[ix.at[i]])),
			Parent: parent,
			Width:  float64(int(obj.Width)),
			Height: float64(int(obj.Height)),
		}
		if parent == n && len(in.Nodes) == n {
			in.Nodes = append(in.Nodes, empty)
		}
	}
	// Edge ids walk the parents (AbsID), which never ends on a cycle; without
	// edges, dagro.LayoutTyped reports the cycle.
	ix.sc.cycleState = zeroed(ix.sc.cycleState, len(in.Nodes))
	if hasParentCycleState(in.Nodes, ix.sc.cycleState) {
		in.Edges = ix.sc.in.Edges[:0]
		return in
	}

	in.Edges = grow(ix.sc.in.Edges, len(g.Edges))
	for i, edge := range g.Edges {
		v, w := ix.node(resolvedSrc[i], n), ix.node(resolvedDst[i], n)
		width := edge.LabelDimensions.Width
		height := edge.LabelDimensions.Height
		// We want to leave some gap between multiple edges
		if bothWays[i] > 1 {
			switch g.Root.Direction.Value {
			case "down", "up", "":
				width += EDGE_LABEL_GAP
			case "left", "right":
				height += EDGE_LABEL_GAP
			}
		}
		// Minlen 1, Weight 1 and LabelOffset 10 are the defaults the original
		// left in place.
		e := dagro.NewLayoutEdge(v, w)
		e.Width = float64(width)
		e.Height = float64(height)
		e.LabelPos = "c"
		// The original named every edge by its AbsID. dagro uses a name only
		// in the edge's id (v, w, name), and two edges can have the same
		// endpoints only when they connect the same two objects or an object
		// outside g.Objects (all of which are the node ""). Those edges keep
		// their AbsID, so ids repeat exactly where the original's did; every
		// other edge's id is unique whatever its name, and it gets "".
		e.HasName = true
		if sameWay[i] > 1 || v == n || w == n {
			e.Name = edge.AbsID()
		}
		in.Edges[i] = e
		if (v == n || w == n) && len(in.Nodes) == n {
			in.Nodes = append(in.Nodes, empty)
		}
	}

	return in
}

// hasParentCycleState reports whether following Parent from some node never
// reaches a node without a parent, with state, len(nodes) zero bytes, as its
// working storage.
func hasParentCycleState(nodes []dagro.LayoutNode, state []uint8) bool {
	const (
		unvisited = iota
		onPath
		done
	)
	for i := range nodes {
		j := i
		for j >= 0 && state[j] == unvisited {
			state[j] = onPath
			j = nodes[j].Parent
		}
		// Nodes left onPath by earlier walks were all marked done, so this
		// one is on the current walk.
		if j >= 0 && state[j] == onPath {
			return true
		}
		for j = i; j >= 0 && state[j] == onPath; j = nodes[j].Parent {
			state[j] = done
		}
	}
	return false
}
