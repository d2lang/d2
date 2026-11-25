package d2ir

import "slices"

// An unresolved later spread can reopen the map before an indexed access.
type arrayAccessCheck struct {
	array, access fieldOrder
	ref           *FieldReference
	reopened      bool
}

func (c *compiler) noteMapOpening(f *Field, order fieldOrder) {
	for _, check := range c.fieldArrayAccess[f] {
		if check.array.before(order) && order.before(check.access) {
			check.reopened = true
		}
	}
}

// An array replaces the whole previous map. When a deferred assignment
// introduces that array, later dotted keys must see the same type error they
// would see inline. A subsequent map literal reopens it with fresh children.
func (s *spreadSite) applyArrayReset(f *Field) (keepMap bool) {
	if s == nil || s.compiler == nil || f.Map() == nil {
		return
	}
	var reopen, index *FieldReference
	for _, ref := range f.References {
		if !s.before(ref.order) || ref.KeyPath == nil {
			continue
		}
		if ref.Primary() && ref.Context_.Key.Value.Map != nil {
			if reopen == nil || ref.order.before(reopen.order) {
				reopen = ref
			}
		} else if ref.KeyPathIndex()+1 < len(ref.KeyPath.Path) {
			if index == nil || ref.order.before(index.order) {
				index = ref
			}
		}
	}
	if index != nil && (reopen == nil || index.order.before(reopen.order)) {
		check := &arrayAccessCheck{array: s.fieldOrder, access: index.order, ref: index}
		c := s.compiler
		if c.fieldArrayAccess == nil {
			c.fieldArrayAccess = make(map[*Field][]*arrayAccessCheck)
		}
		c.fieldArrayAccess[f] = append(c.fieldArrayAccess[f], check)
		c.arrayAccess = append(c.arrayAccess, check)
	}
	if reopen != nil || index != nil {
		if f.Map().Parent() != f {
			if !s.compiler.reserveVariableCopy(s.source.LastRef().AST(), f.Map()) {
				return
			}
			f.setComposite(f.copyAliasMap(f))
		}
		f.Map().discardBefore(s.fieldOrder)
		f.mapReset = s.fieldOrder
		return true
	}
	return false
}

func (m *Map) sortFieldsByDeclaration() {
	slices.SortStableFunc(m.Fields, func(a, b *Field) int {
		if a.declaration.before(b.declaration) {
			return -1
		}
		if b.declaration.before(a.declaration) {
			return 1
		}
		return 0
	})
}

// A deferred null or array discarded the old map at the use site. Retain
// each later occurrence, including a bare key that recreated a cleared field.
func (m *Map) discardBefore(site fieldOrder) {
	for i := len(m.Fields) - 1; i >= 0; i-- {
		if !m.Fields[i].retainAfter(site) {
			m.removeField(i)
		}
	}
	m.sortFieldsByDeclaration()
	for i := len(m.Edges) - 1; i >= 0; i-- {
		e := m.Edges[i]
		if !site.before(e.References[0].order) {
			m.removeEdge(i)
		}
	}
}

func (f *Field) retainAfter(site fieldOrder) bool {
	var first fieldOrder
	remember := func(order fieldOrder) {
		if site.before(order) && (first == (fieldOrder{}) || order.before(first)) {
			first = order
		}
	}
	remember(f.declaration)
	for _, ref := range f.References {
		remember(ref.order)
	}
	if s := f.Primary_; s != nil {
		remember(s.order)
		if !site.before(s.order) {
			f.Primary_ = nil
		}
	}
	if f.Composite != nil {
		remember(f.compositeOrder)
		if m := f.Map(); m != nil && m.Parent() == f {
			m.discardBefore(site)
			if len(m.Fields) != 0 {
				remember(m.Fields[0].declaration)
			}
			if len(m.Fields)+len(m.Edges) == 0 && !site.before(f.compositeOrder) {
				f.setComposite(nil)
			}
		} else if !site.before(f.compositeOrder) {
			f.setComposite(nil)
		}
	}
	if first == (fieldOrder{}) {
		return false
	}
	f.declaration = first
	// Implicit holders (for example a synthetic common edge scope) may
	// survive solely through their children. Keep their diagnostic location.
	if slices.ContainsFunc(f.References, func(ref *FieldReference) bool { return site.before(ref.order) }) {
		f.References = slices.DeleteFunc(f.References, func(ref *FieldReference) bool { return !site.before(ref.order) })
	}
	return true
}
