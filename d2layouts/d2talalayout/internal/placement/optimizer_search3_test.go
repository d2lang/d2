package placement

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/grouping"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/hierarchy"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/placementcost"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/proximity"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/trees"
	"github.com/d2lang/d2/lib/geo"
)

// search3Case builds one random optimizer state for the moveNodeToBest and
// swap-search differential tests: a graph with directional margins, loop
// offsets and tables, a node to place, candidate points around it (with its
// own position and duplicates mixed in), and a work limit that is sometimes
// tight.
type search3Case struct {
	optim       *sizedOptimizer
	node        *layoutgraph.Node
	points      []geo.Point
	mustImprove bool
	limit       uint64
	afterSearch bool
	median      geo.Point
}

// rowSpacingGraph places equal-height nodes in a row with gaps of exactly
// NodeGap and connects only nodes that are not neighbours in the row, so
// neighbours are adjacent and can often trade places.
func rowSpacingGraph(random *rand.Rand, nodes int) (*layoutgraph.Graph, []*layoutgraph.Node) {
	g := layoutgraph.NewGraph()
	created := make([]*layoutgraph.Node, 0, nodes)
	x := float64(random.Intn(10)*20 - 100)
	for i := 0; i < nodes; i++ {
		width := float64(40 + 20*random.Intn(2))
		node := layoutgraph.NewNode(layoutgraph.EntityID(i+1), width, 40)
		node.TopLeft = geo.NewPoint(x, 0)
		x += width + layoutgraph.NodeGap
		g.AddNodeUnchecked(node)
		created = append(created, node)
	}
	for i := 0; i < nodes; i++ {
		for j := i + 2; j < nodes; j++ {
			if random.Intn(3) == 0 {
				g.Connect(created[i], created[j])
			}
		}
	}
	g.CellSize = layoutgraph.NodeGap
	return g, created
}

// crossingRowGraph places equal nodes in a row (gaps of NodeGap) and fixed
// anchors far below, each row node connected to the anchor under another
// row position, so trading places with a neighbour often shortens edges.
func crossingRowGraph(random *rand.Rand, nodes int) (*layoutgraph.Graph, []*layoutgraph.Node) {
	g := layoutgraph.NewGraph()
	row := make([]*layoutgraph.Node, 0, nodes)
	for i := 0; i < nodes; i++ {
		node := layoutgraph.NewNode(layoutgraph.EntityID(i+1), 40, 40)
		node.TopLeft = geo.NewPoint(float64(i)*(40+layoutgraph.NodeGap), 0)
		g.AddNodeUnchecked(node)
		row = append(row, node)
	}
	for i, j := range random.Perm(nodes) {
		anchor := layoutgraph.NewNode(layoutgraph.EntityID(nodes+i+1), 40, 40)
		anchor.TopLeft = geo.NewPoint(float64(j)*(40+layoutgraph.NodeGap), 400)
		anchor.FixedTopLeft = anchor.TopLeft.Copy()
		g.AddNodeUnchecked(anchor)
		g.Connect(row[i], anchor)
	}
	g.CellSize = layoutgraph.NodeGap
	return g, row
}

func buildSearch3Case(seed int64) search3Case {
	random := rand.New(rand.NewSource(seed))
	cellSize := float64([]int{5, 10, 20}[random.Intn(3)])
	var g *layoutgraph.Graph
	var nodes []*layoutgraph.Node
	if random.Intn(3) == 0 {
		g, nodes = rowSpacingGraph(random, 3+random.Intn(8))
		cellSize = g.CellSize
	} else {
		g, nodes = randomSpacingGraph(random, 2+random.Intn(14), cellSize)
	}
	for _, n := range nodes {
		if n.TopLeft == nil {
			n.TopLeft = geo.NewPoint(float64(random.Intn(50)*10-150), float64(random.Intn(50)*10-150))
		}
	}
	node := nodes[random.Intn(len(nodes))]
	optim := &sizedOptimizer{g: g, cellSize: g.CellSize, symmetryCost: g.CellSize, randGenerator: rand.New(rand.NewSource(seed))}
	if random.Intn(5) == 0 {
		optim.fixedOrigin = geo.NewPoint(float64(random.Intn(30)*10-200), float64(random.Intn(30)*10-200))
	}
	if random.Intn(3) == 0 {
		optim.obstacles = []geo.Box{*geo.NewBox(geo.NewPoint(float64(random.Intn(40)*10-100), float64(random.Intn(40)*10-100)), 80, 60)}
	}
	// A node at x = +0 is also offered -0, which compares equal but has other
	// bits (the optimizer's "current position" tests use ==, its score
	// records compare bits).
	negativeZero := random.Intn(5) == 0
	if negativeZero {
		node.TopLeft.X = 0
	}
	var points []geo.Point
	spread := 4 + random.Intn(20)
	for k := 0; k < 3+random.Intn(50); k++ {
		points = append(points, geo.Point{
			X: roundToNearestCellSize(node.TopLeft.X+float64(random.Intn(2*spread)-spread)*cellSize, cellSize),
			Y: roundToNearestCellSize(node.TopLeft.Y+float64(random.Intn(2*spread)-spread)*cellSize, cellSize),
		})
		if random.Intn(8) == 0 {
			points = append(points, *node.TopLeft)
		}
		if negativeZero && random.Intn(4) == 0 {
			points = append(points, geo.Point{X: math.Copysign(0, -1), Y: node.TopLeft.Y})
		}
		if random.Intn(10) == 0 && len(points) > 1 {
			points = append(points, points[random.Intn(len(points)-1)])
		}
	}
	limit := uint64(math.MaxUint64)
	switch random.Intn(4) {
	case 0:
		limit = uint64(random.Intn(4000))
	case 1:
		limit = uint64(random.Intn(40000))
	}
	median := geo.Point{
		X: roundToNearestCellSize(node.TopLeft.X+float64(random.Intn(10)-5)*cellSize, cellSize),
		Y: node.TopLeft.Y,
	}
	return search3Case{optim, node, points, random.Intn(2) == 0, limit, random.Intn(2) == 0, median}
}

