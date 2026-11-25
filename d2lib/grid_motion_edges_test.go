package d2lib

import (
	"math"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/log"
)

func gridEdgeTestShape(id string, x, y int) d2target.Shape {
	s := *d2target.BaseShape()
	s.ID, s.Type = id, d2target.ShapeRectangle
	s.Pos, s.Width, s.Height = d2target.Point{X: x, Y: y}, 40, 40
	return s
}

func gridEdgeTestConnection(id, src, dst string, route ...*geo.Point) d2target.Connection {
	e := *d2target.BaseConnection()
	e.ID, e.Src, e.Dst, e.Route = id, src, dst, route
	e.Label, e.LabelPosition = "consume", "INSIDE_MIDDLE_CENTER"
	e.FontSize, e.LabelWidth, e.LabelHeight = 12, 48, 12
	return e
}

func TestGridMotionAttachedRouting(t *testing.T) {
	a, b := gridEdgeTestShape("a", 0, 0), gridEdgeTestShape("b", 200, 0)
	a.Type = d2target.ShapeOval
	e := gridEdgeTestConnection("link", "a", "b", geo.NewPoint(40, 20), geo.NewPoint(200, 20))
	before := &d2target.Diagram{Shapes: []d2target.Shape{a, b}, Connections: []d2target.Connection{e}}
	a.Pos.X, b.Pos.X = 200, 0
	e.Route = []*geo.Point{geo.NewPoint(200, 20), geo.NewPoint(40, 20)}
	after := &d2target.Diagram{Shapes: []d2target.Shape{a, b}, Connections: []d2target.Connection{e}}
	m := newGridMotionEdges(before, after, nil, nil, map[string]bool{"a": true, "b": true})
	a.Pos = d2target.Point{X: 100, Y: -60}
	b.Pos = d2target.Point{X: 100, Y: 60}
	// The arc sends the two cells around different sides. The connection
	// follows their vertical separation instead of collapsing at the center.
	edges := m.sample(after.Connections, []d2target.Shape{a, b}, 0.5, 1, 1, nil)
	if len(edges) != 1 || !edges[0].Route[0].Equals(geo.NewPoint(120, -20)) || !edges[0].Route[1].Equals(geo.NewPoint(120, 60)) {
		t.Fatalf("sampled route = %+v", edges)
	}
	if err := d2target.ValidateRenderTarget(&d2target.Diagram{Shapes: []d2target.Shape{a, b}, Connections: edges}); err != nil {
		t.Fatal(err)
	}
	if !before.Connections[0].Route[0].Equals(geo.NewPoint(40, 20)) || !after.Connections[0].Route[1].Equals(geo.NewPoint(40, 20)) {
		t.Fatal("mutated authored endpoint route")
	}
	a.Pos = b.Pos
	if edges := m.sample(after.Connections, []d2target.Shape{a, b}, 0.5, 1, 1, nil); len(edges) != 0 {
		t.Fatal("drew a shaft between overlapping cells")
	}
}

func TestGridMotionKeepsCenteredSQLPorts(t *testing.T) {
	board, graph := compileGridMotionTest(t, `grid-rows: 1
a: {shape: sql_table; id: int; value: int}
b: {shape: sql_table; fk: int; value: int}
a.id -> b.fk`)
	edges := newGridMotionEdges(board, board, graph, graph, map[string]bool{"b": true})
	shapes := slices.Clone(board.Shapes)
	shapes[1].Pos.Y += 80
	actual := edges.sample(board.Connections, shapes, .5, 1, 1, nil)
	if len(actual) != 1 {
		t.Fatalf("moving cell lost its row edge")
	}
	original, sampled := board.Connections[0].Route, actual[0].Route
	want := geo.NewPoint(original[1].X, original[1].Y+80)
	if !sampled[0].Equals(original[0]) || !sampled[1].Equals(want) {
		t.Fatalf("row ports moved off their table rows: %v", sampled)
	}
}

func TestGridMotionPreservesOutsideCaptionPort(t *testing.T) {
	board, graph := compileGridMotionTest(t, `grid-rows: 1
a: Caption {width: 80; height: 60; label.near: outside-right-center}
b: "" {width: 80; height: 60}
a -> b`)
	captionPort := board.Connections[0].Route[0]
	a := shapeByID(t, board, "a")
	if captionPort.X <= float64(a.Pos.X+a.Width) {
		t.Fatalf("fixture missed the caption: %v", captionPort)
	}
	m := newGridMotionEdges(board, board, graph, graph, map[string]bool{"b": true})
	shapes := slices.Clone(board.Shapes)
	shapes[1].Pos.X += 50
	got := m.sample(board.Connections, shapes, .5, 1, 1, nil)
	if len(got) != 1 || !got[0].Route[0].Equals(captionPort) {
		t.Fatal("moving the destination pulled the wire through the source caption")
	}
}

