package placement

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/placementcost"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

// legacyFindClosestUnoccupiedDistance and legacyFindUnoccupied are the search
// as it was before the counted walk (checked set as a Go map); they are the
// reference the counted search must match exactly (distance, error, guard
// units and checked set).
func legacyFindClosestUnoccupiedDistance(optim *sizedOptimizer, node *layoutgraph.Node, p *geo.Point, minimizingSelf bool, checked map[geo.Point]struct{}, guard *limits.OptimizationWorkGuard) (float64, error) {
	if node == nil || node.Graph == nil || node.TopLeft == nil || p == nil {
		return 0, fmt.Errorf("TALA %s placement search requires a positioned node, graph, and point", guard.Location())
	}
	if optim.cellSize <= 0 || math.IsNaN(optim.cellSize) || math.IsInf(optim.cellSize, 0) {
		return 0, fmt.Errorf("TALA %s placement search requires a finite positive cell size", guard.Location())
	}
	if len(checked) > maxOptimizerPlacementCandidates {
		return 0, fmt.Errorf("%w: TALA %s checked placement count exceeds limit %d", limits.ErrOptimizationResourceLimit, guard.Location(), maxOptimizerPlacementCandidates)
	}
	cellSize := optim.cellSize
	for curr := 0.0; curr <= 100.0; curr++ {
		if err := guard.Step(); err != nil {
			return 0, err
		}
		currCell := curr * cellSize
		x := currCell
		y := 0.0
		for x >= -currCell {
			if err := guard.Step(); err != nil {
				return 0, err
			}
			if y != 0 {
				candidate := geo.Point{
					X: roundToNearestCellSize(p.X+x, cellSize),
					Y: roundToNearestCellSize(p.Y-y, cellSize),
				}
				if optim.fixedOrigin == nil || (candidate.X >= optim.fixedOrigin.X && candidate.Y >= optim.fixedOrigin.Y) {
					unoccupied, err := legacyFindUnoccupied(optim, x, -y, node.Width, node.Height, minimizingSelf, node, p, checked, guard)
					if err != nil {
						return 0, err
					}
					if unoccupied {
						return curr, nil
					}
				}
			}
			candidate := geo.Point{
				X: roundToNearestCellSize(p.X+x, cellSize),
				Y: roundToNearestCellSize(p.Y+y, cellSize),
			}
			if optim.fixedOrigin == nil || (candidate.X >= optim.fixedOrigin.X && candidate.Y >= optim.fixedOrigin.Y) {
				unoccupied, err := legacyFindUnoccupied(optim, x, y, node.Width, node.Height, minimizingSelf, node, p, checked, guard)
				if err != nil {
					return 0, err
				}
				if unoccupied {
					return curr, nil
				}
			}
			if x > 0 {
				y += cellSize
			} else {
				y -= cellSize
			}
			x -= cellSize
		}
	}
	return 0, fmt.Errorf("could not find closest unoccupied distance for p=%v, node=%s", p, node.DebugID())
}

func legacyFindUnoccupied(optim *sizedOptimizer, x, y, width, height float64, minimizingSelf bool, node *layoutgraph.Node, p *geo.Point, checked map[geo.Point]struct{}, guard *limits.OptimizationWorkGuard) (bool, error) {
	if err := guard.Step(); err != nil {
		return false, err
	}
	skip := false
	offset := geo.Point{X: x, Y: y}
	if checked != nil {
		if _, ok := checked[offset]; ok {
			skip = true
		} else {
			if len(checked) >= maxOptimizerPlacementCandidates {
				return false, fmt.Errorf("%w: TALA %s checked placement count exceeds limit %d", limits.ErrOptimizationResourceLimit, guard.Location(), maxOptimizerPlacementCandidates)
			}
			checked[offset] = struct{}{}
		}
	}
	if !skip {
		occupied, err := optim.isPointOccupiedGuarded(x, y, p, node, guard)
		if err != nil {
			return false, err
		}
		if !occupied {
			return true, nil
		}
	}
	if !minimizingSelf {
		return false, nil
	}
	for i := x - width; i <= x; i += optim.cellSize {
		for j := y - height; j <= y; j += optim.cellSize {
			if err := guard.Step(); err != nil {
				return false, err
			}
			if i == x && j == y {
				continue
			}
			if checked != nil {
				offset.X = i
				offset.Y = j
				if _, ok := checked[offset]; ok {
					continue
				}
				if len(checked) >= maxOptimizerPlacementCandidates {
					return false, fmt.Errorf("%w: TALA %s checked placement count exceeds limit %d", limits.ErrOptimizationResourceLimit, guard.Location(), maxOptimizerPlacementCandidates)
				}
				checked[offset] = struct{}{}
			}
			occupied, err := optim.isPointOccupiedGuarded(i, j, p, node, guard)
			if err != nil {
				return false, err
			}
			if !occupied {
				return true, nil
			}
		}
	}
	return false, nil
}

