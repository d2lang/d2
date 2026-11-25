package d2ir

import (
	"slices"

	"github.com/d2lang/d2/d2ast"
)

type spreadNull struct {
	field  *Field
	order  fieldOrder
	source d2ast.Node
}

func (c *compiler) setFieldPrimary(f *Field, value d2ast.Scalar) {
	order := c.assignmentOrder()
	f.Primary_ = &Scalar{parent: f, Value: value, order: order}
	if _, null := value.(*d2ast.Null); null {
		// Ordinary nulls delete their field during compilation. A null inside
		// vars remains a value so it can shadow a variable from an outer scope.
		c.hasTemplateNull = true
		f.templateNull = &order
	}
}

// A literal null in a map variable becomes a deletion where the map is spread.
// Delay removal until the walk is done: a deletion can precede its own spread
// placeholder in the field slice, and another spread can recreate the field.
func (c *compiler) collectSpreadNulls(dst, src *Map, site *spreadSite) {
	for _, from := range src.Fields {
		if from.Name == nil || from.Name.IsUnquoted() && from.Name.ScalarString() == "vars" {
			continue
		}
		to := dst.getFieldIndexed(from.Name)
		if to == nil {
			continue
		}
		if from.templateNull != nil {
			order := site.place(*from.templateNull)
			c.spreadNulls = append(c.spreadNulls, spreadNull{to, order, site.source.LastRef().AST()})
		}
		if from.Map() != nil && to.Map() != nil {
			c.collectSpreadNulls(to.Map(), from.Map(), site)
		}
	}
	for _, from := range src.Edges {
		if from.Map() == nil {
			continue
		}
		matches := dst.getEdgesIndexed(from.ID)
		if len(matches) > 0 && matches[0].Map() != nil {
			c.collectSpreadNulls(matches[0].Map(), from.Map(), site)
		}
	}
}

func (c *compiler) finishSpreadNulls() {
	if len(c.spreadNulls) == 0 || c.stopped() {
		return
	}
	reorder := make(map[*Map]bool)
	for _, deletion := range c.spreadNulls {
		f, order := deletion.field, deletion.order
		if f.templateNull == nil || order.before(*f.templateNull) {
			continue
		}
		m := ParentMap(f)
		f.templateNull = nil
		// Imports may still contain an earlier spread supplied by the caller.
		c.recordSpreadDeletionBefore(m, &spreadMember{field: f.Name}, deletion.source, &order)
		if !c.reserveVariableCopy(deletion.source, f) {
			return
		}
		if f.Map() != nil && f.Map().Parent() != f {
			f.setComposite(f.copyAliasMap(f))
		}
		// A later reference can recreate an endpoint. Its earlier incident
		// edges still belonged to the deleted occurrence.
		m.deleteEdgesForReferences(f.References, &order)
		if f.retainAfter(order) {
			reorder[m] = true
		} else {
			m.deleteField(f)
		}
	}
	for m := range reorder {
		m.sortFieldsByDeclaration()
	}
}

// Several uses of an edge template can share an edge ID and source context.
// Retain only the creation and edits after its endpoint was recreated.
func (e *Edge) recreatedAfter(cutoff fieldOrder) bool {
	recreated := slices.ContainsFunc(e.References, func(ref *EdgeReference) bool {
		return cutoff.before(ref.order) && ref.Context_.Key.EdgeIndex == nil
	})
	if !recreated {
		return false
	}
	e.References = slices.DeleteFunc(e.References, func(ref *EdgeReference) bool {
		return !cutoff.before(ref.order)
	})
	if e.Primary_ != nil && !cutoff.before(e.Primary_.order) {
		e.Primary_ = nil
	}
	if m := e.Map(); m != nil {
		m.discardBefore(cutoff)
		if len(m.Fields)+len(m.Edges) == 0 && !slices.ContainsFunc(e.References, func(ref *EdgeReference) bool { return ref.Context_.Key.Value.Map != nil }) {
			e.Map_ = nil
		}
	}
	return true
}
