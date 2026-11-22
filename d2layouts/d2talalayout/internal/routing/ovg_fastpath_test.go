package routing

import (
	"context"
	"maps"
	"math"
	"math/rand"
	"slices"
	"testing"

	"github.com/d2lang/d2/lib/geo"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

// stepN must be indistinguishable from n literal steps: same errors, same
// work and node counts, same stage-aggregate charges (added plus pending).
func TestOVGBuildGuardStepNMatchesSteps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	random := rand.New(rand.NewSource(11))
	for trial := range 400 {
		stageLimit := uint64(random.Intn(5000))
		ownLimit := uint64(random.Intn(5000))
		stageUsed := uint64(random.Intn(int(stageLimit) + 1))
		withStage := random.Intn(4) != 0
		startWork := uint64(random.Intn(int(ownLimit) + 1))
		newGuard := func() (*ovgBuildGuard, *routeWorkGuard) {
			limits := defaultOVGBuildLimits()
			limits.work = ownLimit
			if !withStage {
				guard, err := newOVGBuildGuard(ctx, limits)
				if err != nil {
					t.Fatal(err)
				}
				guard.work = startWork
				return guard, nil
			}
			stage := &routeWorkGuard{ctx: ctx, done: ctx.Done(), location: "EdgeRouting", used: stageUsed, limit: stageLimit}
			guard, err := newOVGBuildGuard(contextWithRouteAggregateWork(ctx, stage), limits)
			if err != nil {
				t.Fatal(err)
			}
			guard.work = startWork
			return guard, stage
		}
		batched, batchedStage := newGuard()
		reference, referenceStage := newGuard()
		for op := range 60 {
			n := uint64(random.Intn(3000))
			if random.Intn(3) == 0 {
				n = uint64(random.Intn(5))
			}
			gotErr := batched.stepN(n)
			var wantErr error
			for range n {
				if wantErr = reference.step(); wantErr != nil {
					break
				}
			}
			if (gotErr == nil) != (wantErr == nil) || (gotErr != nil && gotErr.Error() != wantErr.Error()) {
				t.Fatalf("trial %d op %d n=%d: error %v, want %v", trial, op, n, gotErr, wantErr)
			}
			if batched.work != reference.work || batched.pending != reference.pending || batched.room != reference.room {
				t.Fatalf("trial %d op %d: work %d/%d pending %d/%d room %d/%d", trial, op,
					batched.work, reference.work, batched.pending, reference.pending, batched.room, reference.room)
			}
			if withStage && batchedStage.used != referenceStage.used {
				t.Fatalf("trial %d op %d: stage %d, want %d", trial, op, batchedStage.used, referenceStage.used)
			}
			if gotErr != nil {
				break
			}
		}
	}
}

// fastPathTestGraph builds a graph with overlapping shapes, containers with
// children, invisible nodes and shared sequences, so the predicates meet
// every branch.
func fastPathTestGraph(random *rand.Rand, count int) *layoutgraph.Graph {
	graph := layoutgraph.NewGraph()
	id := layoutgraph.EntityID(1)
	var sequences []*layoutgraph.Sequence
	for i := range 2 {
		// A vessel outside any graph keeps the sequence inactive for
		// container lookups; the predicates only compare sequence identity.
		sequences = append(sequences, &layoutgraph.Sequence{Vessel: layoutgraph.NewNode(layoutgraph.EntityID(100000+i), 1, 1)})
	}
	// A coarse grid in some graphs makes equal coordinates (sort ties,
	// touching and aligned shapes) frequent.
	grid := 1
	if random.Intn(2) == 0 {
		grid = 40
	}
	coordinate := func(limit int) float64 {
		value := float64(random.Intn(limit/grid) * grid)
		if random.Intn(4) == 0 {
			value += 0.5
		}
		return value
	}
	for i := 0; i < count; i++ {
		width := float64(10 + random.Intn(60))
		height := float64(10 + random.Intn(60))
		if random.Intn(6) == 0 && i+2 < count {
			container := layoutgraph.NewNode(id, width+120, height+120)
			id++
			container.TopLeft = geo.NewPoint(coordinate(600), coordinate(600))
			graph.AddNewNodeToContainer(nil, container)
			for range 1 + random.Intn(2) {
				child := layoutgraph.NewNode(id, width, height)
				id++
				child.TopLeft = geo.NewPoint(container.TopLeft.X+coordinate(100), container.TopLeft.Y+coordinate(100))
				graph.AddNewNodeToContainer(container, child)
				i++
			}
			continue
		}
		node := layoutgraph.NewNode(id, width, height)
		id++
		node.TopLeft = geo.NewPoint(coordinate(700), coordinate(700))
		node.IsInvisible = random.Intn(10) == 0
		graph.AddNode(node)
	}
	for _, node := range graph.Nodes {
		if random.Intn(5) == 0 {
			sequence := sequences[random.Intn(len(sequences))]
			node.Sequence = sequence
			sequence.Nodes = append(sequence.Nodes, node)
		}
	}
	return graph
}

