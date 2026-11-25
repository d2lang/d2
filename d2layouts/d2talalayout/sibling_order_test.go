package d2talalayout

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func TestBranchSlotsPreserveAuthoredPathDirection(t *testing.T) {
	// The adapter normalizes all three edges to originate at hub. D2
	// reverses b's waypoints on export, which affects rounded SVG corners.
	source, _, err := d2compiler.Compile("test.d2", strings.NewReader("hub -> a\nb <- hub\nhub -> c\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, obj := range source.Objects {
		obj.Box = geo.NewBox(geo.NewPoint(0, 0), 60, 40)
	}
	input, err := newSeedInput(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	g := input.graph
	h := layoutgraph.NewHierarchy()
	for _, n := range g.Nodes {
		n.Hierarchy = h
		n.Label = nil
		h.Levels()[n] = 1
		switch *n.D2ID {
		case "hub":
			n.TopLeft = geo.NewPoint(110, 0)
			h.Levels()[n] = 0
		case "a":
			n.TopLeft = geo.NewPoint(200, 100)
		case "b":
			n.TopLeft = geo.NewPoint(0, 100)
		case "c":
			n.TopLeft = geo.NewPoint(100, 100)
		}
	}
	for _, e := range g.Edges {
		if *e.From.D2ID != "hub" {
			t.Fatalf("edge %s was not normalized", *e.D2ID)
		}
		x := e.To.TopLeft.X + 30
		e.Points = []*geo.Point{geo.NewPoint(140, 40), geo.NewPoint(x, 40), geo.NewPoint(x, 100)}
	}
	if err := orderEquivalentBranchSlots(t.Context(), source, seedResult{graph: g, bindings: input.bindings}); err != nil {
		t.Fatal(err)
	}
	for _, e := range g.Edges {
		// a and c can exchange paths. b has to keep the path authored in reverse.
		want := map[string]float64{"a": 100, "b": 0, "c": 200}[*e.To.D2ID]
		if e.To.TopLeft.X != want || e.Points[len(e.Points)-1].X != want+30 {
			t.Errorf("%s ended at x=%v, leaf x=%v, want leaf x=%v", *e.D2ID, e.Points[len(e.Points)-1].X, e.To.TopLeft.X, want)
		}
	}
}
