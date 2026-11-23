package hierarchy

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

func TestAlignRankChainAcrossOccupiedLevel(t *testing.T) {
	for _, tc := range []struct {
		name      string
		obstacleX float64
		align     bool
	}{
		{"clear", 500, true},
		{"occupied", 65, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := layoutgraph.NewGraph()
			node := func(id int64, x, y float64) *layoutgraph.Node {
				n := layoutgraph.NewNode(id, 40, 40)
				n.TopLeft = geo.NewPoint(x, y)
				g.AddNewNodeToContainer(nil, n)
				return n
			}
			a := node(1, 0, 0)
			obstacle := node(2, tc.obstacleX, 150)
			b := node(3, 130, 300)
			g.Connect(a, b)
			levels := map[*layoutgraph.Node]int{a: 0, obstacle: 1, b: 2}
			guard, err := limits.NewWorkGuard(t.Context(), "TestAlignRankChain", limits.MaxEngineWorkUnits)
			if err != nil {
				t.Fatal(err)
			}
			if err := alignRankChains(g.Nodes, levels, false, siblingSpacing, guard); err != nil {
				t.Fatal(err)
			}
			if got := a.TopLeft.X == b.TopLeft.X; got != tc.align {
				t.Errorf("alignment = %t, want %t: %v %v", got, tc.align, a.TopLeft, b.TopLeft)
			}
			if !tc.align && (a.TopLeft.X != 0 || b.TopLeft.X != 130) {
				t.Error("occupied corridor moved the seed positions")
			}
		})
	}
}

func TestFlatPathOrder(t *testing.T) {
	for _, tc := range []struct {
		name              string
		edges             [][2]int
		initial, expected []int
		pin               int
	}{
		{"path", [][2]int{{0, 1}, {1, 2}, {2, 3}}, []int{0, 2, 1, 3}, []int{0, 1, 2, 3}, -1},
		{"reversed", [][2]int{{0, 1}, {1, 2}}, []int{2, 0, 1}, []int{2, 1, 0}, -1},
		{"cycle", [][2]int{{0, 1}, {1, 2}, {2, 0}}, []int{0, 2, 1}, []int{0, 2, 1}, -1},
		{"branch", [][2]int{{0, 1}, {1, 2}, {1, 3}}, []int{0, 2, 1, 3}, []int{0, 2, 1, 3}, -1},
		{"interleaved", [][2]int{{0, 1}, {1, 2}}, []int{0, 3, 2, 1}, []int{0, 3, 2, 1}, -1},
		{"pinned", [][2]int{{0, 1}, {1, 2}}, []int{0, 2, 1}, []int{0, 2, 1}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ordered := make([]*layoutgraph.Node, len(tc.initial))
			nodes := make([]*layoutgraph.Node, len(ordered))
			for i := range nodes {
				nodes[i] = layoutgraph.NewNode(int64(i), 40, 40)
			}
			if tc.pin != -1 {
				nodes[tc.pin].FixedTopLeft = geo.NewPoint(75, 0)
			}
			for i, id := range tc.initial {
				ordered[i] = nodes[id]
			}
			paths := make(flatRankPaths)
			for _, edge := range tc.edges {
				a, b := nodes[edge[0]], nodes[edge[1]]
				paths.link(a, b, 0)
				paths.link(b, a, 0)
			}
			paths.order(ordered)
			for i, n := range ordered {
				if n != nodes[tc.expected[i]] {
					t.Errorf("slot %d has node %d, want %d", i, n.ID, tc.expected[i])
				}
			}
		})
	}
}

func TestAlignRankChainsWideLayer(t *testing.T) {
	// An offset of two pixels per lane should be resolved locally. A full
	// board scan per node would exceed this work budget even at 256 lanes.
	const lanes, depth = 256, 2
	g := layoutgraph.NewGraph()
	levels := make(map[*layoutgraph.Node]int, lanes*depth)
	for lane := range lanes {
		var prev *layoutgraph.Node
		for row := range depth {
			n := layoutgraph.NewNode(int64(lane*depth+row), 20, 20)
			n.TopLeft = geo.NewPoint(float64(110*lane+2*(row%2)), float64(100*row))
			g.AddNewNodeToContainer(nil, n)
			levels[n] = row
			if prev != nil {
				g.Connect(prev, n)
			}
			prev = n
		}
	}
	guard, err := limits.NewWorkGuard(t.Context(), "TestWideLanes", 66*lanes*depth)
	if err != nil {
		t.Fatal(err)
	}
	if err := alignRankChains(g.Nodes, levels, false, siblingSpacing, guard); err != nil {
		t.Fatal(err)
	}
	for lane := range lanes {
		for row := 1; row < depth; row++ {
			if g.Nodes[lane*depth].TopLeft.X != g.Nodes[lane*depth+row].TopLeft.X {
				t.Fatalf("lane %d misaligned at row %d", lane, row)
			}
		}
	}
	t.Logf("%d nodes: %d charged units", len(g.Nodes), guard.Used())
}
