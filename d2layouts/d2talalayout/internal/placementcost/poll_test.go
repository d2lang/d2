package placementcost

import (
	"context"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func TestScoringPollCallsErrAtEveryPointWithoutDone(t *testing.T) {
	ctx := &scorerCountingContext{Context: context.Background()}
	p := newScoringPoll(ctx)
	if err := p.entry(); err != nil {
		t.Fatal(err)
	}
	for i := range 200 {
		if err := p.at(i); err != nil {
			t.Fatal(err)
		}
		if err := p.check(); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.slice(129); err != nil {
		t.Fatal(err)
	}
	// entry + 4 loop points (0, 64, 128, 192) + 200 checks + 3 slice points.
	if want := 1 + 4 + 200 + 3; ctx.checks != want || p.checks != want {
		t.Fatalf("Err calls = %d, polling points = %d; want %d", ctx.checks, p.checks, want)
	}
}

func TestScoringPollSamplesDoneWithinInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := newScoringPoll(ctx)
	if err := p.entry(); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if err := p.check(); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	for i := 1; ; i++ {
		err := p.check()
		if err != nil {
			requireCanceledAt(t, err, "EdgeLength")
			break
		}
		if i > scoringSampleInterval {
			t.Fatalf("cancellation not observed within %d polling points", scoringSampleInterval)
		}
	}
	// A new evaluation observes cancellation at its entry.
	q := newScoringPoll(ctx)
	requireCanceledAt(t, q.entry(), "EdgeLength")
}

func TestScoringEntryPointsObserveCanceledDoneContext(t *testing.T) {
	g := newEdgeLengthRuntimeGraph()
	node := g.Nodes[0]
	options := EdgeLengthOptions{EdgeAbductions: nil, IncludeNodeSizes: true, PenalizeDirection: true}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scorer := NewNodeEdgeLengthScorer(node, options)
	defer scorer.Close()
	for name, run := range map[string]func() error{
		"EdgeLength": func() error { _, err := EdgeLength(ctx, g, options); return err },
		"NodesEdgeLength": func() error {
			_, err := NodesEdgeLength(ctx, layoutgraph.Nodes(g.Nodes), options)
			return err
		},
		"NodeEdgeLength":         func() error { _, err := NodeEdgeLength(ctx, node, options); return err },
		"Score":                  func() error { _, err := scorer.Score(ctx); return err },
		"NodeSymmetry":           func() error { _, err := NodeSymmetry(ctx, node, nil); return err },
		"NodesSymmetry":          func() error { _, err := NodesSymmetry(ctx, layoutgraph.Nodes(g.Nodes), nil); return err },
		"ColumnCrossingCost":     func() error { _, err := ColumnCrossingCost(ctx, node, nil); return err },
		"GraphEdgeCrossings":     func() error { _, err := GraphEdgeCrossings(ctx, g); return err },
		"ContainerAlignmentCost": func() error { _, err := ContainerAlignmentCost(ctx, g); return err },
	} {
		t.Run(name, func(t *testing.T) {
			requireCanceledAt(t, run(), "EdgeLength")
		})
	}
}

func TestScoringResultsDoNotDependOnPollingMode(t *testing.T) {
	g := newEdgeLengthRuntimeGraph()
	// A labeled multi-edge and a crossing make every polling loop run.
	extra := layoutgraph.NewNode(100, 30, 30)
	extra.TopLeft = geo.NewPoint(200, 200)
	g.AddNode(extra)
	g.AddNodeToContainer(nil, extra)
	g.Connect(extra, g.Nodes[0])
	g.Connect(g.Nodes[3], g.Nodes[7])
	options := EdgeLengthOptions{EdgeAbductions: nil, IncludeNodeSizes: true, PenalizeDirection: true}
	run := func(ctx context.Context) []float64 {
		var out []float64
		for _, node := range g.Nodes {
			length, err := NodeEdgeLength(ctx, node, options)
			if err != nil {
				t.Fatal(err)
			}
			symmetry, err := NodeSymmetry(ctx, node, nil)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, length, symmetry)
		}
		crossings, err := GraphEdgeCrossings(ctx, g)
		if err != nil {
			t.Fatal(err)
		}
		return append(out, float64(crossings))
	}
	withoutDone := run(&scorerCountingContext{Context: context.Background()})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	withDone := run(ctx)
	for i := range withoutDone {
		if withoutDone[i] != withDone[i] {
			t.Fatalf("value %d: %v without Done, %v with Done", i, withoutDone[i], withDone[i])
		}
	}
}
