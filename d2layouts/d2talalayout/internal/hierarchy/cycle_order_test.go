package hierarchy

import (
	"context"
	"math/rand"
	"sort"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
)

// TestFindCycleEdgesUnitsDoNotDependOnMapOrder: the sink and source passes
// visited the remaining nodes in map order while pruning removed nodes, so
// the units charged varied from run to run. The edges found never depended on the order.
func TestFindCycleEdgesUnitsDoNotDependOnMapOrder(t *testing.T) {
	build := func(seed int64) *layoutgraph.Graph {
		rng := rand.New(rand.NewSource(seed))
		g := layoutgraph.NewGraph()
		n := 8 + rng.Intn(24)
		nodes := make([]*layoutgraph.Node, n)
		for i := range nodes {
			nodes[i] = g.AddNode(layoutgraph.NewNode(layoutgraph.EntityID(i+1), 10, 10))
		}
		for e := 0; e < n+rng.Intn(2*n); e++ {
			from, to := nodes[rng.Intn(n)], nodes[rng.Intn(n)]
			if from == to {
				continue
			}
			edge := g.Connect(from, to)
			edge.TargetArrowhead = layoutgraph.TriangleArrowhead
		}
		return g
	}
	edgeKeys := func(edges map[*layoutgraph.Edge]struct{}) [][2]layoutgraph.EntityID {
		var keys [][2]layoutgraph.EntityID
		for edge := range edges {
			keys = append(keys, [2]layoutgraph.EntityID{edge.From.ID, edge.To.ID})
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i][0] != keys[j][0] {
				return keys[i][0] < keys[j][0]
			}
			return keys[i][1] < keys[j][1]
		})
		return keys
	}
	for seed := int64(0); seed < 60; seed++ {
		var wantUnits int64
		var wantEdges [][2]layoutgraph.EntityID
		for run := 0; run < 8; run++ {
			guard, err := limits.NewWorkGuard(context.Background(), "FindCycleEdges", limits.MaxEngineWorkUnits)
			if err != nil {
				t.Fatal(err)
			}
			edges, err := findCycleEdgesGuarded(build(seed), guard)
			if err != nil {
				t.Fatal(err)
			}
			keys := edgeKeys(edges)
			if run == 0 {
				wantUnits, wantEdges = guard.Used(), keys
				continue
			}
			if guard.Used() != wantUnits {
				t.Fatalf("seed %d run %d: %d units, first run %d", seed, run, guard.Used(), wantUnits)
			}
			if len(keys) != len(wantEdges) {
				t.Fatalf("seed %d run %d: %d cycle edges, first run %d", seed, run, len(keys), len(wantEdges))
			}
			for i := range keys {
				if keys[i] != wantEdges[i] {
					t.Fatalf("seed %d run %d: cycle edges differ", seed, run)
				}
			}
		}
	}
}
