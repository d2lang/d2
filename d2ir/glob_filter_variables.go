package d2ir

import (
	"slices"

	"github.com/d2lang/d2/d2ast"
)

type globFilter struct {
	*compiler
	candidate Node
	pending   bool
}

type filterOperand struct {
	scope *Map
	value d2ast.Scalar
}

type filterResult struct {
	value   d2ast.Scalar
	pending bool
	errors  []d2ast.Error
}

// The selector's lexical scope is shared by all its candidates.
func (g *globFilter) operand(s *Scalar, scope *Map) d2ast.Scalar {
	if g.collectFilterVars {
		g.pending = true
		return nil
	}
	key := filterOperand{scope, s.Value}
	result, ok := g.filterOperands[key]
	if !ok {
		before := len(g.err.Errors)
		result.value = g.scalar(s, true)
		result.pending = g.pending
		result.errors = g.takeFilterImportErrors(before)
		if g.filterOperands == nil {
			g.filterOperands = make(map[filterOperand]filterResult)
		}
		g.filterOperands[key] = result
	}
	// One lexical read can affect several candidates, only some of which
	// belong to the selected export.
	for _, err := range result.errors {
		g.reportFilterImportError(g.candidate, err)
	}
	g.pending = result.pending
	return result.value
}

func (g *globFilter) takeFilterImportErrors(before int) []d2ast.Error {
	if len(g.filterImports) == 0 || g.stopped() {
		return nil
	}
	errors := slices.Clone(g.err.Errors[before:])
	g.err.Errors = g.err.Errors[:before]
	return errors
}

func (g *globFilter) scalar(s *Scalar, operand bool) d2ast.Scalar {
	value, _ := g.value(s, operand)
	return value
}

// Evaluate a copy: the same filter expression can run in several boards, and
// a label's ordinary substitution still needs its original expression.
func (g *globFilter) value(s *Scalar, operand bool) (d2ast.Scalar, *Array) {
	if !hasSubstitution(s.Value) {
		return filterScalarValue(s, operand), nil
	}
	if g.collectFilterVars {
		g.pending = true
		return nil, nil
	}
	g.filterReads++
	local := ParentMap(s)
	var origin Node = local
	scopes := g.filterVarScopes(local, nil)
	for i := len(g.filterImports) - 1; i >= 0; i-- {
		origin, scopes = g.filterImportScopes(g.filterImports[i], origin, scopes)
	}
	if !g.reserveVariableExpansion(s.Value, variableExpansionScalarValueUnits(s.Value)) {
		return nil, nil
	}
	copy := s.Copy(s.Parent()).(*Scalar)
	previous, visits := g.scalarResolutions, g.substitutionVisits
	g.scalarResolutions = make(map[*Scalar]scalarResolution)
	g.substitutionVisits = make(map[Node]struct{})
	before := len(g.err.Errors)
	g.resolveSubstitutions(scopes, copy)
	g.scalarResolutions, g.substitutionVisits = previous, visits
	if len(g.err.Errors) != before {
		if !operand {
			for _, err := range g.takeFilterImportErrors(before) {
				g.reportFilterImportError(g.candidate, err)
			}
		}
		g.pending = true
		return nil, nil
	}
	if text, ok := copy.Value.(*d2ast.UnquotedString); ok && hasSubstitution(text) {
		// A whole-value substitution of a map or array supplies no scalar.
		if operand {
			g.errorf(s.Value, "glob filters cannot be composites")
			g.pending = true
		} else if len(text.Value) == 1 && text.Value[0].Substitution != nil {
			// readVariable already resolved this binding in these scopes.
			field, _ := g.resolveVariable(scopes, copy, text.Value[0].Substitution)
			if field != nil {
				array, _ := field.Composite.(*Array)
				return nil, array
			}
		}
		return nil, nil
	}
	return filterScalarValue(copy, operand), nil
}

type filterImport struct {
	site   Node
	source *d2ast.Import
	errors []filterImportError
}

