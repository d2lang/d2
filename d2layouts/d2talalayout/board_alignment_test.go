package d2talalayout

import (
	"context"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

func TestBoardAnchorsPreferScopedIdentity(t *testing.T) {
	old, current := d2graph.NewGraph(), d2graph.NewGraph()
	source := appendD2Object(old.Root, "source")
	target := appendD2Object(current.Root, "target")
	oldWorker := appendD2Object(source, "Worker")
	newWorker := appendD2Object(target, "worker")
	oldWorker.Label.Value, newWorker.Label.Value = "Process order", "Process order"
	if got := boardAnchors(old, current); !slices.Equal(got, []boardAnchor{{oldWorker, newWorker}}) {
		t.Fatal("lost an unambiguous reparented leaf")
	}
	newWorker.Label.Value = "Different job"
	if got := boardAnchors(old, current); len(got) != 0 {
		t.Fatal("matched a changed label across containers")
	}
	newWorker.Label.Value = oldWorker.Label.Value
	other := appendD2Object(old.Root, "other")
	appendD2Object(other, "worker")
	if got := boardAnchors(old, current); len(got) != 0 {
		t.Fatal("matched a nonunique local ID across containers")
	}
	oldAPI, newAPI := appendD2Object(old.Root, "API"), appendD2Object(current.Root, "api")
	oldAPI.Label.Value, newAPI.Label.Value = "Endpoint", "Public endpoint"
	expected := []boardAnchor{{oldAPI, newAPI}}
	if got := boardAnchors(old, current); !slices.Equal(got, expected) {
		t.Fatal("lost a surviving full-path identity when its caption changed")
	}
	db1, db2 := appendD2Object(source, "db"), appendD2Object(target, "db")
	if got := boardAnchors(old, current); !slices.Equal(got, expected) {
		t.Fatal("a reparented leaf steered the frame away from the fixed-scope anchor")
	}
	newAPI.Style.Opacity = &d2graph.Scalar{Value: "0"}
	if got := boardAnchors(old, current); !slices.Equal(got, []boardAnchor{{db1, db2}}) {
		t.Fatal("an invisible common leaf displaced a visible moved leaf")
	}
	newAPI.Style.Opacity = nil
	newAPI.Shape.Value = "text"
	newAPI.Style.FontColor = &d2graph.Scalar{Value: "transparent"}
	if got := boardAnchors(old, current); !slices.Equal(got, []boardAnchor{{db1, db2}}) {
		t.Fatal("invisible plain text acquired a fictitious painted body")
	}
}

func TestBoardOffsetVotingAndRounding(t *testing.T) {
	for _, tt := range []struct {
		name    string
		offsets []float64
		want    float64
	}{
		{"majority", []float64{25, -500, 25}, 25},
		{"no-travel-regression", []float64{0, 1, 2, 3, 1000, 1000}, 2},
		{"integer-median-after-resize", []float64{-2.5, -2.5, 90.5}, -2},
		{"independent-of-declaration-order", []float64{9, -9}, -9},
		{"already-centered", []float64{-1.5, 1.5}, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var anchors []boardAnchor
			for _, offset := range tt.offsets {
				old := &d2graph.Object{Box: geo.NewBox(geo.NewPoint(float64(int(offset)), 0), 20+2*(offset-float64(int(offset))), 20)}
				current := &d2graph.Object{Box: geo.NewBox(geo.NewPoint(0, 0), 20, 20)}
				anchors = append(anchors, boardAnchor{old, current})
			}
			for i := 0; i < 2; i++ {
				if dx, dy := chooseBoardOffset(anchors); dx != tt.want || dy != 0 {
					t.Fatalf("shift = (%g, %g), want (%g, 0)", dx, dy, tt.want)
				}
				slices.Reverse(anchors)
			}
		})
	}
}

func TestBoardAlignmentCoordinateBounds(t *testing.T) {
	g, e := newD2TransactionGraph(false)
	// Opposite fractional origins restrict shifts in both directions.
	g.Objects[0].TopLeft.X, g.Objects[1].TopLeft.X = -2.5, 3.25
	if r := boardOffsetBounds(g.Objects, g.Edges); r.minX != -3 || r.maxX != 2 {
		t.Fatalf("unexpected fractional export limits: %+v", r)
	}
	if canShiftBoardObjects(g.Objects, g.Edges, -4, 0) || canShiftBoardObjects(g.Objects, g.Edges, 0.5, 0) ||
		!canShiftBoardObjects(g.Objects, g.Edges, -3, 0) {
		t.Fatal("translation detached ports from exported shapes")
	}
	e.Route[0].Y = limits.MaxGraphSize
	if canShiftBoardObjects(g.Objects, g.Edges, 0, 1) {
		t.Fatal("route escaped the graph coordinate limit")
	}
	g.Objects[1].TopLeft.Y = limits.MaxGraphSize - g.Objects[1].Height + 1
	if boardCanAlign(g) {
		t.Fatal("node extent escaped the graph coordinate limit")
	}
}

func TestBoardAlignmentRejectsMalformedGeometry(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*d2graph.Graph)
	}{
		{"cyclic root parent", func(g *d2graph.Graph) { g.Root.Parent = g.Objects[0] }},
		{"repeated node", func(g *d2graph.Graph) { g.Objects[1] = g.Objects[0] }},
		{"repeated route", func(g *d2graph.Graph) { g.Edges = append(g.Edges, g.Edges[0]) }},
		{"absent port", func(g *d2graph.Graph) { g.Edges[0].Dst = g.Root }},
		{"missing point", func(g *d2graph.Graph) { g.Edges[0].Route[1] = nil }},
		{"missing child", func(g *d2graph.Graph) { g.Root.ChildrenArray = g.Root.ChildrenArray[:1] }},
	} {
		t.Run(test.name, func(t *testing.T) {
			old, _ := newD2TransactionGraph(false)
			current, e := newD2TransactionGraph(false)
			test.edit(current)
			box, port := current.Objects[0].Box, e.Route[0]
			if ok, err := ContinueBoard(t.Context(), old, current); ok || err != nil || current.Objects[0].Box != box || e.Route[0] != port {
				t.Fatal("changed a malformed board")
			}
		})
	}
}

func TestContinueBoardRejectsNilContext(t *testing.T) {
	old, _ := newD2TransactionGraph(false)
	current, _ := newD2TransactionGraph(false)
	before := snapshotD2Graph(t, current)
	//lint:ignore SA1012 Verify rejection of an invalid public API argument.
	if ok, err := ContinueBoard(nil, old, current); ok || err == nil {
		t.Fatal("ContinueBoard accepted a nil context")
	}
	before.assertUnchanged(t, current)
}

func TestBoardAlignmentRestoresGeometryWhenLocalSearchIsCanceled(t *testing.T) {
	old, _ := newD2TransactionGraph(false)
	current, edge := newD2TransactionGraph(false)
	for _, n := range current.Objects {
		n.TopLeft.X += 80
	}
	edge.Move(80, 0)
	box, point := current.Objects[0].Box, edge.Route[0]
	x, port := box.TopLeft.X, point.X
	base, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := &cancelAfterContext{Context: base, cancel: cancel, after: 2}
	ok, err := alignBoard(ctx, old, current)
	if ok || err != context.Canceled {
		t.Fatalf("cancellation during local search returned %v, %v", ok, err)
	}
	if current.Objects[0].Box != box || edge.Route[0] != point || box.TopLeft.X != x || point.X != port {
		t.Fatal("cancellation retained modified geometry")
	}
}
