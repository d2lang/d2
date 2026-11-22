package placementcost

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/grouping"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// newRandomScoringGraph builds a graph with nested containers, chords,
// labels, nears, herd fences and uncle siblings at random positions (some
// boxes with negative sizes), optionally with active clusters.
func newRandomScoringGraph(t *testing.T, random *rand.Rand, clusters bool) *layoutgraph.Graph {
	t.Helper()
	g := layoutgraph.NewGraph()
	id := layoutgraph.EntityID(1)
	var containers []*layoutgraph.Node
	var nodes []*layoutgraph.Node
	pick := func(values []*layoutgraph.Node) *layoutgraph.Node {
		if len(values) == 0 {
			return nil
		}
		return values[random.Intn(len(values))]
	}
	for i := 0; i < 4; i++ {
		c := layoutgraph.NewNode(id, float64(300+random.Intn(400)), float64(200+random.Intn(300)))
		id++
		c.TopLeft = geo.NewPoint(float64(20*random.Intn(100)-1000), float64(20*random.Intn(100)-1000))
		c.SetContainer(true)
		var parent *layoutgraph.Node
		if random.Intn(2) == 0 {
			parent = pick(containers)
		}
		g.AddNewNodeToContainer(parent, c)
		containers = append(containers, c)
		nodes = append(nodes, c)
	}
	count := 12 + random.Intn(20)
	for i := 0; i < count; i++ {
		w, h := float64(20+20*random.Intn(3)), float64(20+20*random.Intn(2))
		if random.Intn(15) == 0 {
			w = -w
		}
		n := layoutgraph.NewNode(id, w, h)
		id++
		n.TopLeft = geo.NewPoint(float64(20*random.Intn(60)-600), float64(20*random.Intn(60)-600))
		var parent *layoutgraph.Node
		if random.Intn(3) == 0 {
			parent = pick(containers)
		}
		g.AddNewNodeToContainer(parent, n)
		nodes = append(nodes, n)
	}
	for i := range nodes {
		for k := 0; k < 1+random.Intn(3); k++ {
			other := pick(nodes)
			if other == nodes[i] {
				continue
			}
			e := g.Connect(nodes[i], other)
			if random.Intn(4) == 0 {
				e.Label = &layoutgraph.Label{Text: "label", Width: 20, Height: 8}
			}
		}
	}
	nodes[len(nodes)-1].AddNear(nodes[len(nodes)-2])
	nodes[len(nodes)-3].HerdAssignment = &layoutgraph.HerdAssignment{Orientation: geo.Left, Val: -50}
	g.CommonUncleSiblings = map[*layoutgraph.Node]layoutgraph.Nodes{
		nodes[len(nodes)-4]: {nodes[len(nodes)-4], nodes[len(nodes)-5], nodes[len(nodes)-6]},
	}
	g.ComputeCellSize()
	if clusters {
		if err := grouping.AddClusters(context.Background(), g, 1, rand.New(rand.NewSource(random.Int63()))); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

// evaluateNodeWith runs one node evaluation through evaluate, polling ctx.
func evaluateNodeWith(ctx context.Context, n *layoutgraph.Node, options EdgeLengthOptions,
	evaluate func(*scoringPoll, *layoutgraph.Node, EdgeLengthOptions, *edgeScratch, edgeLengthStatic) (float64, error),
) (float64, error) {
	p := newScoringPoll(ctx)
	s := scratchPool.Get()
	defer putEdgeScratch(s)
	setup, err := prepareNodeEdgeLength(&p, n, options, s)
	if err != nil {
		return 0, err
	}
	return evaluate(&p, n, options, s, setup)
}

// The obstacle loop of evaluateNodeEdgeLength inlines bounds.excludes and
// IsDescendantOf; it must give the previous function's values bit for bit and
// poll the context at the same points.
func TestEvaluateNodeEdgeLengthMatchesReference(t *testing.T) {
	random := rand.New(rand.NewSource(11))
	for trial := 0; trial < 60; trial++ {
		g := newRandomScoringGraph(t, random, trial%2 == 1)
		var abductions []*layoutgraph.EdgeAbduction
		for _, e := range g.Edges {
			if e.From.Container != nil && random.Intn(2) == 0 {
				abductions = append(abductions, &layoutgraph.EdgeAbduction{
					Edge: e, OriginallyFrom: e.From, CurrentFrom: e.From.Container, CurrentTo: e.To,
				})
			}
		}
		for _, options := range []EdgeLengthOptions{
			{IncludeNodeSizes: true, PenalizeDirection: true},
			{IncludeNodeSizes: true, EnforceMinimumGap: true},
			{EdgeAbductions: abductions, IncludeNodeSizes: true, PenalizeDirection: true},
			{IncludeNodeSizes: false, PenalizeDirection: true},
		} {
			for _, n := range g.Nodes {
				if random.Intn(4) == 0 {
					// Non-finite, NaN and huge geometry, on the node or on a
					// possible obstacle, take the slow paths.
					odd := g.Nodes[random.Intn(len(g.Nodes))]
					for _, m := range []*layoutgraph.Node{n, odd} {
						saved, savedWidth := *m.TopLeft, m.Width
						value := []float64{math.Inf(1), math.Inf(-1), math.NaN(), -1e308, 1e308}[random.Intn(5)]
						switch random.Intn(3) {
						case 0:
							m.TopLeft.X = value
						case 1:
							m.TopLeft.Y = value
						default:
							m.Width = value
						}
						compareNodeEvaluation(t, n, options)
						*m.TopLeft, m.Width = saved, savedWidth
					}
				}
				compareNodeEvaluation(t, n, options)
			}
		}
	}
}

func compareNodeEvaluation(t *testing.T, n *layoutgraph.Node, options EdgeLengthOptions) {
	t.Helper()
	ctx := &cancelAfterErrChecks{Context: context.Background(), remaining: 1 << 30}
	got, gotErr := evaluateNodeWith(ctx, n, options, evaluateNodeEdgeLength)
	gotPolls := 1<<30 - ctx.remaining
	refCtx := &cancelAfterErrChecks{Context: context.Background(), remaining: 1 << 30}
	want, wantErr := evaluateNodeWith(refCtx, n, options, referenceEvaluateNodeEdgeLength)
	wantPolls := 1<<30 - refCtx.remaining
	if gotErr != nil || wantErr != nil {
		t.Fatalf("errors %v / %v", gotErr, wantErr)
	}
	if math.Float64bits(got) != math.Float64bits(want) {
		t.Fatalf("node %d: evaluateNodeEdgeLength %.17g, reference %.17g", n.ID, got, want)
	}
	if gotPolls != wantPolls {
		t.Fatalf("node %d: %d polls, reference %d", n.ID, gotPolls, wantPolls)
	}
	// Cancellation is observed at the same polling point.
	for _, k := range []int{0, 1, 2, wantPolls / 2, wantPolls - 1} {
		if k < 0 || k >= wantPolls {
			continue
		}
		ctx := &cancelAfterErrChecks{Context: context.Background(), remaining: k}
		_, gotErr := evaluateNodeWith(ctx, n, options, evaluateNodeEdgeLength)
		refCtx := &cancelAfterErrChecks{Context: context.Background(), remaining: k}
		_, wantErr := evaluateNodeWith(refCtx, n, options, referenceEvaluateNodeEdgeLength)
		if !errors.Is(gotErr, context.Canceled) || !errors.Is(wantErr, context.Canceled) || ctx.remaining != refCtx.remaining {
			t.Fatalf("node %d cancel after %d: %v (%d left) / %v (%d left)", n.ID, k, gotErr, ctx.remaining, wantErr, refCtx.remaining)
		}
	}
}

// appendScoringAncestry and scoringAncestryHas answer IsDescendantOf for
// containers, cluster vessels (including missing ones), sequence vessels and
// nil ancestors.
func TestScoringAncestryMatchesIsDescendantOf(t *testing.T) {
	random := rand.New(rand.NewSource(3))
	for trial := 0; trial < 300; trial++ {
		nodes := make([]*layoutgraph.Node, 12)
		for i := range nodes {
			nodes[i] = layoutgraph.NewNode(layoutgraph.EntityID(i+1), 10, 10)
		}
		// Parents always have a larger index, so there is no cycle.
		for i, n := range nodes {
			if i == len(nodes)-1 {
				break
			}
			parent := nodes[i+1+random.Intn(len(nodes)-i-1)]
			switch random.Intn(5) {
			case 0:
				n.Container = parent
			case 1:
				n.Cluster = &layoutgraph.Cluster{Vessel: parent}
			case 2:
				n.Cluster = &layoutgraph.Cluster{}
			case 3:
				n.Sequence = &layoutgraph.Sequence{Vessel: parent}
			}
			if random.Intn(4) == 0 && n.Container == nil {
				n.Sequence = &layoutgraph.Sequence{Vessel: parent}
			}
		}
		for _, n := range nodes {
			chain, ok := appendScoringAncestry(nil, n)
			if !ok {
				t.Fatal("acyclic chain reported as too deep")
			}
			for _, ancestor := range append([]*layoutgraph.Node{nil}, nodes...) {
				if got, want := scoringAncestryHas(chain, ok, n, ancestor), n.IsDescendantOf(ancestor); got != want {
					t.Fatalf("trial %d: ancestry %v, IsDescendantOf %v", trial, got, want)
				}
			}
		}
	}
}

// A sweep scorer, which keeps the obstacles of the scored node's edges between
// Score calls, must score every candidate position exactly as NodeEdgeLength
// does (and as the reference evaluator), including nodes that carry
// descendants and abductions that replace the node by one of its children.
func TestNodeEdgeLengthSweepScorerMatchesNodeEdgeLength(t *testing.T) {
	random := rand.New(rand.NewSource(5))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for trial := 0; trial < 40; trial++ {
		g := newRandomScoringGraph(t, random, trial%2 == 1)
		var abductions []*layoutgraph.EdgeAbduction
		for _, e := range g.Edges {
			if e.From.Container != nil && random.Intn(2) == 0 {
				abductions = append(abductions, &layoutgraph.EdgeAbduction{
					Edge: e, OriginallyFrom: e.From, CurrentFrom: e.From.Container, CurrentTo: e.To,
				})
			}
		}
		for _, options := range []EdgeLengthOptions{
			{IncludeNodeSizes: true, PenalizeDirection: true},
			{EdgeAbductions: abductions, IncludeNodeSizes: true, PenalizeDirection: true},
		} {
			for _, node := range g.Nodes {
				if node.TopLeft == nil {
					continue
				}
				startX, startY := node.TopLeft.X, node.TopLeft.Y
				scorer := NewNodeEdgeLengthSweepScorer(node, options)
				for step := 0; step < 12; step++ {
					if step > 0 {
						node.MoveAbsWithChildren(startX+float64(20*(random.Intn(40)-20)), startY+float64(20*(random.Intn(40)-20)))
					}
					got, err := scorer.Score(ctx)
					if err != nil {
						t.Fatal(err)
					}
					want, err := NodeEdgeLength(ctx, node, options)
					if err != nil {
						t.Fatal(err)
					}
					reference, err := evaluateNodeWith(context.Background(), node, options, referenceEvaluateNodeEdgeLength)
					if err != nil {
						t.Fatal(err)
					}
					if math.Float64bits(got) != math.Float64bits(want) || math.Float64bits(got) != math.Float64bits(reference) {
						t.Fatalf("trial %d node %d step %d: sweep %.17g, NodeEdgeLength %.17g, reference %.17g", trial, node.ID, step, got, want, reference)
					}
				}
				scorer.Close()
				node.MoveAbsWithChildren(startX, startY)
			}
		}
	}
}

// Without a Done channel every polling point is observed, so a sweep scorer
// polls exactly like NodeEdgeLength.
func TestNodeEdgeLengthSweepScorerPollsLikeNodeEdgeLengthWithoutDone(t *testing.T) {
	random := rand.New(rand.NewSource(9))
	g := newRandomScoringGraph(t, random, false)
	options := EdgeLengthOptions{IncludeNodeSizes: true, PenalizeDirection: true}
	for _, node := range g.Nodes {
		scorer := NewNodeEdgeLengthSweepScorer(node, options)
		for step := 0; step < 3; step++ {
			node.MoveWithChildren(20, 0)
			ctx := &cancelAfterErrChecks{Context: context.Background(), remaining: 1 << 30}
			got, err := scorer.Score(ctx)
			if err != nil {
				t.Fatal(err)
			}
			plainCtx := &cancelAfterErrChecks{Context: context.Background(), remaining: 1 << 30}
			want, err := NodeEdgeLength(plainCtx, node, options)
			if err != nil {
				t.Fatal(err)
			}
			if math.Float64bits(got) != math.Float64bits(want) || ctx.remaining != plainCtx.remaining {
				t.Fatalf("node %d: sweep %.17g after %d polls, NodeEdgeLength %.17g after %d", node.ID, got, 1<<30-ctx.remaining, want, 1<<30-plainCtx.remaining)
			}
		}
		scorer.Close()
	}
}

// ScoreUnlessAbove may stop early, but a caller that discards candidates
// comparing above the bound must decide exactly as with Score, and it must
// return Score's value whenever the candidate is kept.
func TestScoreUnlessAboveDecidesLikeScore(t *testing.T) {
	random := rand.New(rand.NewSource(17))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	discards := func(score, offset, bound float64) bool {
		return !math.IsInf(bound, 1) && geo.PrecisionCompare(score-offset, bound, geo.PRECISION) == 1
	}
	stopped := 0
	for trial := 0; trial < 30; trial++ {
		g := newRandomScoringGraph(t, random, trial%3 == 1)
		// Initialize the routing costs as the layout does before optimizing.
		if _, err := EdgeLength(ctx, g, EdgeLengthOptions{IncludeNodeSizes: true, PenalizeDirection: true}); err != nil {
			t.Fatal(err)
		}
		options := EdgeLengthOptions{IncludeNodeSizes: true, PenalizeDirection: true, EnforceMinimumGap: trial%2 == 0}
		for _, node := range g.Nodes {
			startX, startY := node.TopLeft.X, node.TopLeft.Y
			scorer := NewNodeEdgeLengthSweepScorer(node, options)
			for step := 0; step < 8; step++ {
				node.MoveAbsWithChildren(startX+float64(20*(random.Intn(40)-20)), startY+float64(20*(random.Intn(40)-20)))
				want, err := NodeEdgeLength(ctx, node, options)
				if err != nil {
					t.Fatal(err)
				}
				offset := float64(random.Intn(300))
				bounds := []float64{
					want - offset, want - offset - 0.0001, want - offset + 0.0001, want - offset - 1, want - offset + 1,
					want/2 - offset, -offset, math.Inf(-1), math.Inf(1), math.NaN(), float64(random.Intn(5000)),
				}
				for _, bound := range bounds {
					got, err := scorer.ScoreUnlessAbove(ctx, offset, bound)
					if err != nil {
						t.Fatal(err)
					}
					if discards(got, offset, bound) != discards(want, offset, bound) {
						t.Fatalf("trial %d node %d: bound %v offset %v: ScoreUnlessAbove %.17g decides differently from Score %.17g", trial, node.ID, bound, offset, got, want)
					}
					if !discards(want, offset, bound) && math.Float64bits(got) != math.Float64bits(want) {
						t.Fatalf("trial %d node %d: kept candidate scored %.17g, Score %.17g", trial, node.ID, got, want)
					}
					if math.Float64bits(got) != math.Float64bits(want) {
						stopped++
					}
				}
			}
			scorer.Close()
			node.MoveAbsWithChildren(startX, startY)
		}
	}
	if stopped == 0 {
		t.Fatal("ScoreUnlessAbove never stopped early")
	}
}

// A common-uncle term can be negative (an axis score above 1), so a node with
// one is always scored in full.
func TestScoreUnlessAboveScoresUncleSiblingsInFull(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := layoutgraph.NewGraph()
	add := func(id layoutgraph.EntityID, x, y, w, h float64) *layoutgraph.Node {
		n := layoutgraph.NewNode(id, w, h)
		n.TopLeft = geo.NewPoint(x, y)
		g.AddNewNodeToContainer(nil, n)
		return n
	}
	largest := add(1, 0, 0, 100, 100)
	left := add(2, -200, 10, 40, 50)
	right := add(3, 200, 10, 40, 50)
	far := add(4, -2000, 900, 40, 40)
	g.Connect(left, far)
	g.CommonUncleSiblings = map[*layoutgraph.Node]layoutgraph.Nodes{left: {largest, left, right}}
	g.ComputeCellSize()
	options := EdgeLengthOptions{IncludeNodeSizes: true, PenalizeDirection: true}
	if _, err := EdgeLength(ctx, g, options); err != nil {
		t.Fatal(err)
	}
	if AxisScore(g.CommonUncleSiblings[left]) <= 1 {
		t.Fatal("fixture's axis score does not make the uncle term negative")
	}
	want, err := NodeEdgeLength(ctx, left, options)
	if err != nil {
		t.Fatal(err)
	}
	scorer := NewNodeEdgeLengthSweepScorer(left, options)
	defer scorer.Close()
	for _, bound := range []float64{want + 0.5, want + 1e3, want - 0.5} {
		got, err := scorer.ScoreUnlessAbove(ctx, 0, bound)
		if err != nil {
			t.Fatal(err)
		}
		if math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("bound %v: ScoreUnlessAbove %.17g, Score %.17g", bound, got, want)
		}
	}
}
