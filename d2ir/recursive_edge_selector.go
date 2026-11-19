package d2ir

import (
	"cmp"
	"slices"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
)

func sameRecursiveEndpoints(edge *d2ast.Edge) bool {
	return len(edge.Src.Path) == 1 && len(edge.Dst.Path) == 1 &&
		edge.Src.HasMultiGlob() && edge.Dst.HasMultiGlob() &&
		edge.Src.Path[0].ScalarString() == edge.Dst.Path[0].ScalarString()
}

// Recursive globs can pass through board keywords only for ***. Both kinds
// skip definitions such as classes and vars.
func skipRecursiveGlobField(name d2ast.String, triple bool) bool {
	if name == nil {
		return true
	}
	if !name.IsUnquoted() {
		return false
	}
	key := name.ScalarString()
	_, reserved := d2ast.ReservedKeywords[key]
	return reserved && skipRecursiveGlobSubtree(key, triple)
}

func (m *Map) recursiveGlobContains(f *Field, triple bool) bool {
	if f == nil || f.Name == nil || NodeBoardKind(f) != "" {
		return false
	}
	if _, reserved := d2ast.ReservedKeywords[f.Name.ScalarString()]; reserved && f.Name.IsUnquoted() {
		return false
	}
	path := m.pathToField(f)
	for _, part := range path {
		if skipRecursiveGlobField(part.Name, triple) {
			return false
		}
	}
	return len(path) != 0
}

// selectRecursiveEdges matches an existing-edge selector such as (** -> **)[*].
// Both endpoints use the same field traversal. Only real edges can contribute
// a match, even while the lazy glob is replayed as new fields are declared.
func (m *Map) selectRecursiveEdges(id *EdgeID, ref *RefContext, glob *globContext, c *compiler) []*Edge {
	type match struct {
		edge     *Edge
		src, dst *Field
		key      string
	}
	triple := ref.Edge.Src.HasTripleGlob()
	var candidates []match
	var visit func(*Map) bool
	visit = func(scope *Map) bool {
		for _, e := range scope.Edges {
			if !c.reserveEdgeWork(ref.Edge) {
				return false
			}
			direct := id.SrcArrow == e.ID.SrcArrow && id.DstArrow == e.ID.DstArrow
			reverse := id.SrcArrow == e.ID.DstArrow && id.DstArrow == e.ID.SrcArrow
			if !direct && !reverse {
				continue
			}
			key := glob.edgeKey(e)
			if _, applied := glob.appliedEdges[key]; applied {
				continue
			}
			src := scope.getFieldIndexed(e.ID.SrcPath...)
			dst := scope.getFieldIndexed(e.ID.DstPath...)
			if !direct {
				src, dst = dst, src
			}
			if !m.recursiveGlobContains(src, triple) || !m.recursiveGlobContains(dst, triple) {
				continue
			}
			if !c.reserveEdgePair(ref.Edge, glob, src, dst, true) {
				return false
			}
			candidates = append(candidates, match{e, src, dst, key})
		}
		for _, f := range scope.edgeFields {
			if !c.reserveGlobWork(ref.Edge, 1) {
				return false
			}
			if skipRecursiveGlobField(f.Name, triple) || !canTrimEdgeScope(f.Name) {
				continue
			}
			if child := f.Map(); child.mayHaveEdges() && !visit(child) {
				return false
			}
		}
		return true
	}
	if !visit(m) || len(candidates) == 0 {
		return nil
	}

	// A leading selector normally sees one new edge at a time. For a batch,
	// retain the original source-then-destination order and the declaration
	// order of parallel edges within each endpoint pair.
	if len(candidates) > 1 {
		fields, _ := m.multiGlob(ref.Edge.Src.Path[0].UnquotedString.Pattern, func(*Field) bool {
			return c.reserveGlobWork(ref.Edge, 1)
		})
		if c.stopped() {
			return nil
		}
		order := make(map[*Field]int, len(fields))
		for i, f := range fields {
			if _, exists := order[f]; !exists {
				order[f] = i
			}
		}
		if id.SrcArrow == id.DstArrow {
			for i := range candidates {
				match := &candidates[i]
				if order[match.dst] < order[match.src] {
					match.src, match.dst = match.dst, match.src
				}
			}
		}
		slices.SortStableFunc(candidates, func(a, b match) int {
			return cmp.Or(cmp.Compare(order[a.src], order[b.src]), cmp.Compare(order[a.dst], order[b.dst]))
		})
	}
	var edges []*Edge
	for _, match := range candidates {
		if _, applied := glob.appliedEdges[match.key]; !applied {
			glob.appliedEdges[match.key] = struct{}{}
			edges = append(edges, match.edge)
		}
	}
	return edges
}

func (g *globContext) edgeKey(e *Edge) string {
	if g.refctx.Key.HasTripleGlob() {
		return d2format.FormatKeyPath(IDA(e))
	}
	return d2format.FormatKeyPath(BoardIDA(e))
}
