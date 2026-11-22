package placementcost

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func TestCountLabeledEdgePairsMatchesMapCount(t *testing.T) {
	for _, edges := range []int{0, 1, 5, 40, 64, 65, 100, 200} {
		g := layoutgraph.NewGraph()
		hub := layoutgraph.NewNode(1, 10, 10)
		hub.TopLeft = geo.NewPoint(0, 0)
		g.AddNode(hub)
		neighbors := make([]*layoutgraph.Node, 7)
		for i := range neighbors {
			neighbors[i] = layoutgraph.NewNode(layoutgraph.EntityID(i+2), 10, 10)
			neighbors[i].TopLeft = geo.NewPoint(float64(i*30), 50)
			g.AddNode(neighbors[i])
		}
		for i := 0; i < edges; i++ {
			var e *layoutgraph.Edge
			if i%3 == 0 {
				e = g.Connect(neighbors[(i*5)%7], hub)
			} else {
				e = g.Connect(hub, neighbors[(i*3)%7])
			}
			if i%4 != 3 {
				e.Label = &layoutgraph.Label{Text: "x", Width: 5, Height: 5}
			}
		}
		nRepl := make([]*layoutgraph.Node, len(hub.Edges))
		aRepl := make([]*layoutgraph.Node, len(hub.Edges))
		labeled := 0
		want := map[labeledEdgePair]int{}
		for i, e := range hub.Edges {
			nRepl[i], aRepl[i] = hub, hub.Adjacent(e)
			if i%5 == 0 {
				// A replacement with the same ID as another node still shares its pair.
				aRepl[i] = layoutgraph.NewNode(aRepl[i].ID, 1, 1)
			}
			if e.Label != nil {
				labeled++
				want[labeledEdgePair{from: nRepl[i].ID, to: aRepl[i].ID}]++
			}
		}
		s := new(edgeScratch)
		for range 2 { // the second pass reuses the scratch
			countLabeledEdgePairs(hub, nRepl, aRepl, labeled, s)
			if len(s.labelCounts) != len(hub.Edges) {
				t.Fatalf("%d edges: %d counts", edges, len(s.labelCounts))
			}
			for i, e := range hub.Edges {
				expected := 0
				if e.Label != nil {
					expected = want[labeledEdgePair{from: nRepl[i].ID, to: aRepl[i].ID}]
				}
				if s.labelCounts[i] != expected {
					t.Fatalf("%d edges: edge %d count %d, want %d", edges, i, s.labelCounts[i], expected)
				}
			}
		}
	}
}
