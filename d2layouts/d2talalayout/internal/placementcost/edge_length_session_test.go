package placementcost

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/grouping"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// newClusteredSessionGraph has active clusters (hubs with interchangeable
// leaves), a container, nears, herd fences, uncle siblings and labels.
func newClusteredSessionGraph(t *testing.T) *layoutgraph.Graph {
	t.Helper()
	g := layoutgraph.NewGraph()
	id := layoutgraph.EntityID(1)
	newNode := func(container *layoutgraph.Node, x, y, w, h float64) *layoutgraph.Node {
		n := layoutgraph.NewNode(id, w, h)
		id++
		n.TopLeft = geo.NewPoint(x, y)
		g.AddNewNodeToContainer(container, n)
		return n
	}
	container := newNode(nil, 900, 0, 400, 300)
	container.SetContainer(true)
	var hubs []*layoutgraph.Node
	for h := 0; h < 4; h++ {
		hub := newNode(nil, float64(h)*220, 400, 60, 40)
		hubs = append(hubs, hub)
		for leaf := 0; leaf < 3; leaf++ {
			n := newNode(nil, float64(h)*220+float64(leaf)*70, 520, 40, 30)
			e := g.Connect(hub, n)
			if leaf == 1 {
				e.Label = &layoutgraph.Label{Text: "l", Width: 10, Height: 8}
			}
		}
		if h > 0 {
			g.Connect(hubs[h-1], hub)
		}
	}
	inner := []*layoutgraph.Node{
		newNode(container, 920, 20, 50, 40),
		newNode(container, 1000, 20, 50, 40),
		newNode(container, 1080, 120, 50, 40),
	}
	g.Connect(inner[0], inner[1])
	g.Connect(inner[1], hubs[2])
	g.Connect(inner[2], hubs[3])
	g.Connect(container, hubs[0])
	hubs[0].AddNear(hubs[3])
	hubs[1].HerdAssignment = &layoutgraph.HerdAssignment{Orientation: geo.Top, Val: 380}
	g.CommonUncleSiblings = map[*layoutgraph.Node]layoutgraph.Nodes{
		inner[1]: {inner[1], inner[2]},
	}
	g.ComputeCellSize()
	if err := grouping.AddClusters(context.Background(), g, 1, rand.New(rand.NewSource(1))); err != nil {
		t.Fatal(err)
	}
	if len(g.Clusters) == 0 {
		t.Fatal("fixture has no clusters")
	}
	return g
}

func plainEdgeLength(t *testing.T, g *layoutgraph.Graph, options EdgeLengthOptions) (float64, int64) {
	t.Helper()
	ctx := context.Background()
	p := newScoringPoll(ctx)
	total, err := sumNodeEdgeLengths(ctx, &p, g, options)
	if err != nil {
		t.Fatal(err)
	}
	crossings, err := GraphEdgeCrossings(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	return addCrossingCost(total, g, crossings), crossings
}

// plainEvaluationChecks compares every incremental result of the sessions
// with the plain evaluation and panics on a difference (see sessionVerifier).
type plainEvaluationChecks struct{}

func (plainEvaluationChecks) resumedState(graph *layoutgraph.Graph, costs layoutgraph.RoutingCostState, options EdgeLengthOptions, state uint64) {
	if state != placementEdgeLengthState(graph, costs, options) {
		panic("EdgeLengthSession: resumed cache key differs from placementEdgeLengthState")
	}
}

func (plainEvaluationChecks) edgeLength(graph *layoutgraph.Graph, options EdgeLengthOptions, got float64) {
	ctx := context.Background()
	p := newScoringPoll(ctx)
	want, err := sumNodeEdgeLengths(ctx, &p, graph, options)
	if err != nil {
		panic(err)
	}
	crossings, err := GraphEdgeCrossings(ctx, graph)
	if err != nil {
		panic(err)
	}
	want = addCrossingCost(want, graph, crossings)
	if math.Float64bits(want) != math.Float64bits(got) {
		panic(fmt.Sprintf("EdgeLengthSession: EdgeLength %v (%#x), plain evaluation %v (%#x)", got, math.Float64bits(got), want, math.Float64bits(want)))
	}
}

func (plainEvaluationChecks) crossings(graph *layoutgraph.Graph, got int64) {
	want, err := GraphEdgeCrossings(context.Background(), graph)
	if err != nil {
		panic(err)
	}
	if want != got {
		panic(fmt.Sprintf("EdgeLengthSession: GraphEdgeCrossings %d, plain count %d", got, want))
	}
}

// TestEdgeLengthSessionMatchesEdgeLength moves random nodes (with their
// children) around, back, and far away, resizes some, and compares every
// session result with the plain evaluation bit for bit.
func TestEdgeLengthSessionMatchesEdgeLength(t *testing.T) {
	skipSlowUnderRace(t, 92)
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(0))
	sessionVerifier = plainEvaluationChecks{}
	defer func() { sessionVerifier = nil }()
	fixtures := map[string]func(*testing.T) *layoutgraph.Graph{
		"large":     func(*testing.T) *layoutgraph.Graph { return newLargeEdgeLengthGraph() },
		"symmetry":  func(*testing.T) *layoutgraph.Graph { return newWideSymmetryGraph() },
		"clustered": newClusteredSessionGraph,
	}
	for name, fixture := range fixtures {
		for _, procs := range []int{1, 8} {
			runtime.GOMAXPROCS(procs)
			for seed := int64(1); seed <= 3; seed++ {
				g := fixture(t)
				options := EdgeLengthOptions{IncludeNodeSizes: true, PenalizeDirection: seed != 2, EnforceMinimumGap: seed == 3}
				runSessionTrials(t, name, g, options, rand.New(rand.NewSource(seed)))
			}
		}
	}
}