// TestCountedMoveAndSwapMatchSteppedWithDone runs, on identical graphs, the
// optimizer's counted moveNodeToBest followed by the swap search that reuses
// its scores, with a context that has a Done channel (the only kind for which
// the sweep scorer and ScoreUnlessAbove change how scores are computed), and
// the stepped moveNodeToBest followed by a swap search that scores everything,
// with a context without one (every score in full, as before both). Outcome,
// error, guard units and every position must agree.
func TestCountedMoveAndSwapMatchSteppedWithDone(t *testing.T) {
	cancelable, cancel := context.WithCancel(context.Background())
	defer cancel()
	var reused, swapped, failures, moved int
	for trial := 0; trial < 1200; trial++ {
		seed := int64(7000 + trial)
		run := func(fast bool) (string, uint64, string) {
			ctx := context.Background()
			if fast {
				ctx = cancelable
			}
			c := buildSearch3Case(seed)
			optim, node := c.optim, c.node
			setup, _ := limits.NewOptimizationWorkGuard(ctx, "LocalOptimize", limits.MaxOptimizationWorkUnits)
			if err := optim.rebuildSpatialIndex(setup); err != nil {
				t.Fatal(err)
			}
			guard, _ := limits.NewOptimizationWorkGuard(ctx, "LocalOptimize", c.limit)
			var err error
			if c.afterSearch {
				if fast {
					_, err = optim.findClosestUnoccupiedDistanceGuarded(node, &c.median, true, nil, guard)
				} else {
					_, err = optim.findClosestUnoccupiedDistanceStepped(node, &c.median, true, nil, guard)
				}
			}
			var movedResult bool
			if err == nil {
				switch {
				case fast && c.afterSearch:
					movedResult, err = optim.moveNodeToBestAfterSearch(ctx, node, c.points, c.mustImprove, guard)
				case fast:
					movedResult, err = optim.moveNodeToBestGuarded(ctx, node, c.points, c.mustImprove, guard)
				default:
					movedResult, err = optim.moveNodeToBestStepped(ctx, node, c.points, c.mustImprove, guard)
				}
			}
			result := fmt.Sprintf("moved %v err %v", movedResult, err)
			if err == nil && !movedResult {
				if fast && optim.current.matches(node) {
					reused++
				}
				var candidate *layoutgraph.Node
				if fast {
					candidate, err = optim.bestSwapCandidateAfterMove(ctx, node, guard)
				} else {
					candidate, err = optim.bestSwapCandidateGuarded(ctx, node, guard)
				}
				result += fmt.Sprintf(" swap %v err %v", candidate.IDValue(), err)
				if fast && err == nil && candidate != nil {
					swapped++
				}
			}
			if fast && err == nil && movedResult {
				moved++
			}
			var positions []string
			for _, n := range optim.g.Nodes {
				positions = append(positions, fmt.Sprintf("%v", *n.TopLeft))
			}
			return result, guard.Used(), fmt.Sprint(positions)
		}
		gotResult, gotUnits, gotPositions := run(true)
		wantResult, wantUnits, wantPositions := run(false)
		if gotResult != wantResult || gotUnits != wantUnits || gotPositions != wantPositions {
			t.Fatalf("trial %d: got %s (%d units) %s, want %s (%d units) %s", trial, gotResult, gotUnits, gotPositions, wantResult, wantUnits, wantPositions)
		}
		if gotUnits >= buildSearch3Case(seed).limit {
			failures++
		}
	}
	t.Logf("%d reuses, %d swaps, %d moves, %d at the budget", reused, swapped, moved, failures)
	if reused == 0 || swapped == 0 || moved == 0 {
		t.Errorf("exercised %d reuses, %d swaps, %d moves", reused, swapped, moved)
	}
}

// TestCountedMoveRecordsFullScores checks the values the swap search reuses
// directly: after a counted moveNodeToBest that leaves the node in place,
// whatever optim.current holds for it equals a fresh NodeEdgeLength (and
// ColumnCrossingCost and NodeSymmetry at level all), bit for bit, with and
// without a Done channel.
func TestCountedMoveRecordsFullScores(t *testing.T) {
	cancelable, cancel := context.WithCancel(context.Background())
	defer cancel()
	var checked, checkedAll int
	for _, ctx := range []context.Context{cancelable, context.Background()} {
		for trial := 0; trial < 800; trial++ {
			c := buildSearch3Case(int64(9000 + trial))
			optim, node := c.optim, c.node
			setup, _ := limits.NewOptimizationWorkGuard(ctx, "LocalOptimize", limits.MaxOptimizationWorkUnits)
			if err := optim.rebuildSpatialIndex(setup); err != nil {
				t.Fatal(err)
			}
			guard, _ := limits.NewOptimizationWorkGuard(ctx, "LocalOptimize", limits.MaxOptimizationWorkUnits)
			moved, err := optim.moveNodeToBestGuarded(ctx, node, c.points, c.mustImprove, guard)
			if err != nil || moved || !optim.current.matches(node) {
				continue
			}
			options := placementcost.EdgeLengthOptions{IncludeNodeSizes: true, PenalizeDirection: true}
			want, err := placementcost.NodeEdgeLength(context.Background(), node, options)
			if err != nil {
				t.Fatal(err)
			}
			if math.Float64bits(optim.current.edgeLength) != math.Float64bits(want) {
				t.Fatalf("trial %d: recorded edge length %v, NodeEdgeLength %v", trial, optim.current.edgeLength, want)
			}
			checked++
			if optim.current.level == optimizerScoredAll {
				cc, err := placementcost.ColumnCrossingCost(context.Background(), node, nil)
				if err != nil {
					t.Fatal(err)
				}
				symmetry, err := placementcost.NodeSymmetry(context.Background(), node, nil)
				if err != nil {
					t.Fatal(err)
				}
				if math.Float64bits(optim.current.columnCrossing) != math.Float64bits(cc) || math.Float64bits(optim.current.symmetry) != math.Float64bits(symmetry) {
					t.Fatalf("trial %d: recorded (%v, %v), fresh (%v, %v)", trial, optim.current.columnCrossing, optim.current.symmetry, cc, symmetry)
				}
				checkedAll++
			}
		}
	}
	t.Logf("%d recorded scores checked, %d with all three", checked, checkedAll)
	if checked == 0 || checkedAll == 0 {
		t.Errorf("checked %d recorded scores, %d with all three", checked, checkedAll)
	}
}

