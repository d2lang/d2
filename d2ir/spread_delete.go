package d2ir

import "github.com/d2lang/d2/d2ast"

// A null deletes the field immediately. An earlier, unresolved spread must also
// omit that field when it eventually expands. Keep the deletion on the affected
// placeholders so it follows copies into steps and imports, without affecting a
// later spread or a different use of the same variable.
type spreadDeletion struct {
	path *spreadMember
	next *spreadDeletion
}

type spreadMember struct {
	field d2ast.String
	edge  *EdgeID
	child *spreadMember
}

// Record the deletion in the edge's owning scope. Either endpoint can use
// underscores or a shared path prefix to name a different containing map.
func (c *compiler) recordSpreadEdgeDeletion(m *Map, id *EdgeID, source d2ast.Node) {
	if !c.hasMapSpread {
		return
	}
	edge, scope, common, err := id.resolve(m)
	if err != nil {
		return
	}
	for len(common) > 0 {
		f := scope.getFieldIndexed(common[0])
		if f == nil || f.Map() == nil {
			break
		}
		scope = f.Map()
		common = common[1:]
	}
	path := &spreadMember{edge: edge}
	for i := len(common) - 1; i >= 0; i-- {
		path = &spreadMember{field: common[i], child: path}
	}
	c.recordSpreadDeletion(scope, path, source)
}

func (c *compiler) recordSpreadDeletion(m *Map, path *spreadMember, source d2ast.Node) {
	var cutoff *fieldOrder
	if c.lazyGlobBeingApplied && len(c.globRefContextStack) != 0 {
		cutoff = &c.globRefContextStack[0].order
	}
	c.recordSpreadDeletionBefore(m, path, source, cutoff)
}

func (c *compiler) recordSpreadDeletionBefore(m *Map, path *spreadMember, source d2ast.Node, cutoff *fieldOrder) {
	if !c.hasMapSpread {
		return
	}
	for m != nil {
		if len(m.pendingSpreads) > 0 {
			if !c.reserveVariableExpansion(source, int64(len(m.pendingSpreads))) {
				return
			}
			for _, f := range m.pendingSpreads {
				if cutoff != nil && !f.declaration.before(*cutoff) {
					continue
				}
				f.spreadDeletions = &spreadDeletion{path: path, next: f.spreadDeletions}
			}
		}
		if m.Root() || NodeBoardKind(m) != "" {
			return
		}
		parent := m.Parent()
		switch n := parent.(type) {
		case *Field:
			path = &spreadMember{field: n.Name, child: path}
		case *Edge:
			path = &spreadMember{edge: n.ID, child: path}
		default:
			return
		}
		m = ParentMap(parent)
	}
}

// A lazy rule targeting a child runs when its containing shape first appears.
// During spread replay, that shape already contains its later explicit writes.
func (c *compiler) keepChildAfterGlobDeletion(f *Field, ref *RefContext) bool {
	if !c.replayingSpreadGlobs || len(c.globRefContextStack) == 0 {
		return false
	}
	rule := c.globRefContextStack[0]
	if key := ref.Key.Key; rule == ref && key != nil && key.FirstGlob() == len(key.Path)-1 {
		return false
	}
	return (f.Primary_ != nil && rule.order.before(f.Primary_.order)) ||
		(f.Composite != nil && rule.order.before(f.compositeOrder))
}

func (d *spreadDeletion) apply(c *compiler, m *Map, source d2ast.Node) bool {
	for ; d != nil; d = d.next {
		parent := m
		for member := d.path; parent != nil; member = member.child {
			if c != nil && !c.reserveVariableExpansion(source, 1) {
				return false
			}
			if member.field != nil {
				field := parent.getFieldIndexed(member.field)
				if field == nil {
					break
				}
				if member.child == nil {
					parent.deleteField(field)
					break
				}
				parent = field.Map()
			} else {
				if member.child == nil {
					parent.DeleteEdge(member.edge)
					break
				}
				edges := parent.getEdgesIndexed(member.edge)
				if len(edges) == 0 {
					break
				}
				parent = edges[0].Map()
			}
		}
	}
	return true
}
