package d2talalayout

import (
	"maps"
	"strings"

	"github.com/d2lang/d2/d2graph"
)

// A scope change replaces every full ID in a panel. Transfer an entire panel
// only when its tree, geometry-bearing content and internal connections agree.
// Edges crossing its boundary belong to the surrounding graph and may change.
func movedBoardPanels(previous, current *d2graph.Graph) (map[*d2graph.Object][]boardAnchor, map[*d2graph.Edge]*d2graph.Edge) {
	index := func(g *d2graph.Graph) (map[string]*d2graph.Object, map[string]bool) {
		local := make(map[string]*d2graph.Object, len(g.Objects))
		paths := make(map[string]bool, len(g.Objects))
		for _, n := range g.Objects {
			k := strings.ToLower(n.ID)
			if _, exists := local[k]; exists {
				local[k] = nil
			} else {
				local[k] = n
			}
			paths[strings.ToLower(n.AbsID())] = true
		}
		return local, paths
	}
	oldLocal, oldPaths := index(previous)
	newLocal, newPaths := index(current)
	panels := make(map[*d2graph.Object][]boardAnchor)
	edges := make(map[*d2graph.Edge]*d2graph.Edge)
	var visit func(*d2graph.Object)
	visit = func(parent *d2graph.Object) {
		for _, n := range parent.ChildrenArray {
			if !n.IsContainer() {
				continue
			}
			id := strings.ToLower(n.ID)
			o := oldLocal[id]
			if o != nil && o.IsContainer() && newLocal[id] == n &&
				!oldPaths[strings.ToLower(n.AbsID())] && !newPaths[strings.ToLower(o.AbsID())] {
				if pairs := movedPanelObjects(o, n); pairs != nil {
					if wires, ok := movedPanelEdges(previous.Edges, current.Edges, pairs); ok {
						panels[n] = pairs
						maps.Copy(edges, wires)
						continue // descendants are part of this rigid unit
					}
				}
			}
			visit(n)
		}
	}
	visit(current.Root)
	return panels, edges
}

func sameBoardSetting[T comparable](a, b *T) bool {
	return a == b || a != nil && b != nil && *a == *b
}

func movedPanelObjects(old, current *d2graph.Object) []boardAnchor {
	var pairs []boardAnchor
	var visit func(*d2graph.Object, *d2graph.Object) bool
	visit = func(o, n *d2graph.Object) bool {
		if o == nil || o.Shape.Value != n.Shape.Value || o.Label.Value != n.Label.Value ||
			o.LabelDimensions != n.LabelDimensions || !sameBoardSetting(o.LabelPosition, n.LabelPosition) ||
			!sameBoardSetting(o.IconPosition, n.IconPosition) || len(o.Children) != len(n.Children) ||
			!n.IsContainer() && (o.Width != n.Width || o.Height != n.Height) {
			return false
		}
		pairs = append(pairs, boardAnchor{o, n})
		for _, child := range n.ChildrenArray {
			if !visit(o.Children[strings.ToLower(child.ID)], child) {
				return false
			}
		}
		return true
	}
	if !visit(old, current) {
		return nil
	}
	return pairs
}

func movedPanelEdges(previous, current []*d2graph.Edge, pairs []boardAnchor) (map[*d2graph.Edge]*d2graph.Edge, bool) {
	// Use old endpoint pointers as canonical IDs. This also distinguishes
	// quoted names containing dots from nested paths in a moved subtree.
	members := make(map[*d2graph.Object]*d2graph.Object, 2*len(pairs))
	for _, p := range pairs {
		members[p.old], members[p.current] = p.old, p.old
	}
	type edgeKey struct {
		src, dst           *d2graph.Object
		srcArrow, dstArrow bool
		index              int
	}
	key := func(e *d2graph.Edge) edgeKey {
		return edgeKey{members[e.Src], members[e.Dst], e.SrcArrow, e.DstArrow, e.Index}
	}
	remaining := make(map[edgeKey]*d2graph.Edge)
	for _, e := range previous {
		k := key(e)
		if k.src == nil || k.dst == nil {
			continue
		}
		if remaining[k] != nil {
			return nil, false
		}
		remaining[k] = e
	}
	matched := make(map[*d2graph.Edge]*d2graph.Edge)
	for _, e := range current {
		k := key(e)
		if k.src == nil || k.dst == nil {
			continue
		}
		o := remaining[k]
		if o == nil || o.IsCurve != e.IsCurve || o.Label.Value != e.Label.Value || o.LabelDimensions != e.LabelDimensions ||
			o.SrcArrowhead != nil || e.SrcArrowhead != nil || o.DstArrowhead != nil || e.DstArrowhead != nil ||
			!sameBoardSetting(o.SrcTableColumnIndex, e.SrcTableColumnIndex) || !sameBoardSetting(o.DstTableColumnIndex, e.DstTableColumnIndex) ||
			e.Label.Value != "" && (!sameBoardSetting(o.LabelPosition, e.LabelPosition) || !sameBoardSetting(o.LabelPercentage, e.LabelPercentage)) {
			return nil, false
		}
		matched[e] = o
		delete(remaining, k)
	}
	return matched, len(remaining) == 0
}
