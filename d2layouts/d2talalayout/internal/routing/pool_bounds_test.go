package routing

import "testing"

// TestPooledRouteScratchIsBounded checks that route-search and OVG-line
// scratch grown past maxPooledRouteSearchNodes is not returned to its pool
// (TinyGo's sync.Pool never frees, so every
// released scratch, sized to the largest OVG routed, stayed allocated).
func TestPooledRouteScratchIsBounded(t *testing.T) {
	for _, nodes := range []int{100, maxPooledRouteSearchNodes, maxPooledRouteSearchNodes + 1} {
		router := &ovgEdgeRouter{}
		router.carveSearchState(nodes)
		scratch := router.scratch
		if got, want := scratch.retainable(), nodes <= maxPooledRouteSearchNodes; got != want {
			t.Fatalf("%d nodes: retainable %v", nodes, got)
		}
		router.releaseSearchScratch()
		if nodes > maxPooledRouteSearchNodes {
			for i := 0; i < 4; i++ {
				if routeSearchScratchPool.Get() == scratch {
					t.Fatalf("%d nodes: oversized search scratch returned to the pool", nodes)
				}
			}
		}
	}
	// A large queue alone also keeps the scratch out of the pool.
	router := &ovgEdgeRouter{}
	router.carveSearchState(10)
	scratch := router.scratch
	scratch.queue.items = make([]priorityQueueItem, 0, maxPooledRouteSearchNodes+1)
	router.releaseSearchScratch()
	for i := 0; i < 4; i++ {
		if routeSearchScratchPool.Get() == scratch {
			t.Fatal("search scratch with an oversized queue returned to the pool")
		}
	}

	lines := ovgLineScratchPool.Get()
	if !lines.retainable() {
		t.Fatal("empty line scratch not retainable")
	}
	lines.eligible = make([]*OVGNode, 0, maxPooledRouteSearchNodes+1)
	lines.release()
	for i := 0; i < 4; i++ {
		if ovgLineScratchPool.Get() == lines {
			t.Fatal("oversized OVG line scratch returned to the pool")
		}
	}
	lines = ovgLineScratchPool.Get()
	lines.vertical.nodes = make([]*OVGNode, maxPooledRouteSearchNodes+1)
	lines.release()
	for i := 0; i < 4; i++ {
		if ovgLineScratchPool.Get() == lines {
			t.Fatal("OVG line scratch with oversized line groups returned to the pool")
		}
	}
}
