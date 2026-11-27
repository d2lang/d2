package d2dagrelayout

import (
	"cmp"
	"fmt"
	"math"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// crossRankTestGraph builds a random nested graph whose objects carry
// labels and icons on all sides and sit on a coarse grid (many equal
// coordinates), with edges routed between object centers. The same seed
// builds the same graph.
func crossRankTestGraph(seed int64) (*d2graph.Graph, *objectIndex) {
	r := rand.New(rand.NewSource(seed))
	g := d2graph.NewGraph()
	positions := []label.Position{
		label.OutsideTopLeft, label.OutsideTopCenter, label.OutsideRightMiddle,
		label.OutsideLeftMiddle, label.OutsideBottomCenter, label.InsideMiddleLeft,
		label.InsideMiddleRight, label.InsideTopCenter, label.InsideMiddleCenter,
	}
	n := 2 + r.Intn(40)
	topLefts := make([]geo.Point, n)
	for i := 0; i < n; i++ {
		parent := g.Root
		if i > 0 && r.Intn(3) == 0 {
			parent = g.Objects[r.Intn(len(g.Objects))]
		}
		id := fmt.Sprintf("o%d", i)
		obj := &d2graph.Object{Graph: g, Parent: parent, ID: id}
		obj.Box = geo.NewBox(nil, float64(20*(1+r.Intn(6))), float64(20*(1+r.Intn(4))))
		topLefts[i] = geo.Point{X: float64(40 * r.Intn(12)), Y: float64(40 * r.Intn(12))}
		obj.TopLeft = &topLefts[i]
		obj.Label.Value = id
		obj.LabelDimensions.Width = 10 * (1 + r.Intn(8))
		obj.LabelDimensions.Height = 10 * (1 + r.Intn(3))
		obj.LabelPosition = go2String(positions[r.Intn(len(positions))].String())
		if r.Intn(4) == 0 {
			obj.Style.Multiple = &d2graph.Scalar{Value: "true"}
		}
		if parent.Children == nil {
			parent.Children = make(map[string]*d2graph.Object)
		}
		parent.Children[strings.ToLower(id)] = obj
		parent.ChildrenArray = append(parent.ChildrenArray, obj)
		g.Objects = append(g.Objects, obj)
	}
	for e := r.Intn(2 * n); e > 0; e-- {
		src, dst := g.Objects[r.Intn(n)], g.Objects[r.Intn(n)]
		edge := &d2graph.Edge{Src: src, Dst: dst}
		edge.Route = []*geo.Point{src.Center(), dst.Center()}
		g.Edges = append(g.Edges, edge)
	}
	ix := newObjectIndex(g)
	ix.topLefts = topLefts
	ix.closeOver()
	return g, ix
}

func go2String(s string) *string { return &s }

// sameGeometry fails the test unless both graphs have the same object boxes
// and edge routes, bit for bit.
func sameGeometry(t *testing.T, what string, want, got *d2graph.Graph) {
	t.Helper()
	same := func(a, b float64) bool { return math.Float64bits(a) == math.Float64bits(b) }
	for i, w := range want.Objects {
		o := got.Objects[i]
		if !same(o.TopLeft.X, w.TopLeft.X) || !same(o.TopLeft.Y, w.TopLeft.Y) || !same(o.Width, w.Width) || !same(o.Height, w.Height) {
			t.Fatalf("%s: %s at %v %vx%v, want %v %vx%v", what, o.ID, *o.TopLeft, o.Width, o.Height, *w.TopLeft, w.Width, w.Height)
		}
	}
	for i, w := range want.Edges {
		e := got.Edges[i]
		for j := range w.Route {
			if !same(e.Route[j].X, w.Route[j].X) || !same(e.Route[j].Y, w.Route[j].Y) {
				t.Fatalf("%s: edge %d point %d at %v, want %v", what, i, j, *e.Route[j], *w.Route[j])
			}
		}
	}
}

// legacyOrders records the orders a run of the legacy passes
// (legacy_spacing_test.go) took from ranging over their maps.
type legacyOrders struct {
	visits    [][]*d2graph.Object // per shift: the objects its grow pass visited
	inserted  [][]*d2graph.Object // per shift: the keys of seen, in insertion order
	ancestors [][]*d2graph.Object // per rank: starting, then ending ancestors, as ranged over
	positions [][]float64         // their target positions
	levels    [][]float64         // per getRanks: the rank coordinates, as ranged over
}

func (r *legacyOrders) beginShift() {
	if r != nil {
		r.visits = append(r.visits, nil)
		r.inserted = append(r.inserted, nil)
	}
}

func (r *legacyOrders) insert(o *d2graph.Object) {
	if r != nil {
		r.inserted[len(r.inserted)-1] = append(r.inserted[len(r.inserted)-1], o)
	}
}

func (r *legacyOrders) visit(o *d2graph.Object) {
	if r != nil {
		r.visits[len(r.visits)-1] = append(r.visits[len(r.visits)-1], o)
	}
}

func (r *legacyOrders) ancestorOrder(order []*d2graph.Object, positions map[*d2graph.Object]float64) {
	if r != nil {
		pos := make([]float64, len(order))
		for i, a := range order {
			pos[i] = positions[a]
		}
		r.ancestors = append(r.ancestors, slices.Clone(order))
		r.positions = append(r.positions, pos)
	}
}

func (r *legacyOrders) levelOrder(levels []float64) {
	if r != nil {
		r.levels = append(r.levels, slices.Clone(levels))
	}
}

// mapReplay is the mapOrders that makes the rewrite take the orders a legacy
// run recorded, checking on the way that the rewrite's maps have the
// legacy's keys, inserted in the same order, with the same values, and that
// the legacy visited only objects the rewrite had seen by then.
type mapReplay struct {
	t    *testing.T
	what string
	rec  *legacyOrders
	cur  map[*d2graph.Object]*d2graph.Object // legacy object -> rewrite's

	shifts, ranks, getRanks int
	// What the replay covered.
	growPasses, addedVisits, tiedRanks, nanRanks int
}

func newMapReplay(t *testing.T, what string, rec *legacyOrders, legacy, current *d2graph.Graph) *mapReplay {
	m := &mapReplay{t: t, what: what, rec: rec, cur: map[*d2graph.Object]*d2graph.Object{legacy.Root: current.Root}}
	for i, o := range legacy.Objects {
		m.cur[o] = current.Objects[i]
	}
	return m
}

func (m *mapReplay) fatalf(format string, args ...any) {
	m.t.Helper()
	m.t.Fatalf("%s: "+format, append([]any{m.what}, args...)...)
}

func (m *mapReplay) sameObjects(what string, got, legacy []*d2graph.Object) {
	m.t.Helper()
	if len(got) != len(legacy) {
		m.fatalf("%s: %d objects, legacy %d", what, len(got), len(legacy))
	}
	for i, o := range legacy {
		if got[i] != m.cur[o] {
			m.fatalf("%s: object %d is %s, legacy %s", what, i, got[i].ID, o.ID)
		}
	}
}

func (m *mapReplay) growPass(seen map[*d2graph.Object]struct{}, visit func(*d2graph.Object), inserted func() []*d2graph.Object) {
	m.t.Helper()
	i := m.shifts
	m.shifts++
	if i >= len(m.rec.visits) {
		m.fatalf("shift %d, legacy made %d", i, len(m.rec.visits))
	}
	if seen == nil {
		return
	}
	m.growPasses++
	n0 := len(seen)
	if n0 > len(m.rec.inserted[i]) {
		m.fatalf("shift %d: %d objects seen, legacy %d", i, n0, len(m.rec.inserted[i]))
	}
	m.sameObjects(fmt.Sprintf("shift %d: seen before the grow pass", i), inserted(), m.rec.inserted[i][:n0])
	added := make(map[*d2graph.Object]bool)
	for _, o := range m.rec.inserted[i][n0:] {
		added[o] = true
	}
	for _, o := range m.rec.visits[i] {
		if _, ok := seen[m.cur[o]]; !ok {
			m.fatalf("shift %d: legacy visited %s, not seen here", i, o.ID)
		}
		if added[o] {
			m.addedVisits++
		}
		visit(m.cur[o])
	}
	m.sameObjects(fmt.Sprintf("shift %d: seen", i), inserted(), m.rec.inserted[i])
}

func (m *mapReplay) ancestors(positions map[*d2graph.Object]float64) []*d2graph.Object {
	m.t.Helper()
	i := m.ranks
	m.ranks++
	if i >= len(m.rec.ancestors) {
		m.fatalf("ancestor list %d, legacy made %d", i, len(m.rec.ancestors))
	}
	if positions == nil {
		return nil
	}
	m.tiedRanks++
	if len(positions) != len(m.rec.ancestors[i]) {
		m.fatalf("ancestor list %d: %d ancestors, legacy %d", i, len(positions), len(m.rec.ancestors[i]))
	}
	order := make([]*d2graph.Object, len(positions))
	for j, o := range m.rec.ancestors[i] {
		p, ok := positions[m.cur[o]]
		if !ok || math.Float64bits(p) != math.Float64bits(m.rec.positions[i][j]) {
			m.fatalf("ancestor list %d: %s at %v (%v), legacy %v", i, o.ID, p, ok, m.rec.positions[i][j])
		}
		order[j] = m.cur[o]
	}
	return order
}

func (m *mapReplay) levels(aligned map[float64][]*d2graph.Object) []float64 {
	m.t.Helper()
	i := m.getRanks
	m.getRanks++
	if i >= len(m.rec.levels) {
		m.fatalf("getRanks %d, legacy ran %d", i, len(m.rec.levels))
	}
	if aligned == nil {
		return nil
	}
	m.nanRanks++
	if len(aligned) != len(m.rec.levels[i]) {
		m.fatalf("getRanks %d: %d coordinates, legacy %d", i, len(aligned), len(m.rec.levels[i]))
	}
	for _, l := range m.rec.levels[i] {
		if _, ok := aligned[l]; !ok && !math.IsNaN(l) {
			m.fatalf("getRanks %d: no coordinate %v", i, l)
		}
	}
	return slices.Clone(m.rec.levels[i])
}

// done fails the test unless the rewrite consumed every recorded order.
func (m *mapReplay) done() {
	m.t.Helper()
	if m.shifts != len(m.rec.visits) || m.ranks != len(m.rec.ancestors) || m.getRanks != len(m.rec.levels) {
		m.fatalf("replayed %d shifts, %d ancestor lists and %d getRanks; legacy made %d, %d and %d",
			m.shifts, m.ranks, m.getRanks, len(m.rec.visits), len(m.rec.ancestors), len(m.rec.levels))
	}
}

// TestSpacingPassesMatchLegacy runs the rank-spacing and cross-rank passes
// and the original versions (legacy_spacing_test.go) on the same random
// graphs. The original's results depend on the orders it takes from its
// maps (see map_order.go), which are random, so the rewrite replays the
// orders of the legacy run (mapReplay): whatever orders the original takes,
// the rewrite must give the same result in them.
func TestSpacingPassesMatchLegacy(t *testing.T) {
	t.Parallel()
	var growPasses, addedVisits, tiedRanks int
	for seed := int64(0); seed < 300; seed++ {
		for _, isHorizontal := range []bool{false, true} {
			legacy, _ := crossRankTestGraph(seed)
			current, ix := crossRankTestGraph(seed)
			what := fmt.Sprintf("seed %d horizontal %v", seed, isHorizontal)

			rec := new(legacyOrders)
			replay := newMapReplay(t, what+" rank spacing", rec, legacy, current)
			legacyAdjustRankSpacing(legacy, MIN_RANK_SEP, isHorizontal, rec)
			adjustRankSpacingWith(current, ix, MIN_RANK_SEP, isHorizontal, replay)
			replay.done()
			sameGeometry(t, what+" rank spacing", legacy, current)
			tiedRanks += replay.tiedRanks

			rec = new(legacyOrders)
			replay = newMapReplay(t, what+" cross-rank spacing", rec, legacy, current)
			legacyAdjustCrossRankSpacing(legacy, MIN_RANK_SEP, !isHorizontal, rec)
			adjustCrossRankSpacingWith(current, ix, MIN_RANK_SEP, !isHorizontal, false, replay)
			replay.done()
			sameGeometry(t, what+" cross-rank spacing", legacy, current)
			growPasses += replay.growPasses
			addedVisits += replay.addedVisits
		}
	}
	t.Logf("%d grow passes (%d visits of objects added during them), %d ranks with tied ancestors", growPasses, addedVisits, tiedRanks)
	if growPasses < 1000 || addedVisits < 100 || tiedRanks < 100 {
		t.Fatalf("the graphs exercise too little: %d grow passes, %d added visits, %d tied ranks", growPasses, addedVisits, tiedRanks)
	}
}

// TestSpacingPassesMatchLegacyWithNaN is TestSpacingPassesMatchLegacy on
// graphs in which some objects have a NaN coordinate or size along the rank
// axis. Each such object was a rank of its own in the original, with no
// objects, placed among the ranks by the map's order (getRanksMapOrder).
func TestSpacingPassesMatchLegacyWithNaN(t *testing.T) {
	t.Parallel()
	spoil := func(g *d2graph.Graph, seed int64, isHorizontal bool) {
		r := rand.New(rand.NewSource(seed))
		nan := math.Float64frombits(0x7ff8000000000000 | uint64(seed))
		for n := 1 + r.Intn(3); n > 0; n-- {
			obj := g.Objects[r.Intn(len(g.Objects))]
			switch {
			case r.Intn(2) == 0:
				obj.Height, obj.Width = nan, nan
			case isHorizontal:
				obj.TopLeft.X = nan
			default:
				obj.TopLeft.Y = nan
			}
		}
	}
	nanRanks := 0
	for seed := int64(0); seed < 300; seed++ {
		for _, isHorizontal := range []bool{false, true} {
			legacy, _ := crossRankTestGraph(seed)
			current, ix := crossRankTestGraph(seed)
			spoil(legacy, seed, isHorizontal)
			spoil(current, seed, isHorizontal)
			what := fmt.Sprintf("seed %d horizontal %v", seed, isHorizontal)

			rec := new(legacyOrders)
			replay := newMapReplay(t, what+" rank spacing", rec, legacy, current)
			legacyAdjustRankSpacing(legacy, MIN_RANK_SEP, isHorizontal, rec)
			adjustRankSpacingWith(current, ix, MIN_RANK_SEP, isHorizontal, replay)
			replay.done()
			sameGeometry(t, what+" rank spacing", legacy, current)
			nanRanks += replay.nanRanks

			rec = new(legacyOrders)
			replay = newMapReplay(t, what+" cross-rank spacing", rec, legacy, current)
			legacyAdjustCrossRankSpacing(legacy, MIN_RANK_SEP, !isHorizontal, rec)
			adjustCrossRankSpacingWith(current, ix, MIN_RANK_SEP, !isHorizontal, false, replay)
			replay.done()
			sameGeometry(t, what+" cross-rank spacing", legacy, current)
		}
	}
	if nanRanks < 300 {
		t.Fatalf("only %d getRanks with a NaN coordinate", nanRanks)
	}
}

// seenOrderGrow is a fixed mapOrders for comparing two versions of the
// rewrite with each other: grow passes visit the seen objects in the order
// they were seen, including those seen during the pass, and the other maps
// are ranged over in key order.
type seenOrderGrow struct{}

func (seenOrderGrow) growPass(seen map[*d2graph.Object]struct{}, visit func(*d2graph.Object), inserted func() []*d2graph.Object) {
	if seen == nil {
		return
	}
	for i := 0; i < len(inserted()); i++ {
		visit(inserted()[i])
	}
}

func (seenOrderGrow) ancestors(positions map[*d2graph.Object]float64) []*d2graph.Object {
	if positions == nil {
		return nil
	}
	keys := make([]*d2graph.Object, 0, len(positions))
	for o := range positions {
		keys = append(keys, o)
	}
	slices.SortFunc(keys, func(a, b *d2graph.Object) int { return strings.Compare(a.AbsID(), b.AbsID()) })
	return keys
}

func (seenOrderGrow) levels(aligned map[float64][]*d2graph.Object) []float64 {
	if aligned == nil {
		return nil
	}
	keys := make([]float64, 0, len(aligned))
	for l := range aligned {
		keys = append(keys, l)
	}
	slices.SortFunc(keys, func(a, b float64) int { return cmp.Compare(a, b) })
	return keys
}

func TestCrossRankAxisOrderMatchesScan(t *testing.T) {
	t.Parallel()
	for seed := int64(0); seed < 300; seed++ {
		for _, isHorizontal := range []bool{false, true} {
			scanned, scannedIx := crossRankTestGraph(seed)
			searched, searchedIx := crossRankTestGraph(seed)
			if !regularForAxisOrder(searchedIx) {
				t.Fatalf("seed %d: test graph is not regular", seed)
			}
			adjustCrossRankSpacingWith(scanned, scannedIx, MIN_RANK_SEP, isHorizontal, true, seenOrderGrow{})
			adjustCrossRankSpacingWith(searched, searchedIx, MIN_RANK_SEP, isHorizontal, false, seenOrderGrow{})
			for i, want := range scanned.Objects {
				got := searched.Objects[i]
				if *got.TopLeft != *want.TopLeft || got.Width != want.Width || got.Height != want.Height {
					t.Fatalf("seed %d horizontal %v: %s at %v %vx%v, want %v %vx%v", seed, isHorizontal, got.ID,
						*got.TopLeft, got.Width, got.Height, *want.TopLeft, want.Width, want.Height)
				}
			}
			for i, want := range scanned.Edges {
				got := searched.Edges[i]
				for j := range want.Route {
					if *got.Route[j] != *want.Route[j] {
						t.Fatalf("seed %d horizontal %v: edge %d point %d at %v, want %v", seed, isHorizontal, i, j, *got.Route[j], *want.Route[j])
					}
				}
			}
		}
	}
}

// TestSpacingPassesAreDeterministic lays each graph out several times and
// requires the same geometry every time. Three steps of the passes used to
// take orders from ranging over Go maps (see map_order.go), and the layout of
// some of these graphs changed from run to run.
func TestSpacingPassesAreDeterministic(t *testing.T) {
	t.Parallel()
	for seed := int64(0); seed < 300; seed++ {
		for _, isHorizontal := range []bool{false, true} {
			var first *d2graph.Graph
			for run := 0; run < 8; run++ {
				g, ix := crossRankTestGraph(seed)
				adjustRankSpacing(g, ix, MIN_RANK_SEP, isHorizontal)
				adjustCrossRankSpacing(g, ix, MIN_RANK_SEP, !isHorizontal)
				if first == nil {
					first = g
					continue
				}
				sameGeometry(t, fmt.Sprintf("seed %d horizontal %v run %d", seed, isHorizontal, run), first, g)
			}
		}
	}
}
