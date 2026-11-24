package d2talalayout_test

import (
	"testing"

	"github.com/d2lang/d2/d2target"
)

// n3's outgoing links can leave together and then fork. Replacing that shared
// lane with a transverse crossing used to pass the compact-layout veto.
func TestCompactHierarchyKeepsSharedRouteDeparture(t *testing.T) {
	const source = `shape: hierarchy
direction: up
vars: {d2-config: {data: {tala-seeds: [1;2;3]}}}
n0 -> n2
n1 -> n2
n1 -> n3
n1 -> n4
n2 -> n5: transport assurance
n3 -> n5
n3 -> n6
n5 -> n6`
	diagram := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	shapes := make(map[string]d2target.Shape, len(diagram.Shapes))
	for _, s := range diagram.Shapes {
		shapes[s.ID] = s
	}
	var fan []d2target.Connection
	for _, edge := range diagram.Connections {
		if edge.Src == "n3" {
			fan = append(fan, edge)
		}
	}
	if len(fan) != 2 {
		t.Fatalf("found %d edges from n3, want two", len(fan))
	}
	for _, gaps := range d2target.ConnectionCrossings(fan, shapes, false, nil) {
		if len(gaps) > 0 {
			t.Fatal("n3→n5 crosses n3→n6")
		}
	}
}
