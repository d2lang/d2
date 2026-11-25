package d2cli

import (
	"math"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

func compileGridRoutes(t *testing.T, engine, source string) *d2graph.Graph {
	t.Helper()
	ctx := log.WithDefault(t.Context())
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	_, g, err := d2lib.Compile(ctx, source, &d2lib.CompileOptions{
		Ruler: ruler, Layout: &engine, LayoutResolver: LayoutResolver(ctx, nil), RouterResolver: RouterResolver(ctx, nil),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestGridEdgeRouting(t *testing.T) {
	for _, engine := range builtinLayoutNames() {
		t.Run(engine, func(t *testing.T) {
			t.Run("obstacle", func(t *testing.T) {
				g := compileGridRoutes(t, engine, "grid-rows: 1\na; b; c\na -> c")
				route, middle := g.Edges[0].Route, g.Root.Children["b"].Box
				crosses := false
				for i, p := range route[1:] {
					if middle.Intersects(geo.Segment{Start: route[i], End: p}, -1) {
						crosses = true
					}
				}
				if crosses == (engine == "tala") || (engine != "tala" && len(route) != 2) {
					t.Fatalf("route %v crosses middle cell: %v", route, crosses)
				}
			})
			t.Run("SQL row ports", func(t *testing.T) {
				const users = "users: {shape: sql_table; id: int; email: text; name: text}"
				const orders = "orders: {shape: sql_table; id: int; total: int; user_id: int}"
				for _, source := range []string{
					"grid-rows: 1\n" + users + "\n" + orders + "\nusers.id -> orders.user_id",
					"grid-rows: 1\na: {grid-rows: 1; grid-gap: 0; " + users + "}\nb: {grid-rows: 1; grid-gap: 0; " + orders + "}\na.users.id -> b.orders.user_id",
				} {
					e := compileGridRoutes(t, engine, source).Edges[0]
					start, end := e.Route[0], e.Route[len(e.Route)-1]
					fromY, toY := e.Src.TopLeft.Y+1.5*e.Src.Height/4, e.Dst.TopLeft.Y+3.5*e.Dst.Height/4
					if math.Abs(start.Y-fromY) > 1 || math.Abs(end.Y-toY) > 1 {
						t.Fatalf("ports %v and %v, want row centers %.1f and %.1f", start, end, fromY, toY)
					}
				}
			})
			t.Run("SQL row to circle", func(t *testing.T) {
				const source = "grid-rows: 1\na: {shape: sql_table; id: int; email: text; name: text}\nb: {shape: circle; width: 144; height: 144}\na.id -> b"
				e := compileGridRoutes(t, engine, source).Edges[0]
				first, last := e.Route[0], e.Route[len(e.Route)-1]
				if want := e.Src.TopLeft.Y + 1.5*e.Src.Height/4; math.Abs(first.Y-want) > 1 {
					t.Fatalf("SQL endpoint %v left row at %.1f", first, want)
				}
				center := e.Dst.Center()
				if math.Abs(math.Hypot(last.X-center.X, last.Y-center.Y)-e.Dst.Width/2) > 1.5 {
					t.Fatalf("endpoint %v is off circle %v", last, e.Dst.Box)
				}
			})
			t.Run("reciprocal SQL ports", func(t *testing.T) {
				const source = "grid-rows: 1\na: {shape: sql_table; id: int; more: int}\nb: {shape: sql_table; more: int; fk: int}\na.id -> b.fk\nb.fk -> a.id"
				g := compileGridRoutes(t, engine, source)
				a, b := g.Edges[0], g.Edges[1]
				wantA, wantB := a.Src.TopLeft.Y+a.Src.Height/2, a.Dst.TopLeft.Y+2.5*a.Dst.Height/3
				for _, e := range g.Edges {
					first, last := e.Route[0], e.Route[len(e.Route)-1]
					if e == b {
						first, last = last, first
					}
					if math.Abs(first.Y-wantA) > 1 || math.Abs(last.Y-wantB) > 1 {
						t.Fatalf("endpoints %v and %v left their rows", first, last)
					}
				}
				if len(a.Route) == len(b.Route) {
					for i := range a.Route {
						if !a.Route[i].Equals(b.Route[len(b.Route)-1-i]) {
							return
						}
					}
					t.Fatal("reciprocal SQL connections follow the same route")
				}
			})

			t.Run("touching cells", func(t *testing.T) {
				for _, axis := range []string{"grid-rows", "grid-columns"} {
					g := compileGridRoutes(t, engine, axis+": 1\ngrid-gap: 0\na -> b: start\nb -> a: done")
					for _, e := range g.Edges {
						if len(e.Route) < 2 || geo.Route(e.Route).Length() < 10 {
							t.Fatalf("%s touching cells collapse connection %v", axis, e.Route)
						}
					}
				}
			})

			t.Run("reciprocal", func(t *testing.T) {
				g := compileGridRoutes(t, engine, "grid-rows: 1\na -> b: start\nb -> a: done")
				forward, back := g.Edges[0].Route, g.Edges[1].Route
				if forward[0].Equals(back[len(back)-1]) && forward[len(forward)-1].Equals(back[0]) {
					t.Fatalf("reciprocal edges share attachments: %v and %v", forward, back)
				}
			})
			if engine != "dagre" { // Dagre does not support loops on containers.
				t.Run("container loops", func(t *testing.T) {
					for _, source := range []string{
						"worker: {grid-rows: 1; job}\nworker -> worker",
						"grid-rows: 1\nworker: {job}\nworker -> worker",
					} {
						e := compileGridRoutes(t, engine, source).Edges[0]
						if len(e.Route) < 3 || e.Route[0].Equals(e.Route[len(e.Route)-1]) {
							t.Fatalf("invalid loop on %s: %v", e.Src.AbsID(), e.Route)
						}
					}
				})
			}
			t.Run("loop", func(t *testing.T) {
				g := compileGridRoutes(t, engine, "grid-rows: 1\na -> a: retry")
				e := g.Edges[0]
				if len(e.Route) < 3 || e.Route[0].Equals(e.Route[len(e.Route)-1]) {
					t.Fatalf("loop did not leave and return to a distinct port: %v", e.Route)
				}
				for _, p := range e.Route {
					if !e.Src.Box.Contains(p) {
						return
					}
				}
				t.Fatal("loop remained inside its owner")
			})
		})
	}
}

func TestNestedGridContainsRoutedDetour(t *testing.T) {
	g := compileGridRoutes(t, "tala", `grid-columns: 1
grid-gap: 0
inner: "" {grid-gap: 0; grid-rows: 1; a; b; c; a -> c}
next`)
	inner := g.Root.Children["inner"]
	for _, p := range g.Edges[0].Route {
		if !inner.Box.Contains(p) {
			t.Fatalf("route %v left grid %v", g.Edges[0].Route, inner.Box)
		}
	}
}

func TestGridRoutesAroundFrozenLoop(t *testing.T) {
	const source = "grid-rows: 1\na -> a: an existing long caption"
	original := compileGridRoutes(t, "tala", source).Edges[0].Route
	g := compileGridRoutes(t, "tala", source+"\na -> a: x")
	frozen, added := g.Edges[0], g.Edges[1]
	frozen.Route, added.Route = original, nil
	snapshot := make([]geo.Point, len(original))
	for i, point := range original {
		snapshot[i] = *point
	}
	if err := d2talalayout.RouteEdges(log.WithDefault(t.Context()), g, []*d2graph.Edge{added}); err != nil {
		t.Fatal(err)
	}
	if len(frozen.Route) != len(snapshot) {
		t.Fatalf("retained route changed: %v", frozen.Route)
	}
	for i, p := range snapshot {
		if !p.Equals(frozen.Route[i]) {
			t.Fatalf("retained route moved: %v", frozen.Route)
		}
	}
	if len(added.Route) < 3 || added.Route[0].Equals(added.Route[len(added.Route)-1]) || geo.Route(added.Route).Length() < 10 {
		t.Fatalf("new loop has no visible route: %v", added.Route)
	}
	if len(snapshot) == len(added.Route) {
		for i, p := range snapshot {
			if !p.Equals(added.Route[i]) {
				return
			}
		}
		t.Fatalf("new loop reused retained loop's route: %v", added.Route)
	}
}