// randomPlaceGraph builds a graph for the placement pipeline: top-level
// nodes, some containers with children, a hub with spokes, random edges
// (labeled, directed or not, between any two nodes that are not nested in
// each other).
func randomPlaceGraph(random *rand.Rand) *layoutgraph.Graph {
	g := layoutgraph.NewGraph()
	id := layoutgraph.EntityID(1)
	var all []*layoutgraph.Node
	add := func(container *layoutgraph.Node, width, height float64) *layoutgraph.Node {
		node := layoutgraph.NewNode(id, width, height)
		id++
		g.AddNewNodeToContainer(container, node)
		all = append(all, node)
		return node
	}
	top := 3 + random.Intn(9)
	for i := 0; i < top; i++ {
		node := add(nil, float64(20+10*random.Intn(12)), float64(20+10*random.Intn(8)))
		if random.Intn(5) == 0 {
			for k := 0; k < 1+random.Intn(4); k++ {
				add(node, float64(20+10*random.Intn(6)), float64(20+10*random.Intn(5)))
			}
		}
	}
	if random.Intn(2) == 0 {
		hub := add(nil, 60, 40)
		for k := 0; k < 4+random.Intn(5); k++ {
			g.Connect(hub, add(nil, 30, 30))
		}
	}
	for k := 0; k < len(all)+random.Intn(len(all)); k++ {
		a, b := all[random.Intn(len(all))], all[random.Intn(len(all))]
		if a == b || a.IsDescendantOf(b) || b.IsDescendantOf(a) {
			continue
		}
		edge := g.Connect(a, b)
		if random.Intn(2) == 0 {
			edge.TargetArrowhead = layoutgraph.TriangleArrowhead
		}
		if random.Intn(4) == 0 {
			edge.Label = &layoutgraph.Label{Text: "l", Width: float64(10 + random.Intn(30)), Height: 10}
		}
	}
	return g
}

// prepareForPlacement runs the engine's stages before node placement.
func prepareForPlacement(ctx context.Context, g *layoutgraph.Graph, seed int64) error {
	random := rand.New(rand.NewSource(seed))
	Prescale(g)
	if err := grouping.AddSequences(ctx, g, random); err != nil {
		return err
	}
	Prepare(g)
	if err := trees.Preprocess(ctx, g); err != nil {
		return err
	}
	if err := hierarchy.Assign(ctx, g, nil, hierarchy.Candidates(g)); err != nil {
		return err
	}
	if err := hierarchy.Place(ctx, g, nil, rand.New(rand.NewSource(seed))); err != nil {
		return err
	}
	hierarchy.RemoveIsolatedMemberships(g)
	if err := grouping.AddClusters(ctx, g, seed, random); err != nil {
		return err
	}
	return proximity.AddHubs(ctx, g)
}

// placedGeometry lists every node's geometry (graph nodes and descendants)
// as exact bits.
func placedGeometry(g *layoutgraph.Graph) string {
	var b strings.Builder
	for _, node := range g.Nodes {
		for _, n := range append([]*layoutgraph.Node{node}, g.AllDescendantNodes(node, true)...) {
			if n.TopLeft == nil {
				fmt.Fprintf(&b, "%d:nil %x %x;", n.ID, math.Float64bits(n.Width), math.Float64bits(n.Height))
				continue
			}
			fmt.Fprintf(&b, "%d:%x,%x %x %x;", n.ID, math.Float64bits(n.TopLeft.X), math.Float64bits(n.TopLeft.Y), math.Float64bits(n.Width), math.Float64bits(n.Height))
		}
	}
	return b.String()
}