func TestGridMotionSQLRowSwitchesFaces(t *testing.T) {
	for _, tc := range []struct{ engine, link string }{
		{"dagre", "a.id -> b"}, {"dagre", "b -> a.id"}, {"dagre", "a.id -> b.id"},
		{"tala", "a.id -> b"}, {"tala", "b -> a.id"},
	} {
		t.Run(tc.engine+"/"+tc.link, func(t *testing.T) {
			link := tc.link
			peer := "shape: circle; width: 120; height: 120"
			if link == "a.id -> b.id" {
				peer = "shape: sql_table; id: int; name: string"
			}
			source := `vars: {d2-config: {animate-interval: 1s; animate-duration: 600ms}}
grid-rows: 1
a: {shape: sql_table; id: int; name: string; value: text}
b: {` + peer + `}
` + link + `
steps: {swap: {
 a: null
 a: {shape: sql_table; id: int; name: string; value: text}
 ` + link + `
}}`
			opts := dagreCompileOptions(new(int))
			if tc.engine == "tala" {
				opts.Layout = &tc.engine
				opts.LayoutResolver = func(string) (d2graph.LayoutGraph, error) { return d2talalayout.DefaultLayout, nil }
				opts.RouterResolver = func(string) (d2graph.RouteEdges, error) { return d2talalayout.RouteEdges, nil }
			}
			board, _, err := Compile(log.WithDefault(t.Context()), source, opts, nil)
			if err != nil {
				t.Fatal(err)
			}
			frames := board.Steps[0].TweenFrames
			var left, right, crossing bool
			for i, f := range frames {
				a, b := shapeByID(t, f, "a"), shapeByID(t, f, "b")
				wantX := a.Pos.X
				switch {
				case a.Pos.X+a.Width < b.Pos.X:
					wantX += a.Width
					left = true
				case b.Pos.X+b.Width < a.Pos.X:
					right = true
				default:
					// A straight connection between the facing ports disappears as
					// the shapes pass through one another.
					if a.Pos.X < b.Pos.X+b.Width/2 && b.Pos.X < a.Pos.X+a.Width/2 {
						crossing = true
						if len(f.Connections) != 0 {
							t.Fatalf("frame %d drew a shaft inside the overlapping shapes", i)
						}
					}
					continue
				}
				if len(f.Connections) != 1 || len(f.Connections[0].Route) < 2 {
					t.Fatalf("frame %d lost the connection between separated shapes", i)
				}
				e := f.Connections[0]
				port, other := e.Route[0], e.Route[len(e.Route)-1]
				if e.Src != "a" {
					port, other = other, port
				}
				wantY := float64(a.Pos.Y) + 1.5*float64(a.Height)/4
				if math.Abs(port.X-float64(wantX)) > 1 || math.Abs(port.Y-wantY) > 1 {
					t.Fatalf("frame %d row endpoint (%.1f,%.1f), want (%d,%.1f)", i, port.X, port.Y, wantX, wantY)
				}
				if b.Type == d2target.ShapeSQLTable {
					otherX := b.Pos.X
					if wantX == a.Pos.X {
						otherX += b.Width
					}
					otherY := float64(b.Pos.Y) + 1.5*float64(b.Height)/3
					if math.Abs(other.X-float64(otherX)) > 1 || math.Abs(other.Y-otherY) > 1 {
						t.Fatalf("frame %d peer row endpoint (%.1f,%.1f), want (%d,%.1f)", i, other.X, other.Y, otherX, otherY)
					}
				} else if b.Type == d2target.ShapeOval {
					center := gridEdgeCenter(&b)
					if math.Abs(math.Hypot(other.X-center.X, other.Y-center.Y)-float64(b.Width)/2) > 1.5 {
						t.Fatalf("frame %d endpoint (%.1f,%.1f) left circle outline", i, other.X, other.Y)
					}
				}
			}
			if !left || !right || !crossing {
				t.Fatal("transition did not exercise separation on both sides and overlap")
			}
		})
	}
}