// randomSpacingGraph is randomOptimizerGraph with fractional sizes, outside
// labels (directional margins), loop offsets and tables, so DeltaTo depends
// on the orientation for many pairs.
func randomSpacingGraph(random *rand.Rand, nodes int, cellSize float64) (*layoutgraph.Graph, []*layoutgraph.Node) {
	g := layoutgraph.NewGraph()
	created := make([]*layoutgraph.Node, 0, nodes)
	positions := []label.Position{label.OutsideTopCenter, label.OutsideBottomLeft, label.OutsideLeftMiddle, label.OutsideRightTop}
	for i := 0; i < nodes; i++ {
		width := float64(10 + random.Intn(160))
		height := float64(10 + random.Intn(120))
		if random.Intn(3) == 0 {
			width += float64(random.Intn(8)) / 8
			height += 0.3 * float64(random.Intn(4))
		}
		node := layoutgraph.NewNode(layoutgraph.EntityID(i+1), width, height)
		node.TopLeft = geo.NewPoint(float64(random.Intn(50)*10-150), float64(random.Intn(50)*10-150))
		switch random.Intn(6) {
		case 0:
			node.Label = &layoutgraph.Label{Text: "x", Position: positions[random.Intn(len(positions))], Width: float64(5 + random.Intn(40)), Height: float64(5 + random.Intn(30))}
			node.Label.FixPosition()
			node.UpdateSpacing()
		case 1:
			node.LoopOffsets = map[geo.Orientation]float64{geo.Orientation(random.Intn(8)): float64(10 + random.Intn(60))}
		case 2:
			node.SetShape(shape.TABLE_TYPE)
		}
		if random.Intn(8) == 0 {
			node.TopLeft = nil
		}
		g.AddNodeUnchecked(node)
		created = append(created, node)
	}
	for i := 0; i < nodes; i++ {
		for j := i + 1; j < nodes; j++ {
			if random.Intn(4) == 0 {
				edge := g.Connect(created[i], created[j])
				edge.MinWidth = random.Intn(3) * 40
				edge.MinHeight = random.Intn(3) * 30
			}
		}
	}
	g.CellSize = cellSize
	return g, created
}

func checkedSetKeys(set *optimizerCheckedSet) []string {
	keys := make([]string, 0, set.len())
	for _, key := range set.keys {
		keys = append(keys, fmt.Sprintf("%v,%v", key.X, key.Y))
	}
	sort.Strings(keys)
	return keys
}

func checkedMapKeys(set map[geo.Point]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, fmt.Sprintf("%v,%v", key.X, key.Y))
	}
	sort.Strings(keys)
	return keys
}

