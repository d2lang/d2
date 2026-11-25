package d2ir

import "slices"

// A deferred spread has to distinguish children declared before and after it,
// even inside an existing map or across files. Ordinary fields have one
// sequence number. Moving a template into a spread or reusing a cached import
// puts its declarations under that occurrence, preserving their internal order.
type fieldOrder struct {
	sequence uint64
	outer    *fieldOrder
}

func (c *compiler) nextFieldOrder() fieldOrder {
	c.fieldOrder++
	return fieldOrder{sequence: c.fieldOrder}
}

func (o fieldOrder) within(outer *fieldOrder) fieldOrder {
	if o.outer == nil {
		o.outer = outer
	} else {
		parent := o.outer.within(outer)
		o.outer = &parent
	}
	return o
}

func (o fieldOrder) before(other fieldOrder) bool {
	if o.outer == other.outer {
		return o.sequence < other.sequence
	}
	// Nested declarations compare lexicographically, starting at the
	// outermost spread/import. Short paths fit on the stack.
	var a, b [4]uint64
	return slices.Compare(o.path(a[:0]), other.path(b[:0])) < 0
}

func (o fieldOrder) path(buf []uint64) []uint64 {
	for curr := &o; curr != nil; curr = curr.outer {
		buf = append(buf, curr.sequence)
	}
	slices.Reverse(buf)
	return buf
}

// Lazy globs run again when new fields appear. Their assignments still occur
// at the rule, even when the compiler materializes a field after a spread.
func (c *compiler) assignmentOrder() fieldOrder {
	order := c.nextFieldOrder()
	if len(c.globRefContextStack) != 0 {
		order.outer = &c.globRefContextStack[0].order
	}
	return order
}

// Copying an alias materializes this occurrence of its variable. Establish
// its use-site order whether the copy is for an import, a board, or an edit.
func (f *Field) copyAliasMap(parent *Field) *Map {
	order := f.compositeOrder
	m := f.Map().Copy(parent).(*Map)
	m.nestDeclarationOrder(&order)
	return m
}

func (f *Field) nestDeclarationOrder(site *fieldOrder) {
	if f.templateNull != nil {
		order := f.templateNull.within(site)
		f.templateNull = &order
	}
	f.declaration = f.declaration.within(site)
	f.References = fieldRefsWithin(f.References, site)
	if f.Primary_ != nil {
		f.Primary_.order = f.Primary_.order.within(site)
	}
	if f.Composite != nil {
		f.compositeOrder = f.compositeOrder.within(site)
	}
	if f.mapReset.sequence != 0 {
		f.mapReset = f.mapReset.within(site)
	}
	if m := f.Map(); m != nil && m.Parent() == f {
		m.nestDeclarationOrder(site)
	}
}

func (e *Edge) nestDeclarationOrder(site *fieldOrder) {
	e.References = edgeRefsWithin(e.References, site)
	if e.Primary_ != nil {
		e.Primary_.order = e.Primary_.order.within(site)
	}
	e.Map().nestDeclarationOrder(site)
}

func (m *Map) nestDeclarationOrder(site *fieldOrder) {
	if m == nil {
		return
	}
	for _, field := range m.Fields {
		field.nestDeclarationOrder(site)
	}
	for _, edge := range m.Edges {
		edge.nestDeclarationOrder(site)
	}
}

// References stay attached to their original source keys. A moved template
// gets its own occurrence positions without changing those of its siblings.
func fieldRefsWithin(refs []*FieldReference, site *fieldOrder) []*FieldReference {
	moved := make([]*FieldReference, len(refs))
	copies := make([]FieldReference, len(refs))
	for i, ref := range refs {
		copies[i] = *ref
		copies[i].order = ref.order.within(site)
		moved[i] = &copies[i]
	}
	return moved
}

func edgeRefsWithin(refs []*EdgeReference, site *fieldOrder) []*EdgeReference {
	moved := make([]*EdgeReference, len(refs))
	copies := make([]EdgeReference, len(refs))
	for i, ref := range refs {
		copies[i] = *ref
		copies[i].order = ref.order.within(site)
		moved[i] = &copies[i]
	}
	return moved
}
