package d2ir

import (
	"slices"

	"github.com/d2lang/d2/d2ast"
)

// A spread can introduce the endpoints of an indexed property selector long
// after its declaration. A copied step inherits the selector but can materialize
// a different set of edges, so its matches belong to that physical scope.
func (c *compiler) edgePropertyGlobsForSpread(m *Map, source d2ast.Node) []*globContext {
	var globs []*globContext
	crossedBoard := false
	for scope := m; scope != nil; scope = ParentMap(scope) {
		if !c.reserveGlobWork(source, 1) {
			return nil
		}
		for rule := scope.edgePropertyRules; rule != nil; rule = rule.next {
			g := rule.glob
			if !c.reserveGlobWork(g.refctx.Key, 1) {
				return nil
			}
			if crossedBoard && !g.refctx.Key.HasTripleGlob() {
				continue
			}
			copy := c.spreadRuleContext(scope, g)
			if copy == nil {
				return nil
			}
			globs = append(globs, copy)
		}
		if NodeBoardKind(scope) != "" {
			crossedBoard = true
		}
	}
	if len(globs) == 0 {
		return m.globs
	}
	if !c.reserveGlobWork(source, int64(len(m.globs))) {
		return nil
	}
	for _, g := range m.globs {
		if !replaysEdgeProperties(g.refctx.Key) {
			globs = append(globs, g)
		}
	}
	slices.SortStableFunc(globs, func(a, b *globContext) int {
		if a.refctx.order.before(b.refctx.order) {
			return -1
		}
		if b.refctx.order.before(a.refctx.order) {
			return 1
		}
		return 0
	})
	return globs
}

type spreadRuleScope struct {
	scope *Map
	rule  *globContext
}

func (c *compiler) spreadRuleContext(scope *Map, g *globContext) *globContext {
	key := spreadRuleScope{scope, g}
	if existing := c.spreadRuleCopies[key]; existing != nil {
		return existing
	}
	if !c.reserveGlobWork(g.refctx.Key, 1+int64(len(g.appliedEdges)+len(g.appliedFields))) {
		return nil
	}
	copy := *g
	copy.refctx = g.refctx.Copy()
	copy.refctx.ScopeMap = scope
	copy.copyApplied(g)
	if c.spreadRuleCopies == nil {
		c.spreadRuleCopies = make(map[spreadRuleScope]*globContext)
	}
	c.spreadRuleCopies[key] = &copy
	return &copy
}

func replaysEdgeProperties(k *d2ast.Key) bool {
	if k.EdgeKey != nil && (k.Primary.Null != nil || k.Value.Null != nil) {
		return true
	}
	// Existing-edge [*] selectors accept initially empty matches. Numeric
	// setters keep their normal validation that an indexed edge already exists.
	return k.EdgeIndex != nil && k.EdgeIndex.Glob && (k.EdgeKey != nil || (k.Primary.Unbox() == nil && k.Value.Map != nil))
}

// Board copies may share history. Reopening a scope or declaring another
// selector adds one entry without mutating a preceding board's rules.
type spreadEdgeRule struct {
	glob *globContext
	next *spreadEdgeRule
	// The owning map builds this index only when combining independent
	// histories. Copies share the immutable tail and build their own index.
	index map[*globContext]struct{}
}

func (rules *spreadEdgeRule) clone(cloneGlob func(*globContext) *globContext) *spreadEdgeRule {
	var head *spreadEdgeRule
	tail := &head
	for r := rules; r != nil; r = r.next {
		*tail = &spreadEdgeRule{glob: cloneGlob(r.glob)}
		tail = &(*tail).next
	}
	return head
}

func edgeRuleWithin(rule *globContext, site *fieldOrder) *globContext {
	g := *rule
	g.refctx = rule.refctx.Copy()
	g.refctx.order = rule.refctx.order.within(site)
	return &g
}

func (rules *spreadEdgeRule) fork() *spreadEdgeRule {
	if rules == nil || rules.index == nil {
		return rules
	}
	return &spreadEdgeRule{glob: rules.glob, next: rules.next}
}

func (m *Map) addEdgeRule(g *globContext) {
	r := &spreadEdgeRule{glob: g, next: m.edgePropertyRules}
	if r.next != nil && r.next.index != nil {
		r.index = r.next.index
		r.index[g] = struct{}{}
	}
	m.edgePropertyRules = r
}

func (m *Map) mergeEdgeRules(rules *spreadEdgeRule, site *spreadSite) {
	if site != nil {
		for r := rules; r != nil; r = r.next {
			m.addEdgeRule(edgeRuleWithin(r.glob, &site.fieldOrder))
		}
		return
	}
	if m.edgePropertyRules == rules {
		return
	}
	if m.edgePropertyRules == nil {
		m.edgePropertyRules = rules.fork()
		return
	}
	if m.edgePropertyRules.index == nil {
		head := *m.edgePropertyRules
		head.index = make(map[*globContext]struct{})
		for r := m.edgePropertyRules; r != nil; r = r.next {
			head.index[r.glob] = struct{}{}
		}
		m.edgePropertyRules = &head
	}
	for r := rules; r != nil; r = r.next {
		if _, ok := m.edgePropertyRules.index[r.glob]; !ok {
			m.addEdgeRule(r.glob)
		}
	}
}
