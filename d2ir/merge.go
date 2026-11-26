package d2ir

import "slices"

func structureCounts(m *Map) (fields, edges int) {
	if m == nil {
		return 0, 0
	}
	return m.FieldCountRecursive(), m.EdgeCountRecursive()
}

func markStructureCountDelta(parent *Map, beforeFields, beforeEdges int, after *Map) {
	afterFields, afterEdges := structureCounts(after)
	if beforeFields != afterFields || beforeEdges != afterEdges {
		parent.markStructureChanged()
	}
}

func OverlayMap(base, overlay *Map) {
	overlayMap(base, overlay, false)
}

func overlayMapIndexed(base, overlay *Map) {
	overlayMap(base, overlay, true)
}

func overlayMap(base, overlay *Map, indexed bool) {
	mergeMap(base, overlay, indexed, nil, 0)
}

func ExpandSubstitution(m, resolved *Map, placeholder *Field) {
	expandSubstitution(m, resolved, placeholder, false, nil)
}

type spreadSite struct {
	fieldOrder
	compiler *compiler
	source   Node
}

func (s *spreadSite) allows(order fieldOrder) bool {
	return s == nil || s.compiler == nil || !s.before(order)
}

func (s *spreadSite) place(order fieldOrder) fieldOrder {
	if s == nil {
		return order
	}
	return order.within(&s.fieldOrder)
}

func (r *FieldReference) occurrence() fieldOrder { return r.order }
func (r *EdgeReference) occurrence() fieldOrder  { return r.order }

// Keep the later explicit assignments at the end for diagnostics and edits.
// Successive earlier spreads normally move just that trailing assignment.
func insertEarlierRefs[R interface{ occurrence() fieldOrder }](dst, incoming []R, site *spreadSite) []R {
	at := len(dst)
	for at > 0 && site.before(dst[at-1].occurrence()) {
		at--
	}
	if !site.compiler.reserveVariableExpansion(site.source.LastRef().AST(), int64(len(dst)-at)) {
		return dst
	}
	return slices.Insert(dst, at, incoming...)
}

func (s *spreadSite) insertionIndex(m *Map) int {
	if s == nil {
		return 0
	}
	at := 0
	for at < len(m.Fields) && !s.before(m.Fields[at].declaration) {
		at++
	}
	return at
}

func expandSubstitution(m, resolved *Map, placeholder *Field, indexed bool, c *compiler) {
	at := slices.Index(m.Fields, placeholder)
	if at < 0 {
		at = len(m.Fields)
	}
	site := spreadSite{compiler: c, source: placeholder}
	if placeholder != nil {
		site.fieldOrder = placeholder.declaration
	}
	if placeholder != nil && placeholder.spreadDeletions != nil {
		if c != nil && !c.reserveVariableCopy(placeholder.LastRef().AST(), resolved) {
			return
		}
		resolved = resolved.Copy(nil).(*Map)
		if !placeholder.spreadDeletions.apply(c, resolved, placeholder.LastRef().AST()) {
			return
		}
	}
	mergeMap(m, resolved, indexed, &site, at)
	if c != nil && c.hasTemplateNull && !c.stopped() && !IsVar(m) {
		c.collectSpreadNulls(m, resolved, &site)
	}
}

// Merge a deferred spread at its original position. Earlier declarations retain
// their slots, while fields first mentioned by later keys or edge endpoints go
// in the spread's order. Apply the same rule inside overlapping containers.
func mergeMap(m, overlay *Map, indexed bool, site *spreadSite, at int) {
	if overlay.edgePropertyRules != nil {
		m.mergeEdgeRules(overlay.edgePropertyRules, site)
	}
	var spread map[*Field]bool
	var insert []*Field
	if site != nil {
		spread = make(map[*Field]bool, len(overlay.Fields))
	}
	for _, of := range overlay.Fields {
		var bf *Field
		if of.Name != nil { // An unresolved spread placeholder always has its own slot.
			if indexed {
				bf = m.getFieldIndexed(of.Name)
			} else {
				bf = m.getFieldLinear(false, of.Name)
			}
		}
		if bf == nil {
			bf = of.Copy(m).(*Field)
			if site != nil {
				bf.nestDeclarationOrder(&site.fieldOrder)
			}
			m.appendField(bf)
		} else {
			overlayFieldAt(bf, of, indexed, site)
			if site != nil && site.compiler != nil && site.compiler.halted {
				return
			}
		}
		if site != nil && !spread[bf] {
			spread[bf] = true
			insert = append(insert, bf)
		}
	}
	if site != nil {
		for _, f := range m.Fields[:at] {
			delete(spread, f)
		}
		if len(spread) != 0 {
			insert = slices.DeleteFunc(insert, func(f *Field) bool { return !spread[f] })
			tail := slices.DeleteFunc(m.Fields[at:], func(f *Field) bool { return spread[f] })
			copy(m.Fields[at+len(insert):], tail)
			copy(m.Fields[at:], insert)
		}
	}

	for _, oe := range overlay.Edges {
		var bea []*Edge
		if indexed {
			bea = m.getEdgesIndexed(oe.ID)
		} else {
			bea = m.GetEdges(oe.ID, nil, nil)
		}
		if len(bea) == 0 {
			edge := oe.Copy(m).(*Edge)
			if site != nil {
				edge.nestDeclarationOrder(&site.fieldOrder)
			}
			m.appendEdge(edge)
			continue
		}
		overlayEdge(bea[0], oe, indexed, site)
	}
}

