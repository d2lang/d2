package quality

import (
	"context"
	"errors"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"strings"
)

func authoredFlowTestGraph(x, y float64) (*layoutgraph.Graph, *layoutgraph.Edge) {
	g := layoutgraph.NewGraph()
	a := g.AddNode(layoutgraph.NewNode(1, 20, 20))
	b := g.AddNode(layoutgraph.NewNode(2, 20, 20))
	a.TopLeft, b.TopLeft = geo.NewPoint(0, 0), geo.NewPoint(x, y)
	edge := g.Connect(a, b)
	edge.TargetArrowhead = layoutgraph.TriangleArrowhead
	edge.Points = []*geo.Point{a.Center(), b.Center()}
	return g, edge
}

func authoredFlowTestScore(t *testing.T, g *layoutgraph.Graph) float64 {
	t.Helper()
	guard, err := newEvaluationWorkGuard(t.Context(), maxEvaluationWorkUnits)
	if err != nil {
		t.Fatal(err)
	}
	score, err := scoreAuthoredFlow(g, guard)
	if err != nil {
		t.Fatal(err)
	}
	return score
}

func TestAuthoredFlowDirectionsAndScale(t *testing.T) {
	for _, tc := range []struct {
		name       string
		direction  geo.Orientation
		x, y, want float64
	}{
		{"forward", geo.Right, 100, 0, 0}, {"perpendicular", geo.Right, 0, 100, 1},
		{"reverse", geo.Right, -100, 0, 2}, {"diagonal", geo.Right, 100, 100, .5},
		{"left", geo.Left, -100, 0, 0}, {"up", geo.Top, 0, -100, 0},
		{"down", geo.Bottom, 0, 100, 0}, {"unspecified", geo.NONE, -100, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, _ := authoredFlowTestGraph(tc.x, tc.y)
			g.Directions[nil] = tc.direction
			if got := authoredFlowTestScore(t, g); got != tc.want {
				t.Fatalf("score=%v want=%v", got, tc.want)
			}
			for _, n := range g.Nodes {
				n.TopLeft.X = n.TopLeft.X*3 + 17
				n.TopLeft.Y = n.TopLeft.Y*3 - 31
				n.Width *= 3
				n.Height *= 3
			}
			if got := authoredFlowTestScore(t, g); got != tc.want {
				t.Fatalf("transformed score=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestAuthoredFlowArrowSemantics(t *testing.T) {
	for _, tc := range []struct {
		name           string
		source, target layoutgraph.Arrowhead
		invisible      bool
		want           float64
	}{
		{"target", layoutgraph.NoArrowhead, layoutgraph.TriangleArrowhead, false, 2},
		{"source", layoutgraph.TriangleArrowhead, layoutgraph.NoArrowhead, false, 0},
		{"undirected", layoutgraph.NoArrowhead, layoutgraph.NoArrowhead, false, 0},
		{"bidirectional", layoutgraph.TriangleArrowhead, layoutgraph.TriangleArrowhead, false, 0},
		{"invisible", layoutgraph.NoArrowhead, layoutgraph.TriangleArrowhead, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, e := authoredFlowTestGraph(-100, 0)
			g.Directions[nil] = geo.Right
			e.SourceArrowhead, e.TargetArrowhead = tc.source, tc.target
			e.IsInvisible = tc.invisible
			if got := authoredFlowTestScore(t, g); got != tc.want {
				t.Fatalf("score=%v want=%v", got, tc.want)
			}
		})
	}
	g, e := authoredFlowTestGraph(-100, 0)
	g.Directions[nil] = geo.Right
	e.To = e.From
	if got := authoredFlowTestScore(t, g); got != 0 {
		t.Fatalf("self loop score=%v", got)
	}
}

func TestAuthoredFlowUsesSharedOwningScope(t *testing.T) {
	g, e := authoredFlowTestGraph(0, 100)
	outer := g.AddNode(layoutgraph.NewNode(3, 300, 300))
	outer.TopLeft = geo.NewPoint(-20, -20)
	inner := g.AddNode(layoutgraph.NewNode(4, 250, 250))
	inner.TopLeft = geo.NewPoint(-10, -10)
	inner.Container = outer
	e.From.Container, e.To.Container = inner, inner
	g.Directions[nil] = geo.Right
	g.Directions[outer] = geo.Bottom
	if got := authoredFlowTestScore(t, g); got != 0 {
		t.Fatalf("did not inherit shared outer direction: %v", got)
	}
	g.Directions[inner] = geo.Top
	if got := authoredFlowTestScore(t, g); got != 2 {
		t.Fatalf("did not use nearer direction: %v", got)
	}
	e.To.Container = nil
	if got := authoredFlowTestScore(t, g); got != 1 {
		t.Fatalf("local direction leaked outside container: %v", got)
	}
	delete(g.Directions, nil)
	if got := authoredFlowTestScore(t, g); got != 0 {
		t.Fatalf("unshared direction affected edge: %v", got)
	}
}

func TestAuthoredFlowAddsToExistingPenalty(t *testing.T) {
	g, _ := authoredFlowTestGraph(0, 100)
	before, area, err := EvaluateWithArea(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	g.Directions[nil] = geo.Right
	after, afterArea, err := EvaluateWithArea(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	if after != before+1 || area != afterArea {
		t.Fatalf("score/area before=%v/%v after=%v/%v", before, area, after, afterArea)
	}
}

func TestAuthoredFlowIsBudgetedAndCancelable(t *testing.T) {
	g, _ := authoredFlowTestGraph(0, 100)
	g.Directions[nil] = geo.Right
	guard, err := newEvaluationWorkGuard(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scoreAuthoredFlow(g, guard); err == nil || !strings.Contains(err.Error(), "work exceeds limit") {
		t.Fatalf("budget error=%v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Evaluate(ctx, g); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
}
