package d2compiler_test

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/util-go/assert"
)

func TestReservedVariableNames(t *testing.T) {
	for name := range d2ast.ReservedKeywords {
		t.Run(name, func(t *testing.T) {
			source := fmt.Sprintf("vars: {%s: 3}; count: ${%s}", name, strings.ToUpper(name))
			g, _, err := d2compiler.Compile("main.d2", strings.NewReader(source), nil)
			assert.Success(t, err)
			assert.String(t, "3", g.Root.Children["count"].Label.Value)
		})
	}
}

func TestDottedVariableDeclarations(t *testing.T) {
	const source = `vars.steps: 3
vars.width.label: build
group.vars: {classes.layers: 4}
group.read: "${width.label} / ${steps} / ${classes.layers}"
imported: @lib.vars.steps
layers.summary: {vars.scenarios: 5; count: ${scenarios}}
`
	fs := fstest.MapFS{"lib.d2": {Data: []byte(`vars."steps": done`)}}
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader(source), &d2compiler.CompileOptions{FS: fs})
	assert.Success(t, err)
	assert.String(t, "build / 3 / 4", g.Root.Children["group"].Children["read"].Label.Value)
	assert.String(t, "done", g.Root.Children["imported"].Label.Value)
	assert.Equal(t, 1, len(g.Layers))
	assert.String(t, "5", g.Layers[0].Root.Children["count"].Label.Value)
}

func TestVariableNameQuotingAndScopes(t *testing.T) {
	const source = `vars: {
  style: outer
  width: wide
  "height": tall
  vars: {tone: inner}
  name: ${tone}
  link: layers.overview
  template: {shape: diamond; style.fill: red}
}
group: {
  vars: {"style": local}
  read: "${STYLE} / ${"style"} / ${"width"} / ${height} / ${name} / ${link}"
  copied: ${template}
}
both: {
  vars: {style: first; "style": second}
  read: "${style} / ${"style"}"
}
`
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader(source), nil)
	assert.Success(t, err)
	group := g.Root.Children["group"]
	assert.String(t, "outer / local / wide / tall / inner / root.layers.overview", group.Children["read"].Label.Value)
	assert.String(t, "diamond", group.Children["copied"].Shape.Value)
	assert.String(t, "red", group.Children["copied"].Style.Fill.Value)
	assert.String(t, "first / second", g.Root.Children["both"].Children["read"].Label.Value)
}

func TestVariableDataExcludesConfigAndLegend(t *testing.T) {
	for _, source := range []string{
		`vars: {D2-CONFIG: {data: {width.v: val}}}`,
		`vars: {D2-LEGEND: {classes: {unit}}}`,
	} {
		_, _, err := d2compiler.Compile("main.d2", strings.NewReader(source), nil)
		if err == nil {
			t.Fatalf("expected invalid diagram or configuration key in %q", source)
		}
	}
}

func TestImportedKeywordVariables(t *testing.T) {
	fs := fstest.MapFS{
		"lib.d2": {Data: []byte(`"classes": {item: {value: literal}}
classes: {item: {label: imported}}
steps: {release: {label: deployed}}
`)},
		"mid.d2": {Data: []byte(`vars: {lib: @lib; style: @lib; steps: @lib}`)},
	}
	const source = `...@mid
literal: ${lib."classes".item.value}
imported: ${lib.classes.item.label}
board: ${style.steps.release.label}
selected: @mid.vars.lib.classes.item.label
combined: @mid.vars.steps.classes.item.label
group: {
  vars: {lib: {"classes": {item: {label: local}}}}
  from-outer: ${lib.classes.item.label}
  from-inner: ${lib."classes".item.label}
}
`
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader(source), &d2compiler.CompileOptions{FS: fs})
	assert.Success(t, err)
	for id, want := range map[string]string{
		"literal": "literal", "imported": "imported", "board": "deployed",
		"selected": "imported", "combined": "imported",
	} {
		assert.String(t, want, g.Root.Children[id].Label.Value)
	}
	group := g.Root.Children["group"]
	assert.String(t, "imported", group.Children["from-outer"].Label.Value)
	assert.String(t, "local", group.Children["from-inner"].Label.Value)
}
