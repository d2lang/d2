package d2format_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/util-go/assert"
)

func TestBoardDeclarations(t *testing.T) {
	for _, kind := range []string{"layers", "scenarios", "steps"} {
		t.Run(kind, func(t *testing.T) {
			files := fstest.MapFS{"lib.d2": {Data: []byte("first: {item}")}}
			source := kind + ": @lib\n" + kind + ".second: @lib.first"
			checkBoardRoundTrip(t, source, files, func(g *d2graph.Graph) {
				boards := g.Layers
				switch kind {
				case "scenarios":
					boards = g.Scenarios
				case "steps":
					boards = g.Steps
				}
				assert.Equal(t, 2, len(boards))
				for i, name := range []string{"first", "second"} {
					assert.String(t, name, boards[i].Name)
					assert.Equal(t, 1, len(boards[i].Objects))
					assert.String(t, "item", boards[i].Objects[0].AbsID())
				}
			})
		})
	}

	t.Run("substitution", func(t *testing.T) {
		checkBoardRoundTrip(t, "vars: {set: {first: {item}}}\nlayers: ${set}", nil, func(g *d2graph.Graph) {
			assert.Equal(t, 1, len(g.Layers))
			assert.String(t, "item", g.Layers[0].Objects[0].AbsID())
		})
	})
	t.Run("mixed-declaration-order", func(t *testing.T) {
		source := "layers.first: {}\nlayers: {second: {}}\nlayers.third: {}"
		checkBoardRoundTrip(t, source, nil, func(g *d2graph.Graph) {
			assert.Equal(t, 3, len(g.Layers))
			for i, name := range []string{"first", "second", "third"} {
				assert.String(t, name, g.Layers[i].Name)
			}
		})
	})
	t.Run("null", func(t *testing.T) {
		checkBoardRoundTrip(t, "layers: {base: {item}}\nlayers: null", nil, func(g *d2graph.Graph) {
			assert.Equal(t, 0, len(g.Layers))
		})
	})
	for _, kind := range []string{"steps", "scenarios"} {
		t.Run(kind+"-inheritance", func(t *testing.T) {
			checkBoardRoundTrip(t, "a\n"+kind+": {first: {}; bare}\nb", nil, func(g *d2graph.Graph) {
				assert.Equal(t, 2, len(g.Objects))
				first := g.GetBoard("first")
				bare := g.GetBoard("bare")
				if first == nil || bare == nil {
					t.Fatal("missing a named board")
				}
				assert.Equal(t, 1, len(first.Objects))
				assert.String(t, "a", first.Objects[0].AbsID())
				assert.Equal(t, 0, len(bare.Objects))
			})
		})
	}
}

func TestExplicitEmptyMapValues(t *testing.T) {
	checkBoardRoundTrip(t, "vars: {config: {}}\ncopy: ${config}\na.class: {}\na -> b: before\n(a -> b)[0].label: {}", nil, func(g *d2graph.Graph) {
		assert.Equal(t, 3, len(g.Objects))
		assert.Equal(t, 1, len(g.Edges))
		assert.String(t, "before", g.Edges[0].Label.Value)
	})
	// Even an ordinary empty object can be used as a map by another file.
	for _, lib := range []string{"item: {}", formatBoardSource(t, "item: {}")} {
		fs := fstest.MapFS{"lib.d2": {Data: []byte(lib)}}
		g, _, err := d2compiler.Compile("main.d2", strings.NewReader("...@lib.item"), &d2compiler.CompileOptions{FS: fs})
		assert.Success(t, err)
		assert.Equal(t, 0, len(g.Objects))
	}
}

func checkBoardRoundTrip(t *testing.T, source string, fs fstest.MapFS, check func(*d2graph.Graph)) {
	t.Helper()
	formatted := formatBoardSource(t, source)
	assert.String(t, formatted, formatBoardSource(t, formatted))
	for _, input := range []string{source, formatted} {
		g, _, err := d2compiler.Compile("main.d2", strings.NewReader(input), &d2compiler.CompileOptions{FS: fs})
		assert.Success(t, err)
		check(g)
	}
}

func formatBoardSource(t *testing.T, source string) string {
	t.Helper()
	m, err := d2parser.Parse("main.d2", strings.NewReader(source), nil)
	assert.Success(t, err)
	return d2format.Format(m)
}
