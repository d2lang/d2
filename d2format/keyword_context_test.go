package d2format_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/util-go/assert"
)

func TestReservedWordsInLabelsAndQuotedKeys(t *testing.T) {
	t.Parallel()

	const source = `heading: STYLE
layers: {release: {child}}
"steps": WIDTH
'scenarios': {'layers': SHAPE}
"layers": visible
`
	const want = `heading: STYLE

layers: {release: {child}}
"steps": WIDTH
'scenarios': {'layers': SHAPE}
"layers": visible
`
	ast, err := d2parser.Parse("labels.d2", strings.NewReader(source), nil)
	assert.Success(t, err)
	formatted := d2format.Format(ast)
	assert.String(t, want, formatted)

	for _, input := range []string{source, formatted} {
		g, _, err := d2compiler.Compile("labels.d2", strings.NewReader(input), nil)
		assert.Success(t, err)
		labels := map[string]string{
			"heading": "STYLE", "steps": "WIDTH", "scenarios": "scenarios",
			"scenarios.layers": "SHAPE", "layers": "visible",
		}
		assert.Equal(t, len(labels), len(g.Objects))
		for _, o := range g.Objects {
			assert.String(t, labels[o.AbsID()], o.Label.Value)
		}
		assert.Equal(t, 1, len(g.Layers))
		assert.Equal(t, "release", g.Layers[0].Name)
		assert.Equal(t, 1, len(g.Layers[0].Objects))
		assert.Equal(t, "child", g.Layers[0].Objects[0].AbsID())
	}
}

func TestConstructedReservedWord(t *testing.T) {
	t.Parallel()

	assert.String(t, "WIDTH", d2format.Format(d2ast.FlatUnquotedString("WIDTH")))
	path := &d2ast.KeyPath{Path: []*d2ast.StringBox{
		{DoubleQuotedString: d2ast.FlatDoubleQuotedString("WIDTH")},
		{UnquotedString: d2ast.FlatUnquotedString("LABEL")},
	}}
	assert.String(t, `"WIDTH".label`, d2format.Format(path))
}

func TestKeywordsInNamedDefinitionsAndData(t *testing.T) {
	t.Parallel()
	const source = `VaRs: {
  STYLE: visible
  d2-config: {data: {WIDTH: 37}}
}
content: |md ${STYLE} |
ClAsSeS: {FILL: {STYLE.FILL: red}}
item.CLASS: FILL
item.LINK: layers.WIDTH
LaYeRs: {WIDTH: {item.LABEL: LABEL}; "STEPS"}
`
	m, err := d2parser.Parse("test.d2", strings.NewReader(source), nil)
	assert.Success(t, err)
	formatted := d2format.Format(m)
	for _, piece := range []string{"vars: {", "STYLE: visible", "data: {WIDTH: 37}", "classes: {FILL: {style.fill: red}}", "item.class: FILL", `layers: {WIDTH: {item.label: LABEL}; "STEPS"}`} {
		if !strings.Contains(formatted, piece) {
			t.Fatalf("missing %q in formatted diagram:\n%s", piece, formatted)
		}
	}
	for _, input := range []string{source, formatted} {
		g, config, err := d2compiler.Compile("test.d2", strings.NewReader(input), nil)
		assert.Success(t, err)
		assert.String(t, "visible", g.Root.Children["content"].Label.Value)
		assert.String(t, "red", g.Root.Children["item"].Style.Fill.Value)
		assert.String(t, "root.layers.WIDTH", g.Root.Children["item"].Link.Value)
		assert.Equal(t, 2, len(g.Layers))
		assert.String(t, "STEPS", g.Layers[1].Name)
		assert.String(t, "WIDTH", g.Layers[0].Name)
		assert.String(t, "LABEL", g.Layers[0].Root.Children["item"].Label.Value)
		if _, ok := config.Data["WIDTH"]; !ok {
			t.Fatalf("lost plugin option WIDTH: %v", config.Data)
		}
	}
}
