package hierarchy

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

func TestLoopRankClearanceMovesFollowingBlocksTogether(t *testing.T) {
	g := layoutgraph.NewGraph()
	previous := g.AddNode(layoutgraph.NewNode(1, 60, 60))
	host := g.AddNode(layoutgraph.NewNode(2, 60, 60))
	child := g.AddNode(layoutgraph.NewNode(3, 10, 10))
	following := g.AddNode(layoutgraph.NewNode(4, 60, 60))
	previous.TopLeft, host.TopLeft = geo.NewPoint(0, 0), geo.NewPoint(0, 70)
	child.TopLeft, following.TopLeft = geo.NewPoint(15, 85), geo.NewPoint(0, 190)
	host.LoopOffsets = map[geo.Orientation]float64{geo.Top: 55}
	host.SetContainer(true)
	child.Container = host
	g.Containers[host] = []*layoutgraph.Node{child}
	levels := map[int][]*placementNode{
		0: {newPlacementNode(0, previous)},
		1: {newPlacementNode(1, host)},
		2: {newPlacementNode(2, following)},
	}
	guard, err := limits.NewWorkGuard(t.Context(), "test loop clearance", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := clearLoopRanks(g, levels, geo.Bottom, guard); err != nil {
		t.Fatal(err)
	}
	if host.TopLeft.Y-55 != 60+layoutgraph.MinPortClearance ||
		child.TopLeft.Y-host.TopLeft.Y != 15 || following.TopLeft.Y-host.TopLeft.Y != 120 {
		t.Fatalf("rank, descendant or downstream offsets lost: host=%v child=%v following=%v", host.TopLeft, child.TopLeft, following.TopLeft)
	}
	guard, err = limits.NewWorkGuard(t.Context(), "test loop clearance", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := clearLoopRanks(g, levels, geo.Bottom, guard); err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("bounded clearance: got %v", err)
	}
}