// twinOVGs returns two OVGs with the same ports and nodes; the port map is
// shared (read-only during the sweeps under test).
func twinOVGs(t *testing.T, graph *layoutgraph.Graph) (*OVG, *OVG) {
	t.Helper()
	first := NewOVG(graph.Nodes)
	if err := first.addPorts(graph, newBackgroundOVGBuildGuardForTest(t)); err != nil {
		t.Fatal(err)
	}
	second := NewOVG(graph.Nodes)
	second.Ports = first.Ports
	second.Nodes = slices.Clone(first.Nodes)
	second.OccupiedPoints = maps.Clone(first.OccupiedPoints)
	return first, second
}

func ovgNodePoints(ovg *OVG) []geo.Point {
	points := make([]geo.Point, len(ovg.Nodes))
	for i, node := range ovg.Nodes {
		points[i] = *node.Point
	}
	return points
}

type guardPair struct {
	stepped, counted           *ovgBuildGuard
	steppedStage, countedStage *routeWorkGuard
}

func newGuardPair(t *testing.T, ctx context.Context, random *rand.Rand, workLimit uint64) guardPair {
	t.Helper()
	withStage := random.Intn(2) == 0
	stageLimit := uint64(random.Intn(int(2*workLimit) + 1))
	newGuard := func() (*ovgBuildGuard, *routeWorkGuard) {
		limits := defaultOVGBuildLimits()
		limits.work = workLimit
		if !withStage {
			guard, err := newOVGBuildGuard(ctx, limits)
			if err != nil {
				t.Fatal(err)
			}
			return guard, nil
		}
		stage := &routeWorkGuard{ctx: ctx, done: ctx.Done(), location: "EdgeRouting", limit: stageLimit}
		guard, err := newOVGBuildGuard(contextWithRouteAggregateWork(ctx, stage), limits)
		if err != nil {
			t.Fatal(err)
		}
		return guard, stage
	}
	var pair guardPair
	pair.stepped, pair.steppedStage = newGuard()
	pair.counted, pair.countedStage = newGuard()
	return pair
}

func (pair guardPair) compare(t *testing.T, label string, steppedErr, countedErr error) {
	t.Helper()
	if (steppedErr == nil) != (countedErr == nil) || (steppedErr != nil && steppedErr.Error() != countedErr.Error()) {
		t.Fatalf("%s: error %v, want %v", label, countedErr, steppedErr)
	}
	s, c := pair.stepped, pair.counted
	if s.work != c.work || s.nodes != c.nodes || s.edges != c.edges || s.candidates != c.candidates || s.pending != c.pending {
		t.Fatalf("%s: counted work/nodes/edges/candidates/pending %d/%d/%d/%d/%d, stepped %d/%d/%d/%d/%d", label,
			c.work, c.nodes, c.edges, c.candidates, c.pending, s.work, s.nodes, s.edges, s.candidates, s.pending)
	}
	if pair.steppedStage != nil && pair.steppedStage.used != pair.countedStage.used {
		t.Fatalf("%s: stage %d, want %d", label, pair.countedStage.used, pair.steppedStage.used)
	}
}

