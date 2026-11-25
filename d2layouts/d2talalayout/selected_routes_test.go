package d2talalayout

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/textmeasure"
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

func TestBidirectionalPeerRingKeepsCenteredArrivals(t *testing.T) {
	g, _, err := d2compiler.Compile("ring.d2", strings.NewReader("A <-> B\nB <-> C\nC <-> D\nD <-> A"), nil)
	if err != nil {
		t.Fatal(err)
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := DefaultLayout(t.Context(), g); err != nil {
		t.Fatal(err)
	}
	for _, edge := range g.Edges {
		if len(edge.Route) != 4 {
			t.Fatalf("%s lost its centered dogleg: %v", edge.AbsID(), edge.Route)
		}
		obj, p := edge.Src, edge.Route[0]
		if obj.ID != "B" && obj.ID != "D" {
			obj, p = edge.Dst, edge.Route[3]
		}
		if p.X != obj.TopLeft.X+obj.Width/2 || (p.Y != obj.TopLeft.Y && p.Y != obj.TopLeft.Y+obj.Height) {
			t.Errorf("%s attaches at %v, want the center of a horizontal face on %s", edge.AbsID(), p, obj.ID)
		}
	}
}
