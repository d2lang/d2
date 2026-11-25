package d2lib

import (
	"math"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
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
