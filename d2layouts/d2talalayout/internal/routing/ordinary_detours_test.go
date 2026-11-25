package routing

import (
	"slices"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func TestOrdinaryDetourKeepsBlockedExteriorBypass(t *testing.T) {
	g := layoutgraph.NewGraph()
	add := func(x, y, w, h float64) *layoutgraph.Node {
		n := layoutgraph.NewNode(int64(len(g.Nodes)+1), w, h)
		n.TopLeft = geo.NewPoint(x, y)
		return g.AddNode(n)
	}
	parent := add(0, 0, 200, 500)
	from, to := add(65, 400, 72, 66), add(65, 65, 72, 66)
	barrier := add(0, 200, 200, 66)
	for _, n := range []*layoutgraph.Node{from, to, barrier} {
		g.AddNodeToContainer(parent, n)
	}
	e := g.Connect(from, to)
	e.Points = shortcutTestPoints(65, 433, -40, 433, -40, 98, 65, 98)
	original := slices.Clone(e.Points)
	if err := RefineOrdinaryDetours(t.Context(), g); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(original, e.Points) {
		t.Fatalf("moved through the blocking child: %v", e.Points)
	}
}

func TestOrdinaryDetourNeedsCrossingsOnPaintedStraightRuns(t *testing.T) {
	bent := shortcutTestPoints(0, 0, 60, 0, 60, 60)
	// The centerlines meet at (58, 0) and (60, 2), inside the two
	// rounded elbows. Both painted paths round away from those corners.
	nearCorner := shortcutTestPoints(58, -50, 58, 2, 120, 2)
	if crossOnStraightRuns(bent, nearCorner) {
		t.Fatal("counted a crossing inside two rounded elbows")
	}
	across := shortcutTestPoints(30, -40, 30, 40)
	if !crossOnStraightRuns(bent, across) {
		t.Fatal("missed crossing on straight portions of both paths")
	}
}