func (c *compiler) filterImportScopes(entry filterImport, origin Node, scopes []*Map) (Node, []*Map) {
	site := entry.site
	parent := ParentMap(site)
	if path := entry.source.IDA(); len(path) > 0 && origin != nil {
		// The selected field's own primary lives in the caller's outer
		// scope. Only its descendants inherit the attachment's local vars.
		m, ok := origin.(*Map)
		if !ok {
			m = ParentMap(origin)
		}
		inside := false
		if m != nil {
			selected := RootMap(m).getFieldIndexedMode(path, true)
			inside = withinImport(origin, selected)
		}
		if !inside {
			return parent, c.filterVarScopes(parent, scopes)
		}
	}
	if m := site.Map(); m != nil {
		return m, c.filterVarScopes(m, scopes)
	}
	// A field import can create its map before declarations later in the
	// caller supply that map's vars. A spread imports directly into its map.
	if field, ok := site.(*Field); ok && parent != nil {
		if source := c.filterScope(parent); source != nil {
			if target := source.getFieldIndexed(field.Name).Map(); target != nil {
				if vars := target.getFieldIndexed(d2ast.FlatUnquotedString("vars")).Map(); vars != nil {
					scopes = append(scopes, vars)
				}
			}
		}
	}
	return site, c.filterVarScopes(parent, scopes)
}

func (c *compiler) filterVarScopes(scope *Map, scopes []*Map) []*Map {
	for m := scope; m != nil; m = ParentMap(m) {
		if frozen := c.filterScope(m); frozen != nil {
			if vars := frozen.getFieldIndexed(d2ast.FlatUnquotedString("vars")).Map(); vars != nil {
				scopes = append(scopes, vars)
			}
		}
		// An earlier matched rule can introduce bindings of its own.
		if vars := m.getFieldIndexed(d2ast.FlatUnquotedString("vars")).Map(); vars != nil {
			scopes = append(scopes, vars)
		}
	}
	return scopes
}

func (c *compiler) filterScope(m *Map) *Map {
	if cached, ok := c.filterScopes[m]; ok {
		return cached
	}
	var scope *Map
	if m.Root() {
		scope = c.collectVariableScopes(m)
	} else if parent := c.filterScope(ParentMap(m)); parent != nil {
		switch owner := m.Parent().(type) {
		case *Field:
			scope = parent.getFieldIndexed(owner.Name).Map()
		case *Edge:
			if matches := parent.getEdgesIndexed(owner.ID); len(matches) != 0 {
				scope = matches[0].Map()
			}
		}
	}
	if c.filterScopes == nil {
		c.filterScopes = make(map[*Map]*Map)
	}
	c.filterScopes[m] = scope
	return scope
}

// A conditional rule can assign a variable after a previous predicate has
// read it. Refresh the snapshot on the next read so dependent variables also
// resolve against the new value.
func (c *compiler) updateFilterVariable(field *Field) {
	if field.Primary() == nil || field.Name == nil {
		return
	}
	root := RootMap(ParentMap(field))
	if c.filterWrites == nil {
		c.filterWrites = make(map[*Map]map[*Field]*Scalar)
	}
	if c.filterWrites[root] == nil {
		c.filterWrites[root] = make(map[*Field]*Scalar)
	}
	c.filterWrites[root][field] = field.Primary()
	c.filterScopes = nil
	c.filterOperands = nil
}

func (c *compiler) applyFilterWrites(root *Map) {
	for field, primary := range c.filterWrites[root] {
		if field.Primary() != primary {
			continue
		}
		parent := c.filterScope(ParentMap(field))
		if parent == nil {
			continue
		}
		frozen := parent.getFieldIndexed(field.Name)
		if frozen == nil {
			if c.reserveVariableCopy(primary.Value, field) {
				parent.appendField(field.Copy(parent).(*Field))
			}
			continue
		}
		// A later explicit declaration must still win. A source reference
		// shared with the live binding means that declaration has executed.
		if ref := frozen.LastPrimaryRef(); ref != nil {
			position := ref.AST().GetRange()
			for _, live := range field.References {
				if live.AST().GetRange() == position {
					if c.reserveVariableExpansion(primary.Value, variableExpansionScalarValueUnits(primary.Value)) {
						frozen.Primary_ = primary.Copy(frozen).(*Scalar)
					}
					break
				}
			}
		}
	}
}

