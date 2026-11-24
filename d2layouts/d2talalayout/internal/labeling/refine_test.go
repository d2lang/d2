package labeling

import (
	"context"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

func refinementLabelBox(edge *layoutgraph.Edge) *layoutgraph.Node {
	box := &layoutgraph.Node{Box: geo.Box{TopLeft: edge.LabelTopLeft(edge.Label.Position, edge.Label.Width, edge.Label.Height), Width: edge.Label.Width, Height: edge.Label.Height}}
	box.SetShape(shape.SQUARE_TYPE)
	return box
}

func TestSharedSegmentsPreserveExclusiveGaps(t *testing.T) {
	for _, vertical := range []bool{false, true} {
		for _, reverse := range []bool{false, true} {
			var edges []*layoutgraph.Edge
			for _, interval := range [][2]float64{{0, 100}, {10, 20}, {30, 40}, {50, 50}} {
				a, b := geo.NewPoint(interval[0], 0), geo.NewPoint(interval[1], 0)
				if vertical {
					a.X, a.Y = a.Y, a.X
					b.X, b.Y = b.Y, b.X
				}
				if reverse {
					a, b = b, a
				}
				edge := layoutgraph.NewEdge(nil, nil)
				edge.Points = []*geo.Point{a, b}
				edges = append(edges, edge)
			}
			segments, err := findSharedSegmentsChecked(edges, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(segments) != 2 {
				t.Fatalf("vertical=%v reverse=%v shared spans=%v, want exactly[10,20] and[30,40]", vertical, reverse, segments)
			}
			coordinate := func(p *geo.Point) float64 {
				if vertical {
					return p.Y
				}
				return p.X
			}
			if coordinate(segments[0].Start) != 10 || coordinate(segments[0].End) != 20 || coordinate(segments[1].Start) != 30 || coordinate(segments[1].End) != 40 {
				t.Fatalf("exclusive20..30gap incorrectly marked shared: %v", segments)
			}
		}
	}
}

func TestLabelPlacementPreservesFixedEdgeLabelsAndReservesTheirSpace(t *testing.T) {
	for _, selected := range []bool{false, true} {
		for _, position := range []label.Position{label.InsideMiddleCenter, label.OutsideTopCenter} {
			g := layoutgraph.NewGraph()
			a := g.AddNode(layoutgraph.NewNode(1, 40, 40))
			a.TopLeft = geo.NewPoint(0, 0)
			b := g.AddNode(layoutgraph.NewNode(2, 40, 40))
			b.TopLeft = geo.NewPoint(300, 0)
			fixed := g.Connect(a, b)
			fixed.Points = []*geo.Point{geo.NewPoint(40, 20), geo.NewPoint(300, 20)}
			fixed.Label = &layoutgraph.Label{Text: "fixed", Position: position, Width: 80, Height: 20}
			fixed.Label.FixPosition()
			fixed.LabelPercentage = 0.25
			movable := g.Connect(a, b)
			movable.Points = append([]*geo.Point(nil), fixed.Points...)
			movable.Label = &layoutgraph.Label{Text: "automatic", Position: label.Unset, Width: 80, Height: 20}
			original := fixed.Label
			var err error
			if selected {
				err = PlaceNewEdges(context.Background(), g, g.Edges)
			} else {
				err = Place(context.Background(), g)
			}
			if err != nil {
				t.Fatal(err)
			}
			if fixed.Label != original || fixed.Label.Position != position || fixed.LabelPercentage != 0.25 {
				t.Fatalf("selected=%v fixed label changed: position=%v percentage=%v", selected, fixed.Label.Position, fixed.LabelPercentage)
			}
			if a, b := refinementLabelBox(fixed), refinementLabelBox(movable); a.TopLeft.X < b.TopLeft.X+b.Width && b.TopLeft.X < a.TopLeft.X+a.Width && a.TopLeft.Y < b.TopLeft.Y+b.Height && b.TopLeft.Y < a.TopLeft.Y+a.Height {
				t.Fatalf("selected=%v automatic label occupied caller-fixed label rectangle", selected)
			}
		}
	}
}

func TestParallelLabelRepairKeepsCompletedPlacementWhenBudgetRunsOut(t *testing.T) {
	newGraph := func() *layoutgraph.Graph {
		graph := layoutgraph.NewGraph()
		a := graph.AddNode(layoutgraph.NewNode(1, 160, 160))
		b := graph.AddNode(layoutgraph.NewNode(2, 160, 160))
		a.TopLeft = geo.NewPoint(0, 0)
		b.TopLeft = geo.NewPoint(0, 319)
		for i, x := range []float64{40, 80, 120} {
			edge := graph.Connect(a, b)
			edge.ID = layoutgraph.EntityID(10 + i)
			edge.Points = []*geo.Point{geo.NewPoint(x, 160), geo.NewPoint(x, 319)}
			edge.Label = &layoutgraph.Label{Text: "four lines", Width: 75, Height: 69, Position: label.Unset}
		}
		return graph
	}
	// Measure mandatory placement on the same routes so the test can interrupt
	// the optional search without tying the budget to a particular work count.
	graph := newGraph()
	guard, err := newLabelPlacementWorkGuard(t.Context(), "test", maxLabelPlacementWorkUnits)
	if err != nil {
		t.Fatal(err)
	}
	edges, err := sortLabelPlacementEdges(graph.Edges, guard)
	if err != nil {
		t.Fatal(err)
	}
	var obstacles []*layoutgraph.Node
	var greedy []edgeLabelLocation
	hasCollision := false
	for _, edge := range edges {
		if err := guard.step(); err != nil {
			t.Fatal(err)
		}
		best, err := findBestEdgeLabelPosition(edge, graph, obstacles, nil, nil, guard)
		if err != nil {
			t.Fatal(err)
		}
		obstacles = append(obstacles, best.box)
		greedy = append(greedy, best.edgeLabelLocation)
		hasCollision = hasCollision || best.conflicts != 0
	}
	if !hasCollision {
		t.Fatal("fixture must require moving an earlier label")
	}
	limited := newGraph()
	limitedGuard, err := newLabelPlacementWorkGuard(t.Context(), "test", guard.used+10)
	if err != nil {
		t.Fatal(err)
	}
	if err := placeMovableEdgeLabels(limited, limited.Edges, nil, nil, limitedGuard); err != nil {
		t.Fatal(err)
	}
	for i, edge := range limited.Edges {
		if got := locationOf(edge); got != greedy[i] {
			t.Errorf("edge %d changed to %+v during abandoned repair, want %+v", i, got, greedy[i])
		}
	}
	// With room for repair, the incremental router also clears each label.
	rerouted := newGraph()
	if err := PlaceNewEdges(t.Context(), rerouted, rerouted.Edges); err != nil {
		t.Fatal(err)
	}
	obstacles = append(obstacles[:0], rerouted.Nodes...)
	for _, edge := range rerouted.Edges {
		box := refinementLabelBox(edge)
		for _, obstacle := range obstacles {
			if box.DoesOverlapExact(obstacle) {
				t.Fatalf("rerouted edge %d label overlaps obstacle at %v", edge.ID, *obstacle.TopLeft)
			}
		}
		obstacles = append(obstacles, box)
	}
}
