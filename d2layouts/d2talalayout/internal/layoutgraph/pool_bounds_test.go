package layoutgraph

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/typedpool"
)

// TinyGo's sync.Pool (d2's wasm build) is a list that never frees, so a pool
// must not keep objects of unbounded size. These tests release scratch that
// grew past the bound and check the pool does not hand it out again (gc
// returns a just-released object to the same goroutine, TinyGo returns the
// last one put), and that typical scratch stays poolable.

const overBound = typedpool.MaxRetainedEntries + 1

func nodesForBounds(n int) []*Node {
	nodes := make([]*Node, n)
	for i := range nodes {
		nodes[i] = NewNode(EntityID(i+1), 1, 1)
	}
	return nodes
}

func assertNotPooled[T comparable](t *testing.T, name string, pool *typedpool.Pool[T], released T) {
	t.Helper()
	for i := 0; i < 4; i++ {
		if pool.Get() == released {
			t.Fatalf("%s: oversized scratch returned to the pool", name)
		}
	}
}

func TestPooledLayoutScratchIsBounded(t *testing.T) {
	nodes := nodesForBounds(overBound)
	t.Run("descendantWalk", func(t *testing.T) {
		small := descendantWalkPool.Get()
		small.stack = make([]descendantWalkEntry, 0, 64)
		if !small.retainable() {
			t.Fatal("small walk scratch not retainable")
		}
		for _, grow := range []func(*descendantWalkScratch){
			func(s *descendantWalkScratch) { s.stack = make([]descendantWalkEntry, 0, overBound) },
			func(s *descendantWalkScratch) { s.visited = make([]*Node, 0, overBound) },
			func(s *descendantWalkScratch) { s.descendants = make([]*Node, 0, overBound) },
			func(s *descendantWalkScratch) { s.exceptions = make([]*Node, 0, overBound) },
			func(s *descendantWalkScratch) {
				for _, node := range nodes {
					s.seen[node] = struct{}{}
				}
				s.seenUsed = true
			},
		} {
			scratch := descendantWalkPool.Get()
			grow(scratch)
			releaseDescendantWalkScratch(scratch)
			assertNotPooled(t, "descendant walk", descendantWalkPool, scratch)
		}
	})
	t.Run("connectedSet", func(t *testing.T) {
		for _, grow := range []func(*connectedSetScratch){
			func(s *connectedSetScratch) { s.queue = make([]*Node, 0, overBound) },
			func(s *connectedSetScratch) {
				for _, node := range nodes {
					s.excluded[node] = true
				}
			},
			func(s *connectedSetScratch) {
				for _, node := range nodes {
					s.excludedChildren[node] = true
				}
			},
			func(s *connectedSetScratch) {
				s.owners.more = map[*Node][]*Node{}
				for _, node := range nodes {
					s.owners.more[node] = nil
				}
			},
		} {
			scratch := connectedSetPool.Get()
			grow(scratch)
			scratch.release()
			assertNotPooled(t, "connected set", connectedSetPool, scratch)
		}
	})
	t.Run("preflight", func(t *testing.T) {
		small := preflightScratchPool.Get()
		if !small.retainable() {
			t.Fatal("empty preflight scratch not retainable")
		}
		for _, grow := range []func(*preflightScratch){
			func(s *preflightScratch) { s.edgeQueue = make([]*Edge, 0, overBound) },
			func(s *preflightScratch) { s.childBuffer = make([]*Node, 0, overBound) },
			func(s *preflightScratch) { s.descendantColor = make([]uint8, 0, overBound) },
			func(s *preflightScratch) {
				for i := 0; i < overBound; i++ {
					s.edges[&Edge{}] = struct{}{}
				}
			},
		} {
			scratch := preflightScratchPool.Get()
			grow(scratch)
			scratch.release()
			assertNotPooled(t, "preflight", preflightScratchPool, scratch)
		}
	})
	t.Run("parentRelation", func(t *testing.T) {
		scratch, pooled := borrowParentRelationScratch(10)
		if !pooled {
			t.Fatal("small parent relation scratch not pooled")
		}
		scratch.extraState = map[*Node]uint8{}
		for _, node := range nodes {
			scratch.extraState[node] = 1
		}
		returnParentRelationScratch(scratch, true)
		assertNotPooled(t, "parent relation", parentRelationScratchPool, scratch)
	})
	t.Run("recordStack", func(t *testing.T) {
		stack := recordStackPool.Get()
		releaseRecordStack(stack, make([]*Node, 0, overBound))
		assertNotPooled(t, "record stack", recordStackPool, stack)
	})
	t.Run("transactionEdgeSet", func(t *testing.T) {
		set := transactionEdgeSetPool.Get()
		for i := 0; i < overBound; i++ {
			set.edges[&Edge{}] = struct{}{}
		}
		releaseTransactionEdgeSet(set)
		assertNotPooled(t, "transaction edge set", transactionEdgeSetPool, set)
	})
	t.Run("overlapExceptions", func(t *testing.T) {
		scratch := overlapExceptionPool.Get()
		scratch.nodes = map[*Node]struct{}{}
		for _, node := range nodes {
			scratch.nodes[node] = struct{}{}
		}
		releaseOverlapExceptions(scratch)
		if scratch.nodes != nil {
			t.Fatal("oversized overlap exception set kept")
		}
	})
}

// TestPooledLayoutScratchDropsReferencesAfterAnAbortedWalk: a walk that panics
// before storing its list back leaves appended entries beyond the stored
// length; the release must still drop them (pools keep no references to a
// caller's graph).
func TestPooledLayoutScratchDropsReferencesAfterAnAbortedWalk(t *testing.T) {
	stale := func() []*Node {
		backing := make([]*Node, 8)
		for i := range backing {
			backing[i] = NewNode(EntityID(i+1), 1, 1)
		}
		return backing
	}
	check := func(name string, backing []*Node) {
		t.Helper()
		for i, node := range backing {
			if node != nil {
				t.Fatalf("%s: entry %d still referenced after release", name, i)
			}
		}
	}
	queue := stale()
	set := connectedSetPool.Get()
	set.queue = queue[:0]
	set.release()
	check("connected set queue", queue)

	descendants, exceptions := stale(), stale()
	walk := descendantWalkPool.Get()
	walk.descendants = descendants[:0]
	walk.exceptions = exceptions[:0]
	releaseDescendantWalkScratch(walk)
	check("walk descendants", descendants)
	check("walk exceptions", exceptions)
}
