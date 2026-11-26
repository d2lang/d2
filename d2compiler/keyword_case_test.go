package d2compiler_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/util-go/assert"
)

func TestReservedKeyCaseBeforeAndAfterFormat(t *testing.T) {
	t.Parallel()

	const source = `VaRs: {caption: WIDTH}
ClAsSeS: {Panel: {sHaPe: circle}}
ServiceAPI: {ClAsS: Panel; LaBeL: ${caption}; StYlE: {StRoKe: blue}}
"STYLE": LAYERS
ServiceAPI -> "STYLE": LINK
(ServiceAPI -> "STYLE")[0].TaRgEt-ArRoWhEaD: {ShApE: diamond}
selected: @STYLE."LABEL"
*: {&LaBeL: FromUpper; tooltip: FILL}
"layers": {Launch: {Decoy}}
LaYeRs: {Launch: {Worker: {LaBeL: SHAPE}}}
`
	files := fstest.MapFS{
		"STYLE.d2": {Data: []byte(`"LABEL": FromUpper; label: wrong selection`)},
		"style.d2": {Data: []byte(`"LABEL": FromLower`)},
	}
	ast, err := d2parser.Parse("test.d2", strings.NewReader(source), nil)
	assert.Success(t, err)
	formatted := d2format.Format(ast)
	for _, tc := range []struct{ name, input, firstKey string }{
		{"original", source, "VaRs"},
		{"formatted", formatted, "vars"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, _, err := d2compiler.Compile("test.d2", strings.NewReader(tc.input), &d2compiler.CompileOptions{FS: files})
			assert.Success(t, err)
			assert.String(t, tc.firstKey, g.AST.Nodes[0].MapKey.Key.Path[0].ScalarString())
			assert.Equal(t, 6, len(g.Objects))
			assert.Equal(t, 1, len(g.Edges))
			edge := g.Edges[0]
			assert.String(t, "ServiceAPI", edge.Src.ID)
			assert.String(t, "WIDTH", edge.Src.Label.Value)
			assert.String(t, "circle", edge.Src.Shape.Value)
			assert.True(t, edge.Src.Style.Stroke != nil)
			assert.String(t, "blue", edge.Src.Style.Stroke.Value)
			assert.String(t, "LAYERS", edge.Dst.Label.Value)
			assert.String(t, "LINK", edge.Label.Value)
			assert.True(t, edge.DstArrowhead != nil)
			assert.String(t, "diamond", edge.DstArrowhead.Shape.Value)
			selected := g.Root.Children["selected"]
			assert.True(t, selected != nil)
			assert.String(t, "FromUpper", selected.Label.Value)
			assert.True(t, selected.Tooltip != nil)
			assert.String(t, "FILL", selected.Tooltip.Value)

			assert.Equal(t, 1, len(g.Layers))
			layer := g.Layers[0]
			assert.String(t, "Launch", layer.Name)
			assert.Equal(t, 1, len(layer.Objects))
			assert.String(t, "Worker", layer.Objects[0].ID)
			assert.String(t, "SHAPE", layer.Objects[0].Label.Value)
			assert.True(t, layer.BaseAST != nil)
			assert.Equal(t, 1, len(layer.BaseAST.Nodes))
			assert.String(t, "Worker", layer.BaseAST.Nodes[0].MapKey.Key.Path[0].ScalarString())
		})
	}
}

func TestUnquotedReservedEdgeEndpoint(t *testing.T) {
	t.Parallel()

	const source = "worker -> StYlE"
	ast, err := d2parser.Parse("test.d2", strings.NewReader(source), nil)
	assert.Success(t, err)
	for _, input := range []string{source, d2format.Format(ast)} {
		_, _, err := d2compiler.Compile("test.d2", strings.NewReader(input), nil)
		if err == nil || !strings.Contains(err.Error(), "reserved keywords are prohibited in edges") {
			t.Fatalf("expected a reserved endpoint diagnostic for %q, got %v", input, err)
		}
	}
}

func TestReservedGlobFilterRequiresScalar(t *testing.T) {
	t.Parallel()

	const source = "Worker\n*: {&LaBeL: [one; two]; style.fill: red}"
	ast, err := d2parser.Parse("filter.d2", strings.NewReader(source), nil)
	assert.Success(t, err)
	for _, input := range []string{source, d2format.Format(ast)} {
		_, _, err := d2compiler.Compile("filter.d2", strings.NewReader(input), nil)
		if err == nil || !strings.Contains(err.Error(), "glob filters cannot be composites") {
			t.Fatalf("expected a scalar filter diagnostic for %q, got %v", input, err)
		}
	}
}

func TestQuotedReservedObjectID(t *testing.T) {
	t.Parallel()

	const source = `Worker -> "WIDTH"`
	g, _, err := d2compiler.Compile("quoted.d2", strings.NewReader(source), nil)
	assert.Success(t, err)
	assert.Equal(t, 2, len(g.Objects))
	assert.Equal(t, 1, len(g.Edges))
	edge := g.Edges[0]
	assert.String(t, "Worker", edge.Src.ID)
	assert.String(t, "WIDTH", edge.Dst.ID)
	assert.String(t, "WIDTH", edge.Dst.IDVal)
	assert.String(t, "WIDTH", edge.Dst.Label.Value)

	child, ok := g.Root.HasChild(edge.Dst.AbsIDArray())
	assert.True(t, ok && child == edge.Dst)
	key, err := d2parser.ParseMapKey(`(Worker -> "WIDTH")[0]`)
	assert.Success(t, err)
	edges, ok := g.Root.FindEdges(key)
	assert.True(t, ok)
	assert.Equal(t, 1, len(edges))
	assert.True(t, edges[0] == edge)
}