// TestScoreMemoPlacementMatchesWithout places random graphs (containers,
// hubs, clusters found by the preprocessing, labels) with the sized
// optimizer's score memo on and off, with a context that has a Done channel,
// and requires bit-identical geometry and the same error.
func TestScoreMemoPlacementMatchesWithout(t *testing.T) {
	skipSlowUnderRace(t, 58)
	var placed, failed int
	on := scoreMemoStats{verify: true}
	for trial := 0; trial < 30; trial++ {
		seed := int64(300 + trial)
		run := func(stats *scoreMemoStats) (string, string) {
			g := randomPlaceGraph(rand.New(rand.NewSource(seed)))
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), scoreMemoContextKey{}, stats))
			defer cancel()
			if err := prepareForPlacement(ctx, g, seed); err != nil {
				return "prepare " + err.Error(), ""
			}
			err := Place(ctx, g, seed)
			return fmt.Sprint(err), placedGeometry(g)
		}
		gotErr, got := run(&on)
		wantErr, want := run(&scoreMemoStats{off: true})
		if gotErr != wantErr || got != want {
			t.Fatalf("trial %d: memo on (%s)\n%s\nmemo off (%s)\n%s", trial, gotErr, got, wantErr, want)
		}
		if gotErr == "<nil>" {
			placed++
		} else {
			failed++
		}
	}
	t.Logf("%d placed, %d failed; memo lookups %d, hits %d (%d lower bounds)", placed, failed, on.lookups, on.hits, on.lowerBounded)
	if on.mismatches != 0 {
		t.Fatalf("%d memo hits differ from a fresh evaluation; first: %s", on.mismatches, on.firstMismatch)
	}
	if placed == 0 || on.hits == 0 || on.lowerBounded == 0 {
		t.Errorf("exercised %d placements, %d hits, %d lower-bound hits", placed, on.hits, on.lowerBounded)
	}
}

// TestScoreMemoOptimizeCallsMatchWithout drives one sized optimizer through
// annealing and final passes, some under tight work limits (failed calls roll
// the graph back), with occasional external moves between calls (followed by
// invalidateScores, as the optimizer's owner does), with and without the
// memo on identical graphs, and compares every call's outcome, the positions
// and the random stream.
func TestScoreMemoOptimizeCallsMatchWithout(t *testing.T) {
	skipSlowUnderRace(t, 48)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls, failures int
	on := scoreMemoStats{verify: true}
	for trial := 0; trial < 80; trial++ {
		seed := int64(600 + trial)
		run := func(stats *scoreMemoStats) []string {
			random := rand.New(rand.NewSource(seed))
			var g *layoutgraph.Graph
			var nodes []*layoutgraph.Node
			switch random.Intn(3) {
			case 0:
				g, nodes = rowSpacingGraph(random, 3+random.Intn(8))
			case 1:
				g, nodes = crossingRowGraph(random, 3+random.Intn(6))
			default:
				g, nodes = randomSpacingGraph(random, 3+random.Intn(12), float64([]int{5, 10, 20}[random.Intn(3)]))
			}
			for _, n := range nodes {
				if n.TopLeft == nil {
					n.TopLeft = geo.NewPoint(float64(random.Intn(50)*10-150), float64(random.Intn(50)*10-150))
				}
				if random.Intn(4) == 0 {
					n.HerdAssignment = layoutgraph.NewHerdAssignment()
					n.HerdAssignment.Orientation = []geo.Orientation{geo.Top, geo.Bottom, geo.Left, geo.Right}[random.Intn(4)]
				}
			}
			g.TurnCost()
			optim, err := newSizedOptimizer(ctx, g, nil, nil, rand.New(rand.NewSource(seed)), nil)
			if err != nil {
				return []string{err.Error()}
			}
			optim.enableScoreMemo(context.WithValue(ctx, scoreMemoContextKey{}, stats))
			var log []string
			temps := []float64{2, 1.5, 1, 0.5, 0.2, 0, 0, 0, 0}
			for call, temp := range temps {
				limit := uint64(limits.MaxOptimizationWorkUnits)
				switch random.Intn(6) {
				case 0:
					limit = uint64(random.Intn(20000))
				case 1:
					limit = uint64(random.Intn(400000))
				}
				changed, err := optim.optimizeWithLimit(ctx, temp, limit)
				log = append(log, fmt.Sprintf("call %d: %v %v %s next %d", call, changed, err, positionsBits(g), optim.randGenerator.Int63()))
				switch random.Intn(6) {
				case 0:
					moved := nodes[random.Intn(len(nodes))]
					moved.TopLeft = geo.NewPoint(moved.TopLeft.X+g.CellSize, moved.TopLeft.Y)
					optim.invalidateScores()
				case 1, 2:
					// A stage that moves a node (or none) between calls.
					optim.beginExternalChanges()
					if random.Intn(2) == 0 {
						moved := nodes[random.Intn(len(nodes))]
						moved.TopLeft = geo.NewPoint(moved.TopLeft.X, moved.TopLeft.Y-g.CellSize)
					}
					optim.endExternalChanges()
				}
			}
			return log
		}
		got := run(&on)
		want := run(&scoreMemoStats{off: true})
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("trial %d:\nmemo on  %v\nmemo off %v", trial, got, want)
		}
		for _, line := range got {
			calls++
			if strings.Contains(line, "exceeds") || strings.Contains(line, "limit") {
				failures++
			}
		}
	}
	t.Logf("%d calls, %d failed; memo lookups %d, hits %d (%d lower bounds)", calls, failures, on.lookups, on.hits, on.lowerBounded)
	if on.mismatches != 0 {
		t.Fatalf("%d memo hits differ from a fresh evaluation; first: %s", on.mismatches, on.firstMismatch)
	}
	if failures == 0 || on.hits == 0 || on.lowerBounded == 0 {
		t.Errorf("exercised %d failed calls, %d hits, %d lower-bound hits", failures, on.hits, on.lowerBounded)
	}
}

func positionsBits(g *layoutgraph.Graph) string {
	var b strings.Builder
	for _, n := range g.Nodes {
		fmt.Fprintf(&b, "%x,%x;", math.Float64bits(n.TopLeft.X), math.Float64bits(n.TopLeft.Y))
	}
	return b.String()
}