func TestFindClosestUnoccupiedMatchesLegacy(t *testing.T) {
	random := rand.New(rand.NewSource(23))
	var failures, successes, notFound, counted int
	for trial := 0; trial < 1500; trial++ {
		cellSize := float64([]int{5, 10, 20, 30, 7}[random.Intn(5)])
		g, nodes := randomSpacingGraph(random, 2+random.Intn(18), cellSize)
		node := nodes[random.Intn(len(nodes))]
		if node.TopLeft == nil {
			node.TopLeft = geo.NewPoint(0, 0)
		}
		optim := &sizedOptimizer{g: g, cellSize: g.CellSize}
		if random.Intn(4) == 0 {
			optim.fixedOrigin = geo.NewPoint(float64(random.Intn(30)*10-200), float64(random.Intn(30)*10-200))
		}
		setupGuard, _ := limits.NewOptimizationWorkGuard(context.Background(), "LocalOptimize", limits.MaxOptimizationWorkUnits)
		if err := optim.rebuildSpatialIndex(setupGuard); err != nil {
			t.Fatal(err)
		}
		limit := uint64(math.MaxUint64)
		switch random.Intn(3) {
		case 0:
			limit = uint64(random.Intn(3000))
		case 1:
			limit = uint64(random.Intn(60000))
		}
		guard, _ := limits.NewOptimizationWorkGuard(context.Background(), "LocalOptimize", limit)
		legacyGuard, _ := limits.NewOptimizationWorkGuard(context.Background(), "LocalOptimize", limit)
		var set *optimizerCheckedSet
		var legacySet map[geo.Point]struct{}
		if random.Intn(2) == 0 {
			set = &optimizerCheckedSet{}
			legacySet = map[geo.Point]struct{}{}
		}
		// Two searches share the checked set, like the hub re-search.
		for call := 0; call < 2; call++ {
			p := geo.NewPoint(float64(random.Intn(80)*5-200), float64(random.Intn(80)*5-200))
			if random.Intn(3) == 0 {
				p = node.TopLeft.Copy()
			}
			minimizingSelf := random.Intn(5) != 0
			if call == 1 && len(node.Edges) > 0 && random.Intn(2) == 0 {
				// The hub re-search runs with fewer edges.
				node.Edges = node.Edges[:len(node.Edges)-1]
			}
			before := guard.Used()
			got, err := optim.findClosestUnoccupiedDistanceGuarded(node, p, minimizingSelf, set, guard)
			want, legacyErr := legacyFindClosestUnoccupiedDistance(optim, node, p, minimizingSelf, legacySet, legacyGuard)
			if got != want || fmt.Sprint(err) != fmt.Sprint(legacyErr) || guard.Used() != legacyGuard.Used() {
				t.Fatalf("trial %d call %d: got (%v, %v, %d units), legacy (%v, %v, %d units)", trial, call,
					got, err, guard.Used()-before, want, legacyErr, legacyGuard.Used()-before)
			}
			if set != nil && fmt.Sprint(checkedSetKeys(set)) != fmt.Sprint(checkedMapKeys(legacySet)) {
				t.Fatalf("trial %d call %d: checked sets differ", trial, call)
			}
			if err != nil {
				if errorsIsResourceLimit(err) {
					failures++
				} else {
					notFound++
				}
				break
			}
			successes++
			if optim.countedUnoccupiedUsable(node) {
				counted++
			}
		}
	}
	t.Logf("%d budget failures, %d successes (%d counted), %d other errors", failures, successes, counted, notFound)
	if failures == 0 || successes == 0 || counted == 0 {
		t.Errorf("exercised %d budget failures, %d successes (%d counted), %d other errors", failures, successes, counted, notFound)
	}
}

func errorsIsResourceLimit(err error) bool {
	for e := err; e != nil; {
		if e == limits.ErrOptimizationResourceLimit {
			return true
		}
		unwrapper, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = unwrapper.Unwrap()
	}
	return false
}

// TestScanTableMatchesOverlapScan compares the scan table with the stepped
// overlap scan on points all around nodes with directional spacing.
func TestScanTableMatchesOverlapScan(t *testing.T) {
	random := rand.New(rand.NewSource(5))
	var overlaps, clear int
	for trial := 0; trial < 400; trial++ {
		_, nodes := randomSpacingGraph(random, 2+random.Intn(14), 10)
		node := nodes[random.Intn(len(nodes))]
		if node.TopLeft == nil {
			node.TopLeft = geo.NewPoint(0, 0)
		}
		var table optimizerScanTable
		if !table.build(node) {
			t.Fatal("build failed")
		}
		for i := 0; i < 60; i++ {
			point := geo.NewPoint(float64(random.Intn(120)*5-300)+float64(random.Intn(3))/2, float64(random.Intn(120)*5-300))
			for _, exceptions := range [][]*layoutgraph.Node{nil, {node}} {
				want, wantUnits, complete := optimizerScanOverlap(node, point, exceptions)
				got, units, ok := table.overlaps(point.X, point.Y, uint64(len(exceptions)))
				if got != want || units != wantUnits || ok != complete {
					t.Fatalf("trial %d point %v exceptions %d: got (%v, %d, %v), scan (%v, %d, %v)", trial, point, len(exceptions), got, units, ok, want, wantUnits, complete)
				}
				if want {
					overlaps++
				} else {
					clear++
				}
			}
		}
	}
	if overlaps == 0 || clear == 0 {
		t.Errorf("exercised %d overlaps and %d clear scans", overlaps, clear)
	}
}

