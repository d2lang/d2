package d2dagrelayout

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// largeCrossRankTestGraph builds a random nested graph like
// crossRankTestGraph, with more objects (20 to 79) on a taller grid and more
// nesting, so that many cross-rank shifts see 14 objects or more (TinyGo's
// maps grow at the 14th key) and many grow passes depend on the order of the
// seen objects.
func largeCrossRankTestGraph(seed int64) (*d2graph.Graph, *objectIndex) {
	r := rand.New(rand.NewSource(seed))
	g := d2graph.NewGraph()
	positions := []label.Position{
		label.OutsideTopLeft, label.OutsideTopCenter, label.OutsideRightMiddle,
		label.OutsideLeftMiddle, label.OutsideBottomCenter, label.InsideMiddleLeft,
		label.InsideMiddleRight, label.InsideTopCenter, label.InsideMiddleCenter,
	}
	n := 20 + r.Intn(60)
	topLefts := make([]geo.Point, n)
	for i := 0; i < n; i++ {
		parent := g.Root
		if i > 0 && r.Intn(2) == 0 {
			parent = g.Objects[r.Intn(len(g.Objects))]
		}
		id := fmt.Sprintf("o%d", i)
		obj := &d2graph.Object{Graph: g, Parent: parent, ID: id}
		obj.Box = geo.NewBox(nil, float64(20*(1+r.Intn(6))), float64(20*(1+r.Intn(4))))
		topLefts[i] = geo.Point{X: float64(40 * r.Intn(8)), Y: float64(40 * r.Intn(16))}
		obj.TopLeft = &topLefts[i]
		obj.Label.Value = id
		obj.LabelDimensions.Width = 10 * (1 + r.Intn(8))
		obj.LabelDimensions.Height = 10 * (1 + r.Intn(3))
		obj.LabelPosition = go2String(positions[r.Intn(len(positions))].String())
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

// TestShadowWritesMatchLegacy runs the cross-rank pass with the
// original's maps repeated before every grow pass (shadowMaps) against the
// legacy pass, replaying the legacy run's map orders, on
// crossRankTestGraph's graphs and on larger ones. Before every grow pass
// that ranges over its map, the writes the rewrite repeats into the
// original's maps (originalMaps) must be the legacy run's writes into its
// maps up to that point, in the same order, and the results must be the
// legacy's bit for bit. The shadow maps are compiled in under TinyGo only,
// so under gc this test needs the build tag dagre_shadowmaps. CI runs the
// package's tests with it, so in the other tests too, every grow pass that
// may grow a container ranges over the map originalMaps makes:
//
//	go test -tags dagre_shadowmaps ./d2layouts/d2dagrelayout
func TestShadowWritesMatchLegacy(t *testing.T) {
	if !shadowMaps {
		t.Skip("shadow maps not compiled in; run with -tags dagre_shadowmaps")
	}
	var writes, passes, large int
	for _, gen := range []func(int64) (*d2graph.Graph, *objectIndex){crossRankTestGraph, largeCrossRankTestGraph} {
		for seed := int64(0); seed < 300; seed++ {
			for _, isHorizontal := range []bool{false, true} {
				legacy, _ := gen(seed)
				current, ix := gen(seed)
				what := fmt.Sprintf("graph of %d objects, seed %d horizontal %v", len(legacy.Objects), seed, isHorizontal)
				rec := new(legacyOrders)
				replay := newMapReplay(t, what, rec, legacy, current)
				replay.ix = ix
				legacyAdjustCrossRankSpacing(legacy, MIN_RANK_SEP, isHorizontal, rec)
				adjustCrossRankSpacingWith(current, ix, MIN_RANK_SEP, isHorizontal, false, replay)
				replay.done()
				sameGeometry(t, what, legacy, current)
				writes += replay.writes
				passes += replay.growPasses
				for i, v := range rec.visits {
					seen := 0
					for _, w := range rec.writes[i] {
						if w.m == "seen" {
							seen++
						}
					}
					if seen >= 14 && len(v) > 0 {
						large++
					}
				}
			}
		}
	}
	t.Logf("%d grow passes (%d with 14 or more seen objects), %d writes repeated", passes, large, writes)
	if passes < 5000 || large < 2000 || writes < 100000 {
		t.Fatalf("the graphs exercise too little: %d grow passes, %d large ones, %d writes", passes, large, writes)
	}
}