// TestScoreMemoTableMatchesMap checks the memo's open-addressing table
// against a direct model: a Go map of entries, each valid until the epoch
// ends or a node other than its own moves; random stores (levels only grow,
// lower bounds only give way to full scores, other cost bits replace),
// lookups, moves and invalidations, with growth and rebuilds.
func TestScoreMemoTableMatchesMap(t *testing.T) {
	random := rand.New(rand.NewSource(5))
	nodes := make([]*layoutgraph.Node, 40)
	for i := range nodes {
		nodes[i] = layoutgraph.NewNode(layoutgraph.EntityID(i%7), 10, 10) // shared IDs collide in the hash
	}
	type key struct {
		node *layoutgraph.Node
		x, y uint64
	}
	type entry struct {
		slot  optimizerScoreSlot
		moves int // len(moves) when stored
	}
	var memo optimizerScoreMemo
	memo.enable(context.Background())
	reference := map[key]entry{}
	var moves []*layoutgraph.Node // movers since the epoch began
	valid := func(e entry) bool {
		for _, mover := range moves[e.moves:] {
			if mover != e.slot.node {
				return false
			}
		}
		return true
	}
	rebuilt := 0
	for op := 0; op < 300000; op++ {
		k := key{nodes[random.Intn(len(nodes))], uint64(random.Intn(30)), uint64(random.Intn(30))}
		turn, crossing := uint64(random.Intn(2)), uint64(random.Intn(2))
		switch r := random.Intn(1000); {
		case r < 3:
			memo.invalidate()
			reference = map[key]entry{}
			moves = moves[:0]
		case r < 60:
			mover := nodes[random.Intn(4)] // a few movers, often the same one again
			memo.moved(mover)
			moves = append(moves, mover)
		case r < 500:
			level := uint8(1 + random.Intn(3))
			score := float64(random.Intn(1000))
			slots := len(memo.slots)
			memo.store(k.node, k.x, k.y, turn, crossing, level, score, score+1, score+2)
			if len(memo.slots) != slots {
				rebuilt++
			}
			if old, ok := reference[k]; !ok || !valid(old) || old.slot.level < level || old.slot.turn != turn || old.slot.crossing != crossing {
				reference[k] = entry{optimizerScoreSlot{node: k.node, x: k.x, y: k.y, turn: turn, crossing: crossing, level: level, score: score, columnCrossing: score + 1, symmetry: score + 2}, len(moves)}
			}
		default:
			got := memo.lookup(k.node, k.x, k.y, turn, crossing)
			want, ok := reference[k]
			if !ok || !valid(want) || want.slot.turn != turn || want.slot.crossing != crossing {
				want = entry{}
			}
			got.version, got.clock = 0, 0
			if got != want.slot {
				t.Fatalf("op %d: lookup %+v, want %+v", op, got, want.slot)
			}
		}
	}
	if len(memo.slots) <= 64 || rebuilt == 0 {
		t.Errorf("table never grew (%d slots, %d rebuilds)", len(memo.slots), rebuilt)
	}
}

