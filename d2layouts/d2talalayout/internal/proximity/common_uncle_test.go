package proximity

import (
	"slices"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

func TestCommonUncleTieAcrossContainers(t *testing.T) {
	g := layoutgraph.NewGraph()
	add := func(parent *layoutgraph.Node) *layoutgraph.Node {
		n := layoutgraph.NewNode(int64(len(g.Nodes)+1), 80, 60)
		g.AddNewNodeToContainer(parent, n)
		return n
	}
	// Containers are visited in reverse sibling order. Discover a first,
	// then visit a container whose children encounter b before a.
	late, early := add(nil), add(nil)
	a, b := add(nil), add(nil)
	g.Connect(add(early), a)
	x, y, z := add(late), add(late), add(late)
	g.Connect(x, b)
	g.Connect(x, a)
	g.Connect(y, b)
	g.Connect(z, a)
	got := CommonUncleSiblings(g)
	if len(got) != 3 || !slices.Equal(got[x], layoutgraph.Nodes{x, z}) || !slices.Equal(got[y], layoutgraph.Nodes{x, y}) || !slices.Equal(got[z], layoutgraph.Nodes{x, z}) {
		t.Fatal("equal-size shared-neighbor groups lost their original discovery order")
	}
}
