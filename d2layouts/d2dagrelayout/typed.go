package d2dagrelayout

import (
	"fmt"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/dagro"
)

// dagroInput returns the dagro.LayoutTyped input for g. dagro guarantees that
// LayoutTyped(in) produces exactly what Layout produces for the Graph that
// in.Graph() builds, and in.Graph() makes the calls the original adapter
// made, in the same order: SetGraph; SetNode and, unless the parent is g.Root,
// SetParent for each object; SetEdge for each edge, between its resolved
// endpoints. So node i is g.Objects[i], with the original's node id: the
// position of the object's last occurrence in g.Objects (as objectMapper
// numbered them), and edge i is g.Edges[i], with the same label values.
//
// A parent other than g.Root or a resolved endpoint that is not in g.Objects
// was the node "" to the original, which the first SetParent or SetEdge call
// naming it created without a label. Here it is one more node,
// in.Nodes[len(g.Objects)], with id "". in.Graph() creates it at the same
// point among the nodes: at that SetParent call or, when only endpoints name
// it, with SetNode right after the objects' nodes and before any edge. Its
// label of width 0 and height 0 lays out like no label.
//
// It also returns the errors the original returned for g besides those of
// the layout itself: buildErr, which it returned before it laid out anything
// (the error of the SetParent call that closes a parent cycle), and readErr,
// which it returned when it read the result, after it had set the position of
// every object: the node "" has no label, and edges that share an id are one
// edge in dagro, so fewer edges come back.
func dagroInput(g *d2graph.Graph, ix *objectIndex, rootAttrs dagreOpts, resolvedSrc, resolvedDst []*d2graph.Object, sameWay, bothWays []int) (in dagro.LayoutInput, buildErr, readErr error) {
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
	ix.sc.cycleState = zeroed(ix.sc.cycleState, len(in.Nodes))
	if hasParentCycleState(in.Nodes, ix.sc.cycleState) {
		_, err := in.Graph()
		return in, err, nil
	}

	in.Edges = grow(ix.sc.in.Edges, len(g.Edges))
	named := 0
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
			named++
		}
		in.Edges[i] = e
		if (v == n || w == n) && len(in.Nodes) == n {
			in.Nodes = append(in.Nodes, empty)
		}
	}

	edges := len(g.Edges)
	if named > 1 {
		edges -= repeatedEdgeIDs(in.Edges, named)
	}
	switch {
	case len(in.Nodes) > n:
		readErr = fmt.Errorf("dagro returned invalid node label for %q", "")
	case edges != len(g.Edges):
		readErr = fmt.Errorf("dagro returned %d edges, expected %d", edges, len(g.Edges))
	}
	return in, nil, readErr
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

// repeatedEdgeIDs returns the number of edges whose endpoints and name are
// those of an earlier edge. Only the named edges (named of them) can repeat
// an id: no other edge has the endpoints of an unnamed one. A name is an
// AbsID, which is never empty.
func repeatedEdgeIDs(edges []dagro.LayoutEdge, named int) int {
	type edgeID struct {
		v, w int
		name string
	}
	repeated := 0
	if named <= 32 {
		var ids [32]edgeID
		n := 0
	next:
		for i := range edges {
			if edges[i].Name == "" {
				continue
			}
			id := edgeID{edges[i].V, edges[i].W, edges[i].Name}
			for _, other := range ids[:n] {
				if other == id {
					repeated++
					continue next
				}
			}
			ids[n] = id
			n++
		}
		return repeated
	}
	seen := make(map[edgeID]struct{}, named)
	for i := range edges {
		if edges[i].Name == "" {
			continue
		}
		id := edgeID{edges[i].V, edges[i].W, edges[i].Name}
		if _, ok := seen[id]; ok {
			repeated++
			continue
		}
		seen[id] = struct{}{}
	}
	return repeated
}
