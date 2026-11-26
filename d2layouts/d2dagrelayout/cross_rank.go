package d2dagrelayout

import (
	"math"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

// crossRankIndex lists the incident edges of every indexed object in g.Edges
// order, a self-loop once: the edges of object k are the positions
// list[start[k]:start[k+1]] of g.Edges.
type crossRankIndex struct {
	start []int32
	list  []int32
}

func newCrossRankIndex(ix *objectIndex) *crossRankIndex {
	m, e := len(ix.objs), len(ix.edgeSrc)
	// start is counted from zero; next and list are written before they
	// are read.
	block := grow(ix.sc.crossBlock, 2*m+1+2*e)
	ix.sc.crossBlock = block
	clear(block[:m+1])
	start := block[: m+1 : m+1]
	next := block[m+1 : 2*m+1 : 2*m+1]
	for i := range ix.edgeSrc {
		start[ix.edgeSrc[i]+1]++
		if ix.edgeDst[i] != ix.edgeSrc[i] {
			start[ix.edgeDst[i]+1]++
		}
	}
	for k := 1; k < len(start); k++ {
		start[k] += start[k-1]
	}
	list := block[2*m+1 : 2*m+1+int(start[m])]
	copy(next, start)
	for i := range ix.edgeSrc {
		src, dst := ix.edgeSrc[i], ix.edgeDst[i]
		list[next[src]] = int32(i)
		next[src]++
		if dst != src {
			list[next[dst]] = int32(i)
			next[dst]++
		}
	}
	ix.sc.crossIndex = crossRankIndex{start: start, list: list}
	return &ix.sc.crossIndex
}

func (c *crossRankIndex) incident(k int32) []int32 {
	return c.list[c.start[k]:c.start[k+1]]
}

// crossRankMarks are the sets of one shift. A set holds object k when its
// mark equals the shift's epoch, so starting a shift empties every set.
type crossRankMarks struct {
	seen, shifted, needsMove, grown, increased uint32
}

// crossRankShifter shifts objects across ranks (shiftReachableDownIndexed in
// earlier versions, which kept the same sets in maps keyed by pointer).
type crossRankShifter struct {
	g            *d2graph.Graph
	ix           *objectIndex
	index        *crossRankIndex
	isHorizontal bool

	epoch       uint32
	marks       []crossRankMarks
	edgeShifted []uint32
	// scan[i] holds the Box and TopLeft of g.Objects[i]. No object's Box or
	// TopLeft is replaced while the shifter runs (positions and sizes change
	// in place), so checkBelow reads the same values through them, with
	// fewer dependent loads per object it scans.
	scan []scanRef
	// useAxis tells shift to build axis, which checkBelow then searches
	// instead of scanning (see axisOrder).
	useAxis bool
	axis    *axisOrder

	// orders, in tests, gives the grow pass's order (see growContainers).
	orders mapOrders

	// The shift in progress.
	obj      int32
	start    float64
	distance float64
	isMargin bool
	queue    []int32
	head     int
	// seenOrder holds the seen objects in the order they were seen, which
	// is the order the original inserted them into its map of seen
	// objects. While a grow pass with orders runs, seenSet is that map, and
	// the objects seen meanwhile are inserted into it too.
	seenOrder  []int32
	seenSet    map[*d2graph.Object]struct{}
	movedOrder []int32
	increased  []int32
}

const crossRankThreshold = 100.

type scanRef struct {
	tl  *geo.Point
	box *geo.Box
}

func newCrossRankShifter(g *d2graph.Graph, ix *objectIndex, isHorizontal, scanAll bool, orders mapOrders) *crossRankShifter {
	sc := ix.sc
	scan := grow(sc.scan, len(g.Objects))
	for i, obj := range g.Objects {
		scan[i] = scanRef{tl: obj.TopLeft, box: obj.Box}
	}
	sc.scan = scan
	sc.marks = zeroed(sc.marks, len(ix.objs))
	sc.edgeShifted = zeroed(sc.edgeShifted, len(g.Edges))
	sc.shifter = crossRankShifter{
		g:            g,
		ix:           ix,
		index:        newCrossRankIndex(ix),
		isHorizontal: isHorizontal,
		marks:        sc.marks,
		edgeShifted:  sc.edgeShifted,
		scan:         scan,
		useAxis:      !scanAll && regularForAxisOrder(ix),
		orders:       orders,
		queue:        sc.queue[:0],
		seenOrder:    sc.seenOrder[:0],
		movedOrder:   sc.movedOrder[:0],
		increased:    sc.increased[:0],
	}
	return &sc.shifter
}

// keepStorage returns the lists s grew to the scratch for the next call.
func (s *crossRankShifter) keepStorage() {
	sc := s.ix.sc
	sc.queue, sc.seenOrder, sc.movedOrder, sc.increased = s.queue[:0], s.seenOrder[:0], s.movedOrder[:0], s.increased[:0]
}

func (s *crossRankShifter) enqueue(o int32) {
	if s.marks[o].seen == s.epoch {
		return
	}
	s.queue = append(s.queue, o)
}

// checkBelow queues the objects below curr that would come within the
// threshold after the shift, in g.Objects order. The tests are side-effect
// free, so the geometric ones run before the ancestor walk.
func (s *crossRankShifter) checkBelow(curr int32) {
	ix := s.ix
	c := ix.objs[curr]
	distance := s.distance
	currBottom := c.TopLeft.Y + c.Height
	currRight := c.TopLeft.X + c.Width
	if s.isHorizontal {
		originalRight := currRight
		if s.marks[curr].shifted == s.epoch {
			originalRight -= distance
		}
		limit := originalRight + distance + crossRankThreshold
		if s.axis != nil {
			s.queueInWindow(originalRight, limit, func(i int) bool {
				other := s.scan[i]
				return c.TopLeft.Y < other.tl.Y+other.box.Height &&
					other.tl.Y < currBottom &&
					s.g.Objects[i] != c && !c.IsDescendantOf(s.g.Objects[i])
			})
			return
		}
		for i, other := range s.scan {
			if originalRight < other.tl.X &&
				other.tl.X < limit &&
				c.TopLeft.Y < other.tl.Y+other.box.Height &&
				other.tl.Y < currBottom &&
				s.g.Objects[i] != c && !c.IsDescendantOf(s.g.Objects[i]) {
				s.enqueue(ix.at[i])
			}
		}
	} else {
		originalBottom := currBottom
		if s.marks[curr].shifted == s.epoch {
			originalBottom -= distance
		}
		limit := originalBottom + distance + crossRankThreshold
		if s.axis != nil {
			s.queueInWindow(originalBottom, limit, func(i int) bool {
				other := s.scan[i]
				return c.TopLeft.X < other.tl.X+other.box.Width &&
					other.tl.X < currRight &&
					s.g.Objects[i] != c && !c.IsDescendantOf(s.g.Objects[i])
			})
			return
		}
		for i, other := range s.scan {
			if originalBottom < other.tl.Y &&
				other.tl.Y < limit &&
				c.TopLeft.X < other.tl.X+other.box.Width &&
				other.tl.X < currRight &&
				s.g.Objects[i] != c && !c.IsDescendantOf(s.g.Objects[i]) {
				s.enqueue(ix.at[i])
			}
		}
	}
}

func (s *crossRankShifter) processQueue() {
	ix := s.ix
	epoch := s.epoch
	start, distance := s.start, s.distance
	isHorizontal := s.isHorizontal
	for s.head < len(s.queue) {
		curr := s.queue[s.head]
		s.head++
		m := &s.marks[curr]
		if m.seen == epoch {
			continue
		}
		c := ix.objs[curr]
		// skip other objects behind start
		if curr != s.obj {
			if m.needsMove != epoch {
				if isHorizontal {
					if c.TopLeft.X < start {
						continue
					}
				} else {
					if c.TopLeft.Y < start {
						continue
					}
				}
			}
		}

		shift := m.needsMove == epoch
		if isHorizontal {
			if !shift {
				if !s.isMargin {
					shift = start < c.TopLeft.X
				} else {
					shift = start <= c.TopLeft.X
				}
			}
			if shift {
				c.TopLeft.X += distance
				m.shifted = epoch
				s.movedOrder = append(s.movedOrder, curr)
			}
		} else {
			if !shift {
				if !s.isMargin {
					shift = start < c.TopLeft.Y
				} else {
					shift = start <= c.TopLeft.Y
				}
			}
			if shift {
				c.TopLeft.Y += distance
				m.shifted = epoch
				s.movedOrder = append(s.movedOrder, curr)
			}
		}
		m.seen = epoch
		s.seenOrder = append(s.seenOrder, curr)
		if s.seenSet != nil {
			s.seenSet[c] = struct{}{}
		}

		if ix.parent[curr] != ix.root && !c.IsDescendantOf(ix.objs[s.obj]) {
			s.enqueue(ix.parent[curr])
		}

		for _, child := range ix.children(curr) {
			s.enqueue(child)
		}

		for _, ei := range s.index.incident(curr) {
			if s.edgeShifted[ei] == epoch {
				continue
			}
			e := s.g.Edges[ei]
			src, dst := ix.edgeSrc[ei], ix.edgeDst[ei]
			if src == curr && dst == curr {
				if isHorizontal {
					for _, p := range e.Route {
						p.X += distance
					}
				} else {
					for _, p := range e.Route {
						p.Y += distance
					}
				}
				s.edgeShifted[ei] = epoch
				continue
			} else if src == curr {
				last := e.Route[len(e.Route)-1]
				if isHorizontal {
					if start <= last.X &&
						e.Dst.TopLeft.X+e.Dst.Width < last.X+distance {
						s.marks[dst].needsMove = epoch
					}
				} else {
					if start <= last.Y &&
						e.Dst.TopLeft.Y+e.Dst.Height < last.Y+distance {
						s.marks[dst].needsMove = epoch
					}
				}
				s.enqueue(dst)
				first := e.Route[0]
				startIndex := 0
				wasShifted := s.marks[curr].shifted == epoch
				if isHorizontal {
					if wasShifted && first.X < c.TopLeft.X && first.X < start {
						first.X += distance
						startIndex++
					}
					for i := startIndex; i < len(e.Route); i++ {
						p := e.Route[i]
						if start <= p.X {
							p.X += distance
						}
					}
				} else {
					if wasShifted && first.Y < c.TopLeft.Y && first.Y < start {
						first.Y += distance
						startIndex++
					}
					for i := startIndex; i < len(e.Route); i++ {
						p := e.Route[i]
						if start <= p.Y {
							p.Y += distance
						}
					}
				}
				s.edgeShifted[ei] = epoch
			} else if dst == curr {
				first := e.Route[0]
				if isHorizontal {
					if start <= first.X &&
						e.Src.TopLeft.X+e.Src.Width < first.X+distance {
						s.marks[src].needsMove = epoch
					}
				} else {
					if start <= first.Y &&
						e.Src.TopLeft.Y+e.Src.Height < first.Y+distance {
						s.marks[src].needsMove = epoch
					}
				}
				s.enqueue(src)
				last := e.Route[len(e.Route)-1]
				endIndex := len(e.Route)
				wasShifted := s.marks[curr].shifted == epoch
				if isHorizontal {
					if wasShifted && last.X < c.TopLeft.X && last.X < start {
						last.X += distance
						endIndex--
					}
					for i := 0; i < endIndex; i++ {
						p := e.Route[i]
						if start <= p.X {
							p.X += distance
						}
					}
				} else {
					if wasShifted && last.Y < c.TopLeft.Y && last.Y < start {
						last.Y += distance
						endIndex--
					}
					for i := 0; i < endIndex; i++ {
						p := e.Route[i]
						if start <= p.Y {
							p.Y += distance
						}
					}
				}
				s.edgeShifted[ei] = epoch
			}
		}

		// check for nodes below that need to move from the shift
		s.checkBelow(curr)
	}
}

// shift moves obj, and what is reachable from it at or past start, by
// distance, grows the containers that span start, and returns the moved
// objects whose margin increased (no other moved object lies just above them).
func (s *crossRankShifter) shift(obj int32, start, distance float64, isMargin bool) []int32 {
	ix := s.ix
	s.epoch++
	epoch := s.epoch
	s.obj, s.start, s.distance, s.isMargin = obj, start, distance, isMargin
	s.queue = append(s.queue[:0], obj)
	s.head = 0
	s.seenOrder = s.seenOrder[:0]
	s.movedOrder = s.movedOrder[:0]
	// The keys must be the coordinates from before this shift moves anything.
	if s.useAxis && s.axis == nil {
		s.buildAxisOrder()
	}

	s.processQueue()
	s.growContainers()
	s.moveShifted()

	// The result is a set: whether a moved object counts depends only on the
	// final positions of the moved objects, not on the order they moved in.
	s.increased = s.increased[:0]
	for _, moved := range s.movedOrder {
		if s.marks[moved].increased == epoch {
			continue
		}
		mo := ix.objs[moved]
		counts := true
		// check if any other shifted is directly above
		for _, other := range s.movedOrder {
			if other == moved {
				continue
			}
			oo := ix.objs[other]
			if s.isHorizontal {
				if oo.TopLeft.Y+oo.Height < mo.TopLeft.Y ||
					mo.TopLeft.Y+mo.Height < oo.TopLeft.Y {
					// doesn't line up vertically
					continue
				}

				// above and within threshold
				if oo.TopLeft.X < mo.TopLeft.X &&
					mo.TopLeft.X < oo.TopLeft.X+oo.Width+crossRankThreshold {
					counts = false
					break
				}
			} else {
				if oo.TopLeft.X+oo.Width < mo.TopLeft.X ||
					mo.TopLeft.X+mo.Width < oo.TopLeft.X {
					// doesn't line up horizontally
					continue
				}

				// above and within threshold
				if oo.TopLeft.Y < mo.TopLeft.Y &&
					mo.TopLeft.Y < oo.TopLeft.Y+oo.Height+crossRankThreshold {
					counts = false
					break
				}
			}
		}
		if counts {
			s.marks[moved].increased = epoch
			s.increased = append(s.increased, moved)
		}
	}
	return s.increased
}

// growContainers is the grow pass of a shift: for every seen object, it
// grows the ancestors that span start (up to the first shifted or grown
// one), and processes what that moves.
//
// The original ranged over its map of seen objects, which also received the
// objects seen while growing:
//
//	for o := range seen { ... }
//
// so the objects the pass visited and their order, and with them the layout,
// could change from run to run. The pass takes one of the orders that loop
// took under gc: that of an iteration starting at the map's first slot (see
// map_order.go). It visits the objects in the order they were seen
// (seenOrder), including those seen during the pass while the map fits in
// one group of mapGroupSlots, whose slots fill in insertion order, and no
// object seen after the map outgrew it, nor any seen during the pass if it
// started out larger.
//
// Until the pass grows a container it only reads, so if no seen object can
// lead it to grow one (mayGrow), it changes nothing in any order.
//
// In tests, orders gives the order instead, over a map of the seen objects
// like the original's (seenSet).
func (s *crossRankShifter) growContainers() {
	if !s.mayGrow() {
		if s.orders != nil {
			s.orders.growPass(nil, nil, nil)
		}
		return
	}
	if s.orders != nil {
		seen := make(map[*d2graph.Object]struct{})
		for _, k := range s.seenOrder {
			seen[s.ix.objs[k]] = struct{}{}
		}
		s.seenSet = seen
		s.orders.growPass(seen, s.growFrom, s.insertedObjects)
		s.seenSet = nil
		return
	}
	visits := len(s.seenOrder)
	if visits <= mapGroupSlots {
		visits = mapGroupSlots
	}
	for i := 0; i < visits && i < len(s.seenOrder); i++ {
		s.growFrom(s.ix.objs[s.seenOrder[i]])
	}
}

// mayGrow reports whether the grow pass can grow a container: whether
// walking up from some seen object's parent, before the root or a shifted
// ancestor (none is grown yet), reaches an ancestor that lies before start.
// It also reports true where it cannot tell (an ancestor whose position
// cannot be read, or a cycle of parents), so that the pass does there
// whatever it did.
func (s *crossRankShifter) mayGrow() bool {
	ix := s.ix
	epoch := s.epoch
	for _, o := range s.seenOrder {
		steps := 0
		for parent := ix.parent[o]; parent != ix.root; parent = ix.parent[parent] {
			if s.marks[parent].shifted == epoch {
				break
			}
			if steps++; steps > len(ix.objs) {
				return true
			}
			po := ix.objs[parent]
			if po == nil || po.Box == nil || po.TopLeft == nil {
				return true
			}
			if s.isHorizontal {
				if po.TopLeft.X < s.start {
					return true
				}
			} else {
				if po.TopLeft.Y < s.start {
					return true
				}
			}
		}
	}
	return false
}

// growFrom is the grow pass's work for the seen object obj.
func (s *crossRankShifter) growFrom(obj *d2graph.Object) {
	ix := s.ix
	epoch := s.epoch
	start, distance := s.start, s.distance
	o, _ := ix.lookup(obj)
	p := ix.parent[o]
	if p == ix.root {
		return
	}
	if s.marks[p].shifted == epoch {
		return
	}
	if s.marks[p].grown == epoch {
		return
	}

	for parent := p; parent != ix.root; parent = ix.parent[parent] {
		if s.marks[parent].shifted == epoch {
			break
		}
		if s.marks[parent].grown == epoch {
			break
		}

		po := ix.objs[parent]
		if s.isHorizontal {
			if po.TopLeft.X < start {
				po.Width += distance
				s.marks[parent].grown = epoch
				s.movedOrder = append(s.movedOrder, parent)

				s.checkBelow(parent)
				s.processQueue()
			}
		} else {
			if po.TopLeft.Y < start {
				po.Height += distance
				s.marks[parent].grown = epoch
				s.movedOrder = append(s.movedOrder, parent)

				s.checkBelow(parent)
				s.processQueue()
			}
		}
	}
}

// insertedObjects returns the objects of seenSet in the order they were
// inserted (for mapOrders).
func (s *crossRankShifter) insertedObjects() []*d2graph.Object {
	objs := make([]*d2graph.Object, len(s.seenOrder))
	for i, k := range s.seenOrder {
		objs[i] = s.ix.objs[k]
	}
	return objs
}

func adjustCrossRankSpacing(g *d2graph.Graph, ix *objectIndex, rankSep float64, isHorizontal bool) {
	adjustCrossRankSpacingWith(g, ix, rankSep, isHorizontal, false, nil)
}

// adjustCrossRankSpacingWith is adjustCrossRankSpacing; scanAll makes
// checkBelow test every object instead of searching the sorted axis (tests
// compare the two), and orders, when not nil, replaces the grow passes'
// map iteration (tests).
func adjustCrossRankSpacingWith(g *d2graph.Graph, ix *objectIndex, rankSep float64, isHorizontal, scanAll bool, orders mapOrders) {
	// prevMargin[k] is the largest margin by which object k was already moved
	// away from the side being processed. The earlier maps held an entry only
	// for objects that were moved; an entry is the Max of 0 and margins that
	// are > 0, so it is > 0 exactly when the map had it, and 0 otherwise.
	// Both, and the shifter, are made at the first shift (most graphs have
	// none); nothing they hold changes before it.
	var s *crossRankShifter
	var prevMarginBefore, prevMarginAfter []float64
	shift := func(obj int32, start, distance float64, isMargin bool) []int32 {
		if s == nil {
			s = newCrossRankShifter(g, ix, isHorizontal, scanAll, orders)
			prevMargins := zeroed(ix.sc.prevMargins, 2*len(ix.objs))
			ix.sc.prevMargins = prevMargins
			prevMarginBefore, prevMarginAfter = prevMargins[:len(ix.objs)], prevMargins[len(ix.objs):]
		}
		return s.shift(obj, start, distance, isMargin)
	}
	moved := func(prev []float64, k int32) bool {
		return prev != nil && prev[k] > 0
	}
	raise := func(prev []float64, increased []int32, margin float64) {
		for _, o := range increased {
			prev[o] = math.Max(prev[o], margin)
		}
	}
	for i, obj := range g.Objects {
		if obj.IsGridDiagram() {
			continue
		}
		k := ix.at[i]
		margin, padding := ix.spacing(k)
		if !isHorizontal {
			if moved(prevMarginAfter, k) {
				margin.Bottom -= prevMarginAfter[k]
			}
			if margin.Bottom > 0 {
				increased := shift(k, obj.TopLeft.Y+obj.Height, margin.Bottom, true)
				raise(prevMarginAfter, increased, margin.Bottom)
			}
			if padding.Bottom > 0 {
				shift(k, obj.TopLeft.Y+obj.Height, padding.Bottom, false)
				obj.Height += padding.Bottom
			}
			if moved(prevMarginBefore, k) {
				margin.Top -= prevMarginBefore[k]
			}
			if margin.Top > 0 {
				increased := shift(k, obj.TopLeft.Y, margin.Top, true)
				raise(prevMarginBefore, increased, margin.Top)
			}
			if padding.Top > 0 {
				shift(k, obj.TopLeft.Y, padding.Top, false)
				obj.Height += padding.Top
			}
		} else {
			if moved(prevMarginAfter, k) {
				margin.Right -= prevMarginAfter[k]
			}
			if margin.Right > 0 {
				increased := shift(k, obj.TopLeft.X+obj.Width, margin.Right, true)
				raise(prevMarginAfter, increased, margin.Right)
			}
			if padding.Right > 0 {
				shift(k, obj.TopLeft.X+obj.Width, padding.Right, false)
				obj.Width += padding.Right
			}
			if moved(prevMarginBefore, k) {
				margin.Left -= prevMarginBefore[k]
			}
			if margin.Left > 0 {
				increased := shift(k, obj.TopLeft.X, margin.Left, true)
				raise(prevMarginBefore, increased, margin.Left)
			}
			if padding.Left > 0 {
				shift(k, obj.TopLeft.X, padding.Left, false)
				obj.Width += padding.Left
			}
		}
	}
	if s != nil {
		s.keepStorage()
	}
}