// TestScoreMemoBookkeepingRandomOperations applies random sequences of the
// sized optimizer's state-changing steps (moveNodeToBest with recordMove,
// commitSwap, transposeTracked, syncHerdFencesGuarded, reSearchHub, external
// stages with or without a move) to graphs with herds, scoring with the memo
// in verify mode: every memo hit is compared with a fresh evaluation, so any
// step whose bookkeeping lets a stale score survive is found.
func TestScoreMemoBookkeepingRandomOperations(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	stats := scoreMemoStats{verify: true}
	counts := map[string]int{}
	for trial := 0; trial < 120; trial++ {
		random := rand.New(rand.NewSource(int64(900 + trial)))
		var g *layoutgraph.Graph
		var nodes []*layoutgraph.Node
		switch random.Intn(3) {
		case 0:
			g, nodes = rowSpacingGraph(random, 3+random.Intn(8))
		case 1:
			g, nodes = crossingRowGraph(random, 3+random.Intn(6))
		default:
			g, nodes = randomSpacingGraph(random, 3+random.Intn(10), float64([]int{5, 10, 20}[random.Intn(3)]))
		}
		for _, n := range nodes {
			if n.TopLeft == nil {
				n.TopLeft = geo.NewPoint(float64(random.Intn(50)*10-150), float64(random.Intn(50)*10-150))
			}
			if random.Intn(3) == 0 {
				n.HerdAssignment = layoutgraph.NewHerdAssignment()
				n.HerdAssignment.Orientation = []geo.Orientation{geo.Top, geo.Bottom, geo.Left, geo.Right}[random.Intn(4)]
			}
		}
		g.TurnCost()
		ctx, txGuard, err := layoutgraph.EnsureTransactionWorkGuard(base, "LocalOptimizeTransactions")
		if err != nil {
			t.Fatal(err)
		}
		optim, err := newSizedOptimizer(ctx, g, nil, nil, rand.New(rand.NewSource(int64(trial))), nil)
		if err != nil {
			t.Fatal(err)
		}
		optim.enableScoreMemo(context.WithValue(ctx, scoreMemoContextKey{}, &stats))
		guard, _ := limits.NewOptimizationWorkGuard(ctx, "LocalOptimize", limits.MaxOptimizationWorkUnits)
		if _, err := captureOptimizerMutationStateInto(g, guard, &optim.mutationScratch); err != nil {
			t.Fatal(err)
		}
		optim.txGuard = txGuard
		pointsAround := func(node *layoutgraph.Node) []geo.Point {
			var points []geo.Point
			for k := 0; k < 4+random.Intn(30); k++ {
				points = append(points, geo.Point{
					X: roundToNearestCellSize(node.TopLeft.X+float64(random.Intn(12)-6)*g.CellSize, g.CellSize),
					Y: roundToNearestCellSize(node.TopLeft.Y+float64(random.Intn(12)-6)*g.CellSize, g.CellSize),
				})
				if random.Intn(5) == 0 {
					points = append(points, *node.TopLeft)
				}
			}
			return points
		}
		for op := 0; op < 40; op++ {
			node := nodes[random.Intn(len(nodes))]
			if err := optim.rebuildSpatialIndex(guard); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind := random.Intn(10); {
			case kind < 4:
				counts["move"]++
				points := pointsAround(node)
				if len(node.Edges) > 0 && random.Intn(2) == 0 {
					points = append(points, pointsAround(node.Adjacent(node.Edges[0]))...)
				}
				var moved bool
				moved, err = optim.moveNodeToBestGuarded(ctx, node, points, random.Intn(2) == 0, guard)
				if err == nil {
					optim.recordMove(node, moved)
					if !moved {
						// The swap search scores adjacent nodes in place
						// through the memo too.
						counts["swap search"]++
						_, err = optim.bestSwapCandidateAfterMove(ctx, node, guard)
					}
				}
			case kind < 5:
				counts["swap"]++
				// Mostly with a neighbour, whose position node's scores read,
				// then node is scored again around both places.
				other := nodes[random.Intn(len(nodes))]
				if len(node.Edges) > 0 && random.Intn(4) != 0 {
					other = node.Adjacent(node.Edges[random.Intn(len(node.Edges))])
				}
				if other != node && other.FixedTopLeft == nil {
					points := append(pointsAround(node), pointsAround(other)...)
					err = optim.commitSwap(node, other, guard)
					if err == nil {
						if err = optim.rebuildSpatialIndex(guard); err == nil {
							var moved bool
							moved, err = optim.moveNodeToBestGuarded(ctx, node, points, true, guard)
							if err == nil {
								optim.recordMove(node, moved)
							}
						}
					}
				}
			case kind < 6:
				counts["transpose"]++
				_, err = optim.transposeTracked(ctx, node)
			case kind < 7:
				counts["herds"]++
				before := g.Nodes[0].TopLeft.Copy()
				g.Nodes[0].TopLeft = geo.NewPoint(before.X-g.CellSize, before.Y)
				optim.recordMove(g.Nodes[0], true)
				err = optim.syncHerdFencesGuarded(guard)
			case kind < 8:
				counts["hub"]++
				var spokes []*layoutgraph.Node
				for _, e := range node.Edges {
					if random.Intn(2) == 0 {
						spokes = append(spokes, node.Adjacent(e))
					}
				}
				err = optim.reSearchHub(ctx, node, spokes, 1, nil, true, nil, guard)
			default:
				counts["stage"]++
				optim.beginExternalChanges()
				if random.Intn(2) == 0 {
					node.TopLeft = geo.NewPoint(node.TopLeft.X, node.TopLeft.Y+g.CellSize)
				}
				optim.endExternalChanges()
			}
			if err != nil {
				// The optimizer abandons a pass that fails (its owner
				// restores the graph and the memo starts a new epoch).
				optim.scores.invalidate()
			}
		}
		optim.txGuard = nil
		optim.mutationScratch.release()
	}
	t.Logf("operations %v; memo lookups %d, hits %d (%d lower bounds)", counts, stats.lookups, stats.hits, stats.lowerBounded)
	if stats.mismatches != 0 {
		t.Fatalf("%d memo hits differ from a fresh evaluation; first: %s", stats.mismatches, stats.firstMismatch)
	}
	if stats.hits == 0 || stats.lowerBounded == 0 {
		t.Errorf("exercised %d hits, %d lower-bound hits", stats.hits, stats.lowerBounded)
	}
}

// nestedMoveGraph builds a graph whose nodes carry descendants: nested
// containers, a cluster vessel with members, a sequence vessel, and
// optionally a nil child; every node is placed (some members unplaced when
// unplaced is set).
func nestedMoveGraph(random *rand.Rand, unplaced, nilChild bool) (*layoutgraph.Graph, []*layoutgraph.Node) {
	g := layoutgraph.NewGraph()
	id := layoutgraph.EntityID(1)
	var all []*layoutgraph.Node
	add := func(container *layoutgraph.Node) *layoutgraph.Node {
		node := layoutgraph.NewNode(id, float64(10+random.Intn(60)), float64(10+random.Intn(40)))
		id++
		node.TopLeft = geo.NewPoint(float64(random.Intn(40)*10-200)+float64(random.Intn(3))/4, float64(random.Intn(40)*10-200))
		g.AddNewNodeToContainer(container, node)
		all = append(all, node)
		return node
	}
	for i := 0; i < 2+random.Intn(4); i++ {
		top := add(nil)
		for k := 0; k < random.Intn(4); k++ {
			child := add(top)
			for j := 0; j < random.Intn(3); j++ {
				add(child)
			}
		}
	}
	if random.Intn(2) == 0 {
		members := []*layoutgraph.Node{add(nil), add(nil)}
		cluster := &layoutgraph.Cluster{Nodes: members, Graph: g, Arrangement: layoutgraph.Row, DesiredArrangement: layoutgraph.Row}
		cluster.Vessel = grouping.CreateVessel(cluster, id)
		id++
		grouping.AddCluster(g, cluster)
		cluster.Vessel.TopLeft = geo.NewPoint(0, 0)
		all = append(all, cluster.Vessel)
	}
	if random.Intn(3) == 0 {
		vessel := add(nil)
		g.Sequences[vessel] = &layoutgraph.Sequence{Vessel: vessel, Nodes: []*layoutgraph.Node{add(nil), add(nil), add(nil)}, Graph: g}
	}
	if unplaced {
		all[random.Intn(len(all))].TopLeft = nil
	}
	if nilChild {
		parent := all[random.Intn(len(all))]
		if parent.IsContainer() {
			g.Containers[parent] = append(g.Containers[parent], nil)
		}
	}
	return g, all
}

