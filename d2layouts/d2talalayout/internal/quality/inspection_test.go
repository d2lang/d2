package quality

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func TestInspectFindsRepairTriggersWithoutChangingLegacyScore(t *testing.T) {
	g := layoutgraph.NewGraph()
	a := g.AddNode(layoutgraph.NewNode(1, 20, 20))
	a.TopLeft = geo.NewPoint(0, 0)
	b := g.AddNode(layoutgraph.NewNode(2, 20, 20))
	b.TopLeft = geo.NewPoint(100, 0)
	obstacle := g.AddNode(layoutgraph.NewNode(3, 20, 20))
	obstacle.TopLeft = geo.NewPoint(50, 30)
	for range 2 {
		edge := g.Connect(a, b)
		edge.Points = []*geo.Point{geo.NewPoint(10, 20), geo.NewPoint(10, 40), geo.NewPoint(110, 40), geo.NewPoint(110, 20)}
		edge.Label = &layoutgraph.Label{Text: "relationship", Position: label.UnlockedMiddle, Width: 40, Height: 10}
		edge.LabelPercentage = 0.5
	}
	before, err := Evaluate(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := Inspect(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.RouteObstructions == 0 || metrics.TextOcclusions == 0 || metrics.Detour <= 0 || metrics.RouteLength <= 0 {
		t.Fatalf("inspection missed repair triggers: %+v", metrics)
	}
	after, err := Evaluate(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	penalty, area, err := EvaluateWithArea(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	if before != after || after.Penalty != penalty || after.Area != area {
		t.Fatalf("diagnostic inspection changed the original score: before=%+v after=%+v penalty=%v area=%v", before, after, penalty, area)
	}
}

func TestScoreRetainsOriginalExactPenaltyAreaOrdering(t *testing.T) {
	for _, test := range []struct {
		a, b Score
		want int
	}{
		{Score{Penalty: 1, Area: 100}, Score{Penalty: 2, Area: 1}, -1},
		{Score{Penalty: 1, Area: 10}, Score{Penalty: math.Nextafter(1, 2), Area: 1}, -1},
		{Score{Penalty: 1, Area: 10}, Score{Penalty: 1, Area: 20}, -1},
		{Score{Penalty: math.NaN(), Area: 10}, Score{Penalty: math.Inf(1), Area: 20}, -1},
		{Score{Penalty: 1, Area: -1}, Score{Penalty: 1, Area: 20}, 1},
		{Score{Penalty: 1, Area: 100}, Score{Penalty: math.NaN(), Area: 1}, -1},
		{Score{Penalty: math.Inf(1), Area: 1}, Score{Penalty: 1, Area: 100}, 1},
		{Score{Penalty: 1, Area: 10}, Score{Penalty: 1, Area: math.NaN()}, -1},
		{Score{Penalty: 1, Area: math.Inf(1)}, Score{Penalty: 1, Area: 10}, 1},
		{Score{Penalty: math.NaN(), Area: math.Inf(1)}, Score{Penalty: math.Inf(1), Area: math.NaN()}, 0},
	} {
		if got := test.a.Compare(test.b); got != test.want {
			t.Fatalf("%+v compared with %+v = %v, want %v", test.a, test.b, got, test.want)
		}
	}
}

func TestInspectRejectsCancellationBeforeWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Inspect(ctx, layoutgraph.NewGraph()); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
}

func TestInteriorCrossingsDistinguishForksFromThroughCrossings(t *testing.T) {
	a, b := geo.NewPoint(0, 0), geo.NewPoint(10, 0)
	for _, tc := range []struct {
		name     string
		c, d     *geo.Point
		interior bool
	}{
		{"crossing", geo.NewPoint(5, -10), geo.NewPoint(5, 10), true},
		{"diagonal crossing", geo.NewPoint(1, -3), geo.NewPoint(9, 3), true},
		{"fork", geo.NewPoint(0, -10), geo.NewPoint(0, 10), false},
		{"shared end", geo.NewPoint(10, 0), geo.NewPoint(10, 10), false},
		{"collinear overlap", geo.NewPoint(5, 0), geo.NewPoint(15, 0), false},
		{"disjoint", geo.NewPoint(25, -10), geo.NewPoint(25, 10), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, forward := classifyCrossing(a, b, tc.c, tc.d)
			_, reverse := classifyCrossing(tc.d, tc.c, b, a)
			if forward != tc.interior || reverse != tc.interior {
				t.Fatalf("segment interior intersection in forward/reversed order = %t/%t, want %t", forward, reverse, tc.interior)
			}
		})
	}
}
