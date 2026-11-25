package d2dagrelayout

// The distribution of adjustRankSpacing's results on ranks whose
// starting and/or ending ancestors tie, original (legacyAdjustRankSpacing,
// real maps) against the rewrite, for the map sizes at which TinyGo's maps
// grow (12 to 14 and 25, 26 keys). It extends TestStartingTieOrderDistribution
// (starting ancestors only) to the ending ancestors and to both sides at once.
//
// Under TinyGo, the order in which a map is iterated depends on its own
// history (when it grew: TinyGo also grows on an assignment to an existing
// key) and on how many random numbers the runtime drew between its creation,
// its growth and the start of the iteration: every map creation, growth and
// iteration start draws from one xorshift32 stream, whose low bits depend on
// those of the few draws before. The original made both ancestor maps of a
// rank, filled them, and then ranged over both, so each map's iteration
// started after the other map's draws.
//
//	D2_TINYGO_ORDER=1 go test -run TestAncestorTieOrderDistribution -v ./d2layouts/d2dagrelayout
//	tinygo test -target=wasm -run TestAncestorTieOrderDistribution -v ./d2layouts/d2dagrelayout
//
// No CI job runs these TinyGo tests: d2's ci/test-wasm.sh does not list this
// package. Run as that script runs packages (`tinygo test -target=wasm
// -stack-size=1MB -no-debug ./d2layouts/d2dagrelayout`, 1 to 3.5 minutes on
// an Apple M4 depending on load), the package passed 10 runs in a row and
// fails when sortAncestors' tie maps are not the original's, but it passes
// when the grow pass ranges over a map made without the original's other
// maps (crossRankShifter.originalMaps): only TestGrowPassVisitOrder
// catches that, and it is slow and gated. After changing either,
// run from the module root, with TinyGo 0.44 and node on PATH (2 to 3
// minutes on an Apple M4, about one of them the tests):
//
//	tinygo test -target=wasm -stack-size=1MB -no-debug -tags slowtests -run 'TieOrderDistribution|GrowPassVisitOrder' -v ./d2layouts/d2dagrelayout
//
// -tags slowtests enables the slow tests (slowTests): D2_SLOW=1 does not
// reach a program that tinygo test runs.

