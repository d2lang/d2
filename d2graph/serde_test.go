package d2graph_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
)

func TestSerialization(t *testing.T) {
	t.Parallel()

	g, _, err := d2compiler.Compile("", strings.NewReader("a.a.b -> a.a.c"), nil)
	assert.Nil(t, err)

	asserts := func(g *d2graph.Graph) {
		a := g.Root.ChildrenArray[0]
		a_a := a.ChildrenArray[0]

		assert.Equal(t, 4, len(g.Objects))
		assert.Equal(t, 1, len(g.Root.ChildrenArray))
		assert.Equal(t, 1, len(a.ChildrenArray))
		assert.Equal(t, 2, len(a_a.ChildrenArray))
		assert.Equal(t, a, a_a.Parent)
		assert.Equal(t, g.Root, a.Parent)

		assert.Contains(t, a.Children, "a")
		assert.Contains(t, a_a.Children, "b")
		assert.Contains(t, a_a.Children, "c")

		assert.Equal(t, 1, len(g.Edges))
		assert.Equal(t, "b", g.Edges[0].Src.ID)
		assert.Equal(t, "c", g.Edges[0].Dst.ID)
	}

	asserts(g)

	b, err := d2graph.SerializeGraph(g)
	assert.Nil(t, err)

	var newG d2graph.Graph
	err = d2graph.DeserializeGraph(b, &newG)
	assert.Nil(t, err)

	asserts(&newG)
}

func TestCasingRegression(t *testing.T) {
	t.Parallel()

	script := `UserCreatedTypeField`

	g, _, err := d2compiler.Compile("", strings.NewReader(script), nil)
	assert.Nil(t, err)

	_, ok := g.Root.HasChild([]string{"UserCreatedTypeField"})
	assert.True(t, ok)

	b, err := d2graph.SerializeGraph(g)
	assert.Nil(t, err)

	var newG d2graph.Graph
	err = d2graph.DeserializeGraph(b, &newG)
	assert.Nil(t, err)

	_, ok = newG.Root.HasChild([]string{"UserCreatedTypeField"})
	assert.True(t, ok)
}

func TestCompareSerializedCompatibility(t *testing.T) {
	t.Parallel()

	g, _, err := d2compiler.Compile("", strings.NewReader("a -> b"), nil)
	assert.NoError(t, err)

	g.IsPartialLayout = true
	b, err := d2graph.SerializeGraph(g)
	assert.NoError(t, err)

	var roundTrip d2graph.Graph
	assert.NoError(t, d2graph.DeserializeGraph(b, &roundTrip))

	assert.True(t, roundTrip.IsPartialLayout)
	assert.NoError(t, d2graph.CompareSerializedGraph(g, &roundTrip))
	assert.NoError(t, d2graph.CompareSerializedObject(g.Root, roundTrip.Root))
	assert.NoError(t, d2graph.CompareSerializedEdge(g.Edges[0], roundTrip.Edges[0]))
}

func TestWholeLayoutRecognizesLegacyNestedGraphs(t *testing.T) {
	g := d2graph.NewGraph()
	assert.True(t, g.IsWholeLayout())
	g.RootLevel = 1
	assert.False(t, g.IsWholeLayout())

	// An older core recorded the nesting level without an explicit partial flag.
	const nested = `{"root":{"AbsID":"","attributes":{}},"edges":[],"objects":[],"rootLevel":1}`
	var restored d2graph.Graph
	if err := d2graph.DeserializeGraph([]byte(nested), &restored); err != nil {
		t.Fatal(err)
	}
	assert.False(t, restored.IsWholeLayout())
	restored.RootLevel = 0
	restored.IsPartialLayout = true
	assert.False(t, restored.IsWholeLayout())
}
