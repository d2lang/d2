package d2elklayout

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// randomBendGraph builds a graph with n objects and m edges and randomizes,
// from seed, object boxes (negative sizes, NaN, infinities and huge coordinates
// included), shapes, 3d/multiple styles, SQL tables with column ends, routes
// snapped to a coarse grid (shared corners, overlaps, touching segments),
// shared point objects, negative zero, and duplicated objects and edges.
// The same arguments build the same graph.
func randomBendGraph(t *testing.T, n, m int, seed int64) (*d2graph.Graph, map[*d2graph.Edge]bool) {
	g := compileBenchmarkGraph(t, denseBenchmarkInput(n, m))
	rng := rand.New(rand.NewSource(seed))
	shapes := []string{d2target.ShapeRectangle, d2target.ShapeCircle, d2target.ShapeDiamond, d2target.ShapeCylinder,
		d2target.ShapeHexagon, d2target.ShapeOval, d2target.ShapePerson, d2target.ShapeCloud}
	coordinate := func(scale float64) float64 {
		switch r := rng.Intn(200); {
		case r == 0:
			return math.NaN()
		case r == 1:
			return math.Inf(1 - 2*rng.Intn(2))
		case r == 2:
			return math.Copysign(0, -1)
		case r == 3:
			return 1e12 * float64(1-2*rng.Intn(2))
		case r < 120:
			return math.Round(rng.Float64()*scale/10) * 10
		default:
			return rng.Float64() * scale
		}
	}
	for _, obj := range g.Objects {
		w, h := math.Round(rng.Float64()*200), math.Round(rng.Float64()*120)
		if rng.Intn(30) == 0 {
			w = -w
		}
		obj.Box = geo.NewBox(geo.NewPoint(coordinate(3000), coordinate(2000)), w, h)
		obj.Shape.Value = shapes[rng.Intn(len(shapes))]
		switch rng.Intn(12) {
		case 0:
			obj.Shape.Value = d2target.ShapeRectangle
			obj.Style.ThreeDee = &d2graph.Scalar{Value: "true"}
		case 1:
			obj.Style.Multiple = &d2graph.Scalar{Value: "true"}
		case 2:
			obj.Shape.Value = d2target.ShapeSQLTable
			columns := 1 + rng.Intn(5)
			obj.SQLTable = &d2target.SQLTable{}
			for c := 0; c < columns; c++ {
				obj.SQLTable.Columns = append(obj.SQLTable.Columns, d2target.SQLColumn{Name: d2target.Text{Label: fmt.Sprint("c", c)}})
			}
		}
	}
	var shared []*geo.Point
	for _, edge := range g.Edges {
		points := 2 + rng.Intn(9)
		x, y := coordinate(3000), coordinate(2000)
		route := make([]*geo.Point, 0, points)
		for p := 0; p < points; p++ {
			if p > 0 {
				if p%2 == 1 {
					x = coordinate(3000)
				} else {
					y = coordinate(2000)
				}
			}
			if len(shared) > 0 && rng.Intn(15) == 0 {
				route = append(route, shared[rng.Intn(len(shared))])
				continue
			}
			point := geo.NewPoint(x, y)
			if rng.Intn(10) == 0 {
				shared = append(shared, point)
			}
			route = append(route, point)
		}
		edge.Route = route
		if edge.Src.SQLTable != nil && rng.Intn(2) == 0 {
			edge.SrcTableColumnIndex = go2.Pointer(rng.Intn(len(edge.Src.SQLTable.Columns)))
		}
		if edge.Dst.SQLTable != nil && rng.Intn(2) == 0 {
			edge.DstTableColumnIndex = go2.Pointer(rng.Intn(len(edge.Dst.SQLTable.Columns)))
		}
	}
	if rng.Intn(3) == 0 && len(g.Objects) > 0 {
		g.Objects = append(g.Objects, g.Objects[rng.Intn(len(g.Objects))])
	}
	if rng.Intn(3) == 0 && len(g.Edges) > 0 {
		g.Edges = append(g.Edges, g.Edges[rng.Intn(len(g.Edges))])
	}
	var keep map[*d2graph.Edge]bool
	if rng.Intn(4) == 0 {
		keep = make(map[*d2graph.Edge]bool)
		for _, edge := range g.Edges {
			if rng.Intn(3) == 0 {
				keep[edge] = true
			}
		}
	}
	return g, keep
}

