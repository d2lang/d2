package layoutguard

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
)

func TestCheckGraphPreservesSparseGraphs(t *testing.T) {
	t.Parallel()
	g := testGraph(maxLayoutObjects)
	for i := 1; i < len(g.Objects); i++ {
		g.Edges = append(g.Edges, &d2graph.Edge{Src: g.Objects[0], Dst: g.Objects[i]})
	}
	if err := CheckGraph(context.Background(), "test", g); err != nil {
		t.Fatal(err)
	}
}

func TestCheckGraphRawObjectBoundary(t *testing.T) {
	t.Parallel()
	if err := CheckGraph(context.Background(), "test", testGraph(maxLayoutObjects)); err != nil {
		t.Fatalf("object boundary: %v", err)
	}
	if err := CheckGraph(context.Background(), "test", testGraph(maxLayoutObjects+1)); err == nil || !strings.Contains(err.Error(), "object count 1025 exceeds safe limit of 1024") {
		t.Fatalf("over-limit object error = %v", err)
	}
}

func TestCheckGraphRawEdgeBoundary(t *testing.T) {
	t.Parallel()
	g := testGraph(512)
	for i := 0; i < maxLayoutEdges; i++ {
		g.Edges = append(g.Edges, &d2graph.Edge{
			Src: g.Objects[i%len(g.Objects)],
			Dst: g.Objects[(i+1)%len(g.Objects)],
		})
	}
	if err := CheckGraph(context.Background(), "test", g); err != nil {
		t.Fatalf("edge boundary: %v", err)
	}
	g.Edges = append(g.Edges, &d2graph.Edge{Src: g.Objects[0], Dst: g.Objects[1]})
	if err := CheckGraph(context.Background(), "test", g); err == nil || !strings.Contains(err.Error(), "edge count 1025 exceeds safe limit of 1024") {
		t.Fatalf("over-limit edge error = %v", err)
	}
}

func TestCheckGraphRejectsDenseSubgraphs(t *testing.T) {
	t.Parallel()
	g := completeTestGraph(20)
	// Unrelated objects must not dilute the dense component's work estimate.
	for i := 0; i < 1_000; i++ {
		g.Objects = append(g.Objects, &d2graph.Object{})
	}
	err := CheckGraph(context.Background(), "test", g)
	if err == nil || !strings.Contains(err.Error(), "edge interaction work exceeds safe limit of 8192") {
		t.Fatalf("CheckGraph error = %v, want interaction-work limit", err)
	}
}

func TestCheckGraphDenseBoundary(t *testing.T) {
	t.Parallel()
	if err := CheckGraph(context.Background(), "test", completeTestGraph(16)); err != nil {
		t.Fatalf("16-object complete graph: %v", err)
	}
	if err := CheckGraph(context.Background(), "test", completeTestGraph(17)); err == nil {
		t.Fatal("17-object complete graph was not rejected")
	}
}

func TestCheckGraphPreservesSmallParallelGraphs(t *testing.T) {
	t.Parallel()
	g := testGraph(2)
	for i := 0; i < unconditionallyAllowedEdges; i++ {
		g.Edges = append(g.Edges, &d2graph.Edge{Src: g.Objects[0], Dst: g.Objects[1]})
	}
	if err := CheckGraph(context.Background(), "test", g); err != nil {
		t.Fatal(err)
	}
}

func TestCheckGraphRejectsMalformedSmallGraph(t *testing.T) {
	t.Parallel()

	tests := map[string]*d2graph.Edge{
		"nil edge":      nil,
		"nil source":    {Dst: &d2graph.Object{}},
		"nil target":    {Src: &d2graph.Object{}},
		"nil endpoints": {},
	}
	for name, edge := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := d2graph.NewGraph()
			g.Edges = append(g.Edges, edge)
			if err := CheckGraph(context.Background(), "test", g); err == nil || !strings.Contains(err.Error(), "edge without endpoints") {
				t.Fatalf("CheckGraph error = %v, want missing-endpoint error", err)
			}
		})
	}
}

func TestCheckGraphObservesCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CheckGraph(ctx, "test", &d2graph.Graph{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("CheckGraph error = %v, want context.Canceled", err)
	}
}

func completeTestGraph(objectCount int) *d2graph.Graph {
	g := testGraph(objectCount)
	for src := range g.Objects {
		for dst := range g.Objects {
			if src == dst {
				continue
			}
			g.Edges = append(g.Edges, &d2graph.Edge{Src: g.Objects[src], Dst: g.Objects[dst]})
		}
	}
	return g
}

func testGraph(objectCount int) *d2graph.Graph {
	g := d2graph.NewGraph()
	for i := 0; i < objectCount; i++ {
		g.Objects = append(g.Objects, &d2graph.Object{Graph: g, Parent: g.Root})
	}
	return g
}