func TestOptimizerCheckedSetMatchesMap(t *testing.T) {
	random := rand.New(rand.NewSource(3))
	var set optimizerCheckedSet
	reference := map[geo.Point]struct{}{}
	values := []float64{0, math.Copysign(0, -1), 1, -1, 2.5, math.NaN(), 1e9, -7}
	for i := 0; i < 20000; i++ {
		x := values[random.Intn(len(values))] + float64(random.Intn(40))
		y := values[random.Intn(len(values))] - float64(random.Intn(40))
		_, want := reference[geo.Point{X: x, Y: y}]
		if got := set.has(x, y); got != want {
			t.Fatalf("has(%v, %v) = %v, map %v", x, y, got, want)
		}
		if !want {
			reference[geo.Point{X: x, Y: y}] = struct{}{}
			set.add(x, y)
		}
		if set.len() != len(reference) {
			t.Fatalf("len %d, map %d", set.len(), len(reference))
		}
		if random.Intn(500) == 0 {
			// Roll back to a random mark; the reference is rebuilt from the
			// surviving keys (a NaN key is a new map entry each time).
			set.truncate(random.Intn(set.len() + 1))
			reference = map[geo.Point]struct{}{}
			for _, key := range set.keys {
				reference[key] = struct{}{}
			}
			if set.len() != len(reference) {
				t.Fatalf("after truncate: len %d, map %d", set.len(), len(reference))
			}
		}
	}
}