// OverlayField applies one field overlay directly to another field.
//
// Deprecated: construct overlays as maps and apply them with OverlayMap.
// OverlayField is an internal IR mutation detail and will be removed after one
// compatibility release.
func OverlayField(bf, of *Field) {
	overlayField(bf, of, false)
}

func overlayFieldIndexed(bf, of *Field) {
	overlayField(bf, of, true)
}

func overlayField(bf, of *Field, indexed bool) {
	overlayFieldAt(bf, of, indexed, nil)
}

func overlayFieldAt(bf, of *Field, indexed bool, site *spreadSite) {
	if of.templateNull != nil {
		order := site.place(*of.templateNull)
		if bf.templateNull == nil || bf.templateNull.before(order) {
			bf.templateNull = &order
		}
	}
	// Composite substitutions share their variable's map until an overlay
	// edits it. Detach this occurrence and anchor its children at the use,
	// which may precede the variable definition or follow an earlier reference.
	if site != nil && of.Map() != nil && bf.Map() != nil && bf.Map().Parent() != bf {
		if site.compiler != nil && !site.compiler.reserveVariableCopy(site.source.LastRef().AST(), bf.Map()) {
			return
		}
		bf.setComposite(bf.copyAliasMap(bf))
	}
	if site != nil && site.before(bf.declaration) {
		bf.declaration = of.declaration.within(&site.fieldOrder)
	}
	applyPrimary := of.Primary_ != nil && (bf.Primary_ == nil || site.allows(bf.Primary_.order))
	if applyPrimary {
		bf.Primary_ = of.Primary_.Copy(bf).(*Scalar)
		bf.Primary_.order = site.place(bf.Primary_.order)
	}

	if of.Map() != nil && site != nil && site.compiler != nil {
		site.compiler.noteMapOpening(bf, site.fieldOrder)
	}
	_, array := of.Composite.(*Array)
	if of.Composite != nil && !(array && site.applyArrayReset(bf)) {
		if bf.Map() != nil && of.Map() != nil {
			if site.allows(bf.mapReset) {
				mergeMap(bf.Map(), of.Map(), indexed, site, site.insertionIndex(bf.Map()))
			}
			if site.allows(bf.compositeOrder) {
				bf.compositeOrder = site.place(of.compositeOrder)
			}
		} else if site.allows(bf.compositeOrder) {
			beforeFields, beforeEdges := structureCounts(bf.Map())
			if of.Map() != nil && of.Map().Parent() != of {
				bf.setComposite(of.copyAliasMap(bf))
			} else {
				bf.setComposite(of.Composite.Copy(bf).(Composite))
			}
			bf.compositeOrder = site.place(of.compositeOrder)
			if _, ok := bf.Composite.(*Array); ok {
				bf.mapReset = bf.compositeOrder
			}
			if site != nil {
				bf.Map().nestDeclarationOrder(&site.fieldOrder)
			}
			markStructureCountDelta(ParentMap(bf), beforeFields, beforeEdges, bf.Map())
		}
	}

	// Preserve the winning assignment as the last primary reference so
	// diagnostics and graph edits address the same source that supplied it.
	refs := of.References
	if site != nil {
		refs = fieldRefsWithin(refs, &site.fieldOrder)
	}
	if of.Primary_ != nil && !applyPrimary {
		bf.References = insertEarlierRefs(bf.References, refs, site)
	} else {
		bf.References = append(bf.References, refs...)
	}
}

// OverlayEdge applies one edge overlay directly to another edge.
//
// Deprecated: construct overlays as maps and apply them with OverlayMap.
// OverlayEdge is an internal IR mutation detail and will be removed after one
// compatibility release.
func OverlayEdge(be, oe *Edge) {
	overlayEdge(be, oe, false, nil)
}

func overlayEdge(be, oe *Edge, indexed bool, site *spreadSite) {
	applyPrimary := oe.Primary_ != nil && (be.Primary_ == nil || site.allows(be.Primary_.order))
	if applyPrimary {
		be.Primary_ = oe.Primary_.Copy(be).(*Scalar)
		be.Primary_.order = site.place(be.Primary_.order)
	}
	if oe.Map_ != nil {
		if be.Map_ != nil {
			mergeMap(be.Map_, oe.Map_, indexed, site, site.insertionIndex(be.Map_))
		} else {
			beforeFields, beforeEdges := structureCounts(be.Map())
			be.Map_ = oe.Map_.Copy(be).(*Map)
			if site != nil {
				be.Map_.nestDeclarationOrder(&site.fieldOrder)
			}
			markStructureCountDelta(ParentMap(be), beforeFields, beforeEdges, be.Map())
		}
	}
	refs := oe.References
	if site != nil {
		refs = edgeRefsWithin(refs, &site.fieldOrder)
	}
	if oe.Primary_ != nil && !applyPrimary {
		be.References = insertEarlierRefs(be.References, refs, site)
	} else {
		be.References = append(be.References, refs...)
	}
}
