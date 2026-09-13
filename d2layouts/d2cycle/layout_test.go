package d2cycle

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

func TestCalculateRadiusKeepsSingleObjectAtCenter(t *testing.T) {
	g, _, err := d2compiler.Compile("", strings.NewReader(`
shape: cycle
a
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	setObjectBoxes(g, 100, 100)

	if got := calculateRadius(g.Root.ChildrenArray); got != 0 {
		t.Fatalf("expected single-object cycle radius 0, got %.2f", got)
	}
}

func TestCycleLayoutPreservesNestedCoreLayout(t *testing.T) {
	g, _, err := d2compiler.Compile("", strings.NewReader(`
shape: cycle
a: {
  x
  y
  x -> y
}
b
a -> b
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	setObjectBoxes(g, 100, 100)

	layoutCalled := false
	coreLayout := func(_ context.Context, g *d2graph.Graph) error {
		layoutCalled = true
		for _, obj := range g.Objects {
			switch obj.ID {
			case "a":
				obj.TopLeft = geo.NewPoint(0, 0)
			case "b":
				obj.TopLeft = geo.NewPoint(300, 0)
			case "x":
				obj.TopLeft = geo.NewPoint(20, 30)
			case "y":
				obj.TopLeft = geo.NewPoint(160, 30)
			}
		}
		for _, edge := range g.Edges {
			edge.Route = []*geo.Point{edge.Src.Center(), edge.Dst.Center()}
		}
		return nil
	}

	if err := Layout(context.Background(), g, coreLayout); err != nil {
		t.Fatal(err)
	}
	if !layoutCalled {
		t.Fatal("expected cycle layout to call the provided core layout")
	}

	var x, y *d2graph.Object
	var internalEdge, cycleEdge *d2graph.Edge
	for _, obj := range g.Objects {
		switch obj.ID {
		case "x":
			x = obj
		case "y":
			y = obj
		}
	}
	for _, edge := range g.Edges {
		switch {
		case edge.Src == x && edge.Dst == y:
			internalEdge = edge
		case edge.Src.Parent == g.Root && edge.Dst.Parent == g.Root:
			cycleEdge = edge
		}
	}

	if x == nil || y == nil || internalEdge == nil || cycleEdge == nil {
		t.Fatal("expected nested objects, nested edge, and root-level cycle edge")
	}
	if internalEdge.IsCurve {
		t.Fatal("expected nested internal edge to keep the core layout route")
	}
	if got := len(internalEdge.Route); got != 2 {
		t.Fatalf("expected nested internal edge route to keep 2 points, got %d", got)
	}
	if !samePoint(internalEdge.Route[0], x.Center()) {
		t.Fatalf("expected nested edge start to move with nested source, got %v want %v", internalEdge.Route[0], x.Center())
	}
	if !samePoint(internalEdge.Route[1], y.Center()) {
		t.Fatalf("expected nested edge end to move with nested destination, got %v want %v", internalEdge.Route[1], y.Center())
	}
	if !cycleEdge.IsCurve {
		t.Fatal("expected root-level cycle edge to be routed as a curve")
	}
}

func TestCycleLayoutMovesCrossChildDescendantEdges(t *testing.T) {
	g, _, err := d2compiler.Compile("", strings.NewReader(`
shape: cycle
a: { x }
b: { y }
a.x -> b.y
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	setObjectBoxes(g, 100, 100)

	coreLayout := func(_ context.Context, g *d2graph.Graph) error {
		for _, obj := range g.Objects {
			switch obj.ID {
			case "a":
				obj.TopLeft = geo.NewPoint(0, 0)
			case "b":
				obj.TopLeft = geo.NewPoint(300, 0)
			case "x", "y":
				obj.TopLeft = geo.NewPoint(20, 30)
			}
		}
		for _, edge := range g.Edges {
			edge.Route = []*geo.Point{edge.Src.Center(), edge.Dst.Center()}
		}
		return nil
	}

	if err := Layout(context.Background(), g, coreLayout); err != nil {
		t.Fatal(err)
	}

	var x, y *d2graph.Object
	for _, obj := range g.Objects {
		switch obj.ID {
		case "x":
			x = obj
		case "y":
			y = obj
		}
	}
	var edge *d2graph.Edge
	for _, e := range g.Edges {
		if e.Src == x && e.Dst == y {
			edge = e
		}
	}

	if edge == nil {
		t.Fatal("expected cross-child descendant edge")
	}
	if len(edge.Route) != 2 {
		t.Fatalf("expected 2-point route, got %d", len(edge.Route))
	}
	if !samePoint(edge.Route[0], x.Center()) {
		t.Fatalf("expected edge start at %v, got %v", x.Center(), edge.Route[0])
	}
	if !samePoint(edge.Route[1], y.Center()) {
		t.Fatalf("expected edge end at %v, got %v", y.Center(), edge.Route[1])
	}
}

func setObjectBoxes(g *d2graph.Graph, width, height float64) {
	for _, obj := range g.Objects {
		obj.Box = geo.NewBox(geo.NewPoint(0, 0), width, height)
	}
}

func samePoint(a, b *geo.Point) bool {
	return math.Abs(a.X-b.X) < 0.001 && math.Abs(a.Y-b.Y) < 0.001
}

func TestCycleLayoutSupportsVariousShapes(t *testing.T) {
	g, _, err := d2compiler.Compile("", strings.NewReader(`
shape: cycle
a: { shape: circle }
b: { shape: rectangle }
c: { shape: cylinder }
d: { shape: hexagon }
a -> b -> c -> d -> a
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	setObjectBoxes(g, 100, 100)

	coreLayout := func(_ context.Context, g *d2graph.Graph) error {
		for _, obj := range g.Objects {
			obj.TopLeft = geo.NewPoint(0, 0)
		}
		return nil
	}

	if err := Layout(context.Background(), g, coreLayout); err != nil {
		t.Fatal(err)
	}

	for _, edge := range g.Edges {
		if !edge.IsCurve {
			t.Fatalf("expected edge %v -> %v to be curve", edge.Src.ID, edge.Dst.ID)
		}
		if len(edge.Route) < 4 {
			t.Fatalf("expected edge %v -> %v route to have at least 4 points, got %d", edge.Src.ID, edge.Dst.ID, len(edge.Route))
		}
		if (len(edge.Route)-1)%3 != 0 {
			t.Fatalf("expected edge %v -> %v route length 1+3n, got %d", edge.Src.ID, edge.Dst.ID, len(edge.Route))
		}
	}
}
