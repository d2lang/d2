package placement

import (
	"math/rand"
	"sort"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// TestNearestSwapCandidatesMatchesSort compares nearestSwapCandidates with a
// direct definition (eligible siblings, stable sort by squared centre
// distance, the first k, back to sibling order) on random sibling lists with
// many equal distances, unplaced, fixed, hierarchy and tree members.
func TestNearestSwapCandidatesMatchesSort(t *testing.T) {
	random := rand.New(rand.NewSource(11))
	var out []*layoutgraph.Node
	for trial := 0; trial < 3000; trial++ {
		g := layoutgraph.NewGraph()
		g.NodeToTree = map[*layoutgraph.Node]*layoutgraph.Tree{}
		n := 1 + random.Intn(14)
		siblings := make([]*layoutgraph.Node, n)
		for i := range siblings {
			node := layoutgraph.NewNode(layoutgraph.EntityID(i+1), float64(10*(1+random.Intn(3))), float64(10*(1+random.Intn(3))))
			if random.Intn(8) != 0 {
				node.TopLeft = geo.NewPoint(float64(10*random.Intn(6)), float64(10*random.Intn(6)))
			}
			switch random.Intn(12) {
			case 0:
				node.FixedTopLeft = geo.NewPoint(0, 0)
			case 1:
				node.Hierarchy = &layoutgraph.Hierarchy{}
			case 2:
				g.NodeToTree[node] = &layoutgraph.Tree{}
			}
			siblings[i] = node
		}
		node := siblings[random.Intn(n)]
		if random.Intn(10) == 0 {
			node = layoutgraph.NewNode(99, 10, 10)
			node.TopLeft = geo.NewPoint(15, 25)
		}
		k := 1 + random.Intn(swapNeighbourhood)
		out = nearestSwapCandidates(g, node, siblings, k, out)

		type candidate struct {
			index    int
			distance float64
		}
		var want []candidate
		if node.TopLeft != nil {
			cx, cy := node.TopLeft.X+node.Width/2, node.TopLeft.Y+node.Height/2
			for i, s := range siblings {
				if s == node || s.TopLeft == nil || s.Hierarchy != nil || s.FixedTopLeft != nil {
					continue
				}
				if _, has := g.NodeToTree[s]; has {
					continue
				}
				dx, dy := s.TopLeft.X+s.Width/2-cx, s.TopLeft.Y+s.Height/2-cy
				want = append(want, candidate{i, dx*dx + dy*dy})
			}
		}
		sort.SliceStable(want, func(a, b int) bool { return want[a].distance < want[b].distance })
		if len(want) > k {
			want = want[:k]
		}
		sort.Slice(want, func(a, b int) bool { return want[a].index < want[b].index })
		if len(out) != len(want) {
			t.Fatalf("trial %d: got %d candidates, want %d", trial, len(out), len(want))
		}
		for i := range want {
			if out[i] != siblings[want[i].index] {
				t.Fatalf("trial %d: candidate %d is node %d, want %d", trial, i, out[i].ID, siblings[want[i].index].ID)
			}
		}
	}
}