func runSessionTrials(t *testing.T, name string, g *layoutgraph.Graph, options EdgeLengthOptions, random *rand.Rand) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Initialize the routing costs as the first EdgeLength of a layout does.
	if _, err := EdgeLength(ctx, g, options); err != nil {
		t.Fatal(err)
	}
	session := NewEdgeLengthSession(g, options)
	defer session.Close()
	type saved struct {
		node          *layoutgraph.Node
		x, y          float64
		width, height float64
	}
	var moved []saved
	incremental := 0
	for step := 0; step < 400; step++ {
		switch action := random.Intn(10); {
		case action < 5:
			n := g.Nodes[random.Intn(len(g.Nodes))]
			moved = append(moved, saved{n, n.TopLeft.X, n.TopLeft.Y, n.Width, n.Height})
			dx, dy := float64(random.Intn(200)-100), float64(random.Intn(200)-100)
			if random.Intn(8) == 0 {
				dx *= 40
			}
			n.MoveWithChildren(dx, dy)
		case action < 6:
			n := g.Nodes[random.Intn(len(g.Nodes))]
			moved = append(moved, saved{n, n.TopLeft.X, n.TopLeft.Y, n.Width, n.Height})
			n.Width += float64(random.Intn(30) - 10)
		case action < 8:
			for i := len(moved) - 1; i >= 0; i-- {
				m := moved[i]
				m.node.MoveWithChildren(m.x-m.node.TopLeft.X, m.y-m.node.TopLeft.Y)
				m.node.Width, m.node.Height = m.width, m.height
			}
			moved = moved[:0]
		}
		want, wantCrossings := plainEdgeLength(t, g, options)
		var got float64
		var err error
		if random.Intn(4) == 0 {
			got, err = session.EdgeLengthBase(ctx)
		} else {
			got, err = session.EdgeLength(ctx)
		}
		if err != nil {
			t.Fatal(err)
		}
		if math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("%s step %d: session EdgeLength %.17g, plain %.17g", name, step, got, want)
		}
		gotCrossings, err := session.GraphEdgeCrossings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if gotCrossings != wantCrossings {
			t.Fatalf("%s step %d: session crossings %d, plain %d", name, step, gotCrossings, wantCrossings)
		}
		if session.baseOK {
			incremental++
		}
	}
	if incremental == 0 {
		t.Fatalf("%s: the session never evaluated incrementally", name)
	}
}

// A context without a Done channel observes every polling point, so the
// session must leave it to the plain functions.
func TestEdgeLengthSessionWithoutDoneUsesPlainFunctions(t *testing.T) {
	g := newLargeEdgeLengthGraph()
	options := EdgeLengthOptions{IncludeNodeSizes: true, PenalizeDirection: true}
	session := NewEdgeLengthSession(g, options)
	defer session.Close()
	for step := 0; step < 3; step++ {
		g.Nodes[step].MoveWithChildren(13, 7)
		// Helpers may poll concurrently; count atomically.
		ctx := &cancelAfterConcurrentChecks{Context: context.Background()}
		ctx.remaining.Store(1 << 30)
		got, err := session.EdgeLength(ctx)
		if err != nil {
			t.Fatal(err)
		}
		sessionChecks := 1<<30 - ctx.remaining.Load()
		g.ResetPlacementCosts()
		plainCtx := &cancelAfterConcurrentChecks{Context: context.Background()}
		plainCtx.remaining.Store(1 << 30)
		want, err := EdgeLength(plainCtx, g, options)
		if err != nil {
			t.Fatal(err)
		}
		if math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("session %.17g, plain %.17g", got, want)
		}
		if plainChecks := 1<<30 - plainCtx.remaining.Load(); plainChecks != sessionChecks {
			t.Fatalf("session polled Err %d times, EdgeLength %d", sessionChecks, plainChecks)
		}
		if session.topoOK || session.baseOK {
			t.Fatal("session evaluated incrementally for a context without Done")
		}
		g.ResetPlacementCosts()
	}
}

func TestObstructionBoundsExcludesGeometryMatchesExcludes(t *testing.T) {
	random := rand.New(rand.NewSource(7))
	values := []float64{0, -0.0, 1, -1, 10, 100, -100, 1e300, -1e300, math.Inf(1), math.Inf(-1), math.NaN(), math.MaxFloat64}
	pick := func() float64 {
		if random.Intn(3) == 0 {
			return values[random.Intn(len(values))]
		}
		return float64(random.Intn(400) - 200)
	}
	for range 20000 {
		bounds := scoringNodeBounds(&layoutgraph.Node{Box: geo.Box{TopLeft: geo.NewPoint(pick(), pick()), Width: pick(), Height: pick()}})
		bounds = bounds.including(scoringNodeBounds(&layoutgraph.Node{Box: geo.Box{TopLeft: geo.NewPoint(pick(), pick()), Width: pick(), Height: pick()}}))
		node := &layoutgraph.Node{Box: geo.Box{Width: pick(), Height: pick()}}
		if random.Intn(10) != 0 {
			node.TopLeft = geo.NewPoint(pick(), pick())
		}
		var x, y float64
		if node.TopLeft != nil {
			x, y = node.TopLeft.X, node.TopLeft.Y
		}
		if got, want := bounds.excludesGeometry(node.TopLeft != nil, x, y, node.Width, node.Height), bounds.excludes(node); got != want {
			t.Fatalf("excludesGeometry = %v, excludes = %v for %+v and %+v", got, want, bounds, node.Box)
		}
	}
}