func snapshotsString(snapshots []nodePositionSnapshot) string {
	var b strings.Builder
	for _, snapshot := range snapshots {
		fmt.Fprintf(&b, "%d %p %v;", snapshot.node.ID, snapshot.topLeft.pointer, snapshot.topLeft.value)
	}
	return b.String()
}

func allPositions(nodes []*layoutgraph.Node) string {
	var b strings.Builder
	for _, n := range nodes {
		if n.TopLeft == nil {
			b.WriteString("nil;")
			continue
		}
		fmt.Fprintf(&b, "%x,%x;", math.Float64bits(n.TopLeft.X), math.Float64bits(n.TopLeft.Y))
	}
	return b.String()
}

// TestMoveScratchMatchesAllocatingHelpers runs the scratch-based descendant
// walk, capture, candidate movement, absolute move and position swap against
// optimizerDescendants, captureOptimizerNodePositions,
// captureOptimizerCandidateMovement, optimizerMoveNodeAbs and
// withOptimizerPositionsSwapped on identical graphs with containers, cluster
// and sequence vessels, unplaced members, nil children and tight budgets, with
// one scratch reused throughout: results, errors, guard units and positions.
func TestMoveScratchMatchesAllocatingHelpers(t *testing.T) {
	var scratch optimizerMoveScratch
	var failures, operations int
	for trial := 0; trial < 400; trial++ {
		seed := int64(1300 + trial)
		build := func() (*rand.Rand, *layoutgraph.Graph, []*layoutgraph.Node) {
			random := rand.New(rand.NewSource(seed))
			g, all := nestedMoveGraph(random, random.Intn(6) == 0, random.Intn(8) == 0)
			return random, g, all
		}
		randomA, _, allA := build()
		randomB, _, allB := build()
		for op := 0; op < 12; op++ {
			kind := randomA.Intn(5)
			randomB.Intn(5)
			i, j := randomA.Intn(len(allA)), randomA.Intn(len(allA))
			randomB.Intn(len(allB))
			randomB.Intn(len(allB))
			limit := uint64(math.MaxUint64)
			if randomA.Intn(3) == 0 {
				limit = uint64(randomA.Intn(40))
				randomB.Intn(3)
				randomB.Intn(40)
			} else {
				randomB.Intn(3)
			}
			dx, dy := float64(randomA.Intn(9)-4)*10, float64(randomA.Intn(9)-4)*10
			randomB.Intn(9)
			randomB.Intn(9)
			guardA, _ := limits.NewOptimizationWorkGuard(context.Background(), "LocalOptimize", limit)
			guardB, _ := limits.NewOptimizationWorkGuard(context.Background(), "LocalOptimize", limit)
			var got, want string
			switch kind {
			case 0:
				d, err := scratch.descendantsInto(allA[i], guardA)
				got = fmt.Sprint(len(d), nodeIDs(d), err)
				d, err = optimizerDescendants(allB[i], guardB)
				want = fmt.Sprint(len(d), nodeIDs(d), err)
			case 1:
				nodesA := []*layoutgraph.Node{allA[i], allA[j]}[:1+randomA.Intn(2)]
				nodesB := []*layoutgraph.Node{allB[i], allB[j]}[:1+randomB.Intn(2)]
				s, err := scratch.captureInto(nodesA, scratch.swapped, guardA)
				got = fmt.Sprint(snapshotsString(s), err)
				s, err = captureOptimizerNodePositions(nodesB, guardB)
				want = fmt.Sprint(snapshotsString(s), err)
				// The snapshots point to different graphs: compare node IDs and values only.
				got, want = stripPointers(got), stripPointers(want)
			case 2:
				m, err := scratch.candidateMovement(allA[i], guardA)
				got = fmt.Sprint(stripPointers(snapshotsString(m.positions)), m.descendantWork, err)
				m, err = captureOptimizerCandidateMovement(allB[i], guardB)
				want = fmt.Sprint(stripPointers(snapshotsString(m.positions)), m.descendantWork, err)
			case 3:
				if allA[i].TopLeft == nil {
					continue
				}
				errA := scratch.moveAbs(allA[i], allA[i].TopLeft.X+dx, allA[i].TopLeft.Y+dy, guardA)
				errB := optimizerMoveNodeAbs(allB[i], allB[i].TopLeft.X+dx, allB[i].TopLeft.Y+dy, guardB)
				got, want = fmt.Sprint(errA), fmt.Sprint(errB)
			case 4:
				var insideA, insideB string
				failCallback := randomA.Intn(4) == 0
				randomB.Intn(4)
				errA := scratch.withPositionsSwapped(allA[i], allA[j], guardA, func() error {
					insideA = allPositions(allA)
					if failCallback {
						return fmt.Errorf("callback")
					}
					return nil
				})
				errB := withOptimizerPositionsSwapped(allB[i], allB[j], guardB, func() error {
					insideB = allPositions(allB)
					if failCallback {
						return fmt.Errorf("callback")
					}
					return nil
				})
				got, want = fmt.Sprint(insideA, errA), fmt.Sprint(insideB, errB)
			}
			operations++
			got += fmt.Sprintf(" units %d positions %s", guardA.Used(), allPositions(allA))
			want += fmt.Sprintf(" units %d positions %s", guardB.Used(), allPositions(allB))
			if got != want {
				t.Fatalf("trial %d op %d kind %d:\nscratch %s\nlegacy  %s", trial, op, kind, got, want)
			}
			if strings.Contains(got, "TALA") {
				failures++
			}
		}
	}
	t.Logf("%d operations, %d failed", operations, failures)
	if failures == 0 {
		t.Errorf("no operation failed")
	}
}

