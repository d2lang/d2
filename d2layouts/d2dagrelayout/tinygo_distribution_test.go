package d2dagrelayout

// Under TinyGo, adjustRankSpacing's tie order for a
// rank with exactly 13 (25, 49, ...) tied starting ancestors is drawn from a
// different distribution than the original's.
//
// The graph: 13 containers side by side, each holding one node; the nodes
// are all on one rank and have an outside-top label, so every container's
// starting target position is the same (the top of its node's label), while
// the containers' own tops differ. The order in which the tied containers
// are adjusted decides the result.
//
// The test lays the graph out N times with the original's code
// (legacyAdjustRankSpacing in legacy_spacing_test.go, real maps, no
// recording) and N times with the rewrite (adjustRankSpacing), prints how
// often each resulting geometry came, and fails when the two samples differ
// (chi-square test of homogeneity, z > 4.5, p < 4e-6). Under gc both columns
// come from the same distribution; under TinyGo they did not before sortAncestors filled its tie maps as the original did (n=13: z 30 and more).
//
//	D2_TINYGO_ORDER=1 go test -run TestStartingTieOrderDistribution -v ./d2layouts/d2dagrelayout
//	tinygo test -target=wasm -run TestStartingTieOrderDistribution -v ./d2layouts/d2dagrelayout

