package placementcost

import (
	"context"
	"errors"
	"math"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func TestEdgeLengthHelpers(t *testing.T) {
	tests := []struct {
		name                           string
		nodes, edges, gomaxprocs, want int
	}{
		{name: "original sequential size", nodes: 10, edges: 500, gomaxprocs: 8, want: 0},
		{name: "single processor", nodes: 300, edges: 300, gomaxprocs: 1, want: 0},
		{name: "little work", nodes: 60, edges: 60, gomaxprocs: 8, want: 0},
		{name: "threshold", nodes: 64, edges: 64, gomaxprocs: 8, want: 1},
		{name: "work bounds helpers", nodes: 200, edges: 200, gomaxprocs: 16, want: 4},
		{name: "processors bound helpers", nodes: 2000, edges: 2000, gomaxprocs: 4, want: 3},
		{name: "chunks bound helpers", nodes: 20, edges: 900, gomaxprocs: 16, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := edgeLengthHelpers(tt.nodes, tt.edges, tt.gomaxprocs); got != tt.want {
				t.Fatalf("edgeLengthHelpers(%d, %d, %d) = %d; want %d", tt.nodes, tt.edges, tt.gomaxprocs, got, tt.want)
			}
		})
	}
}

// newLargeEdgeLengthGraph is large enough for EdgeLength to use helpers. It
// mixes a chain, chords, labels and one container so every term is non-zero.
func newLargeEdgeLengthGraph() *layoutgraph.Graph {
	g := layoutgraph.NewGraph()
	container := layoutgraph.NewNode(1000, 400, 300)
	container.TopLeft = geo.NewPoint(-600, -500)
	container.SetContainer(true)
	g.AddNewNodeToContainer(nil, container)
	nodes := make([]*layoutgraph.Node, 150)
	for i := range nodes {
		nodes[i] = layoutgraph.NewNode(layoutgraph.EntityID(i+1), 20+float64(i%7), 10+float64(i%3))
		nodes[i].TopLeft = geo.NewPoint(float64((i%15)*60), float64((i/15)*45+(i%4)*3))
		parent := (*layoutgraph.Node)(nil)
		if i%25 == 3 {
			parent = container
			nodes[i].TopLeft = geo.NewPoint(-580+float64(i%5)*40, -480+float64(i%3)*30)
		}
		g.AddNewNodeToContainer(parent, nodes[i])
		if i > 0 {
			e := g.Connect(nodes[i-1], nodes[i])
			if i%9 == 0 {
				e.Label = &layoutgraph.Label{Text: "label", Width: 20, Height: 8}
			}
		}
		if i >= 17 && i%4 == 1 {
			g.Connect(nodes[i], nodes[i-17])
		}
	}
	g.Connect(container, nodes[0])
	g.ComputeCellSize()
	return g
}

func TestEdgeLengthSharedEvaluationMatchesInline(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(0))
	g := newLargeEdgeLengthGraph()
	if edgeLengthHelpers(len(g.Nodes), len(g.Edges), 8) == 0 {
		t.Fatal("fixture too small to use helpers")
	}
	for _, options := range []EdgeLengthOptions{
		{EdgeAbductions: nil, IncludeNodeSizes: true, PenalizeDirection: true},
		{EdgeAbductions: nil, IncludeNodeSizes: false, PenalizeDirection: true},
		{EdgeAbductions: []*layoutgraph.EdgeAbduction{}, IncludeNodeSizes: true, PenalizeDirection: false},
	} {
		runtime.GOMAXPROCS(1)
		g.ResetPlacementCosts()
		inline, err := EdgeLength(context.Background(), g, options)
		if err != nil {
			t.Fatal(err)
		}
		runtime.GOMAXPROCS(8)
		for range 20 {
			g.ResetPlacementCosts()
			shared, err := EdgeLength(context.Background(), g, options)
			if err != nil {
				t.Fatal(err)
			}
			if math.Float64bits(shared) != math.Float64bits(inline) {
				t.Fatalf("shared EdgeLength = %.17g; inline = %.17g", shared, inline)
			}
		}
	}
}

func TestEdgeLengthSharedWorkerPanicIsSanitized(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(8))
	g := newLargeEdgeLengthGraph()
	payload := &secretWorkerPanic{}
	ctx := &panicInsideEdgeLengthContext{Context: context.Background(), payload: payload}
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = EdgeLength(ctx, g, EdgeLengthOptions{IncludeNodeSizes: true, PenalizeDirection: true})
	}()
	if message, ok := recovered.(string); !ok || message != edgeLengthWorkerPanic {
		t.Fatalf("recovered panic = %#v, want generic invariant marker", recovered)
	}
	if payload.stringCalled.Load() {
		t.Fatal("worker recovery invoked the secret payload's String method")
	}
}

type cancelAfterConcurrentChecks struct {
	context.Context
	remaining atomic.Int64
}

func (ctx *cancelAfterConcurrentChecks) Err() error {
	if ctx.remaining.Add(-1) < 0 {
		return context.Canceled
	}
	return nil
}

func TestEdgeLengthSharedEvaluationReportsCancellation(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(8))
	g := newLargeEdgeLengthGraph()
	for _, checks := range []int64{1, 5, 50, 500, 2000} {
		ctx := &cancelAfterConcurrentChecks{Context: context.Background()}
		ctx.remaining.Store(checks)
		_, err := EdgeLength(ctx, g, EdgeLengthOptions{IncludeNodeSizes: true, PenalizeDirection: true})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("after %d checks: error = %v, want context.Canceled", checks, err)
		}
	}
	done, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := EdgeLength(done, g, EdgeLengthOptions{IncludeNodeSizes: true, PenalizeDirection: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context: error = %v, want context.Canceled", err)
	}
}

// EdgeLength memoizes symmetry only for contexts with a Done channel; a
// Done-less context evaluates every call. Both must agree exactly.
func TestEdgeLengthSymmetryMemoMatchesUnmemoized(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(0))
	for _, g := range []*layoutgraph.Graph{newWideSymmetryGraph(), newLargeEdgeLengthGraph()} {
		var abductions []*layoutgraph.EdgeAbduction
		for _, e := range g.Edges {
			if e.From.Container != nil {
				abductions = append(abductions, &layoutgraph.EdgeAbduction{
					Edge: e, OriginallyFrom: e.From, CurrentFrom: e.From.Container, CurrentTo: e.To,
				})
			}
		}
		for _, abd := range [][]*layoutgraph.EdgeAbduction{nil, abductions} {
			options := EdgeLengthOptions{EdgeAbductions: abd, IncludeNodeSizes: true, PenalizeDirection: true}
			for _, procs := range []int{1, 8} {
				runtime.GOMAXPROCS(procs)
				g.ResetPlacementCosts()
				// context.Background has no Done channel: every polling point is
				// observed and nothing is memoized.
				unmemoized, err := EdgeLength(context.Background(), g, options)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				g.ResetPlacementCosts()
				memoized, err := EdgeLength(ctx, g, options)
				cancel()
				if err != nil {
					t.Fatal(err)
				}
				if math.Float64bits(memoized) != math.Float64bits(unmemoized) {
					t.Fatalf("procs %d: memoized %.17g, unmemoized %.17g", procs, memoized, unmemoized)
				}
			}
		}
	}
}
