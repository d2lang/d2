package placement

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"math/rand"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

// legacyPlacementScratch and legacyFillPlacementPoints are the generator as it
// was before unit counting and the window bitset; they are the reference the
// optimized generator must match exactly (points, errors and guard units).
type legacyPlacementScratch struct {
	seen   map[uint64]struct{}
	points []geo.Point
}

func legacyFillPlacementPoints(optim *sizedOptimizer, node *layoutgraph.Node, median *geo.Point, minUnocc float64, minimizingSelf bool, scratch *legacyPlacementScratch, guard *limits.OptimizationWorkGuard) ([]geo.Point, error) {
	if scratch.seen == nil {
		scratch.seen = make(map[uint64]struct{}, 128)
	}
	for k := range scratch.seen {
		if err := guard.Step(); err != nil {
			return nil, err
		}
		delete(scratch.seen, k)
	}
	scratch.points = scratch.points[:0]
	dist := minUnocc + 1
	var addErr error
	add := func(offX, offY float64) bool {
		if addErr != nil {
			return true
		}
		if err := guard.Step(); err != nil {
			addErr = err
			return true
		}
		pX := roundToNearestCellSize(median.X+offX, optim.cellSize)
		pY := roundToNearestCellSize(median.Y+offY, optim.cellSize)
		if fo := optim.fixedOrigin; fo != nil && (pX < fo.X || pY < fo.Y) {
			return false
		}
		ix := int64(pX / optim.cellSize)
		iy := int64(pY / optim.cellSize)
		key := uint64(uint32(ix))<<32 | uint64(uint32(iy))
		if _, dup := scratch.seen[key]; dup {
			return false
		}
		if len(scratch.points) >= maxOptimizerPlacementCandidates {
			addErr = fmt.Errorf("%w: TALA LocalOptimize placement candidates exceed limit %d", limits.ErrOptimizationResourceLimit, maxOptimizerPlacementCandidates)
			return true
		}
		scratch.seen[key] = struct{}{}
		scratch.points = append(scratch.points, geo.Point{X: pX, Y: pY})
		return false
	}
	for x := dist; x >= -dist; x-- {
		if err := guard.Step(); err != nil {
			return nil, err
		}
		maxY := dist - math.Abs(x)
		for y := 0.0; y <= maxY; y++ {
			if err := guard.Step(); err != nil {
				return nil, err
			}
			if y != 0 {
				iterPlacementsAroundPoint(node, x, -y, minimizingSelf, add)
			}
			iterPlacementsAroundPoint(node, x, y, minimizingSelf, add)
			if addErr != nil {
				return nil, addErr
			}
		}
	}
	if data := node.LongDistanceNeighborRequirements; data != nil {
		cell := float64(int(optim.cellSize))
		neighbors := make([]*layoutgraph.Node, 0, len(data))
		for adjacent := range data {
			if err := guard.Step(); err != nil {
				return nil, err
			}
			neighbors = append(neighbors, adjacent)
		}
		if err := guard.AddSort(len(neighbors)); err != nil {
			return nil, err
		}
		slices.SortFunc(neighbors, func(a, b *layoutgraph.Node) int {
			return cmp.Compare(a.IDValue(), b.IDValue())
		})
		for _, adj := range neighbors {
			if err := guard.Step(); err != nil {
				return nil, err
			}
			requirements := data[adj]
			if adj == nil || adj.TopLeft == nil {
				return nil, fmt.Errorf("TALA %s found an unpositioned long-distance neighbor", guard.Location())
			}
			maxW := requirements.MaxWidth
			maxH := requirements.MaxHeight
			ax := adj.TopLeft.X - median.X
			ay := adj.TopLeft.Y - median.Y
			if maxW > int(cell) {
				add(math.Floor((ax-node.Width-float64(maxW))/cell)*cell, ay)
				add(math.Ceil((ax+adj.Width+float64(maxW))/cell)*cell, ay)
			}
			if maxH > int(cell) {
				add(ax, math.Floor((ay-node.Height-float64(maxH))/cell)*cell)
				add(ax, math.Ceil((ay+adj.Height+float64(maxH))/cell)*cell)
			}
			if addErr != nil {
				return nil, addErr
			}
		}
	}
	if node.HerdAssignment != nil {
		scratch.points = append(scratch.points, *node.TopLeft)
	}
	if err := guard.Add(uint64(len(scratch.points))); err != nil {
		return nil, err
	}
	return scratch.points, nil
}

func randomOptimizerGraph(random *rand.Rand, nodes int) (*layoutgraph.Graph, []*layoutgraph.Node) {
	g := layoutgraph.NewGraph()
	created := make([]*layoutgraph.Node, 0, nodes)
	for i := 0; i < nodes; i++ {
		node := layoutgraph.NewNode(layoutgraph.EntityID(i+1), float64(10+random.Intn(120)), float64(10+random.Intn(90)))
		node.TopLeft = geo.NewPoint(float64(random.Intn(40)*10-100), float64(random.Intn(40)*10-100))
		g.AddNodeUnchecked(node)
		created = append(created, node)
	}
	for i := 0; i < nodes; i++ {
		for j := i + 1; j < nodes; j++ {
			if random.Intn(4) == 0 {
				edge := g.Connect(created[i], created[j])
				edge.MinWidth = random.Intn(3) * 40
				edge.MinHeight = random.Intn(3) * 40
			}
		}
	}
	g.ComputeCellSize()
	return g, created
}