import (
	"crypto/sha256"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// startingTieGraph builds the graph with n containers.
func startingTieGraph(n int) (*d2graph.Graph, *objectIndex) {
	g := d2graph.NewGraph()
	topLefts := make([]geo.Point, 2*n)
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
	for i := 0; i < n; i++ {
		c := add(2*i, g.Root, fmt.Sprintf("c%d", i), float64(200*i), float64(160+3*i), 150, 300)
		k := add(2*i+1, c, "k", float64(200*i+20), 200, 100, 50)
		k.Label.Value = "x"
		k.LabelDimensions.Width = 20
		k.LabelDimensions.Height = 40
		k.LabelPosition = go2String(label.OutsideTopCenter.String())
	}
	ix := newObjectIndex(g)
	ix.topLefts = topLefts
	ix.closeOver()
	return g, ix
}

func geometryKey(g *d2graph.Graph) string {
	h := sha256.New()
	for _, o := range g.Objects {
		fmt.Fprintf(h, "%x %x %x %x;", math.Float64bits(o.TopLeft.X), math.Float64bits(o.TopLeft.Y), math.Float64bits(o.Width), math.Float64bits(o.Height))
	}
	return fmt.Sprintf("%x", h.Sum(nil)[:6])
}

func TestStartingTieOrderDistribution(t *testing.T) {
	if os.Getenv("D2_TINYGO_ORDER") == "" && runtime.Compiler != "tinygo" {
		t.Skip("set D2_TINYGO_ORDER=1 (runs by default under TinyGo)")
	}
	const iters = 3000
	for _, n := range []int{12, 13, 14} {
		orig := map[string]int{}
		rewr := map[string]int{}
		g, ix := startingTieGraph(n)
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
		keys := map[string]bool{}
		for k := range orig {
			keys[k] = true
		}
		for k := range rewr {
			keys[k] = true
		}
		var ks []string
		for k := range keys {
			ks = append(ks, k)
		}
		sort.Slice(ks, func(i, j int) bool { return orig[ks[i]]+rewr[ks[i]] > orig[ks[j]]+rewr[ks[j]] })
		onlyOrig, onlyRewr := 0, 0
		for _, k := range ks {
			if rewr[k] == 0 {
				onlyOrig += orig[k]
			}
			if orig[k] == 0 {
				onlyRewr += rewr[k]
			}
		}
		z, df := chiSquareZ(orig, rewr)
		t.Logf("%s n=%d: %d layouts by the original's code, %d by the rewrite's; %d of %d original runs gave a layout the rewrite never gave, %d of %d rewrite runs one the original never gave; chi-square z %.1f (df %d)",
			runtime.Compiler, n, len(orig), len(rewr), onlyOrig, iters, onlyRewr, iters, z, df)
		for i, k := range ks {
			if i == 8 {
				break
			}
			t.Logf("  %s original %4d rewrite %4d", k, orig[k], rewr[k])
		}
		if z > 4.5 {
			t.Errorf("%s n=%d: the rewrite's layouts come from another distribution than the original's (z %.1f)", runtime.Compiler, n, z)
		}
	}
}

// TestSpacingPassDistribution compares the cross-rank pass of the
// original (legacyAdjustCrossRankSpacing, real maps) with the rewrite's on
// crossRankTestGraph's random graphs, iters runs each, and reports per seed
// the number of distinct results and the largest difference in how often a
// result came. It also does the same for the rank-spacing pass. It fails
// when a graph's two samples differ with z > 4.5 (p < 4e-6 per graph). It
// takes about two minutes under TinyGo: D2_SLOW=1 (or -tags slowtests, see
// slowTests) runs it.
//
// It runs one pass at a time, each run starting a random number of draws
// from the last (jitterMapDraws). A whole layout chains several
// order-dependent steps and the dagre engine, and under TinyGo the rewrite
// does not repeat the original's draws between them, so whole layouts can
// give the original's results at other frequencies (see map_order.go); this
// test does not cover that.
func TestSpacingPassDistribution(t *testing.T) {
	if os.Getenv("D2_TINYGO_ORDER") == "" && runtime.Compiler != "tinygo" {
		t.Skip("set D2_TINYGO_ORDER=1 (runs by default under TinyGo)")
	}
	if !slowTests {
		t.Skip("slow (minutes under TinyGo); set D2_SLOW=1 (TinyGo: -tags slowtests)")
	}
	const iters = 400
	geometry := func(g *d2graph.Graph) string {
		h := sha256.New()
		for _, o := range g.Objects {
			fmt.Fprintf(h, "%x %x %x %x;", math.Float64bits(o.TopLeft.X), math.Float64bits(o.TopLeft.Y), math.Float64bits(o.Width), math.Float64bits(o.Height))
		}
		for _, e := range g.Edges {
			for _, p := range e.Route {
				fmt.Fprintf(h, "%x %x,", math.Float64bits(p.X), math.Float64bits(p.Y))
			}
		}
		return fmt.Sprintf("%x", h.Sum(nil)[:6])
	}
	for _, pass := range []string{"cross-rank", "rank"} {
		varied, flagged, worst := 0, 0, 0.0
		worstSeed := int64(-1)
		for seed := int64(0); seed < 300; seed++ {
			orig := map[string]int{}
			rewr := map[string]int{}
			g, ix := crossRankTestGraph(seed)
			restore := snapshotGeometry(g)
			for it := 0; it < iters; it++ {
				restore()
				jitterMapDraws()
				if pass == "cross-rank" {
					legacyAdjustCrossRankSpacing(g, MIN_RANK_SEP, false, nil)
				} else {
					legacyAdjustRankSpacing(g, MIN_RANK_SEP, false, nil)
				}
				orig[geometry(g)]++
				restore()
				jitterMapDraws()
				if pass == "cross-rank" {
					adjustCrossRankSpacing(g, ix, MIN_RANK_SEP, false)
				} else {
					adjustRankSpacing(g, ix, MIN_RANK_SEP, false)
				}
				rewr[geometry(g)]++
			}
			if len(orig) == 1 && len(rewr) == 1 {
				if _, ok := rewr[firstKey(orig)]; !ok {
					t.Errorf("%s seed %d: original always %v, rewrite always %v", pass, seed, orig, rewr)
				}
				continue
			}
			varied++
			z, df := chiSquareZ(orig, rewr)
			if z > worst {
				worst, worstSeed = z, seed
			}
			if z > 3.1 {
				flagged++
				t.Logf("%s seed %d: chi-square z %.1f (df %d): %d results by the original, %d by the rewrite", pass, seed, z, df, len(orig), len(rewr))
			}
			if z > 4.5 {
				t.Errorf("%s seed %d: the rewrite's results come from another distribution than the original's (z %.1f)", pass, seed, z)
			}
		}
		t.Logf("%s %s: %d of 300 graphs vary; %d with p < 0.001 (z > 3.1); largest z %.1f (seed %d)", runtime.Compiler, pass, varied, flagged, worst, worstSeed)
	}
}

func firstKey(m map[string]int) string {
	for k := range m {
		return k
	}
	return ""
}

// chiSquareZ is the chi-square test of homogeneity of two samples of equal
// size (cells with an expected count below 5 pooled), as a z score by the
// Wilson-Hilferty approximation: z > 3.1 is p < 0.001.
func chiSquareZ(a, b map[string]int) (float64, int) {
	var na, nb float64
	for _, v := range a {
		na += float64(v)
	}
	for _, v := range b {
		nb += float64(v)
	}
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	type cell struct{ x, y float64 }
	var cells []cell
	var pool cell
	for k := range keys {
		x, y := float64(a[k]), float64(b[k])
		if (x+y)*na/(na+nb) < 5 || (x+y)*nb/(na+nb) < 5 {
			pool.x += x
			pool.y += y
			continue
		}
		cells = append(cells, cell{x, y})
	}
	if pool.x+pool.y > 0 {
		cells = append(cells, pool)
	}
	df := len(cells) - 1
	if df < 1 {
		return 0, 0
	}
	stat := 0.0
	for _, c := range cells {
		ea, eb := (c.x+c.y)*na/(na+nb), (c.x+c.y)*nb/(na+nb)
		if ea > 0 {
			stat += (c.x - ea) * (c.x - ea) / ea
		}
		if eb > 0 {
			stat += (c.y - eb) * (c.y - eb) / eb
		}
	}
	k := float64(df)
	return (math.Cbrt(stat/k) - (1 - 2/(9*k))) / math.Sqrt(2/(9*k)), df
}

// TestSpacingPassFlaggedSeeds repeats TestSpacingPassDistribution's
// cross-rank comparison for the seeds it flagged under TinyGo, with more runs,
// prints the counts, and fails when the two samples differ (z > 4.5). The
// seeds were flagged in runs without jitterMapDraws, whose samples were not
// independent: the same code gave z 11.1 in one process and -0.8 in another.
func TestSpacingPassFlaggedSeeds(t *testing.T) {
	if os.Getenv("D2_TINYGO_ORDER") == "" && runtime.Compiler != "tinygo" {
		t.Skip("set D2_TINYGO_ORDER=1 (runs by default under TinyGo)")
	}
	const iters = 4000
	type job struct {
		pass string
		seed int64
	}
	for _, j := range []job{{"cross-rank", 16}, {"cross-rank", 189}, {"rank", 189}, {"rank", 198}} {
		seed := j.seed
		orig := map[string]int{}
		rewr := map[string]int{}
		g, ix := crossRankTestGraph(seed)
		restore := snapshotGeometry(g)
		for it := 0; it < iters; it++ {
			restore()
			jitterMapDraws()
			if j.pass == "rank" {
				legacyAdjustRankSpacing(g, MIN_RANK_SEP, false, nil)
			} else {
				legacyAdjustCrossRankSpacing(g, MIN_RANK_SEP, false, nil)
			}
			orig[geometryKeyWithRoutes(g)]++
			restore()
			jitterMapDraws()
			if j.pass == "rank" {
				adjustRankSpacing(g, ix, MIN_RANK_SEP, false)
			} else {
				adjustCrossRankSpacing(g, ix, MIN_RANK_SEP, false)
			}
			rewr[geometryKeyWithRoutes(g)]++
		}
		z, df := chiSquareZ(orig, rewr)
		keys := map[string]bool{}
		for k := range orig {
			keys[k] = true
		}
		for k := range rewr {
			keys[k] = true
		}
		var ks []string
		for k := range keys {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		var b strings.Builder
		for _, k := range ks {
			fmt.Fprintf(&b, " %s:%d/%d", k, orig[k], rewr[k])
		}
		t.Logf("%s %s seed %d: z %.1f (df %d); original/rewrite counts:%s", runtime.Compiler, j.pass, seed, z, df, b.String())
		if z > 4.5 {
			t.Errorf("%s %s seed %d: the rewrite's results come from another distribution than the original's (z %.1f)", runtime.Compiler, j.pass, seed, z)
		}
	}
}

func geometryKeyWithRoutes(g *d2graph.Graph) string {
	h := sha256.New()
	for _, o := range g.Objects {
		fmt.Fprintf(h, "%x %x %x %x;", math.Float64bits(o.TopLeft.X), math.Float64bits(o.TopLeft.Y), math.Float64bits(o.Width), math.Float64bits(o.Height))
	}
	for _, e := range g.Edges {
		for _, p := range e.Route {
			fmt.Fprintf(h, "%x %x,", math.Float64bits(p.X), math.Float64bits(p.Y))
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil)[:6])
}

// snapshotGeometry records the geometry of g's objects and edge routes, and
// returns a function that puts it back. Both versions run on the same
// objects (the same pointers): under TinyGo the order of a map with pointer
// keys depends on the pointers' values, so separate copies of a graph would
// compare different distributions.
func snapshotGeometry(g *d2graph.Graph) func() {
	type box struct{ x, y, w, h float64 }
	boxes := make([]box, len(g.Objects))
	for i, o := range g.Objects {
		boxes[i] = box{o.TopLeft.X, o.TopLeft.Y, o.Width, o.Height}
	}
	routes := make([][]geo.Point, len(g.Edges))
	for i, e := range g.Edges {
		for _, p := range e.Route {
			routes[i] = append(routes[i], *p)
		}
	}
	return func() {
		for i, o := range g.Objects {
			b := boxes[i]
			o.TopLeft.X, o.TopLeft.Y, o.Width, o.Height = b.x, b.y, b.w, b.h
		}
		for i, e := range g.Edges {
			for j, p := range e.Route {
				*p = routes[i][j]
			}
		}
	}
}
