package routing

import "github.com/d2lang/d2/d2layouts/d2talalayout/internal/typedpool"

// routeSearchScratch is the storage of one flavor router's searches: the
// per-OVG-node state arrays, the touched-node list and the priority queue.
// Routing calls return it to a pool when their flavor phase has finished, so
// the next call reuses the memory instead of allocating O(OVG nodes) per
// router.
//
// Invariant while pooled: the pointer arrays are nil everywhere (each router
// clears the entries its searches touched before releasing), so a router
// carving a prefix sees exactly the zeroed arrays a fresh allocation would
// give. nodeContextValues is only read where nodeContext is non-nil, i.e.
// after the current search wrote it, so its stale values are never seen.
type routeSearchScratch struct {
	verticalHops      []*OVGNode
	horizontalHops    []*OVGNode
	nodeContext       []*searchNodeContext
	nodeContextValues []searchNodeContext
	touchedNodes      []int
	queue             priorityQueue
}

var routeSearchScratchPool = typedpool.New(func() *routeSearchScratch {
	return &routeSearchScratch{}
})

// maxPooledRouteSearchNodes bounds a pooled search scratch (and an OVG line
// scratch): its per-OVG-node arrays, touched list and queue storage. TinyGo's
// sync.Pool (d2's wasm build) never frees what it holds, so a scratch grown
// by a larger OVG or search is not put back: the collector frees it, as it
// freed the per-router arrays the pool replaces. 8192 entries are about
// 0.8 MB per scratch on wasm32; on the corpus about 6% of the routing calls'
// scratches are larger (OVGs up to 41,000 nodes).
const maxPooledRouteSearchNodes = 8192

// retainable reports whether the scratch is small enough to pool.
func (scratch *routeSearchScratch) retainable() bool {
	return cap(scratch.nodeContext) <= maxPooledRouteSearchNodes &&
		cap(scratch.verticalHops) <= maxPooledRouteSearchNodes &&
		cap(scratch.horizontalHops) <= maxPooledRouteSearchNodes &&
		cap(scratch.nodeContextValues) <= maxPooledRouteSearchNodes &&
		cap(scratch.touchedNodes) <= maxPooledRouteSearchNodes &&
		scratch.queue.storage() <= maxPooledRouteSearchNodes
}

// searchScratch returns the router's scratch, taking one from the pool on
// first use.
func (router *ovgEdgeRouter) searchScratch() *routeSearchScratch {
	if router.scratch == nil {
		scratch := routeSearchScratchPool.Get()
		scratch.touchedNodes = append(scratch.touchedNodes[:0], router.touchedNodes...)
		router.touchedNodes = scratch.touchedNodes
		router.scratch = scratch
	}
	return router.scratch
}

// carveSearchState gives the router zeroed state arrays of nodeCount
// entries, as the four make calls it replaces did. Entries touched by this
// router's earlier searches are cleared first: they are the only non-nil
// pointer entries of the scratch.
func (router *ovgEdgeRouter) carveSearchState(nodeCount int) {
	scratch := router.searchScratch()
	router.clearTouchedState()
	if cap(scratch.nodeContext) < nodeCount {
		scratch.verticalHops = make([]*OVGNode, nodeCount)
		scratch.horizontalHops = make([]*OVGNode, nodeCount)
		scratch.nodeContext = make([]*searchNodeContext, nodeCount)
		scratch.nodeContextValues = make([]searchNodeContext, nodeCount)
	}
	router.verticalHops = scratch.verticalHops[:nodeCount]
	router.horizontalHops = scratch.horizontalHops[:nodeCount]
	router.nodeContext = scratch.nodeContext[:nodeCount]
	router.nodeContextValues = scratch.nodeContextValues[:nodeCount]
}

// clearTouchedState nils the pointer entries the router's last search
// touched (every entry its searches can have set; earlier searches' entries
// were cleared when the next search started).
func (router *ovgEdgeRouter) clearTouchedState() {
	for _, i := range router.touchedNodes {
		if i < len(router.nodeContext) {
			router.verticalHops[i] = nil
			router.horizontalHops[i] = nil
			router.nodeContext[i] = nil
		}
	}
}

// releaseSearchScratch returns the router's scratch to the pool. The router
// must not search afterwards. Routing calls release their routers only after
// every flavor worker has returned normally.
func (router *ovgEdgeRouter) releaseSearchScratch() {
	scratch := router.scratch
	if scratch == nil {
		return
	}
	router.clearTouchedState()
	scratch.touchedNodes = router.touchedNodes[:0]
	scratch.queue.release()
	router.scratch = nil
	router.verticalHops = nil
	router.horizontalHops = nil
	router.nodeContext = nil
	router.nodeContextValues = nil
	router.touchedNodes = nil
	if scratch.retainable() {
		routeSearchScratchPool.Put(scratch)
	}
}

// releaseRouterScratch releases every router's search scratch.
func releaseRouterScratch(routers []*ovgEdgeRouter) {
	for _, router := range routers {
		router.releaseSearchScratch()
	}
}
