package d2compiler_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/util-go/assert"
)

func TestGlobFilterReadsResolvedScalars(t *testing.T) {
	t.Parallel()
	const input = `vars.state: outdated
literal: healthy
from-var: ${state}
later: healthy
nested: {vars.state: local; child: ${state}}
**: {&label: ${state}; style.fill: red}
**: {&style.fill: red; style.fill: blue}
from-var: renamed
later.style.fill: pink
vars: {state: ${actual}; actual: healthy}
`
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader(input), nil)
	assert.Success(t, err)
	want := map[string]string{"literal": "blue", "from-var": "blue", "later": "pink", "nested": "", "nested.child": ""}
	for _, obj := range g.Objects {
		var fill string
		if obj.Style.Fill != nil {
			fill = obj.Style.Fill.Value
		}
		assert.String(t, want[obj.AbsID()], fill)
		delete(want, obj.AbsID())
	}
	assert.Equal(t, 0, len(want))
}

func TestGlobFilterImportsAndForwardRules(t *testing.T) {
	t.Parallel()
	const input = `*: {&label: "${state}"; style.fill: red}
first: "health${suffix}"
second: other
*: {!&label: ${state}; tooltip: unmatched}
vars: @values
first -> second
(* -> *)[*]: {&src.label: healthy; label: matched}
`
	files := fstest.MapFS{"values.d2": {Data: []byte("state: healthy; suffix: y")}}
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader(input), &d2compiler.CompileOptions{FS: files})
	assert.Success(t, err)
	assert.Equal(t, 2, len(g.Objects))
	assert.String(t, "red", g.Objects[0].Style.Fill.Value)
	assert.True(t, g.Objects[0].Tooltip == nil)
	assert.True(t, g.Objects[1].Style.Fill == nil)
	assert.String(t, "unmatched", g.Objects[1].Tooltip.Value)
	assert.Equal(t, 1, len(g.Edges))
	assert.String(t, "matched", g.Edges[0].Label.Value)
}

func TestGlobVariableWriteResolvesDependentFilter(t *testing.T) {
	t.Parallel()
	const input = `vars.pick: target
x: target {vars: {state: before; alias: ${state}}; child: ${alias}}
*: {&label: ${pick}; vars.state: after}
x.*: {&label: after; tooltip: matched}
`
	for _, tc := range []struct {
		tail, label string
		matched     bool
	}{
		{"", "after", true},
		{"x.vars.state: final", "final", false},
	} {
		g, _, err := d2compiler.Compile("main.d2", strings.NewReader(input+tc.tail), nil)
		assert.Success(t, err)
		child := g.Root.Children["x"].Children["child"]
		assert.String(t, tc.label, child.Label.Value)
		assert.Equal(t, tc.matched, child.Tooltip != nil)
	}
}

func TestGlobFilterUsesImportSiteVars(t *testing.T) {
	t.Parallel()
	const input = `a: {vars.state: ready; graph: @module}
b: {vars.state: busy; graph: @module}
`
	files := fstest.MapFS{"module.d2": {Data: []byte("service: ${state}\n*: {&label: ready; style.fill: green}")}}
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader(input), &d2compiler.CompileOptions{FS: files})
	assert.Success(t, err)
	ready := g.Root.Children["a"].Children["graph"].Children["service"]
	busy := g.Root.Children["b"].Children["graph"].Children["service"]
	assert.String(t, "ready", ready.Label.Value)
	assert.String(t, "green", ready.Style.Fill.Value)
	assert.String(t, "busy", busy.Label.Value)
	assert.True(t, busy.Style.Fill == nil)
}

func TestGlobFilterImportAttachment(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ attachment, module string }{
		{"@module", "node: ${state}\n*: {&label: ready; style.fill: green}"},
		{"@module.pkg", "pkg: {node: ${state}; *: {&label: ready; style.fill: green}}"},
		{"{...@module}", "node: ${state}\n*: {&label: ready; style.fill: green}"},
		{"{...@module.pkg}", "pkg: {node: ${state}; *: {&label: ready; style.fill: green}}"},
	} {
		t.Run(tc.attachment, func(t *testing.T) {
			input := "obj: " + tc.attachment + "\nobj.vars.state: ready"
			files := fstest.MapFS{"module.d2": {Data: []byte(tc.module)}}
			g, _, err := d2compiler.Compile("main.d2", strings.NewReader(input), &d2compiler.CompileOptions{FS: files})
			assert.Success(t, err)
			node := g.Root.Children["obj"].Children["node"]
			assert.String(t, "ready", node.Label.Value)
			assert.String(t, "green", node.Style.Fill.Value)
		})
	}
	t.Run("selected primary", func(t *testing.T) {
		const input = "vars.state: outside\nobj: @module.node\nobj.vars.state: inside"
		files := fstest.MapFS{"module.d2": {Data: []byte("node: ${state}\n*: {&label: outside; style.fill: green}")}}
		g, _, err := d2compiler.Compile("main.d2", strings.NewReader(input), &d2compiler.CompileOptions{FS: files})
		assert.Success(t, err)
		node := g.Root.Children["obj"]
		assert.String(t, "outside", node.Label.Value)
		assert.String(t, "green", node.Style.Fill.Value)
	})
}