// TestMoveNodeToBestMatchesStepped runs the counted moveNodeToBest (alone and
// after a closest-unoccupied search, as the optimizer calls it) and the
// stepped reference on identical graphs and compares the outcome, the error,
// the guard units and every node position.
func TestMoveNodeToBestMatchesStepped(t *testing.T) {
	var failures, moved, stayed int
	for trial := 0; trial < 600; trial++ {
		seed := int64(1000 + trial)
		build := func() (*sizedOptimizer, *layoutgraph.Node, []geo.Point, bool, uint64, bool, float64) {
			random := rand.New(rand.NewSource(seed))
			cellSize := float64([]int{5, 10, 20}[random.Intn(3)])
			g, nodes := randomSpacingGraph(random, 2+random.Intn(14), cellSize)
			for _, n := range nodes {
				if n.TopLeft == nil {
					n.TopLeft = geo.NewPoint(float64(random.Intn(50)*10-150), float64(random.Intn(50)*10-150))
				}
			}
			node := nodes[random.Intn(len(nodes))]
			optim := &sizedOptimizer{g: g, cellSize: g.CellSize, symmetryCost: g.CellSize}
			if random.Intn(4) == 0 {
				optim.fixedOrigin = geo.NewPoint(float64(random.Intn(30)*10-200), float64(random.Intn(30)*10-200))
			}
			if random.Intn(3) == 0 {
				optim.obstacles = []geo.Box{*geo.NewBox(geo.NewPoint(float64(random.Intn(40)*10-100), float64(random.Intn(40)*10-100)), 80, 60)}
			}
			var points []geo.Point
			for k := 0; k < 5+random.Intn(60); k++ {
				points = append(points, geo.Point{
					X: roundToNearestCellSize(node.TopLeft.X+float64(random.Intn(40)-20)*cellSize, cellSize),
					Y: roundToNearestCellSize(node.TopLeft.Y+float64(random.Intn(40)-20)*cellSize, cellSize),
				})
				if random.Intn(10) == 0 {
					points = append(points, *node.TopLeft)
				}
				if random.Intn(10) == 0 && len(points) > 1 {
					points = append(points, points[random.Intn(len(points)-1)])
				}
			}
			limit := uint64(math.MaxUint64)
			switch random.Intn(3) {
			case 0:
				limit = uint64(random.Intn(4000))
			case 1:
				limit = uint64(random.Intn(40000))
			}
			afterSearch := random.Intn(2) == 0
			median := roundToNearestCellSize(node.TopLeft.X+float64(random.Intn(10)-5)*cellSize, cellSize)
			return optim, node, points, random.Intn(2) == 0, limit, afterSearch, median
		}
		run := func(counted bool) (bool, error, uint64, []string) {
			optim, node, points, mustImprove, limit, afterSearch, median := build()
			setup, _ := limits.NewOptimizationWorkGuard(context.Background(), "LocalOptimize", limits.MaxOptimizationWorkUnits)
			if err := optim.rebuildSpatialIndex(setup); err != nil {
				t.Fatal(err)
			}
			guard, _ := limits.NewOptimizationWorkGuard(context.Background(), "LocalOptimize", limit)
			var movedResult bool
			var err error
			if afterSearch {
				p := geo.NewPoint(median, node.TopLeft.Y)
				if counted {
					_, err = optim.findClosestUnoccupiedDistanceGuarded(node, p, true, nil, guard)
				} else {
					_, err = optim.findClosestUnoccupiedDistanceStepped(node, p, true, nil, guard)
				}
			}
			if err == nil {
				switch {
				case counted && afterSearch:
					movedResult, err = optim.moveNodeToBestAfterSearch(context.Background(), node, points, mustImprove, guard)
				case counted:
					movedResult, err = optim.moveNodeToBestGuarded(context.Background(), node, points, mustImprove, guard)
				default:
					movedResult, err = optim.moveNodeToBestStepped(context.Background(), node, points, mustImprove, guard)
				}
			}
			var positions []string
			for _, n := range optim.g.Nodes {
				positions = append(positions, fmt.Sprintf("%v", n.TopLeft))
			}
			return movedResult, err, guard.Used(), positions
		}
		gotMoved, gotErr, gotUnits, gotPositions := run(true)
		wantMoved, wantErr, wantUnits, wantPositions := run(false)
		if gotMoved != wantMoved || fmt.Sprint(gotErr) != fmt.Sprint(wantErr) || gotUnits != wantUnits ||
			fmt.Sprint(gotPositions) != fmt.Sprint(wantPositions) {
			t.Fatalf("trial %d: got (%v, %v, %d, %v), stepped (%v, %v, %d, %v)", trial,
				gotMoved, gotErr, gotUnits, gotPositions, wantMoved, wantErr, wantUnits, wantPositions)
		}
		switch {
		case gotErr != nil:
			failures++
		case gotMoved:
			moved++
		default:
			stayed++
		}
	}
	t.Logf("%d failures, %d moved, %d stayed", failures, moved, stayed)
	if failures == 0 || moved == 0 || stayed == 0 {
		t.Errorf("exercised %d failures, %d moves, %d stays; want all", failures, moved, stayed)
	}
}

