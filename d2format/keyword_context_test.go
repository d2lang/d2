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
"steps": WIDTH
'scenarios': {'layers': SHAPE}
"layers": visible

layers: {release: {child}}
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