// The counted candidate loop must add the same nodes in the same order and
// charge the same units as the stepped loop, including when a work limit or
// the stage aggregate runs out part way.
func TestOVGIntersectionCandidatesCountedMatchesStepped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	random := rand.New(rand.NewSource(3))
	defaultThreshold := ovgParallelCandidateThreshold
	defer func() { ovgParallelCandidateThreshold = defaultThreshold }()
	for trial := range 240 {
		// Odd trials evaluate the candidates on several goroutines.
		ovgParallelCandidateThreshold = defaultThreshold
		if trial%2 == 1 {
			ovgParallelCandidateThreshold = 1
		}
		graph := fastPathTestGraph(random, 4+random.Intn(24))
		steppedOVG, countedOVG := twinOVGs(t, graph)

		xs := map[float64]struct{}{}
		ys := map[float64]struct{}{}
		for _, node := range steppedOVG.Nodes {
			xs[node.X] = struct{}{}
			ys[node.Y] = struct{}{}
		}
		xOrder := slices.Sorted(maps.Keys(xs))
		yOrder := slices.Sorted(maps.Keys(ys))
		setup := newOVGBuildGuardForTest(ctx, t)
		fixedOverlaps, err := steppedOVG.fixedOverlapsForBuild(graph, layoutgraph.Nodes(graph.Nodes), setup)
		if err != nil {
			t.Fatal(err)
		}
		portIndex, err := newOVGPortIndex(steppedOVG.Ports, graph, fixedOverlaps, setup)
		if err != nil {
			t.Fatal(err)
		}
		proximityIndex, err := newOVGPointProximityIndex(graph, xOrder, setup)
		if err != nil {
			t.Fatal(err)
		}

		// First without a binding limit, then with limits inside the sweep.
		full := newGuardPair(t, ctx, rand.New(rand.NewSource(int64(trial))), maxOVGWorkUnits)
		full.stepped.stage, full.steppedStage = nil, nil
		full.counted.stage, full.countedStage = nil, nil
		full.stepped.aggregate, full.counted.aggregate = nil, nil
		steppedErr := steppedOVG.addIntersectionCandidatesStepped(xOrder, yOrder, portIndex, proximityIndex, full.stepped)
		countedErr := countedOVG.addIntersectionCandidates(xOrder, yOrder, portIndex, proximityIndex, full.counted)
		full.compare(t, "unlimited", steppedErr, countedErr)
		if !slices.Equal(ovgNodePoints(steppedOVG), ovgNodePoints(countedOVG)) {
			t.Fatalf("trial %d: counted nodes differ from stepped nodes", trial)
		}
		total := full.stepped.work
		if total == 0 {
			continue
		}
		for range 4 {
			limitedStepped, limitedCounted := twinOVGs(t, graph)
			limit := uint64(random.Int63n(int64(total) + 1))
			pair := newGuardPair(t, ctx, random, limit)
			steppedErr := limitedStepped.addIntersectionCandidatesStepped(xOrder, yOrder, portIndex, proximityIndex, pair.stepped)
			countedErr := limitedCounted.addIntersectionCandidates(xOrder, yOrder, portIndex, proximityIndex, pair.counted)
			pair.compare(t, "limited", steppedErr, countedErr)
			if !slices.Equal(ovgNodePoints(limitedStepped), ovgNodePoints(limitedCounted)) {
				t.Fatalf("trial %d limit %d: counted nodes differ from stepped nodes", trial, limit)
			}
		}
	}
}

