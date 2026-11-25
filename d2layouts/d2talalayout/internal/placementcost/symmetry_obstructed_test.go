package placementcost

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// randomObstructionScene builds a graph with containers, nested children,
// unpositioned nodes and odd geometry, plus a foreign node of another graph.
func randomObstructionScene(rng *rand.Rand) (g *layoutgraph.Graph, nodes []*layoutgraph.Node, foreign *layoutgraph.Node) {
	g = layoutgraph.NewGraph()
	id := layoutgraph.EntityID(1)
	coord := func() float64 {
		switch rng.Intn(40) {
		case 0:
			return math.NaN()
		case 1:
			return math.Inf(1 - 2*rng.Intn(2))
		case 2:
			return math.MaxFloat64 / 2
		}
		return float64(rng.Intn(60)) * 10
	}
	size := func() float64 {
		switch rng.Intn(40) {
		case 0:
			return -float64(rng.Intn(5)) * 10
		case 1:
			return math.NaN()
		}
		return float64(1+rng.Intn(8)) * 10
	}
	count := 6 + rng.Intn(30)
	for i := 0; i < count; i++ {
		n := layoutgraph.NewNode(id, size(), size())
		id++
		if rng.Intn(12) != 0 {
			n.TopLeft = geo.NewPoint(coord(), coord())
		}
		var parent *layoutgraph.Node
		if len(nodes) > 0 && rng.Intn(4) == 0 {
			parent = nodes[rng.Intn(len(nodes))]
			if !parent.IsContainer() {
				parent.SetContainer(true)
			}
		}
		g.AddNewNodeToContainer(parent, n)
		nodes = append(nodes, n)
	}
	other := layoutgraph.NewGraph()
	foreign = layoutgraph.NewNode(id, 30, 30)
	foreign.TopLeft = geo.NewPoint(coord(), coord())
	other.AddNewNodeToContainer(nil, foreign)
	return g, nodes, foreign
}

func TestObstructedMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	positioned := func(nodes []*layoutgraph.Node) *layoutgraph.Node {
		for range 100 {
			if n := nodes[rng.Intn(len(nodes))]; n.TopLeft != nil {
				return n
			}
		}
		return nil
	}
	compared, obstructedCount := 0, 0
	for trial := 0; trial < 3000; trial++ {
		g, nodes, foreign := randomObstructionScene(rng)
		center, a, b := positioned(nodes), positioned(nodes), positioned(nodes)
		if center == nil || a == nil || b == nil {
			continue
		}
		pick := func() []*layoutgraph.Node {
			var sibs []*layoutgraph.Node
			if rng.Intn(3) == 0 {
				sibs = append(sibs, g.Containers[nil]...)
			} else {
				for range rng.Intn(90) {
					switch rng.Intn(25) {
					case 0:
						sibs = append(sibs, nil)
					case 1:
						sibs = append(sibs, foreign)
					case 2:
						sibs = append(sibs, center)
					default:
						sibs = append(sibs, nodes[rng.Intn(len(nodes))])
					}
				}
			}
			return sibs
		}
		sib1, sib2 := pick(), pick()
		want, wantErr := refObstructed(context.Background(), center, a, b, sib1, sib2)
		p := newScoringPoll(context.Background())
		got, gotErr := obstructed(&p, center, a, b, sib1, sib2)
		if got != want || (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("trial %d: obstructed = %v, %v; reference %v, %v", trial, got, gotErr, want, wantErr)
		}
		compared++
		if want {
			obstructedCount++
		}

		// Done-less contexts observe every polling point: cancel after k
		// Err calls and compare where both stop.
		if len(sib1)+len(sib2) > 64 {
			for _, k := range []int{0, 1, 2, rng.Intn(4)} {
				wantCtx := &cancelAfterErrChecks{Context: context.Background(), remaining: k}
				gotCtx := &cancelAfterErrChecks{Context: context.Background(), remaining: k}
				want, wantErr := refObstructed(wantCtx, center, a, b, sib1, sib2)
				p := newScoringPoll(gotCtx)
				got, gotErr := obstructed(&p, center, a, b, sib1, sib2)
				if got != want || !errors.Is(gotErr, context.Canceled) != !errors.Is(wantErr, context.Canceled) || gotCtx.remaining != wantCtx.remaining {
					t.Fatalf("trial %d cancel after %d: got %v, %v (left %d); reference %v, %v (left %d)", trial, k, got, gotErr, gotCtx.remaining, want, wantErr, wantCtx.remaining)
				}
			}
		}
	}
	if compared < 1000 || obstructedCount < 50 || compared-obstructedCount < 50 {
		t.Fatalf("weak coverage: %d compared, %d obstructed", compared, obstructedCount)
	}
}
