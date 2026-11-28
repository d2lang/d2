package quality

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/shape"
)

func TestLoopRepairKeepsExistingClearances(t *testing.T) {
	node, route := inspectionItem{1, nodeBody}, inspectionItem{3, edgeRoute}
	source, target := inspectionItem{3, sourceText}, inspectionItem{3, targetText}
	before := &LoopInspection{pairs: make(map[[2]inspectionItem]int64)}
	before.add(node, route, 1, true)
	before.add(source, target, 1, true)
	before.add(inspectionItem{4, edgeRoute}, route, 1, false)

	after := &LoopInspection{pairs: make(map[[2]inspectionItem]int64)}
	after.add(target, source, 1, true)
	if !after.Improves(before) {
		t.Fatal("removing a loop obstruction must count as an improvement")
	}
	for _, replacement := range []inspectionItem{{2, nodeBody}, {3, nodeText}, {5, edgeRoute}} {
		candidate := &LoopInspection{pairs: make(map[[2]inspectionItem]int64)}
		candidate.add(replacement, route, 1, false)
		if candidate.Improves(before) {
			t.Fatalf("moving a collision to %v was accepted", replacement)
		}
	}
	after.add(route, inspectionItem{4, edgeRoute}, 2, false)
	if after.Improves(before) {
		t.Fatal("introducing another crossing between an existing edge pair was accepted")
	}
}

func TestLoopInspectionUsesNonrectangularBoundsOnlyAsVeto(t *testing.T) {
	g := layoutgraph.NewGraph()
	host := g.AddNode(layoutgraph.NewNode(1, 40, 40))
	host.TopLeft = geo.NewPoint(0, 0)
	obstacle := g.AddNode(layoutgraph.NewNode(2, 40, 40))
	obstacle.SetShape(shape.DIAMOND_TYPE)
	obstacle.TopLeft = geo.NewPoint(10, 10)
	host.LoopOffsets = map[geo.Orientation]float64{geo.Top: 30}
	before, err := InspectLoopClearance(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	if before.LoopConflicts != 0 || len(before.pairs) != 1 {
		t.Fatalf("a possible silhouette overlap should veto a move but should not trigger one: %+v", before)
	}
	e := g.Connect(host, obstacle)
	e.IsCurve = true
	result, err := InspectLoopClearance(t.Context(), g)
	if err != nil || !result.incomplete {
		t.Fatalf("curve inspection: %+v, %v", result, err)
	}
}

func TestResizeRoutesYieldToFixedCaptionsOnly(t *testing.T) {
	before := &LoopInspection{pairs: map[[2]inspectionItem]int64{}}
	now := &LoopInspection{pairs: map[[2]inspectionItem]int64{}}
	movable := inspectionItem{10, edgeText}
	fixed := inspectionItem{11, fixedEdgeText}
	movingWire, fixedWire := routeItem(&layoutgraph.Edge{ID: 20}), routeItem(&layoutgraph.Edge{ID: 21})
	now.add(movable, movingWire, 1, false)
	now.add(fixed, fixedWire, 1, false)
	wires, labels := map[layoutgraph.EntityID]bool{}, map[layoutgraph.EntityID]bool{}
	if !now.CollectObstructedRoutes(before, wires, labels, nil) || wires[movingWire.id] || !wires[fixedWire.id] || !labels[movable.id] {
		t.Fatalf("incorrect clearance repairs: routes %v labels %v", wires, labels)
	}
}
