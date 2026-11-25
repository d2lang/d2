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
	compiler    *compiler
	placeholder *Field
}

func expandSubstitution(m, resolved *Map, placeholder *Field, indexed bool, c *compiler) {
	at := slices.Index(m.Fields, placeholder)
	if at < 0 {
		at = len(m.Fields)
	}
	site := spreadSite{compiler: c, placeholder: placeholder}
	if placeholder != nil {
		site.fieldOrder = placeholder.declaration
	}
	mergeMap(m, resolved, indexed, &site, at)
}

// Merge a deferred spread at its original position. Earlier declarations retain
// their slots, while fields first mentioned by later keys or edge endpoints go
// in the spread's order. Apply the same rule inside overlapping containers.
func mergeMap(m, overlay *Map, indexed bool, site *spreadSite, at int) {
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
				bf = m.GetField(of.Name)
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
			m.appendEdge(oe.Copy(m).(*Edge))
			continue
		}
		overlayEdge(bea[0], oe, indexed)
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
	// Composite substitutions share their variable's map until an overlay
	// edits it. Detach this occurrence and anchor its children at the use,
	// which may precede the variable definition or follow an earlier reference.
	if site != nil && of.Map() != nil && bf.Map() != nil && bf.Map().Parent() != bf {
		if site.compiler != nil && !site.compiler.reserveVariableCopy(site.placeholder.LastRef().AST(), bf.Map()) {
			return
		}
		bf.setComposite(bf.copyAliasMap(bf))
	}
	if site != nil && site.before(bf.declaration) {
		bf.declaration = of.declaration.within(&site.fieldOrder)
	}
	if of.Primary_ != nil {
		bf.Primary_ = of.Primary_.Copy(bf).(*Scalar)
		bf.aliasSite = of.aliasSite
		if site != nil && bf.aliasSite != nil {
			order := bf.aliasSite.within(&site.fieldOrder)
			bf.aliasSite = &order
		}
	}

	if of.Composite != nil {
		if bf.Map() != nil && of.Map() != nil {
			at := 0
			if site != nil {
				children := bf.Map().Fields
				for at < len(children) && !site.before(children[at].declaration) {
					at++
				}
			}
			mergeMap(bf.Map(), of.Map(), indexed, site, at)
		} else {
			beforeFields, beforeEdges := structureCounts(bf.Map())
			bf.setComposite(of.Composite.Copy(bf).(Composite))
			if site != nil {
				bf.Map().nestDeclarationOrder(&site.fieldOrder)
			}
			markStructureCountDelta(ParentMap(bf), beforeFields, beforeEdges, bf.Map())
		}
	}

	bf.References = append(bf.References, of.References...)
}

// OverlayEdge applies one edge overlay directly to another edge.
//
// Deprecated: construct overlays as maps and apply them with OverlayMap.
// OverlayEdge is an internal IR mutation detail and will be removed after one
// compatibility release.
func OverlayEdge(be, oe *Edge) {
	overlayEdge(be, oe, false)
}

func overlayEdge(be, oe *Edge, indexed bool) {
	if oe.Primary_ != nil {
		be.Primary_ = oe.Primary_.Copy(be).(*Scalar)
	}
	if oe.Map_ != nil {
		if be.Map_ != nil {
			overlayMap(be.Map(), oe.Map_, indexed)
		} else {
			beforeFields, beforeEdges := structureCounts(be.Map())
			be.Map_ = oe.Map_.Copy(be).(*Map)
			markStructureCountDelta(ParentMap(be), beforeFields, beforeEdges, be.Map())
		}
	}
	be.References = append(be.References, oe.References...)
}