func routeBits(g *d2graph.Graph) [][2]uint64 {
	var out [][2]uint64
	for _, edge := range g.Edges {
		for _, p := range edge.Route {
			out = append(out, [2]uint64{math.Float64bits(p.X), math.Float64bits(p.Y)})
		}
		out = append(out, [2]uint64{math.MaxUint64, uint64(len(edge.Route))})
	}
	return out
}

// TestDeleteBendsMatchesOriginal runs the original bend cleanup
// (legacy_bends_test.go) and the current one in its three forms
// (chosen index, flat lists forced, grids forced, and with Layout's object and
// edge states) on the same random graphs and compares every route point bit for
// bit (or the panic).
func TestDeleteBendsMatchesOriginal(t *testing.T) {
	optionSets := []LayeredOptions{{}, {MergeEdges: true}, {EdgeNode: go2.Pointer(5.0), EdgeEdge: go2.Pointer(2.0)},
		{EdgeNode: go2.Pointer(0.0), EdgeEdge: go2.Pointer(40.0), MergeEdges: true}}
	sizes := [][2]int{{3, 4}, {8, 14}, {20, 40}, {40, 120}, {90, 260}}
	var storage bendGrids
	runs, changed, panics := 0, 0, 0
	for seed := int64(0); seed < 400; seed++ {
		size := sizes[seed%int64(len(sizes))]
		opts := optionSets[(seed/int64(len(sizes)))%int64(len(optionSets))]
		outcome := func(run func(g *d2graph.Graph, keep map[*d2graph.Edge]bool)) (result [][2]uint64, panicked string) {
			g, keep := randomBendGraph(t, size[0], size[1], seed)
			defer func() {
				if r := recover(); r != nil {
					panicked = fmt.Sprint(r)
				}
			}()
			run(g, keep)
			return routeBits(g), ""
		}
		before, _ := outcome(func(*d2graph.Graph, map[*d2graph.Edge]bool) {})
		want, wantPanic := outcome(func(g *d2graph.Graph, keep map[*d2graph.Edge]bool) { origDeleteBends(g, opts, keep) })
		if wantPanic != "" {
			panics++
		} else if fmt.Sprint(before) != fmt.Sprint(want) {
			changed++
		}
		forms := []func(g *d2graph.Graph, keep map[*d2graph.Edge]bool){
			func(g *d2graph.Graph, keep map[*d2graph.Edge]bool) { deleteBends(g, opts, keep) },
			func(g *d2graph.Graph, keep map[*d2graph.Edge]bool) {
				scan := true
				deleteBendsWith(g, opts, keep, &scan, nil)
			},
			func(g *d2graph.Graph, keep map[*d2graph.Edge]bool) {
				scan := false
				deleteBendsWith(g, opts, keep, &scan, nil)
			},
			func(g *d2graph.Graph, keep map[*d2graph.Edge]bool) {
				objects := make(map[*d2graph.Object]*elkObject)
				var states slab[elkObject]
				walkObjects(g, objects, &states, nil)
				edgeStates := make(map[*d2graph.Edge]*elkEdgeState)
				edgeAt := make([]*elkEdgeState, len(g.Edges))
				for i, edge := range g.Edges {
					if edgeStates[edge] == nil {
						edgeStates[edge] = &elkEdgeState{first: i}
					}
					edgeAt[i] = edgeStates[edge]
				}
				storage.states, storage.edgeAt = objects, edgeAt
				defer storage.release()
				deleteBendsWith(g, opts, keep, nil, &storage)
			},
		}
		for form, run := range forms {
			got, gotPanic := outcome(run)
			runs++
			if gotPanic != wantPanic {
				t.Fatalf("seed %d size %v opts %+v form %d: panic %q, original %q", seed, size, opts, form, gotPanic, wantPanic)
			}
			if len(got) != len(want) {
				t.Fatalf("seed %d size %v form %d: %d route entries, original %d", seed, size, form, len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("seed %d size %v opts %+v form %d: route entry %d = %v, original %v", seed, size, opts, form, i, got[i], want[i])
				}
			}
		}
	}
	t.Logf("%d runs compared; the original changed routes in %d of 400 graphs and panicked in %d", runs, changed, panics)
}