// Retain only explicit variable declarations and their containing maps when
// unrelated shapes cannot contribute bindings. Imports, spreads and
// rules writing vars use the full compiler semantics.
func (c *compiler) filterDeclarations(m *d2ast.Map, boardGroup bool) (*d2ast.Map, bool) {
	selected := &d2ast.Map{Range: m.Range}
	for _, entry := range m.Nodes {
		if c.stopped() || entry.Import != nil || entry.Substitution != nil {
			return nil, false
		}
		key := entry.MapKey
		if key == nil {
			continue
		}
		vars, board, group := false, false, false
		if key.Key != nil {
			for i, part := range key.Key.Path {
				keyword := d2ast.Keyword(part.Unbox())
				if _, ok := d2ast.BoardKeywords[keyword]; ok {
					board = true
					group = i == len(key.Key.Path)-1
				}
				vars = vars || keyword == "vars"
			}
		}
		if key.Value.Import != nil && !vars {
			return nil, false
		}
		if key.EdgeKey != nil {
			for _, part := range key.EdgeKey.Path {
				if d2ast.Keyword(part.Unbox()) == "vars" {
					return nil, false
				}
			}
		}
		var children *d2ast.Map
		if key.Value.Map != nil {
			var ok bool
			children, ok = c.filterDeclarations(key.Value.Map, group)
			if !ok {
				return nil, false
			}
		}
		if vars {
			if key.HasGlob() || children != nil && len(children.Nodes) != 0 {
				return nil, false
			}
			selected.Nodes = append(selected.Nodes, entry)
		} else if board || boardGroup || children != nil && len(children.Nodes) != 0 || key.Value.Null != nil || key.Value.Suspension != nil || key.Value.Array != nil {
			if key.HasGlob() || len(key.Edges) != 0 {
				return nil, false
			}
			copy := *key
			copy.Value.Map = children
			if a := key.Value.Array; a != nil {
				copy.Value.Array = &d2ast.Array{Range: a.Range}
			}
			entry.MapKey = &copy
			selected.Nodes = append(selected.Nodes, entry)
		}
	}
	return selected, true
}

// Filter effects must occur at their declaration, but variable bindings can be
// declared, imported, or overridden later. Materialize those lexical scopes
// on demand using the ordinary compiler. The active compilation alone applies
// rules depending on variables. Both walks share work limits and parsed files.
func (c *compiler) collectVariableScopes(root *Map) *Map {
	source := root.LastRef().Context().Scope
	selected, simple := c.filterDeclarations(source, false)
	if !simple {
		selected = source
	}
	if !c.reserveVariableASTCopy(source, selected) {
		return nil
	}
	ast, err := cloneASTMapContext(c.ctx, selected)
	if err != nil {
		c.contextErr, c.halted = err, true
		return nil
	}
	stack := c.importStack
	for i, path := range stack {
		if path == source.GetRange().Path {
			stack = stack[:i+1]
			break
		}
	}
	collector := compiler{
		ctx: c.ctx, fs: c.fs, utf16Pos: c.utf16Pos, parsedImports: c.parsedImports,
		importStack:       append([]string(nil), stack...),
		variableExpansion: c.variableExpansion, globExpansion: c.globExpansion,
		edgeExpansion: c.edgeExpansion, edgeExpansionWork: c.edgeExpansionWork,
		collectFilterVars: true,
	}
	collector.initCompilation()
	values := &Map{}
	values.initRoot()
	values.parent.(*Field).References[0].Context_.Scope = ast
	values.parent.(*Field).References[0].Context_.ScopeAST = ast
	collector.compileMap(values, ast, ast)
	if len(c.filterWrites[root]) > 0 && !collector.stopped() {
		if c.filterScopes == nil {
			c.filterScopes = make(map[*Map]*Map)
		}
		c.filterScopes[root] = values
		c.applyFilterWrites(root)
	}
	collector.compileSubstitutions(values, nil)
	if collector.stopped() {
		c.halted = true
		c.contextErr, c.expansionErr, c.globExpansionErr = collector.contextErr, collector.expansionErr, collector.globExpansionErr
		if len(collector.err.Errors) > 0 {
			c.err.Errors = append(c.err.Errors, collector.err.Errors[len(collector.err.Errors)-1])
		}
		return nil
	}
	return values
}
