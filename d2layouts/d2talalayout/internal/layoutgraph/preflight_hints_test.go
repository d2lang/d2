package layoutgraph

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
)

// validateEngineGraph (inventory through preflightSlot hints) answers exactly
// as validateSharedEngineGraph (inventory in maps) on random graphs, on
// malformed ones (parent cycles, nil references, repeated g.Nodes entries,
// nodes and edges outside the graph, too-deep chains), and with stale hints
// left by validating other graphs that share nodes and edges.
func TestValidateEngineGraphHintsMatchMaps(t *testing.T) {
	ctx := context.Background()
	failures := 0
	for seed := int64(1); seed <= 3000; seed++ {
		r := rand.New(rand.NewSource(seed))
		g, nodes := prefilterGraph(r)
		// A second graph sharing some nodes and edges (as subgraphs do).
		sub := NewGraph()
		for _, n := range g.Nodes {
			if r.Intn(2) == 0 {
				sub.Nodes = append(sub.Nodes, n)
			}
		}
		for _, e := range g.Edges {
			if r.Intn(2) == 0 {
				sub.Edges = append(sub.Edges, e)
			}
		}
		switch r.Intn(8) {
		case 0:
			// A parent cycle.
			a, b := nodes[r.Intn(len(nodes))], nodes[r.Intn(len(nodes))]
			a.Container = b
			b.Container = a
		case 1:
			g.Nodes = append(g.Nodes, g.Nodes[r.Intn(len(g.Nodes))])
		case 2:
			if len(g.Edges) > 0 {
				e := g.Edges[r.Intn(len(g.Edges))]
				e.To = NewNode(9000, 10, 10)
			}
		case 3:
			n := nodes[r.Intn(len(nodes))]
			n.Edges = append(n.Edges, nil)
		case 4:
			// A deep chain of containers outside g.Nodes.
			current := nodes[r.Intn(len(nodes))]
			for d := 0; d < 3+r.Intn(400); d++ {
				parent := NewNode(EntityID(10000+d), 10, 10)
				current.Container = parent
				current = parent
			}
		case 5:
			g.Nodes = append(g.Nodes, nil)
		}
		order := []*Graph{g, sub, g, sub}
		if r.Intn(2) == 0 {
			order = []*Graph{sub, g, sub, g}
		}
		for _, graph := range order {
			hinted := fmt.Sprint(validateEngineGraph(ctx, "test", graph))
			mapped := fmt.Sprint(validateSharedEngineGraph(ctx, "test", graph))
			if hinted != mapped {
				t.Fatalf("seed %d: hints %q, maps %q", seed, hinted, mapped)
			}
			if hinted != "<nil>" {
				failures++
			}
		}
	}
	if failures == 0 {
		t.Fatal("no malformed graph was rejected")
	}
	t.Logf("%d rejected validations", failures)
}