func sortedOVGNodePoints(ovg *OVG) []geo.Point {
	points := ovgNodePoints(ovg)
	slices.SortFunc(points, func(a, b geo.Point) int {
		if a.X != b.X {
			if a.X < b.X {
				return -1
			}
			return 1
		}
		if a.Y < b.Y {
			return -1
		}
		if a.Y > b.Y {
			return 1
		}
		return 0
	})
	return points
}

// Both versions iterate ovg.Ports in map order, which differs between runs,
// so the order of the added nodes may differ; the node set, the work and the
// node counts do not depend on it. With a limit inside the sweep the error
// and the counts at the failure must match.
func TestOVGBoundaryConnectionsCountedMatchesStepped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	random := rand.New(rand.NewSource(5))
	for trial := range 150 {
		graph := fastPathTestGraph(random, 2+random.Intn(20))
		steppedOVG, countedOVG := twinOVGs(t, graph)
		setup := newOVGBuildGuardForTest(ctx, t)
		tl, br, err := setup.tightBoundingBox(layoutgraph.Nodes(steppedOVG.NodesInsideBoundingBox))
		if err != nil {
			t.Fatal(err)
		}
		if random.Intn(3) == 0 {
			// Boundaries close to the nodes make boundary points near some of them.
			tl.X += float64(random.Intn(30))
			tl.Y += float64(random.Intn(30))
		}
		full := newGuardPair(t, ctx, rand.New(rand.NewSource(int64(trial))), maxOVGWorkUnits)
		full.stepped.stage, full.steppedStage = nil, nil
		full.counted.stage, full.countedStage = nil, nil
		full.stepped.aggregate, full.counted.aggregate = nil, nil
		steppedErr := steppedOVG.addPortConnectionNodesAtBoundariesStepped(graph, tl, br, full.stepped)
		countedErr := countedOVG.addPortConnectionNodesAtBoundaries(graph, tl, br, full.counted)
		full.compare(t, "unlimited", steppedErr, countedErr)
		if !slices.Equal(sortedOVGNodePoints(steppedOVG), sortedOVGNodePoints(countedOVG)) {
			t.Fatalf("trial %d: counted node set differs from stepped node set", trial)
		}
		total := full.stepped.work
		for range 3 {
			limitedStepped, limitedCounted := twinOVGs(t, graph)
			limit := uint64(random.Int63n(int64(total) + 1))
			pair := newGuardPair(t, ctx, random, limit)
			steppedErr := limitedStepped.addPortConnectionNodesAtBoundariesStepped(graph, tl, br, pair.stepped)
			countedErr := limitedCounted.addPortConnectionNodesAtBoundaries(graph, tl, br, pair.counted)
			if (steppedErr == nil) != (countedErr == nil) || (steppedErr != nil && steppedErr.Error() != countedErr.Error()) {
				t.Fatalf("trial %d limit %d: error %v, want %v", trial, limit, countedErr, steppedErr)
			}
			if pair.stepped.work != pair.counted.work {
				t.Fatalf("trial %d limit %d: work %d, want %d", trial, limit, pair.counted.work, pair.stepped.work)
			}
			if steppedErr == nil {
				pair.compare(t, "limited", steppedErr, countedErr)
			}
		}
	}
}

func TestOVGNearScanMatchesPointNearGraphNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	random := rand.New(rand.NewSource(9))
	for range 200 {
		graph := fastPathTestGraph(random, 1+random.Intn(12))
		scan := newOVGNearScan(graph.Nodes)
		guard := newOVGBuildGuardForTest(ctx, t)
		for range 50 {
			point := geo.NewPoint(float64(random.Intn(900)-100), float64(random.Intn(900)-100))
			before := guard.work
			want, err := guard.pointNearGraphNode(graph, point)
			if err != nil {
				t.Fatal(err)
			}
			got, units := scan.nearCounted(point)
			if got != want || units != guard.work-before {
				t.Fatalf("point %v: near %v units %d, want %v %d", point, got, units, want, guard.work-before)
			}
		}
	}
}