// TestFillPlacementPointsLargeNodesMatchesLegacy drives the row-wise counted
// generator with nodes many cells wide, fractional sizes and medians, fixed
// origins and tight budgets against the legacy generator.
func TestFillPlacementPointsLargeNodesMatchesLegacy(t *testing.T) {
	random := rand.New(rand.NewSource(41))
	failures, successes := 0, 0
	for trial := 0; trial < 300; trial++ {
		cellSize := float64([]int{3, 5, 10, 20}[random.Intn(4)])
		g, nodes := randomSpacingGraph(random, 2+random.Intn(6), cellSize)
		node := nodes[random.Intn(len(nodes))]
		if node.TopLeft == nil {
			node.TopLeft = geo.NewPoint(0, 0)
		}
		optim := &sizedOptimizer{g: g, cellSize: g.CellSize}
		if random.Intn(3) == 0 {
			optim.fixedOrigin = geo.NewPoint(float64(random.Intn(20)*10-100), float64(random.Intn(20)*10-100))
		}
		if random.Intn(4) == 0 {
			node.HerdAssignment = &layoutgraph.HerdAssignment{}
		}
		var fastScratch placementPointsScratch
		var legacyScratch legacyPlacementScratch
		limit := uint64(math.MaxUint64)
		if random.Intn(2) == 0 {
			limit = uint64(random.Intn(30000))
		}
		fastGuard, _ := limits.NewOptimizationWorkGuard(context.Background(), "LocalOptimize", limit)
		legacyGuard, _ := limits.NewOptimizationWorkGuard(context.Background(), "LocalOptimize", limit)
		for call := 0; call < 3; call++ {
			median := geo.NewPoint(float64(random.Intn(60)*5-150)+float64(random.Intn(4))/4, float64(random.Intn(60)*5-150))
			minUnocc := float64(random.Intn(6))
			minimizingSelf := random.Intn(4) != 0
			fastPoints, fastErr := optim.fillPlacementPointsGuarded(node, median, minUnocc, minimizingSelf, &fastScratch, fastGuard)
			legacyPoints, legacyErr := legacyFillPlacementPoints(optim, node, median, minUnocc, minimizingSelf, &legacyScratch, legacyGuard)
			if fmt.Sprint(fastErr) != fmt.Sprint(legacyErr) || fastGuard.Used() != legacyGuard.Used() {
				t.Fatalf("trial %d call %d: (%v, %d units), legacy (%v, %d units)", trial, call, fastErr, fastGuard.Used(), legacyErr, legacyGuard.Used())
			}
			if fastErr != nil {
				failures++
				break
			}
			successes++
			if fmt.Sprint(fastPoints) != fmt.Sprint(legacyPoints) {
				t.Fatalf("trial %d call %d: points differ\n%v\n%v", trial, call, fastPoints, legacyPoints)
			}
			if fastScratch.seenCount != len(legacyScratch.seen) {
				t.Fatalf("trial %d call %d: retained %d keys, legacy %d", trial, call, fastScratch.seenCount, len(legacyScratch.seen))
			}
		}
	}
	t.Logf("%d budget failures, %d successes", failures, successes)
	if failures == 0 || successes == 0 {
		t.Errorf("exercised %d budget failures and %d successes, want both", failures, successes)
	}
}

// TestSwapSearchAfterMoveMatchesFresh runs moveNodeToBest followed by the swap
// search, as the optimizer does, with the swap search reusing the node's own
// scores from moveNodeToBest, against the stepped moveNodeToBest followed by
// a swap search that scores everything, on identical graphs.
func TestSwapSearchAfterMoveMatchesFresh(t *testing.T) {
	var reused, swapped, failures int
	for trial := 0; trial < 500; trial++ {
		seed := int64(5000 + trial)
		run := func(fast bool) (string, uint64, string) {
			random := rand.New(rand.NewSource(seed))
			cellSize := float64([]int{5, 10, 20}[random.Intn(3)])
			g, nodes := randomSpacingGraph(random, 2+random.Intn(10), cellSize)
			for _, n := range nodes {
				if n.TopLeft == nil {
					n.TopLeft = geo.NewPoint(float64(random.Intn(50)*10-150), float64(random.Intn(50)*10-150))
				}
			}
			node := nodes[random.Intn(len(nodes))]
			optim := &sizedOptimizer{g: g, cellSize: g.CellSize, symmetryCost: g.CellSize, randGenerator: rand.New(rand.NewSource(seed))}
			var points []geo.Point
			for k := 0; k < 3+random.Intn(20); k++ {
				points = append(points, geo.Point{
					X: roundToNearestCellSize(node.TopLeft.X+float64(random.Intn(10)-5)*cellSize, cellSize),
					Y: roundToNearestCellSize(node.TopLeft.Y+float64(random.Intn(10)-5)*cellSize, cellSize),
				})
			}
			if random.Intn(2) == 0 {
				points = append(points, *node.TopLeft)
			}
			mustImprove := random.Intn(2) == 0
			limit := uint64(math.MaxUint64)
			if random.Intn(3) == 0 {
				limit = uint64(random.Intn(3000))
			}
			setup, _ := limits.NewOptimizationWorkGuard(context.Background(), "LocalOptimize", limits.MaxOptimizationWorkUnits)
			if err := optim.rebuildSpatialIndex(setup); err != nil {
				t.Fatal(err)
			}
			guard, _ := limits.NewOptimizationWorkGuard(context.Background(), "LocalOptimize", limit)
			var moved bool
			var err error
			if fast {
				moved, err = optim.moveNodeToBestGuarded(context.Background(), node, points, mustImprove, guard)
			} else {
				moved, err = optim.moveNodeToBestStepped(context.Background(), node, points, mustImprove, guard)
			}
			result := fmt.Sprintf("moved %v err %v", moved, err)
			if err == nil && !moved {
				if fast && optim.current.matches(node) {
					reused++
				}
				var candidate *layoutgraph.Node
				if fast {
					candidate, err = optim.bestSwapCandidateAfterMove(context.Background(), node, guard)
				} else {
					candidate, err = optim.bestSwapCandidateGuarded(context.Background(), node, guard)
				}
				result += fmt.Sprintf(" swap %v err %v", candidate.IDValue(), err)
			}
			var positions []string
			for _, n := range g.Nodes {
				positions = append(positions, fmt.Sprint(*n.TopLeft))
			}
			return result, guard.Used(), fmt.Sprint(positions)
		}
		gotResult, gotUnits, gotPositions := run(true)
		wantResult, wantUnits, wantPositions := run(false)
		if gotResult != wantResult || gotUnits != wantUnits || gotPositions != wantPositions {
			t.Fatalf("trial %d: got %s (%d units) %s, want %s (%d units) %s", trial, gotResult, gotUnits, gotPositions, wantResult, wantUnits, wantPositions)
		}
		if strings.Contains(gotResult, "err <nil> swap") && !strings.Contains(gotResult, "swap 0") {
			swapped++
		}
		if strings.Contains(gotResult, "exceeds limit") {
			failures++
		}
	}
	t.Logf("%d reuses, %d swaps, %d budget failures", reused, swapped, failures)
	if reused == 0 || failures == 0 {
		t.Errorf("exercised %d reuses and %d budget failures", reused, failures)
	}
}

