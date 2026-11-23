package d2compiler_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
)

func TestSameRankGroups(t *testing.T) {
	const source = `
vars: {peer_group: "service peers"}
classes.aligned.rank: ${peer_group}
a.class: aligned
a."rank".rank: Payments
box.b.class: aligned
c.class: aligned
c.rank: Payments
d.rank: payments
"rank".child -> a."rank"
a."rank" -> "rank"
scenarios.variant: {
	a."rank".rank: null
	a.rank: Payments
}
layers.other: {
	x.rank: Payments
}
`
	g, _, err := d2compiler.Compile("rank.d2", strings.NewReader(source), nil)
	require.NoError(t, err)

	groupIDs := func(g *d2graph.Graph) [][]string {
		var ids [][]string
		for _, group := range g.SameRankGroups() {
			var members []string
			for _, obj := range group {
				members = append(members, obj.AbsID())
			}
			ids = append(ids, members)
		}
		return ids
	}
	want := [][]string{{"a", "box.b"}, {"a.rank", "c"}}
	require.Equal(t, want, groupIDs(g))
	require.Equal(t, "rank.child", g.Edges[0].Src.AbsID())
	require.Equal(t, "a.rank", g.Edges[0].Dst.AbsID())
	require.Equal(t, "rank", g.Edges[1].Dst.AbsID())
	require.Equal(t, [][]string{{"a", "c"}}, groupIDs(g.Scenarios[0]))
	require.Empty(t, groupIDs(g.Layers[0]))

	data, err := d2graph.SerializeGraph(g)
	require.NoError(t, err)
	var replica d2graph.Graph
	require.NoError(t, d2graph.DeserializeGraph(data, &replica))
	require.Equal(t, want, groupIDs(&replica))
}

func TestRankOverride(t *testing.T) {
	const source = `
classes.aligned.rank: from class
a: {class: aligned; rank: explicit; "rank"}
b: {"rank"; rank: explicit}
scenarios.reset: {
	a.rank: null
	b.rank: null
}
scenarios.remove_objects: {
	a."rank": null
	b."rank": suspend
}
`
	g, _, err := d2compiler.Compile("rank.d2", strings.NewReader(source), nil)
	require.NoError(t, err)
	reset := g.Scenarios[0]
	a, b := reset.Root.Children["a"], reset.Root.Children["b"]
	require.Equal(t, "from class", a.Rank.Value)
	require.Nil(t, b.Rank)
	require.Len(t, a.Children, 1)
	require.Len(t, b.Children, 1)
	removed := g.Scenarios[1]
	for _, obj := range removed.Objects {
		require.Empty(t, obj.Children)
		require.Equal(t, "explicit", obj.Rank.Value)
	}
}

func TestInvalidRank(t *testing.T) {
	for _, tc := range []struct{ source, message string }{
		{"rank: group", "rank can only be set on shapes"},
		{"a -> b: {rank: group}", "rank can only be set on shapes"},
		{"a -> b: {target-arrowhead.rank: group}", "rank can only be set on shapes"},
		{"classes.example.rank: group\na -> b: {class: example}", "rank can only be set on shapes"},
		{"a.rank", "rank expects a non-empty group name"},
		{`a.rank: "  "`, "rank expects a non-empty group name"},
		{"a.rank: [alpha; beta]", "rank expects a non-empty group name"},
		{"a.rank: {b}", "rank expects a non-empty group name"},
		{`a."rank".shape: image`, `image shape must include an "icon" field`},
		{"a: {shape: sql_table; id.rank: group}", "rank can only be set on a whole sql_table shape"},
		{"a: {shape: class; method().rank: group}", "rank can only be set on a whole class shape"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			_, _, err := d2compiler.Compile("rank.d2", strings.NewReader(tc.source), nil)
			require.ErrorContains(t, err, tc.message)
		})
	}
}