func ovgEdgeEndpoints(ovg *OVG) [][2]geo.Point {
	edges := make([][2]geo.Point, len(ovg.Edges))
	for i, edge := range ovg.Edges {
		edges[i] = [2]geo.Point{*edge.From.Point, *edge.To.Point}
	}
	return edges
}

// connectNodes with the counted line connector must create the same edges in
// the same order with the same units as the stepped connector.
func TestOVGConnectNodesCountedMatchesStepped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	random := rand.New(rand.NewSource(13))
	for trial := range 120 {
		graph := fastPathTestGraph(random, 2+random.Intn(16))
		// A complete OVG node set: ports, intersections and boundary nodes.
		base := NewOVG(graph.Nodes)
		build := newOVGBuildGuardForTest(ctx, t)
		if err := base.addPorts(graph, build); err != nil {
			t.Fatal(err)
		}
		if err := base.addNodesIntersections(graph, build); err != nil {
			t.Fatal(err)
		}
		tl, br, err := build.tightBoundingBox(layoutgraph.Nodes(base.NodesInsideBoundingBox))
		if err != nil {
			t.Fatal(err)
		}
		if err := base.addPortConnectionNodesAtBoundaries(graph, tl, br, build); err != nil {
			t.Fatal(err)
		}
		twin := func() *OVG {
			ovg := NewOVG(graph.Nodes)
			ovg.Ports = base.Ports
			ovg.Nodes = slices.Clone(base.Nodes)
			ovg.OccupiedPoints = maps.Clone(base.OccupiedPoints)
			return ovg
		}
		for attempt := range 4 {
			stepped, counted := twin(), twin()
			limit := uint64(maxOVGWorkUnits)
			if attempt > 0 {
				limit = uint64(random.Intn(40000))
			}
			pair := newGuardPair(t, ctx, random, limit)
			if attempt == 0 {
				pair.stepped.stage, pair.steppedStage = nil, nil
				pair.counted.stage, pair.countedStage = nil, nil
				pair.stepped.aggregate, pair.counted.aggregate = nil, nil
			}
			steppedErr := stepped.connectNodesStepped(graph, pair.stepped)
			countedErr := counted.connectNodes(graph, pair.counted)
			pair.compare(t, "connectNodes", steppedErr, countedErr)
			if !slices.Equal(ovgEdgeEndpoints(stepped), ovgEdgeEndpoints(counted)) {
				t.Fatalf("trial %d attempt %d: counted edges differ from stepped edges", trial, attempt)
			}
			if attempt == 0 && len(stepped.Edges) == 0 {
				break
			}
		}
	}
}

