package routing

import (
	"context"
	"math/rand"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// randomRoutingGraph places size nodes on a coarse grid without overlaps and
// connects random pairs.
func randomRoutingGraph(random *rand.Rand, size int) *layoutgraph.Graph {
	graph := layoutgraph.NewGraph()
	used := map[[2]int]bool{}
	var nodes []*layoutgraph.Node
	for id := 1; len(nodes) < size; id++ {
		cell := [2]int{random.Intn(6), random.Intn(6)}
		if used[cell] {
			continue
		}
		used[cell] = true
		node := layoutgraph.NewNode(layoutgraph.EntityID(id), float64(8+random.Intn(3)*4), float64(8+random.Intn(3)*4))
		node.TopLeft = geo.NewPoint(float64(cell[0]*40), float64(cell[1]*40))
		graph.AddNode(node)
		nodes = append(nodes, node)
	}
	for range size + random.Intn(size) {
		a, b := nodes[random.Intn(len(nodes))], nodes[random.Intn(len(nodes))]
		if a != b {
			graph.Connect(a, b)
		}
	}
	return graph
}

func routedPoints(t *testing.T, graph *layoutgraph.Graph) [][]geo.Point {
	t.Helper()
	if err := RouteEdges(context.Background(), graph, nil); err != nil {
		t.Fatal(err)
	}
	routes := make([][]geo.Point, len(graph.Edges))
	for i, edge := range graph.Edges {
		for _, point := range edge.Points {
			routes[i] = append(routes[i], *point)
		}
	}
	return routes
}

// Routing calls hand their search scratch to the next call. Routing a set of
// graphs in two different orders (so every graph meets scratch left by
// graphs of other sizes) must give the same routes as routing each graph
// alone first.
func TestRouteSearchScratchReuseKeepsRoutes(t *testing.T) {
	const graphs = 8
	seeds := make([]int64, graphs)
	sizes := make([]int, graphs)
	random := rand.New(rand.NewSource(3))
	for i := range graphs {
		seeds[i] = random.Int63()
		sizes[i] = 2 + random.Intn(12)
	}
	build := func(i int) *layoutgraph.Graph {
		return randomRoutingGraph(rand.New(rand.NewSource(seeds[i])), sizes[i])
	}
	want := make([][][]geo.Point, graphs)
	for i := range graphs {
		want[i] = routedPoints(t, build(i))
	}
	for _, order := range [][]int{{7, 6, 5, 4, 3, 2, 1, 0}, {3, 0, 7, 1, 6, 2, 5, 4, 0, 3}} {
		for _, i := range order {
			got := routedPoints(t, build(i))
			if len(got) != len(want[i]) {
				t.Fatalf("graph %d: %d routes, want %d", i, len(got), len(want[i]))
			}
			for e := range got {
				if len(got[e]) != len(want[i][e]) {
					t.Fatalf("graph %d edge %d: %v, want %v", i, e, got[e], want[i][e])
				}
				for p := range got[e] {
					if got[e][p] != want[i][e][p] {
						t.Fatalf("graph %d edge %d: %v, want %v", i, e, got[e], want[i][e])
					}
				}
			}
		}
	}
}

// A released scratch holds no non-nil state pointers and an empty queue, so
// the next router's carved prefix equals a fresh allocation.
func TestRouteSearchScratchCleanAfterRelease(t *testing.T) {
	random := rand.New(rand.NewSource(11))
	for trial := range 20 {
		graph := randomRoutingGraph(random, 3+random.Intn(10))
		ovg, err := buildOVGFromGraphWithLimits(context.Background(), graph, nil, defaultOVGBuildLimits())
		if err != nil {
			t.Fatal(err)
		}
		router, err := newOVGEdgeRouter(context.Background(), Default, ovg, graph, nil, graph.Edges)
		if err != nil {
			t.Fatal(err)
		}
		for _, edge := range graph.Edges {
			if _, _, err := router.search(context.Background(), edge); err != nil {
				t.Fatal(err)
			}
		}
		scratch := router.scratch
		router.releaseSearchScratch()
		if scratch == nil {
			continue
		}
		for name, entries := range map[string][]*OVGNode{"verticalHops": scratch.verticalHops, "horizontalHops": scratch.horizontalHops} {
			for i, entry := range entries[:cap(entries)] {
				if entry != nil {
					t.Fatalf("trial %d: %s[%d] not cleared", trial, name, i)
				}
			}
		}
		for i, entry := range scratch.nodeContext[:cap(scratch.nodeContext)] {
			if entry != nil {
				t.Fatalf("trial %d: nodeContext[%d] not cleared", trial, i)
			}
		}
		if !scratch.queue.empty() || scratch.queue.entryCursor != 0 || scratch.queue.nextOrder != 0 {
			t.Fatalf("trial %d: queue not reset", trial)
		}
		for _, chunk := range scratch.queue.entryChunks {
			for i := range chunk {
				if chunk[i] != (priorityQueueEntry{}) {
					t.Fatalf("trial %d: queue entry %d not cleared", trial, i)
				}
			}
		}
		if router.nodeContext != nil || router.touchedNodes != nil {
			t.Fatalf("trial %d: router still refers to released scratch", trial)
		}
	}
}