func TestGridMotionSQLAcrossSeparatedRows(t *testing.T) {
	for _, engine := range []string{"dagre", "tala"} {
		t.Run(engine, func(t *testing.T) {
			const source = `vars: {d2-config: {animate-interval: 1s; animate-duration: 600ms}}
grid-rows: 2
grid-columns: 2
a: {shape: sql_table; id: int; name: text}
x
y
b: {shape: sql_table; key: int; other: text}
b.key -> a.name
steps: {swap: {
  a: null; x: null; y: null; b: null
  x
  a: {shape: sql_table; id: int; name: text}
  b: {shape: sql_table; key: int; other: text}
  y
  b.key -> a.name
}}`
			opts := dagreCompileOptions(new(int))
			if engine == "tala" {
				opts.Layout = &engine
				opts.LayoutResolver = func(string) (d2graph.LayoutGraph, error) { return d2talalayout.DefaultLayout, nil }
				opts.RouterResolver = func(string) (d2graph.RouteEdges, error) { return d2talalayout.RouteEdges, nil }
			}
			board, _, err := Compile(log.WithDefault(t.Context()), source, opts, nil)
			if err != nil {
				t.Fatal(err)
			}
			crossed := false
			for i, f := range board.Steps[0].TweenFrames {
				a, b := shapeByID(t, f, "a"), shapeByID(t, f, "b")
				if a.Pos.Y+a.Height >= b.Pos.Y || len(f.Connections) != 1 {
					t.Fatalf("frame %d: wire count %d between %v and %v", i, len(f.Connections), a.Pos, b.Pos)
				}
				r := f.Connections[0].Route
				for _, check := range []struct {
					s    d2target.Shape
					port *geo.Point
					row  float64
				}{{b, r[0], 1.5 / 3}, {a, r[len(r)-1], 2.5 / 3}} {
					s, p := check.s, check.port
					onFace := min(math.Abs(p.X-float64(s.Pos.X)), math.Abs(p.X-float64(s.Pos.X+s.Width))) <= 1
					if !onFace || math.Abs(p.Y-float64(s.Pos.Y)-check.row*float64(s.Height)) > 1 {
						t.Fatalf("frame %d: port %v left its row on %s", i, p, s.ID)
					}
				}
				if a.Pos.X+a.Width <= b.Pos.X || b.Pos.X+b.Width <= a.Pos.X {
					continue
				}
				crossed = true
				for _, s := range []d2target.Shape{a, b} {
					box := geo.NewBox(geo.NewPoint(float64(s.Pos.X), float64(s.Pos.Y)), float64(s.Width), float64(s.Height))
					for j := 1; j < len(r); j++ {
						if box.Intersects(*geo.NewSegment(r[j-1], r[j]), -.75) {
							t.Fatalf("frame %d: segment %v..%v enters %s", i, r[j-1], r[j], s.ID)
						}
					}
				}
			}
			if !crossed {
				t.Fatal("fixture never swapped columns")
			}
		})
	}
}

func TestGridMotionEdgeLifecycle(t *testing.T) {
	a, b, c := gridEdgeTestShape("a", 0, 0), gridEdgeTestShape("b", 100, 0), gridEdgeTestShape("c", 0, 100)
	old := gridEdgeTestConnection("old", "a", "b", geo.NewPoint(40, 20), geo.NewPoint(100, 20))
	newEdge := gridEdgeTestConnection("new", "a", "c", geo.NewPoint(20, 40), geo.NewPoint(20, 100))
	old.Opacity, newEdge.Opacity = 0.6, 0.4
	before := &d2target.Diagram{Shapes: []d2target.Shape{a, b}, Connections: []d2target.Connection{old}}
	after := &d2target.Diagram{Shapes: []d2target.Shape{a, c}, Connections: []d2target.Connection{newEdge}}
	m := newGridMotionEdges(before, after, nil, nil, map[string]bool{"b": true, "c": true})
	edges := m.sample(after.Connections, []d2target.Shape{a, b, c}, 0.5, 0.25, 0.8, nil)
	if len(edges) != 2 || edges[0].ID != "new" || edges[0].Opacity != 0.1 || edges[1].ID != "old" || math.Abs(edges[1].Opacity-0.48) > 1e-9 {
		t.Fatalf("expected incoming and outgoing edges to follow their nodes' opacity: %+v", edges)
	}
	if got := m.sample(after.Connections, before.Shapes, 0, 0, 1, nil); len(got) != 1 || got[0].ID != "old" {
		t.Fatalf("unexpected edges at start: %+v", got)
	}
	if got := m.sample(after.Connections, after.Shapes, 1, 1, 0, nil); len(got) != 1 || got[0].ID != "new" {
		t.Fatalf("unexpected edges at end: %+v", got)
	}
}