func nodeIDs(nodes []*layoutgraph.Node) []layoutgraph.EntityID {
	ids := make([]layoutgraph.EntityID, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}

func stripPointers(s string) string {
	fields := strings.Fields(s)
	for i, f := range fields {
		if strings.HasPrefix(f, "0x") {
			fields[i] = "ptr"
		}
	}
	return strings.Join(fields, " ")
}

// levelGraph returns a graph of the top-level nodes of g sharing g's
// containers, clusters and sequences, as placeNodes builds the graph it
// optimizes: descendants are not its nodes. Every node's Graph is the level
// graph.
func levelGraph(g *layoutgraph.Graph, all []*layoutgraph.Node) (*layoutgraph.Graph, []*layoutgraph.Node) {
	level := layoutgraph.NewGraph()
	level.CopyEntitiesFrom(g)
	var top []*layoutgraph.Node
	for _, n := range g.Containers[nil] {
		level.AddNodeUnchecked(n)
		top = append(top, n)
	}
	for _, n := range all {
		n.Graph = level
	}
	level.CellSize = 10
	return level, top
}

// TestCountedMoveWithDescendantsMatchesStepped runs moveNodeToBest of
// containers, cluster and sequence vessels (whose descendants move with them
// and are not nodes of the optimized level) through the counted path and
// through the stepped reference on identical graphs: outcome, error, guard
// units, every position (descendants included), with and without a
// preceding closest-unoccupied search and with tight budgets.
func TestCountedMoveWithDescendantsMatchesStepped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var counted, failures, moved int
	for trial := 0; trial < 500; trial++ {
		seed := int64(2100 + trial)
		run := func(fast bool) (string, uint64) {
			random := rand.New(rand.NewSource(seed))
			full, all := nestedMoveGraph(random, false, false)
			g, top := levelGraph(full, all)
			var node *layoutgraph.Node
			for _, k := range random.Perm(len(top)) {
				if top[k].IsContainer() || top[k].IsClusterVessel() || g.Sequences[top[k]] != nil {
					node = top[k]
					break
				}
			}
			if node == nil {
				return "no container", 0
			}
			for _, n := range all {
				if n.TopLeft == nil {
					n.TopLeft = geo.NewPoint(0, 0)
				}
				n.TopLeft = geo.NewPoint(roundToNearestCellSize(n.TopLeft.X, 10), roundToNearestCellSize(n.TopLeft.Y, 10))
			}
			optim := &sizedOptimizer{g: g, cellSize: g.CellSize, symmetryCost: g.CellSize, randGenerator: rand.New(rand.NewSource(seed))}
			var points []geo.Point
			for k := 0; k < 3+random.Intn(40); k++ {
				points = append(points, geo.Point{
					X: roundToNearestCellSize(node.TopLeft.X+float64(random.Intn(30)-15)*10, 10),
					Y: roundToNearestCellSize(node.TopLeft.Y+float64(random.Intn(30)-15)*10, 10),
				})
				if random.Intn(8) == 0 {
					points = append(points, *node.TopLeft)
				}
			}
			limit := uint64(math.MaxUint64)
			switch random.Intn(3) {
			case 0:
				limit = uint64(random.Intn(1500))
			case 1:
				limit = uint64(random.Intn(30000))
			}
			mustImprove := random.Intn(2) == 0
			afterSearch := random.Intn(2) == 0
			setup, _ := limits.NewOptimizationWorkGuard(ctx, "LocalOptimize", limits.MaxOptimizationWorkUnits)
			if err := optim.rebuildSpatialIndex(setup); err != nil {
				return err.Error(), 0
			}
			guard, _ := limits.NewOptimizationWorkGuard(ctx, "LocalOptimize", limit)
			var err error
			if afterSearch {
				p := geo.NewPoint(node.TopLeft.X+10, node.TopLeft.Y)
				if fast {
					_, err = optim.findClosestUnoccupiedDistanceGuarded(node, p, false, nil, guard)
				} else {
					_, err = optim.findClosestUnoccupiedDistanceStepped(node, p, false, nil, guard)
				}
			}
			var movedResult bool
			if err == nil {
				switch {
				case fast && afterSearch:
					movedResult, err = optim.moveNodeToBestAfterSearch(ctx, node, points, mustImprove, guard)
				case fast:
					movedResult, err = optim.moveNodeToBestGuarded(ctx, node, points, mustImprove, guard)
				default:
					movedResult, err = optim.moveNodeToBestStepped(ctx, node, points, mustImprove, guard)
				}
			}
			if fast && err == nil {
				counted++
				if movedResult {
					moved++
				}
			}
			return fmt.Sprintf("moved %v err %v exact %v positions %s", movedResult, err, optim.moveExact, allPositions(all)), guard.Used()
		}
		got, gotUnits := run(true)
		want, wantUnits := run(false)
		if got != want || gotUnits != wantUnits {
			t.Fatalf("trial %d:\ncounted %s (%d units)\nstepped %s (%d units)", trial, got, gotUnits, want, wantUnits)
		}
		if strings.Contains(got, "TALA") || strings.Contains(got, "exceeds") {
			failures++
		}
	}
	t.Logf("%d counted runs (%d moved), %d failures", counted, moved, failures)
	if counted == 0 || moved == 0 || failures == 0 {
		t.Errorf("exercised %d counted runs, %d moves, %d failures", counted, moved, failures)
	}
}