func TestFillPlacementPointsMatchesLegacy(t *testing.T) {
	random := rand.New(rand.NewSource(7))
	failures, successes := 0, 0
	defer func() {
		if failures == 0 || successes == 0 {
			t.Errorf("exercised %d budget failures and %d successes, want both", failures, successes)
		}
	}()
	for trial := 0; trial < 300; trial++ {
		g, nodes := randomOptimizerGraph(random, 2+random.Intn(8))
		optim := &sizedOptimizer{g: g, cellSize: g.CellSize}
		if random.Intn(3) == 0 {
			optim.fixedOrigin = geo.NewPoint(float64(random.Intn(20)*10-100), float64(random.Intn(20)*10-100))
		}
		node := nodes[random.Intn(len(nodes))]
		if random.Intn(3) == 0 {
			node.LongDistanceNeighborRequirements = map[*layoutgraph.Node]layoutgraph.LongDistanceNeighborRequirements{}
			for _, other := range nodes {
				if other != node && random.Intn(2) == 0 {
					node.LongDistanceNeighborRequirements[other] = layoutgraph.LongDistanceNeighborRequirements{
						EdgeCount: 3, MaxWidth: random.Intn(200), MaxHeight: random.Intn(200),
					}
				}
			}
		}
		if random.Intn(4) == 0 {
			node.HerdAssignment = &layoutgraph.HerdAssignment{}
		}
		var fastScratch placementPointsScratch
		var legacyScratch legacyPlacementScratch
		limit := uint64(math.MaxUint64)
		if random.Intn(2) == 0 {
			limit = uint64(random.Intn(4000))
		}
		fastGuard, err := limits.NewOptimizationWorkGuard(context.Background(), "LocalOptimize", limit)
		if err != nil {
			t.Fatal(err)
		}
		legacyGuard, err := limits.NewOptimizationWorkGuard(context.Background(), "LocalOptimize", limit)
		if err != nil {
			t.Fatal(err)
		}
		// Several calls share scratch: retained keys are charged on the next call.
		for call := 0; call < 4; call++ {
			median := geo.NewPoint(float64(random.Intn(60)*5-150), float64(random.Intn(60)*5-150))
			minUnocc := float64(random.Intn(4))
			minimizingSelf := random.Intn(4) != 0
			fastPoints, fastErr := optim.fillPlacementPointsGuarded(node, median, minUnocc, minimizingSelf, &fastScratch, fastGuard)
			legacyPoints, legacyErr := legacyFillPlacementPoints(optim, node, median, minUnocc, minimizingSelf, &legacyScratch, legacyGuard)
			if fmt.Sprint(fastErr) != fmt.Sprint(legacyErr) {
				t.Fatalf("trial %d call %d: error %v, legacy %v", trial, call, fastErr, legacyErr)
			}
			if fastGuard.Used() != legacyGuard.Used() {
				t.Fatalf("trial %d call %d: units %d, legacy %d", trial, call, fastGuard.Used(), legacyGuard.Used())
			}
			if fastErr != nil {
				failures++
				break
			}
			successes++
			if !slices.Equal(fastPoints, legacyPoints) {
				t.Fatalf("trial %d call %d: points differ\n%v\n%v", trial, call, fastPoints, legacyPoints)
			}
			if fastScratch.seenCount != len(legacyScratch.seen) {
				t.Fatalf("trial %d call %d: retained %d keys, legacy %d", trial, call, fastScratch.seenCount, len(legacyScratch.seen))
			}
		}
	}
}

func TestOptimizerDoesOverlapMatchesStepped(t *testing.T) {
	random := rand.New(rand.NewSource(11))
	failures, overlaps, clear := 0, 0, 0
	for trial := 0; trial < 500; trial++ {
		_, nodes := randomOptimizerGraph(random, 2+random.Intn(12))
		node := nodes[random.Intn(len(nodes))]
		point := geo.NewPoint(float64(random.Intn(60)*10-200), float64(random.Intn(60)*10-200))
		var exceptions []*layoutgraph.Node
		for _, other := range nodes {
			if random.Intn(5) == 0 {
				exceptions = append(exceptions, other)
			}
		}
		limit := uint64(math.MaxUint64)
		if random.Intn(2) == 0 {
			limit = uint64(random.Intn(60))
		}
		fastGuard, _ := limits.NewOptimizationWorkGuard(context.Background(), "LocalOptimize", limit)
		steppedGuard, _ := limits.NewOptimizationWorkGuard(context.Background(), "LocalOptimize", limit)
		fast, fastErr := optimizerDoesOverlap(node, point, exceptions, fastGuard)
		stepped, steppedErr := optimizerDoesOverlapStepped(node, point, exceptions, steppedGuard)
		if fast != stepped || fmt.Sprint(fastErr) != fmt.Sprint(steppedErr) || fastGuard.Used() != steppedGuard.Used() {
			t.Fatalf("trial %d: got (%v, %v, %d), stepped (%v, %v, %d)", trial,
				fast, fastErr, fastGuard.Used(), stepped, steppedErr, steppedGuard.Used())
		}
		switch {
		case fastErr != nil:
			failures++
		case fast:
			overlaps++
		default:
			clear++
		}
	}
	if failures == 0 || overlaps == 0 || clear == 0 {
		t.Errorf("exercised %d budget failures, %d overlaps, %d clear scans; want all", failures, overlaps, clear)
	}
}

func TestOptimizerOccupancyTreatsSignedZeroAsEqual(t *testing.T) {
	var index optimizerSpatialIndex
	index.occupied = make([]optimizerOccupancySlot, 16)
	index.occupancyInsert(0, 30, 4)
	index.occupancyInsert(math.Copysign(0, -1), 30, 9)
	if got, ok := index.occupancyLookup(math.Copysign(0, -1), 30); !ok || got != 4 {
		t.Fatalf("lookup(-0, 30) = %d, %v; want first inserted node 4", got, ok)
	}
	if _, ok := index.occupancyLookup(10, 30); ok {
		t.Fatal("lookup of an absent point succeeded")
	}
}