func TestGridMotionRouteTopology(t *testing.T) {
	a, b := gridEdgeTestShape("a", 0, 0), gridEdgeTestShape("b", 200, 0)
	e := gridEdgeTestConnection("link", "a", "b", geo.NewPoint(40, 10), geo.NewPoint(72, -30), geo.NewPoint(180, -20), geo.NewPoint(200, 10))
	e.IsCurve = true
	before := &d2target.Diagram{Shapes: []d2target.Shape{a, b}, Connections: []d2target.Connection{e}}
	b.Pos.Y = 90
	e.IsCurve = false
	e.Route = []*geo.Point{geo.NewPoint(40, 10), geo.NewPoint(100, 10), geo.NewPoint(100, 100), geo.NewPoint(200, 100)}
	e.Stroke = "#00ff00"
	after := &d2target.Diagram{Shapes: []d2target.Shape{a, b}, Connections: []d2target.Connection{e}}
	m := newGridMotionEdges(before, after, nil, nil, map[string]bool{"b": true})
	// A non-centered port on a moving shape keeps its offset.
	b.Pos.Y = 58
	edges := m.sample(after.Connections, []d2target.Shape{a, b}, 0.5, 1, 1, nil)
	if len(edges) != 1 || edges[0].IsCurve || edges[0].Stroke != "#00ff00" || !edges[0].Route[len(edges[0].Route)-1].Equals(geo.NewPoint(200, 68)) {
		t.Fatalf("intermediate routed link = %+v", edges)
	}
	if err := d2target.ValidateRenderTarget(&d2target.Diagram{Shapes: []d2target.Shape{a, b}, Connections: edges}); err != nil {
		t.Fatal(err)
	}
}

func TestGridSequenceLifelinesFollowActors(t *testing.T) {
	board, _ := compileGridMotionTest(t, `
vars: {d2-config: {animate-interval: 1s; animate-duration: 900ms}}
grid-columns: 2
read: {shape: sequence_diagram; client -> server: request; server -> client: reply}
write: {shape: sequence_diagram; client -> server: append; server -> client: ack}
steps: {
 swap: {read: null; read: {shape: sequence_diagram; client -> server: request; server -> client: reply}}
 remove: {read: null}
 restore: {read: {shape: sequence_diagram; client -> server: request; server -> client: reply}}
}
`)
	// Both panels use the same actor names and synthetic destination IDs.
	// Each lifeline still follows its own fully qualified actor, including
	// during the pop when its shape and the route change scale.
	shapes := gridEdgeShapeMap(board.Shapes)
	lines := 0
	for _, line := range board.Connections {
		if shapes[line.Dst] != nil {
			continue
		}
		lines++
		actor := shapes[line.Src]
		for _, step := range board.Steps {
			if len(step.TweenFrames) == 0 {
				t.Fatalf("step %s has no samples", step.Name)
			}
			for i, frame := range step.TweenFrames {
				actorIndex := slices.IndexFunc(frame.Shapes, func(s d2target.Shape) bool { return s.ID == line.Src })
				edgeIndex := slices.IndexFunc(frame.Connections, func(e d2target.Connection) bool { return e.ID == line.ID })
				if actorIndex < 0 && edgeIndex < 0 {
					continue
				}
				if actorIndex < 0 || edgeIndex < 0 {
					t.Fatalf("%s frame %d: %s and its actor should appear together", step.Name, i, line.ID)
				}
				s, moved := frame.Shapes[actorIndex], frame.Connections[edgeIndex]
				if len(moved.Route) != len(line.Route) || moved.Dst != line.Dst || moved.Opacity != s.Opacity {
					t.Fatalf("%s frame %d changed lifeline topology or visibility %s", step.Name, i, line.ID)
				}
				for j, p := range line.Route {
					wantX := float64(s.Pos.X) + (p.X-float64(actor.Pos.X))*float64(s.Width)/float64(actor.Width)
					wantY := float64(s.Pos.Y) + (p.Y-float64(actor.Pos.Y))*float64(s.Height)/float64(actor.Height)
					got := moved.Route[j]
					if math.Abs(got.X-wantX) > 1 || math.Abs(got.Y-wantY) > 1 {
						t.Fatalf("%s frame %d detached lifeline %s from %s", step.Name, i, line.ID, s.ID)
					}
				}
			}
		}
	}
	if lines != 4 {
		t.Fatalf("expected four lifelines, got %d", lines)
	}
}
