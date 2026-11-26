package d2ir

import (
	"context"
	"errors"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/lib/textmeasure"
)

// Markdown allows literal placeholder spellings such as ${a}b} for a variable
// named "a}b". Keep those patterns while resolving D2 paths in lexical scope.
func (c *compiler) resolveMarkdown(node Node, value string, scopes []*Map, outerStart int) string {
	source := node.LastRef().AST()
	primary := node.Primary()
	replay := newMarkdownReplay(primary, value, len(c.importStack) > 1)
	names := make(map[string]string)
	type declaration struct {
		field *Field
		scope int
	}
	var pending map[string]declaration
	seen := make(map[Node]struct{})
	var outerNames map[string]bool
	for i, scope := range scopes {
		c.collectMarkdownNames(scope, "", source, seen, func(name string, field *Field) {
			if _, exists := names[name]; exists {
				return
			}
			names[name] = field.Primary().Value.ScalarString()
			if hasSubstitution(field.Primary().Value) {
				if pending == nil {
					pending = make(map[string]declaration)
				}
				pending[name] = declaration{field, i}
			}
			if outerStart >= 0 && i >= outerStart {
				if outerNames == nil {
					outerNames = make(map[string]bool)
				}
				outerNames[name] = true
			}
		})
		if c.stopped() {
			return value
		}
	}
	// An imported board can leave the scope that supplied a literal pattern.
	for name, text := range replay.bound {
		if _, exists := names[name]; !exists {
			if !c.reserveVariableExpansion(source, int64(len(name))+3) {
				return value
			}
			names[name] = text
		}
	}
	if len(names) == 0 {
		return value
	}
	resolve := func(key string) (string, bool) {
		previous, captured := replay.lookup(key)
		if captured && !replay.retry {
			return previous, true
		}
		text, exists := names[key]
		outer := outerNames[key]
		token := "${" + key + "}"
		parsed, err := d2parser.ParseValue(token)
		if s, ok := parsed.(*d2ast.UnquotedString); err == nil && ok && len(s.Value) == 1 && s.Value[0].Substitution != nil {
			field, scope, ok := c.readVariable(scopes, node, s.Value[0].Substitution)
			if !ok {
				replay.missing = true
				return token, true
			}
			if field == nil {
				replay.missing = true
				if captured {
					return previous, true
				}
				if exists {
					// A literal pattern can spell an unavailable path or a self-reference.
					return token, true
				}
				return "", false
			}
			if field.Primary() == nil {
				// A local map or empty declaration shadows an outer scalar.
				replay.missing = true
				return token, true
			}
			text, exists = field.Primary().Value.ScalarString(), true
			outer = outerStart >= 0 && scope >= outerStart
		} else if decl, ok := pending[key]; ok {
			// Historical literal patterns can contain a closing brace and
			// consequently have no parseable D2 substitution path.
			if !c.resolveVariableField(scopes[decl.scope:], node, decl.field, source) || decl.field.Primary() == nil {
				replay.missing = true
				return token, true
			}
			text = decl.field.Primary().Value.ScalarString()
		}
		if exists {
			replay.record(key, text)
			if outer {
				if primary.binding.markdownOuter == nil {
					primary.binding.markdownOuter = make(map[string]string)
				}
				primary.binding.markdownOuter[key] = text
			}
		}
		return text, exists
	}

	rendered, used, err := textmeasure.ResolveSubstitutionsMarkdownBounded(c.ctx, replay.source, names, c.variableExpansion.remaining(), resolve)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			c.contextErr = err
			c.halted = true
			return value
		}
		c.errorf(source, "variable substitution expansion exceeds limit of %d work units", c.variableExpansion.limit)
		c.halted = true
		return value
	}
	if !c.reserveVariableExpansion(source, used) {
		return value
	}
	replay.finish(rendered)
	return rendered
}

// Collect full path spellings only. A leaf inside db, for instance, is named
// db.host; it does not declare host in its enclosing variable scope.
func (c *compiler) collectMarkdownNames(vars *Map, prefix string, source d2ast.Node, seen map[Node]struct{}, add func(string, *Field)) {
	if vars == nil || c.stopped() || !c.visitSubstitutionNode(vars, source, seen) {
		return
	}
	for _, f := range vars.Fields {
		if c.stopped() || !c.visitSubstitutionNode(f, source, seen) {
			return
		}
		if f.Name == nil || (f.Primary() == nil && f.Map() == nil) {
			continue
		}
		name := f.Name.ScalarString()
		if !c.reserveVariableExpansion(source, int64(len(prefix))+int64(len(name))+3) {
			return
		}
		path := prefix + name
		if f.Primary() != nil {
			add(path, f)
		}
		if children := f.Map(); children != nil {
			c.collectMarkdownNames(children, path+".", source, seen, add)
		}
	}
}