// addEdgesNodes with counted fill points must add the same nodes in the same
// order with the same units as the stepped version.
func TestOVGEdgesNodesCountedMatchesStepped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	random := rand.New(rand.NewSource(19))
	for trial := range 120 {
		graph := fastPathTestGraph(random, 2+random.Intn(16))
		for range random.Intn(2 * len(graph.Nodes)) {
			from := graph.Nodes[random.Intn(len(graph.Nodes))]
			to := graph.Nodes[random.Intn(len(graph.Nodes))]
			if from != to {
				graph.Connect(from, to)
			}
		}
		if len(graph.Edges) == 0 {
			continue
		}
		hierarchies := map[*layoutgraph.Node]*layoutgraph.Hierarchy{}
		// One port index for both versions: its owner order comes from map
		// iteration and decides the units of early exits.
		indexOVG, _ := twinOVGs(t, graph)
		setup := newOVGBuildGuardForTest(ctx, t)
		fixedOverlaps, err := indexOVG.fixedOverlapsForBuild(graph, layoutgraph.Nodes(graph.Nodes), setup)
		if err != nil {
			t.Fatal(err)
		}
		portIndex, err := newOVGPortIndex(indexOVG.Ports, graph, fixedOverlaps, setup)
		if err != nil {
			t.Fatal(err)
		}
		var total uint64
		for attempt := range 4 {
			stepped, counted := twinOVGs(t, graph)
			stepped.Ports, counted.Ports = indexOVG.Ports, indexOVG.Ports
			limit := uint64(maxOVGWorkUnits)
			if attempt > 0 {
				limit = uint64(random.Int63n(int64(total) + 1))
			}
			pair := newGuardPair(t, ctx, random, limit)
			if attempt == 0 {
				pair.stepped.stage, pair.steppedStage = nil, nil
				pair.counted.stage, pair.countedStage = nil, nil
				pair.stepped.aggregate, pair.counted.aggregate = nil, nil
			}
			steppedErr := stepped.addEdgeFillPointsStepped(graph, hierarchies, portIndex, pair.stepped)
			countedErr := counted.addEdgeFillPoints(graph, hierarchies, portIndex, pair.counted)
			pair.compare(t, "addEdgesNodes", steppedErr, countedErr)
			if !slices.Equal(ovgNodePoints(stepped), ovgNodePoints(counted)) {
				t.Fatalf("trial %d attempt %d: counted nodes differ from stepped nodes", trial, attempt)
			}
			if attempt == 0 {
				total = pair.stepped.work
			}
		}
	}
}

// A panic while evaluating candidates on helper goroutines must not escape
// them; the caller then reruns the sequential loop, which panics in its own
// goroutine as the stepped loop would.
func TestOVGParallelCandidateEvaluationReportsPanics(t *testing.T) {
	broken := layoutgraph.NewNode(1, 10, 10) // no TopLeft: IsPointNear panics
	columns := []ovgCandidateColumn{
		{x: 0, nearNodes: nil},
		{x: 5, nearNodes: []*layoutgraph.Node{broken}},
	}
	rows := []ovgCandidateRow{{}, {}}
	view := &ovgPortView{}
	if _, ok := evaluateIntersectionCandidatesParallel(columns, rows, []float64{0, 5}, view, 2); ok {
		t.Fatal("parallel evaluation reported success despite a panicking candidate")
	}
	columns[1].nearNodes = nil
	results, ok := evaluateIntersectionCandidatesParallel(columns, rows, []float64{0, 5}, view, 2)
	if !ok || len(results) != 4 {
		t.Fatalf("parallel evaluation ok=%v results=%v", ok, results)
	}
	for i, result := range results {
		// No owners at all: pointNear (2 units) then one owner-merge step,
		// unobstructed because nothing is aligned.
		if result != 3<<1|1 {
			t.Fatalf("result %d = %d", i, result)
		}
	}
}

// The segment box must answer exactly as Node.PassesThrough, including
// inverted and NaN boxes, nodes without a position, touching and
// degenerate segments.
func TestOVGSegmentBoxMatchesPassesThrough(t *testing.T) {
	random := rand.New(rand.NewSource(29))
	value := func() float64 {
		switch random.Intn(12) {
		case 0:
			return math.NaN()
		case 1:
			return math.Copysign(0, -1)
		default:
			return float64(random.Intn(9)*5 - 10)
		}
	}
	for range 20000 {
		node := layoutgraph.NewNode(1, value(), value())
		if random.Intn(10) != 0 {
			node.TopLeft = geo.NewPoint(value(), value())
		}
		box := newOVGSegmentBox(node)
		p1 := geo.NewPoint(value(), value())
		p2 := geo.NewPoint(value(), value())
		if random.Intn(2) == 0 {
			p2.X = p1.X
		}
		if got, want := box.passesThrough(node, p1, p2), node.PassesThrough(p1, p2); got != want {
			t.Fatalf("node %v %vx%v segment %v-%v: %v, want %v", node.TopLeft, node.Width, node.Height, p1, p2, got, want)
		}
	}
}
