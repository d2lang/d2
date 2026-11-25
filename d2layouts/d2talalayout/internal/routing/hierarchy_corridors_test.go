package routing

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func corridorFixture() (*layoutgraph.Graph, func(x, y, w, h float64) *layoutgraph.Node) {
	g := layoutgraph.NewGraph()
	h := layoutgraph.NewHierarchy()
	return g, func(x, y, w, height float64) *layoutgraph.Node {
		n := g.AddNode(layoutgraph.NewNode(int64(len(g.Nodes)+1), w, height))
		n.TopLeft = geo.NewPoint(x, y)
		n.Hierarchy = h
		return n
	}
}

func assertCorridor(t *testing.T, g *layoutgraph.Graph, e *layoutgraph.Edge, want ...float64) {
	t.Helper()
	old := append([]*geo.Point(nil), e.Points...)
	positions := make([]geo.Point, len(old))
	for i, p := range old {
		positions[i] = *p
	}
	if err := RefineHierarchyCorridors(t.Context(), g); err != nil {
		t.Fatal(err)
	}
	expected := shortcutTestPoints(want...)
	if !slices.EqualFunc(e.Points, expected, func(a, b *geo.Point) bool { return *a == *b }) {
		t.Fatalf("route = %v, want %v", e.Points, expected)
	}
	for i, p := range old {
		if *p != positions[i] {
			t.Fatalf("modified prior route point %d", i)
		}
	}
}

func TestHierarchyCorridorKeepsPerimeterClearance(t *testing.T) {
	for _, horizontal := range []bool{true, false} {
		g, add := corridorFixture()
		from := add(0, 239, 104, 64)
		add(388, 353, 104, 64)
		to := add(776, 296, 104, 64)
		e := g.Connect(from, to)
		e.Points = shortcutTestPoints(52, 303, 52, 468, 828, 468, 828, 360)
		e.TargetArrowhead = layoutgraph.TriangleArrowhead
		if horizontal {
			assertCorridor(t, g, e, 52, 303, 52, 457, 828, 457, 828, 360)
		} else {
			for _, n := range g.Nodes {
				n.TopLeft.X, n.TopLeft.Y = n.TopLeft.Y, n.TopLeft.X
				n.Width, n.Height = n.Height, n.Width
			}
			for _, p := range e.Points {
				p.X, p.Y = p.Y, p.X
			}
			assertCorridor(t, g, e, 303, 52, 457, 52, 457, 828, 360, 828)
		}
	}
}

func TestHierarchyCorridorCancellationRestoresOriginalPoints(t *testing.T) {
	g, add := corridorFixture()
	from, to := add(290, 624, 63, 66), add(260, 780, 62, 66)
	e := g.Connect(from, to)
	e.Points = routeWithSpareCapacity(shortcutTestPoints(313, 690, 313, 735, 286, 735, 286, 780)...)
	original := captureExactRouteTest(e)
	ctx := &cancelWhenRouteMutates{Context: context.Background(), snapshot: original}
	if err := RefineHierarchyCorridors(ctx, g); !errors.Is(err, context.Canceled) || !ctx.observed {
		t.Fatalf("cancellation after a route changed: observed = %v, error = %v", ctx.observed, err)
	}
	original.assertRestored(t)
}

func TestHierarchyCorridorStraightensFacingBoxPorts(t *testing.T) {
	for _, kind := range []string{"clear", "obstacle", "edge label", "special port", "foreign marker"} {
		t.Run(kind, func(t *testing.T) {
			g, add := corridorFixture()
			from, to := add(290, 624, 63, 66), add(260, 780, 62, 66)
			e := g.Connect(from, to)
			e.TargetArrowhead = layoutgraph.TriangleArrowhead
			e.Points = shortcutTestPoints(313, 690, 313, 735, 286, 735, 286, 780)
			switch kind {
			case "obstacle":
				add(297, 748, 26, 12)
			case "edge label":
				e.Label = &layoutgraph.Label{Text: "keep", Width: 32, Height: 20, Position: label.UnlockedMiddle}
			case "special port":
				to.SetShape("Diamond")
			case "foreign marker":
				// Both potential straight lanes miss the neighboring shaft,
				// but its triangular head covers them.
				n := add(400, 660, 40, 40)
				other := g.Connect(n, to)
				other.Points = shortcutTestPoints(400, 680, 309, 680, 309, 780)
				other.TargetArrowhead = layoutgraph.TriangleArrowhead
			}
			if kind == "clear" {
				assertCorridor(t, g, e, 313, 690, 313, 780)
			} else {
				assertCorridor(t, g, e, 313, 690, 313, 735, 286, 735, 286, 780)
			}
		})
	}
}

func TestHierarchyCorridorSeparatesLongUnmarkedExit(t *testing.T) {
	for _, kind := range []string{"clear", "marked", "short", "other port", "rounded elbow"} {
		t.Run(kind, func(t *testing.T) {
			g, add := corridorFixture()
			orders := add(584, 153, 120, 120)
			stock := add(795, 0, 115, 66)
			events := add(999, 76, 114, 66)
			other := g.Connect(orders, stock)
			other.SourceArrowhead = layoutgraph.NoArrowhead
			other.TargetArrowhead = layoutgraph.TriangleArrowhead
			other.Points = shortcutTestPoints(644, 153, 644, 44, 795, 44)
			e := g.Connect(orders, events)
			e.SourceArrowhead = layoutgraph.NoArrowhead
			e.TargetArrowhead = layoutgraph.TriangleArrowhead
			y := 109.
			switch kind {
			case "marked":
				e.SourceArrowhead = layoutgraph.TriangleArrowhead
			case "short":
				y = 125
			case "other port":
				other.Points[0].X = 642
				other.Points[1].X = 642
			case "rounded elbow":
				// The proposed shafts at x684 and y109 miss this box; the
				// rounded turn between them passes through it.
				add(686, 111, 2, 2)
			}
			e.Points = shortcutTestPoints(644, 153, 644, y, 999, y)
			if kind == "clear" {
				assertCorridor(t, g, e, 684, 153, 684, y, 999, y)
			} else {
				assertCorridor(t, g, e, 644, 153, 644, y, 999, y)
			}
		})
	}
}
