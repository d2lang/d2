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

func (f *Field) nestDeclarationOrder(site *fieldOrder) {
	f.declaration = f.declaration.within(site)
	f.Map().nestDeclarationOrder(site)
}

func (m *Map) nestDeclarationOrder(site *fieldOrder) {
	if m == nil {
		return
	}
	for _, field := range m.Fields {
		field.nestDeclarationOrder(site)
	}
}
