package d2dagrelayout

import (
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/dagro"
)

// dagroInput returns the dagro.LayoutTyped input that is equivalent to the
// Graph layoutAttrs builds: dagro guarantees that LayoutTyped(in) produces
// exactly what Layout produces for that Graph, built by the same calls in the
// same order (SetGraph; SetNode and, unless the parent is g.Root, SetParent
// for each object; SetEdge for each edge). So node i is g.Objects[i] with id
// strconv.Itoa(i), and edge i is g.Edges[i] with its resolved endpoints, the
// same label values and the same name.
//
// It returns false for the graphs whose Graph calls do not map one to one
// onto such an input, which keep layoutAttrs and so its errors at the same
// points: an object repeated in g.Objects (SetNode twice for one id), a
// parent other than g.Root or a resolved edge endpoint that is not in
// g.Objects (layoutAttrs then creates the unlabeled node ""), a parent cycle
// (SetParent fails at the call that closes it, before the context check) and
// two edges with the same endpoints and name (the second SetEdge replaces the
// first edge's label).
func dagroInput(g *d2graph.Graph, ix *objectIndex, rootAttrs dagreOpts, resolvedSrc, resolvedDst []*d2graph.Object, sameWay, bothWays []int) (in dagro.LayoutInput, ok bool) {
	if ix.distinct != len(g.Objects) {
		return in, false
	}
	// Every graph attribute is set: zero values are not dagro's defaults.
	// layoutAttrs sets rankdir, ranksep, edgesep and nodesep; a zero MarginX
	// or MarginY and empty Acyclicer, Ranker and Align mean "not set".
	in.RankDir = rootAttrs.rankdir
	in.RankSep = float64(rootAttrs.ranksep)
	in.EdgeSep = float64(rootAttrs.EdgeSep)
	in.NodeSep = float64(rootAttrs.NodeSep)

	// With no repeated object, object i has index i and its node id is
	// nodeID(i).
	// Every element is written before in is used (an early false return
	// discards it).
	in.Nodes = grow(ix.sc.in.Nodes, len(g.Objects))
	for i, obj := range g.Objects {
		parent := -1
		if obj.Parent != g.Root {
			k, found := ix.lookup(obj.Parent)
			if !found {
				return in, false
			}
			parent = int(k)
		}
		in.Nodes[i] = dagro.LayoutNode{
			ID:     nodeID(i),
			Parent: parent,
			Width:  float64(int(obj.Width)),
			Height: float64(int(obj.Height)),
		}
	}
	ix.sc.cycleState = zeroed(ix.sc.cycleState, len(in.Nodes))
	if hasParentCycleState(in.Nodes, ix.sc.cycleState) {
		return in, false
	}

	in.Edges = grow(ix.sc.in.Edges, len(g.Edges))
	named := 0
	for i, edge := range g.Edges {
		v, foundV := ix.lookup(resolvedSrc[i])
		w, foundW := ix.lookup(resolvedDst[i])
		if !foundV || !foundW {
			return in, false
		}
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
		// Minlen 1, Weight 1 and LabelOffset 10 are the defaults layoutAttrs
		// leaves in place; a width or height it leaves out is dagro's default 0.
		e := dagro.NewLayoutEdge(int(v), int(w))
		e.Width = float64(width)
		e.Height = float64(height)
		e.LabelPos = "c"
		// layoutAttrs always passes a name: "" unless another edge has the
		// same endpoints, then the AbsID.
		e.HasName = true
		if sameWay[i] > 1 {
			e.Name = edge.AbsID()
			named++
		}
		in.Edges[i] = e
	}
	// Only named edges share their endpoints with another edge, so only they
	// can repeat a key.
	if named > 1 && hasDuplicateEdgeKey(in.Edges, sameWay, named) {
		return in, false
	}
	return in, true
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

// hasDuplicateEdgeKey reports whether two of the edges with sameWay > 1
// (named of them) have the same endpoints and name.
func hasDuplicateEdgeKey(edges []dagro.LayoutEdge, sameWay []int, named int) bool {
	type edgeKey struct {
		v, w int
		name string
	}
	if named <= 32 {
		var keys [32]edgeKey
		n := 0
		for i := range edges {
			if sameWay[i] <= 1 {
				continue
			}
			k := edgeKey{edges[i].V, edges[i].W, edges[i].Name}
			for _, other := range keys[:n] {
				if other == k {
					return true
				}
			}
			keys[n] = k
			n++
		}
		return false
	}
	seen := make(map[edgeKey]struct{}, named)
	for i := range edges {
		if sameWay[i] <= 1 {
			continue
		}
		k := edgeKey{edges[i].V, edges[i].W, edges[i].Name}
		if _, dup := seen[k]; dup {
			return true
		}
		seen[k] = struct{}{}
	}
	return false
}