// TestScorerMatchesNodeEdgeLength pins the equality the swap search relies on
// when it reuses moveNodeToBest's Score for NodeEdgeLength.
func TestScorerMatchesNodeEdgeLength(t *testing.T) {
	random := rand.New(rand.NewSource(17))
	for trial := 0; trial < 300; trial++ {
		_, nodes := randomSpacingGraph(random, 2+random.Intn(10), 10)
		for _, n := range nodes {
			if n.TopLeft == nil {
				n.TopLeft = geo.NewPoint(float64(random.Intn(50)*10-150), float64(random.Intn(50)*10-150))
			}
		}
		node := nodes[random.Intn(len(nodes))]
		options := placementcost.EdgeLengthOptions{IncludeNodeSizes: true, PenalizeDirection: true}
		scorer := placementcost.NewNodeEdgeLengthScorer(node, options)
		for k := 0; k < 4; k++ {
			node.TopLeft = geo.NewPoint(float64(random.Intn(50)*10-150), float64(random.Intn(50)*10-150))
			score, err := scorer.Score(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			want, err := placementcost.NodeEdgeLength(context.Background(), node, options)
			if err != nil {
				t.Fatal(err)
			}
			if math.Float64bits(score) != math.Float64bits(want) {
				t.Fatalf("trial %d: Score %v, NodeEdgeLength %v", trial, score, want)
			}
		}
		scorer.Close()
	}
}

// legacyOptimizerMedian is optimizerMedian as it was (sort.Slice always).
func legacyOptimizerMedian(nodes layoutgraph.Nodes, includeSizes bool, guard *limits.OptimizationWorkGuard) (float64, float64, error) {
	if len(nodes) == 1 {
		return optimizerMedian(nodes, includeSizes, guard)
	}
	orderedByX := make([]*layoutgraph.Node, len(nodes))
	orderedByY := make([]*layoutgraph.Node, len(nodes))
	for i, node := range nodes {
		if err := guard.Step(); err != nil {
			return 0, 0, err
		}
		if node == nil || node.TopLeft == nil {
			return 0, 0, fmt.Errorf("TALA %s cannot compute a median with an unpositioned neighbor", guard.Location())
		}
		orderedByX[i] = node
		orderedByY[i] = node
	}
	return optimizerMedianSorted(orderedByX, orderedByY, includeSizes, nodes[0], guard)
}

func TestOptimizerMedianMatchesLegacy(t *testing.T) {
	random := rand.New(rand.NewSource(29))
	values := []float64{0, math.Copysign(0, -1), 10, 10, 20, -5, 7.5, math.NaN(), math.Inf(1)}
	g := layoutgraph.NewGraph()
	g.CellSize = 10
	for trial := 0; trial < 20000; trial++ {
		count := 2 + random.Intn(10)
		pool := make([]*layoutgraph.Node, 0, count)
		var nodes layoutgraph.Nodes
		for len(nodes) < count {
			if len(pool) > 0 && random.Intn(4) == 0 {
				nodes = append(nodes, pool[random.Intn(len(pool))])
				continue
			}
			id := layoutgraph.EntityID(1 + random.Intn(6))
			node := layoutgraph.NewNode(id, values[random.Intn(len(values))], values[random.Intn(len(values))])
			node.TopLeft = geo.NewPoint(values[random.Intn(len(values))], values[random.Intn(len(values))])
			node.Graph = g
			if random.Intn(200) == 0 {
				node.TopLeft = nil
			}
			pool = append(pool, node)
			nodes = append(nodes, node)
		}
		includeSizes := random.Intn(2) == 0
		limit := uint64(math.MaxUint64)
		if random.Intn(4) == 0 {
			limit = uint64(random.Intn(40))
		}
		guard, _ := limits.NewOptimizationWorkGuard(context.Background(), "LocalOptimize", limit)
		legacyGuard, _ := limits.NewOptimizationWorkGuard(context.Background(), "LocalOptimize", limit)
		x, y, err := optimizerMedian(nodes, includeSizes, guard)
		wantX, wantY, wantErr := legacyOptimizerMedian(nodes, includeSizes, legacyGuard)
		if math.Float64bits(x) != math.Float64bits(wantX) || math.Float64bits(y) != math.Float64bits(wantY) ||
			fmt.Sprint(err) != fmt.Sprint(wantErr) || guard.Used() != legacyGuard.Used() {
			t.Fatalf("trial %d: got (%v, %v, %v, %d), legacy (%v, %v, %v, %d)", trial, x, y, err, guard.Used(), wantX, wantY, wantErr, legacyGuard.Used())
		}
	}
}

// TestSizelessOccupiedLogMatchesSnapshot runs sizeless optimize passes with
// the change log and with the former full copy of occupied on identical
// graphs, with budgets that fail some passes, and compares positions and the
// occupied map after every pass.
func TestSizelessOccupiedLogMatchesSnapshot(t *testing.T) {
	var failures, passes int
	for trial := 0; trial < 150; trial++ {
		seed := int64(9000 + trial)
		run := func(snapshot bool) []string {
			sizelessSnapshotOccupied = snapshot
			defer func() { sizelessSnapshotOccupied = false }()
			random := rand.New(rand.NewSource(seed))
			g, nodes := randomOptimizerGraph(random, 2+random.Intn(10))
			for _, n := range nodes {
				n.TopLeft = geo.NewPoint(float64(random.Intn(12)), float64(random.Intn(12)))
			}
			optim, err := newSizelessOptimizer(context.Background(), g, rand.New(rand.NewSource(seed)))
			if err != nil {
				t.Fatal(err)
			}
			var states []string
			for pass := 0; pass < 6; pass++ {
				limit := uint64(math.MaxUint64)
				if random.Intn(3) == 0 {
					limit = uint64(random.Intn(3000))
				}
				err := optim.optimizeWithLimit(context.Background(), 1.5, limit)
				var entries []string
				for key, node := range optim.occupied {
					entries = append(entries, fmt.Sprintf("%v=%d", key, node.IDValue()))
				}
				sort.Strings(entries)
				var positions []string
				for _, n := range g.Nodes {
					positions = append(positions, fmt.Sprint(*n.TopLeft))
				}
				states = append(states, fmt.Sprintf("err=%v occupied=%v positions=%v", err != nil, entries, positions))
			}
			return states
		}
		got := run(false)
		want := run(true)
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("trial %d pass %d:\n got %s\nwant %s", trial, i, got[i], want[i])
			}
			if strings.HasPrefix(got[i], "err=true") {
				failures++
			} else {
				passes++
			}
		}
	}
	t.Logf("%d failed passes, %d passes", failures, passes)
	if failures == 0 || passes == 0 {
		t.Errorf("exercised %d failed passes and %d passes", failures, passes)
	}
}
