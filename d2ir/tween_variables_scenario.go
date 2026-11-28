package d2ir

import (
	"slices"

	"github.com/d2lang/d2/d2ast"
)

// A local variable present only in the outgoing scenario falls back to an
// outer declaration in the incoming one. Interpolate it in the old lexical
// scope so readers outside that scope can keep following the outer variable.
func (c *compiler) scenarioVariableFallbacks(changes []tweenChange, previous, current *Map, objectPath []propertyPathPart) []tweenChange {
	varsKey := d2ast.FlatUnquotedString("vars")
	oldVars := previous.lookupField(varsKey).Map()
	if oldVars == nil {
		return changes
	}
	scopes := tweenVarScopes(current)
	if len(scopes) == 0 {
		return changes
	}
	local := current.lookupField(varsKey).Map()
	var walk func(before, after *Map, framePath []propertyPathPart, variablePath []*d2ast.StringBox)
	walk = func(before, after *Map, framePath []propertyPathPart, variablePath []*d2ast.StringBox) {
		for _, old := range before.Fields {
			if c.stopped() {
				return
			}
			if old.suspended || old.Name == nil || len(variablePath) == 0 && old.Name.ScalarString() == "d2-config" {
				continue
			}
			direct := after.lookupField(old.Name)
			path := append(variablePath, d2ast.MakeValueBox(old.Name).StringBox())
			if old.Map() != nil {
				part := propertyPathPart{field: old.Name}
				if direct == nil {
					// A new local map extends the inherited map. This keeps
					// other entries and whole-map readers in their declaration scope.
					end, _ := c.resolveVariable(scopes, current, &d2ast.Substitution{Path: path})
					if end.Map() != nil {
						part.inherit = &d2ast.Substitution{Path: slices.Clone(path)}
					}
				}
				if direct.Map() != nil || part.inherit != nil {
					walk(old.Map(), direct.Map(), append(framePath, part), path)
				}
			}
			if direct != nil || old.Primary() == nil {
				continue
			}
			end, scope := c.resolveVariable(scopes, current, &d2ast.Substitution{Path: path})
			// Replacing a map- or array-valued variable with a sampled scalar
			// would drop its other contents for whole-value substitutions.
			if end == nil || end.suspended || end.Primary() == nil || end.Composite != nil || scopes[scope] == local {
				continue
			}
			from, to := old.Primary().Value.ScalarString(), end.Primary().Value.ScalarString()
			value, ok := tweenScalarChange(from, to)
			if !ok {
				if from != to {
					continue
				}
				// A fixed local value needs a hold only if the outer scope
				// might otherwise move it during another variable's tween.
				outer, _ := c.resolveVariable(tweenVarScopes(ParentMap(previous)), previous, &d2ast.Substitution{Path: path})
				if outer != nil && outer.Primary() != nil && outer.Primary().Value.ScalarString() == to {
					continue
				}
			}
			changes = append(changes, tweenChange{path: slices.Clone(framePath), property: old, value: value})
		}
	}
	walk(oldVars, local, append(objectPath, propertyPathPart{field: varsKey}), nil)
	return changes
}
