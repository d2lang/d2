package d2dagrelayout

import (
	"sync/atomic"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/dagro"
)

// layoutScratch is the working storage of one Layout call that does not end
// up in the graph: the object index, the resolved edge endpoints, dagro's
// input and output, and the buffers of the spacing passes. Every buffer is
// reinitialised where it is taken, so a call sees exactly the values fresh
// storage would give it.
//
// One scratch is kept between calls in scratchCache. A sync.Pool would not
// do: d2 lays diagrams out one at a time between allocation-heavy work, so
// a garbage collection, which empties sync.Pools, usually runs between two
// calls, and each call would allocate all of its storage again.
type layoutScratch struct {
	ix          objectIndex
	indexBlock  []int32
	spacingsBuf []objectSpacing

	resolvedSrc []*d2graph.Object
	resolvedDst []*d2graph.Object
	counts      []int
	cycleState  []uint8

	in  dagro.LayoutInput
	out dagro.LayoutOutput

	// getRanks and adjustRankSpacing
	rankItems     []rankItem
	rankBlock     []int32
	ranks         [][]int32
	ancestorInts  []int32
	ancestorPos   []float64
	ancestorOrder []int32

	// adjustCrossRankSpacing
	crossBlock  []int32
	scan        []scanRef
	marks       []crossRankMarks
	edgeShifted []uint32
	prevMargins []float64
	queue       []int32
	seenOrder   []int32
	movedOrder  []int32
	increased   []int32
	writes      []int32
	axis        axisOrder
	crossIndex  crossRankIndex
	shifter     crossRankShifter
}

var scratchCache atomic.Pointer[layoutScratch]

// scratchMaxObjects bounds the scratch that is kept between calls: storage
// grown by a graph with more objects or edges is dropped after its call.
// The other buffers are sized by the number of objects and edges, except
// the lists of a cross-rank shift: its queue holds an object once for every
// time it is queued (by a parent, child, edge or object above it), so its
// length is not bounded by the number of objects, and a list longer than
// scratchMaxList is dropped on its own. What is kept is then at most about
// 220 bytes per object and 150 per position in g.Objects and per edge, about
// 1.3 MB at scratchMaxObjects and the layout guard's limits, and a few
// hundred KB after the largest real diagrams (TestPutScratchBoundsKeptStorage).
const (
	scratchMaxObjects = 4096
	scratchMaxList    = 4 * scratchMaxObjects
)

func getScratch() *layoutScratch {
	if s := scratchCache.Swap(nil); s != nil {
		return s
	}
	return new(layoutScratch)
}

// putScratch keeps s for the next call, without its references to the
// caller's graph. It is called only when Layout returns normally, so a call
// that panics never hands its storage on.
func putScratch(s *layoutScratch) {
	if s.release() {
		scratchCache.CompareAndSwap(nil, s)
	}
}

// release drops the references s holds to the caller's graph and the lists
// that grew too long, and reports whether s is small enough to be kept.
func (s *layoutScratch) release() bool {
	if cap(s.ix.objs) > scratchMaxObjects || cap(s.resolvedSrc) > scratchMaxObjects ||
		cap(s.in.Edges) > scratchMaxObjects || cap(s.scan) > scratchMaxObjects {
		return false
	}
	s.queue = atMost(s.queue, scratchMaxList)
	s.seenOrder = atMost(s.seenOrder, scratchMaxList)
	s.movedOrder = atMost(s.movedOrder, scratchMaxList)
	s.increased = atMost(s.increased, scratchMaxList)
	s.writes = atMost(s.writes, scratchMaxList)
	ix := &s.ix
	clear(ix.objs)
	*ix = objectIndex{objs: ix.objs[:0], at: ix.at[:0], lastAt: ix.lastAt[:0]}
	clear(s.resolvedSrc)
	clear(s.resolvedDst)
	clear(s.in.Nodes)
	clear(s.in.Edges)
	s.in = dagro.LayoutInput{Nodes: s.in.Nodes[:0], Edges: s.in.Edges[:0]}
	clear(s.out.Edges)
	clear(s.scan)
	s.shifter = crossRankShifter{}
	return true
}

// atMost returns buf, or nil if it can hold more than n elements.
func atMost[T any](buf []T, n int) []T {
	if cap(buf) > n {
		return nil
	}
	return buf
}

// grow returns buf with length n, reusing its storage. The elements are not
// cleared: callers write every element before reading it.
func grow[T any](buf []T, n int) []T {
	if cap(buf) < n {
		return make([]T, n)
	}
	return buf[:n]
}

// zeroed returns buf with length n and every element zero, reusing its
// storage.
func zeroed[T any](buf []T, n int) []T {
	if cap(buf) < n {
		return make([]T, n)
	}
	buf = buf[:n]
	clear(buf)
	return buf
}
