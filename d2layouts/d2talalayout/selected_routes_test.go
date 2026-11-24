package d2talalayout

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func TestSelectedRouteCancellationAtCommitRestoresWinner(t *testing.T) {
	g := layoutgraph.NewGraph()
	h := layoutgraph.NewHierarchy()
	from := g.AddNode(layoutgraph.NewNode(1, 63, 66))
	to := g.AddNode(layoutgraph.NewNode(2, 62, 66))
	from.Hierarchy, to.Hierarchy = h, h
	from.TopLeft, to.TopLeft = geo.NewPoint(290, 624), geo.NewPoint(260, 780)
	e := g.Connect(from, to)
	e.Points = []*geo.Point{geo.NewPoint(313, 690), geo.NewPoint(313, 735), geo.NewPoint(286, 735), geo.NewPoint(286, 780)}
	original := e.Points

	// Measure the last check in an accepted run, then cancel at exactly
	// that check after the selected headers have been installed.
	probe := &cancelAfterContext{Context: t.Context()}
	if err := refineSelectedRoutes(probe, g); err != nil || len(e.Points) != 2 {
		t.Fatalf("expected a straightened route, got %v, %v", e.Points, err)
	}
	e.Points = original
	base, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := &cancelAfterContext{Context: base, cancel: cancel, after: probe.checks}
	if err := refineSelectedRoutes(ctx, g); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if !slices.Equal(e.Points, original) || &e.Points[0] != &original[0] {
		t.Fatalf("winner retained a partial edit after cancellation: %v", e.Points)
	}
}