import (
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// jitterRand and jitterSink serve jitterMapDraws.
var (
	jitterRand = rand.New(rand.NewSource(1))
	jitterSink map[int]struct{}
)

// slowTests enables this package's slow TinyGo tests: D2_SLOW=1 under gc, or a
// build with -tags slowtests (slowtests_test.go), the only way under
// TinyGo, whose `tinygo test -target=wasm` passes no environment variables
// to the test program.
var slowTests = os.Getenv("D2_SLOW") == "1"

// jitterMapDraws makes between 0 and 255 maps. Under TinyGo every map creation
// draws one number from the runtime's xorshift32 stream, so layouts run one
// after the other in a process start from states of that stream a random
// distance apart: without this, the layouts a test compares start from
// states a fixed distance apart, whose low bits are related, and the
// samples of either version are not independent (the same comparison then
// gives z scores of 10 and more in one process and none in another). Under
// gc it changes nothing that matters.
func jitterMapDraws() {
	for n := jitterRand.Intn(256); n > 0; n-- {
		jitterSink = make(map[int]struct{})
	}
}

// ancestorTieGraph builds n containers side by side, each with two nodes a and
// b on one rank. Node a has an outside-top label when starting is set, so
// that every container's starting target is the top of a's label; node b
// has an outside-bottom label when ending is set, so that every container's
// ending target is the bottom of b's label. The containers' own tops and
// bottoms differ, so the order in which tied containers are adjusted
// decides the result.
func ancestorTieGraph(n int, starting, ending bool) (*d2graph.Graph, *objectIndex) {
	g := d2graph.NewGraph()
	topLefts := make([]geo.Point, 3*n)
	add := func(i int, parent *d2graph.Object, id string, x, y, w, h float64) *d2graph.Object {
		obj := &d2graph.Object{Graph: g, Parent: parent, ID: id}
		obj.Box = geo.NewBox(nil, w, h)
		topLefts[i] = geo.Point{X: x, Y: y}
		obj.TopLeft = &topLefts[i]
		if parent.Children == nil {
			parent.Children = make(map[string]*d2graph.Object)
		}
		parent.Children[strings.ToLower(id)] = obj
		parent.ChildrenArray = append(parent.ChildrenArray, obj)
		g.Objects = append(g.Objects, obj)
		return obj
	}
	const step = 1.5
	labelled := func(o *d2graph.Object, outside bool, pos label.Position) {
		o.Label.Value = "x"
		o.LabelDimensions.Width = 20
		o.LabelDimensions.Height = 40
		if !outside {
			pos = label.InsideMiddleCenter
		}
		o.LabelPosition = go2String(pos.String())
	}
	for i := 0; i < n; i++ {
		top := 160 + step*float64(i)
		bottom := 350 + step*float64(i)
		if ending {
			bottom = 260 + step*float64(i)
		}
		x := float64(200 * i)
		c := add(3*i, g.Root, fmt.Sprintf("c%d", i), x, top, 150, bottom-top)
		a := add(3*i+1, c, "a", x+20, 200, 40, 50)
		b := add(3*i+2, c, "b", x+80, 200, 40, 50)
		labelled(a, starting, label.OutsideTopCenter)
		labelled(b, ending, label.OutsideBottomCenter)
	}
	ix := newObjectIndex(g)
	ix.topLefts = topLefts
	ix.closeOver()
	return g, ix
}

func TestAncestorTieOrderDistribution(t *testing.T) {
	if os.Getenv("D2_TINYGO_ORDER") == "" && runtime.Compiler != "tinygo" {
		t.Skip("set D2_TINYGO_ORDER=1 (runs by default under TinyGo)")
	}
	const iters = 3000
	type config struct {
		n                int
		starting, ending bool
	}
	var configs []config
	for _, n := range []int{12, 13, 14, 25, 26} {
		configs = append(configs, config{n, true, false}, config{n, false, true})
	}
	configs = append(configs, config{13, true, true}, config{14, true, true})
	for _, c := range configs {
		orig := map[string]int{}
		rewr := map[string]int{}
		g, ix := ancestorTieGraph(c.n, c.starting, c.ending)
		restore := snapshotGeometry(g)
		for it := 0; it < iters; it++ {
			restore()
			jitterMapDraws()
			legacyAdjustRankSpacing(g, MIN_RANK_SEP, false, nil)
			orig[geometryKey(g)]++

			restore()
			jitterMapDraws()
			adjustRankSpacing(g, ix, MIN_RANK_SEP, false)
			rewr[geometryKey(g)]++
		}
		onlyOrig, onlyRewr := 0, 0
		for k, v := range orig {
			if rewr[k] == 0 {
				onlyOrig += v
			}
		}
		for k, v := range rewr {
			if orig[k] == 0 {
				onlyRewr += v
			}
		}
		z, df := chiSquareZ(orig, rewr)
		what := fmt.Sprintf("%s n=%d starting=%v ending=%v", runtime.Compiler, c.n, c.starting, c.ending)
		t.Logf("%s: %d layouts by the original's code, %d by the rewrite's; only original %d, only rewrite %d of %d; chi-square z %.1f (df %d)",
			what, len(orig), len(rewr), onlyOrig, onlyRewr, iters, z, df)
		if len(orig) < 2 {
			t.Errorf("%s: the original's result does not depend on the order of the tied ancestors; change the graph", what)
		}
		if z > 4.5 {
			t.Errorf("%s: the rewrite's layouts come from another distribution than the original's (z %.1f)", what, z)
		}
	}
}

// TestLargeGrowPassDistribution compares the distribution of the
// cross-rank pass's results, original (legacyAdjustCrossRankSpacing, real
// maps) against the rewrite, on larger random graphs
// (largeCrossRankTestGraph) whose results depend on the order in which grow
// passes with 14 or more seen objects visit them (seeds found under gc: 2 to
// 6 results in 60 runs).
// Under TinyGo the map of a shift's seen objects grows before such a pass,
// and the order in which it is iterated then depends on the draws of the
// original's other maps of the shift (crossRankShifter.originalMaps).
//
//	D2_SLOW=1 D2_TINYGO_ORDER=1 go test -run TestLargeGrowPassDistribution -v ./d2layouts/d2dagrelayout
//	tinygo test -target=wasm -stack-size=1MB -no-debug -tags slowtests -run TestLargeGrowPassDistribution -v ./d2layouts/d2dagrelayout
//
// It takes about 30 s under TinyGo: D2_SLOW=1 (or -tags slowtests) runs it.
// (These graphs give the original's distribution with and without
// shadowMaps; TestGrowPassVisitOrder shows what shadowMaps changes.)
func TestLargeGrowPassDistribution(t *testing.T) {
	if os.Getenv("D2_TINYGO_ORDER") == "" && runtime.Compiler != "tinygo" {
		t.Skip("set D2_TINYGO_ORDER=1 (runs by default under TinyGo)")
	}
	if !slowTests {
		t.Skip("slow (30 s under TinyGo); set D2_SLOW=1 (TinyGo: -tags slowtests)")
	}
	const iters = 1500
	for _, seed := range []int64{3, 6, 15, 16, 22, 25, 35, 36, 37} {
		orig := map[string]int{}
		rewr := map[string]int{}
		g, ix := largeCrossRankTestGraph(seed)
		restore := snapshotGeometry(g)
		for it := 0; it < iters; it++ {
			restore()
			jitterMapDraws()
			legacyAdjustCrossRankSpacing(g, MIN_RANK_SEP, false, nil)
			orig[geometryKeyWithRoutes(g)]++

			restore()
			jitterMapDraws()
			adjustCrossRankSpacing(g, ix, MIN_RANK_SEP, false)
			rewr[geometryKeyWithRoutes(g)]++
		}
		z, df := chiSquareZ(orig, rewr)
		t.Logf("%s seed %d (%d objects, shadowMaps %v): %d results by the original's code, %d by the rewrite's; chi-square z %.1f (df %d)",
			runtime.Compiler, seed, len(g.Objects), shadowMaps, len(orig), len(rewr), z, df)
		if z > 4.5 {
			t.Errorf("%s seed %d: the rewrite's results come from another distribution than the original's (z %.1f)", runtime.Compiler, seed, z)
		}
	}
}

// visitOrderRecorder is the mapOrders that ranges over the grow passes' maps
// as the production code does and records the order in which shift target
// visited the first three seen objects.
type visitOrderRecorder struct {
	target, shift int
	first         []string
}

func (o *visitOrderRecorder) growPass(seen map[*d2graph.Object]struct{}, visit func(*d2graph.Object), inserted func() []*d2graph.Object, writes []int32) {
	i := o.shift
	o.shift++
	if seen == nil {
		return
	}
	for x := range seen {
		if i == o.target && len(o.first) < 3 {
			o.first = append(o.first, x.ID)
		}
		visit(x)
	}
}

func (o *visitOrderRecorder) ancestors(positions map[*d2graph.Object]float64) []*d2graph.Object {
	if positions == nil {
		return nil
	}
	keys := make([]*d2graph.Object, 0, len(positions))
	for k := range positions {
		keys = append(keys, k)
	}
	return keys
}

func (o *visitOrderRecorder) levels(aligned map[float64][]*d2graph.Object) []float64 {
	if aligned == nil {
		return nil
	}
	keys := make([]float64, 0, len(aligned))
	for k := range aligned {
		keys = append(keys, k)
	}
	return keys
}

// TestGrowPassVisitOrder compares the order in which the first
// grow pass over 14 to 30 seen objects visits them, original
// (legacyAdjustCrossRankSpacing, real maps) against the rewrite, by the
// distribution of the first three objects visited. Under TinyGo such a map
// has grown before the pass, and the order depends on the draws of the
// original's other maps of the shift: without them (shadowMaps false) the
// distributions differ (TinyGo js/wasm: z 13.6, 11.7, 2.6 and 14.1 for the
// four graphs; with the original's maps -0.9, 0.5, -1.1 and 0.1). A graph's later
// shifts depend on the orders its earlier grow passes took, so the shift
// compared need not exist, or be the same shift, in every run: the
// distribution compared includes those outcomes.
//
// It takes about a minute under TinyGo: D2_SLOW=1 (or -tags slowtests) runs it.
//
//	D2_SLOW=1 D2_TINYGO_ORDER=1 go test -run TestGrowPassVisitOrder -v ./d2layouts/d2dagrelayout
//	tinygo test -target=wasm -stack-size=1MB -no-debug -tags slowtests -run TestGrowPassVisitOrder -v ./d2layouts/d2dagrelayout
func TestGrowPassVisitOrder(t *testing.T) {
	if os.Getenv("D2_TINYGO_ORDER") == "" && runtime.Compiler != "tinygo" {
		t.Skip("set D2_TINYGO_ORDER=1 (runs by default under TinyGo)")
	}
	if !slowTests {
		t.Skip("slow (a minute under TinyGo); set D2_SLOW=1 (TinyGo: -tags slowtests)")
	}
	const iters = 4000
	firstThree := func(visits []*d2graph.Object) string {
		var ids []string
		for _, o := range visits {
			if len(ids) == 3 {
				break
			}
			ids = append(ids, o.ID)
		}
		return strings.Join(ids, " ")
	}
	checked := 0
	for seed := int64(0); seed < 40 && checked < 4; seed++ {
		// The first shift whose grow pass ranges over 14 to 30 objects.
		g, ix := largeCrossRankTestGraph(seed)
		restore := snapshotGeometry(g)
		rec := new(legacyOrders)
		legacyAdjustCrossRankSpacing(g, MIN_RANK_SEP, false, rec)
		target := -1
		for i, w := range rec.writes {
			seen := 0
			for _, x := range w {
				if x.m == "seen" {
					seen++
				}
			}
			if seen >= 14 && seen <= 30 && len(rec.visits[i]) >= 3 {
				target = i
				break
			}
		}
		if target < 0 {
			continue
		}
		checked++
		orig := map[string]int{}
		rewr := map[string]int{}
		for it := 0; it < iters; it++ {
			restore()
			jitterMapDraws()
			rec := new(legacyOrders)
			legacyAdjustCrossRankSpacing(g, MIN_RANK_SEP, false, rec)
			if target < len(rec.visits) {
				orig[firstThree(rec.visits[target])]++
			} else {
				orig["no such shift"]++
			}

			restore()
			jitterMapDraws()
			vo := &visitOrderRecorder{target: target}
			adjustCrossRankSpacingWith(g, ix, MIN_RANK_SEP, false, false, vo)
			if target < vo.shift {
				rewr[strings.Join(vo.first, " ")]++
			} else {
				rewr["no such shift"]++
			}
		}
		z, df := chiSquareZ(orig, rewr)
		t.Logf("%s seed %d shift %d (shadowMaps %v): %d first-three orders by the original's code, %d by the rewrite's; chi-square z %.1f (df %d)",
			runtime.Compiler, seed, target, shadowMaps, len(orig), len(rewr), z, df)
		if z > 4.5 {
			t.Errorf("%s seed %d: the rewrite's grow pass visits in another order than the original's (z %.1f)", runtime.Compiler, seed, z)
		}
	}
	if checked < 4 {
		t.Fatalf("only %d graphs with a grow pass over 14 to 30 objects", checked)
	}
}
