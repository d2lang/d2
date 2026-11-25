package d2elklayout

import (
	"math"
	"math/rand"
	"testing"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

func TestBendCollisionIndexMatchesBruteForce(t *testing.T) {
	g := compileBenchmarkGraph(t, denseBenchmarkInput(60, 180))
	setSyntheticRoutes(g)
	index := newBendCollisionIndex(g, LayeredOptions{})

	queries := make([]geo.Segment, 0, len(g.Edges)*2+100)
	for _, edge := range g.Edges {
		for i := 0; i+1 < len(edge.Route); i += 2 {
			queries = append(queries, *geo.NewSegment(edge.Route[i].Copy(), edge.Route[i+1].Copy()))
		}
	}
	random := rand.New(rand.NewSource(1))
	for i := 0; i < 100; i++ {
		x := random.Float64()*4500 - 250
		y := random.Float64()*800 - 150
		dx := random.Float64()*700 - 350
		dy := random.Float64()*200 - 100
		queries = append(queries, *geo.NewSegment(geo.NewPoint(x, y), geo.NewPoint(x+dx, y+dy)))
	}
	// Exercise the fallback path for a query spanning more than the grid's
	// bounded number of cells.
	queries = append(queries, *geo.NewSegment(geo.NewPoint(-100_000, 20), geo.NewPoint(100_000, 20)))

	assertBendCollisionQueriesMatch(t, g, index, queries)

	// The production algorithm mutates routes after accepting a simplification.
	// Verify removals and insertions keep the broad phase exact.
	changed := g.Edges[len(g.Edges)/2]
	changed.Route = []*geo.Point{
		geo.NewPoint(-25_000, -50),
		geo.NewPoint(25_000, -50),
		geo.NewPoint(25_000, 300),
		geo.NewPoint(80, 300),
	}
	index.updateEdge(changed)
	assertBendCollisionQueriesMatch(t, g, index, queries)
}

// TestLinearBendIndexMatchesGrid checks that scanning flat lists counts
// exactly what the grid counts, for graphs of both sizes, awkward queries
// (non-finite, huge, degenerate) and after routes change.
func TestLinearBendIndexMatchesGrid(t *testing.T) {
	for _, size := range [][2]int{{8, 12}, {30, 40}, {60, 180}} {
		g := compileBenchmarkGraph(t, denseBenchmarkInput(size[0], size[1]))
		setSyntheticRoutes(g)
		// A duplicated object and edge are indexed once by the grid.
		g.Objects = append(g.Objects, g.Objects[0])
		g.Edges = append(g.Edges, g.Edges[0])
		for _, opts := range []LayeredOptions{{}, {EdgeNode: go2.Pointer(3.0), EdgeEdge: go2.Pointer(0.5)}} {
			grid := newBendCollisionIndexWith(g, opts, false, nil)
			linear := newBendCollisionIndexWith(g, opts, true, nil)
			random := rand.New(rand.NewSource(int64(size[0])))
			queries := []geo.Segment{
				*geo.NewSegment(geo.NewPoint(-100_000, 20), geo.NewPoint(100_000, 20)),
				*geo.NewSegment(geo.NewPoint(math.NaN(), 0), geo.NewPoint(10, 10)),
				*geo.NewSegment(geo.NewPoint(math.Inf(-1), 5), geo.NewPoint(math.Inf(1), 5)),
				*geo.NewSegment(geo.NewPoint(40, 40), geo.NewPoint(40, 40)),
			}
			for _, edge := range g.Edges {
				for i := 0; i+1 < len(edge.Route); i++ {
					queries = append(queries, *geo.NewSegment(edge.Route[i].Copy(), edge.Route[i+1].Copy()))
				}
			}
			for i := 0; i < 300; i++ {
				x := random.Float64()*4500 - 250
				y := random.Float64()*800 - 150
				queries = append(queries, *geo.NewSegment(geo.NewPoint(x, y), geo.NewPoint(x+random.Float64()*700-350, y)))
				queries = append(queries, *geo.NewSegment(geo.NewPoint(x, y), geo.NewPoint(x, y+random.Float64()*400-200)))
			}
			compare := func() {
				for i, query := range queries {
					source := g.Edges[i%len(g.Edges)]
					if a, b := grid.countObjectIntersects(source.Src, source.Dst, query),
						linear.countObjectIntersects(source.Src, source.Dst, query); a != b {
						t.Fatalf("%v query %d: objects grid %d, linear %d", size, i, a, b)
					}
					a1, a2, a3, a4 := grid.countEdgeIntersects(source, query)
					b1, b2, b3, b4 := linear.countEdgeIntersects(source, query)
					if a1 != b1 || a2 != b2 || a3 != b3 || a4 != b4 {
						t.Fatalf("%v query %d: edges grid (%d,%d,%d,%d), linear (%d,%d,%d,%d)", size, i, a1, a2, a3, a4, b1, b2, b3, b4)
					}
				}
			}
			compare()
			// Routes change as deleteBends commits simplifications.
			for i, edge := range g.Edges {
				if i%3 == 0 && len(edge.Route) > 3 {
					edge.Route = append([]*geo.Point{geo.NewPoint(edge.Route[0].X+5, edge.Route[2].Y)}, edge.Route[3:]...)
					grid.updateEdge(edge)
					linear.updateEdge(edge)
				}
			}
			compare()
			setSyntheticRoutes(g)
		}
	}
}

// TestGridIndexMatchesScan checks the grid against a scan of all items for
// regular, inverted, non-finite and huge bounds, after removals too, and with
// extents too large for 128-unit cells or not finite.
func TestGridIndexMatchesScan(t *testing.T) {
	random := rand.New(rand.NewSource(3))
	value := func() float64 {
		switch random.Intn(20) {
		case 0:
			return math.NaN()
		case 1:
			return math.Inf(1 - 2*random.Intn(2))
		case 2:
			return (random.Float64() - 0.5) * 1e300
		case 3:
			return float64(1<<31) * bendIndexCellSize * (1 - 2*float64(random.Intn(2)))
		}
		return random.Float64()*5000 - 1000
	}
	box := func() spatialBounds {
		x, y := value(), value()
		w, h := random.Float64()*600-50, random.Float64()*600-50
		return spatialBounds{x, y, x + w, y + h}
	}
	var grid gridIndex
	for round := 0; round < 20; round++ {
		var items []spatialBounds
		extent := spatialBounds{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		for i := 0; i < 200; i++ {
			b := box()
			items = append(items, b)
			if round%5 != 4 {
				extent = union(extent, b)
			}
		}
		if round%5 == 3 {
			extent = spatialBounds{-1e300, -1e300, 1e300, 1e300}
		}
		grid.reset(extent)
		alive := make([]bool, len(items))
		for i, b := range items {
			if id := grid.add(b); int(id) != i {
				t.Fatalf("id %d for item %d", id, i)
			}
			alive[i] = true
		}
		for i := range items {
			if random.Intn(4) == 0 {
				grid.remove(int32(i))
				alive[i] = false
			}
		}
		var found []int32
		for q := 0; q < 300; q++ {
			query := box()
			found = grid.collect(query, found[:0])
			want := 0
			for i, b := range items {
				if alive[i] && b.overlaps(query) {
					want++
				}
			}
			seen := map[int32]bool{}
			for _, id := range found {
				if seen[id] || !alive[id] || !items[id].overlaps(query) {
					t.Fatalf("round %d query %v: item %d found wrongly", round, query, id)
				}
				seen[id] = true
			}
			if len(found) != want {
				t.Fatalf("round %d query %v: %d found, want %d", round, query, len(found), want)
			}
		}
	}
}

func assertBendCollisionQueriesMatch(t *testing.T, g *d2graph.Graph, index *bendCollisionIndex, queries []geo.Segment) {
	t.Helper()
	for queryIndex, query := range queries {
		sourceEdge := g.Edges[queryIndex%len(g.Edges)]
		gotObjects := index.countObjectIntersects(sourceEdge.Src, sourceEdge.Dst, query)
		wantObjects := bruteObjectIntersects(g, sourceEdge.Src, sourceEdge.Dst, query)
		if gotObjects != wantObjects {
			t.Fatalf("query %d object intersections = %d, want %d", queryIndex, gotObjects, wantObjects)
		}
		gotCrossings, gotOverlaps, gotClose, gotTouching := index.countEdgeIntersects(sourceEdge, query)
		wantCrossings, wantOverlaps, wantClose, wantTouching := bruteEdgeIntersects(g, sourceEdge, query)
		if gotCrossings != wantCrossings || gotOverlaps != wantOverlaps || gotClose != wantClose || gotTouching != wantTouching {
			t.Fatalf(
				"query %d edge intersections = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
				queryIndex, gotCrossings, gotOverlaps, gotClose, gotTouching,
				wantCrossings, wantOverlaps, wantClose, wantTouching,
			)
		}
	}
}

func bruteObjectIntersects(g *d2graph.Graph, src, dst *d2graph.Object, segment geo.Segment) int {
	count := 0
	for _, obj := range g.Objects {
		if obj == src || obj == dst {
			continue
		}
		if obj.Intersects(segment, float64(edge_node_spacing)-1) {
			count++
		}
	}
	return count
}

func bruteEdgeIntersects(g *d2graph.Graph, sourceEdge *d2graph.Edge, segment geo.Segment) (int, int, int, int) {
	isHorizontal := math.Ceil(segment.Start.Y) == math.Ceil(segment.End.Y)
	crossingsCount := 0
	overlapsCount := 0
	closeOverlapsCount := 0
	touchingCount := 0
	for _, edge := range g.Edges {
		if edge == sourceEdge {
			continue
		}
		for i := 0; i+1 < len(edge.Route); i++ {
			otherSegment := *geo.NewSegment(edge.Route[i], edge.Route[i+1])
			otherIsHorizontal := math.Ceil(otherSegment.Start.Y) == math.Ceil(otherSegment.End.Y)
			if isHorizontal == otherIsHorizontal {
				if segment.Overlaps(otherSegment, !isHorizontal, 0.) {
					if isHorizontal {
						if math.Abs(segment.Start.Y-otherSegment.Start.Y) < float64(edge_node_spacing)/2. {
							overlapsCount++
							if math.Abs(segment.Start.Y-otherSegment.Start.Y) < float64(edge_node_spacing)/4. {
								closeOverlapsCount++
								if math.Abs(segment.Start.Y-otherSegment.Start.Y) < 1. {
									touchingCount++
								}
							}
						}
					} else if math.Abs(segment.Start.X-otherSegment.Start.X) < float64(edge_node_spacing)/2. {
						overlapsCount++
						if math.Abs(segment.Start.X-otherSegment.Start.X) < float64(edge_node_spacing)/4. {
							closeOverlapsCount++
							if math.Abs(segment.Start.Y-otherSegment.Start.Y) < 1. {
								touchingCount++
							}
						}
					}
				}
			} else if segment.Intersects(otherSegment) {
				crossingsCount++
			}
		}
	}
	return crossingsCount, overlapsCount, closeOverlapsCount, touchingCount
}

// TestDeleteBendsSameWithBothIndexForms runs the whole bend cleanup with the
// grid and with flat lists (and their point counting) on the same routes and
// compares the results bit for bit, shared points and merged edges included.
func TestDeleteBendsSameWithBothIndexForms(t *testing.T) {
	for _, size := range [][2]int{{6, 10}, {20, 40}, {60, 180}} {
		for _, opts := range []LayeredOptions{{}, {MergeEdges: true}, {EdgeNode: go2.Pointer(5.0), EdgeEdge: go2.Pointer(2.0)}} {
			results := make([][][2]uint64, 2)
			for form, scan := range []bool{false, true} {
				g := compileBenchmarkGraph(t, denseBenchmarkInput(size[0], size[1]))
				setSyntheticRoutes(g)
				// Ladders whose corners coincide with another route's points,
				// including a shared point object and a negative zero.
				if len(g.Edges) > 3 {
					g.Edges[1].Route[2] = g.Edges[0].Route[2]
					g.Edges[3].Route[3] = geo.NewPoint(g.Edges[2].Route[3].X, g.Edges[2].Route[3].Y)
					g.Edges[2].Route[1].X = math.Copysign(0, -1)
				}
				deleteBendsWith(g, opts, nil, &scan, nil)
				for _, edge := range g.Edges {
					for _, p := range edge.Route {
						results[form] = append(results[form], [2]uint64{math.Float64bits(p.X), math.Float64bits(p.Y)})
					}
					results[form] = append(results[form], [2]uint64{})
				}
			}
			if len(results[0]) != len(results[1]) {
				t.Fatalf("%v: route lengths differ", size)
			}
			for i := range results[0] {
				if results[0][i] != results[1][i] {
					t.Fatalf("%v %+v: point %d differs: grid %v, scan %v", size, opts, i, results[0][i], results[1][i])
				}
			}
		}
	}
}

// TestDeleteBendsSameWithLayoutStates runs the bend cleanup with the storage
// and identities Layout gives it (object states from its walk, edge states
// with their first listing), reused across calls, and without them, on
// graphs with a duplicated object and edge, and compares the routes.
func TestDeleteBendsSameWithLayoutStates(t *testing.T) {
	var storage bendGrids
	for _, size := range [][2]int{{6, 10}, {20, 40}, {60, 180}, {8, 12}} {
		for _, opts := range []LayeredOptions{{}, {MergeEdges: true}} {
			results := make([][][2]uint64, 2)
			for form := range results {
				g := compileBenchmarkGraph(t, denseBenchmarkInput(size[0], size[1]))
				setSyntheticRoutes(g)
				g.Objects = append(g.Objects, g.Objects[0])
				g.Edges = append(g.Edges, g.Edges[0])
				if form == 0 {
					deleteBendsWith(g, opts, nil, nil, nil)
				} else {
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
					deleteBendsWith(g, opts, nil, nil, &storage)
					storage.release()
				}
				for _, edge := range g.Edges {
					for _, p := range edge.Route {
						results[form] = append(results[form], [2]uint64{math.Float64bits(p.X), math.Float64bits(p.Y)})
					}
					results[form] = append(results[form], [2]uint64{})
				}
			}
			if len(results[0]) != len(results[1]) {
				t.Fatalf("%v: route lengths differ", size)
			}
			for i := range results[0] {
				if results[0][i] != results[1][i] {
					t.Fatalf("%v %+v: point %d differs", size, opts, i)
				}
			}
		}
	}
}
