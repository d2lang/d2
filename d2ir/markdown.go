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
	seen := make(map[Node]struct{})
	var outerNames map[string]bool
	for i, scope := range scopes {
		c.collectMarkdownNames(scope, "", source, seen, func(name, text string) {
			if _, exists := names[name]; exists {
				return
			}
			names[name] = text
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
		if text, captured := replay.lookup(key); captured {
			return text, true
		}
		text, exists := names[key]
		outer := outerNames[key]
		token := "${" + key + "}"
		parsed, err := d2parser.ParseValue(token)
		if s, ok := parsed.(*d2ast.UnquotedString); err == nil && ok && len(s.Value) == 1 && s.Value[0].Substitution != nil {
			field, scope := c.resolveVariable(scopes, node, s.Value[0].Substitution)
			if field == nil && exists {
				// A literal pattern may spell an unavailable D2 path, including a
				// self-reference. Do not substitute an unrelated flattened value.
				return token, true
			}
			if field != nil {
				if field.Primary() == nil {
					// Markdown keeps placeholders with no scalar value as prose.
					return token, true
				}
				text, exists = field.Primary().Value.ScalarString(), true
				outer = outerStart >= 0 && scope >= outerStart
			}
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
func (c *compiler) collectMarkdownNames(vars *Map, prefix string, source d2ast.Node, seen map[Node]struct{}, add func(string, string)) {
	if vars == nil || c.stopped() || !c.visitSubstitutionNode(vars, source, seen) {
		return
	}
	for _, f := range vars.Fields {
		if c.stopped() || !c.visitSubstitutionNode(f, source, seen) {
			return
		}
		if f.Primary() == nil && f.Map() == nil {
			continue
		}
		name := f.Name.ScalarString()
		if !c.reserveVariableExpansion(source, int64(len(prefix))+int64(len(name))+3) {
			return
		}
		path := prefix + name
		if f.Primary() != nil {
			add(path, f.Primary().Value.ScalarString())
		}
		if children := f.Map(); children != nil {
			c.collectMarkdownNames(children, path+".", source, seen, add)
		}
	}
}
