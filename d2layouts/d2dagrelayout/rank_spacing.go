package d2dagrelayout

import (
	"cmp"
	"math"
	"sort"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/xsort"
)

// rankItem is an object to be ranked and the coordinate that ranks it.
type rankItem struct {
	level float64
	obj   int32
}

// objectRanks are the rank of every non-container object and the first and
// last rank of every container that holds one, by object index, -1 where the
// earlier maps had no entry.
type objectRanks struct {
	ranks          [][]int32 // object indices, in g.Objects order within a rank
	object         []int32
	startingParent []int32
	endingParent   []int32
}

// orZero reads a rank the way the earlier map lookups without a presence
// test did: a missing entry reads as rank 0.
func orZero(r int32) int32 {
	if r < 0 {
		return 0
	}
	return r
}

// getRanks groups the non-container objects into ranks by the rounded-up
// coordinate of their center along the rank axis, in increasing order. The
// original grouped them in a map keyed by that coordinate and sorted the
// keys; when no coordinate is NaN, the keys are distinct and sort to one
// order whatever order they come in, and sorting the objects stably by
// coordinate gives the same groups (+0 and -0 are one key and one group) in
// that order, each in g.Objects order. With a NaN coordinate the ranks are
// numbered as the original numbered them (getRanksMapOrder).
func getRanks(g *d2graph.Graph, ix *objectIndex, isHorizontal bool, orders mapOrders) objectRanks {
	type aligned = rankItem
	items := ix.sc.rankItems[:0]
	for i, obj := range g.Objects {
		if obj.IsContainer() {
			continue
		}
		var level float64
		if !isHorizontal {
			level = math.Ceil(obj.TopLeft.Y + obj.Height/2)
		} else {
			level = math.Ceil(obj.TopLeft.X + obj.Width/2)
		}
		if math.IsNaN(level) {
			ix.sc.rankItems = items
			return getRanksMapOrder(g, ix, isHorizontal, orders)
		}
		items = append(items, aligned{level: level, obj: ix.at[i]})
	}
	ix.sc.rankItems = items
	if orders != nil {
		orders.levels(nil)
	}
	xsort.StableFunc(items, func(a, b aligned) int {
		return cmp.Compare(a.level, b.level)
	})

	m := len(ix.objs)
	// The first 3*m elements are set here, the members as the ranks are
	// formed.
	block := grow(ix.sc.rankBlock, 3*m+len(items))
	ix.sc.rankBlock = block
	for k := range block[:3*m] {
		block[k] = -1
	}
	r := objectRanks{
		ranks:          ix.sc.ranks[:0],
		object:         block[:m:m],
		startingParent: block[m : 2*m : 2*m],
		endingParent:   block[2*m : 3*m : 3*m],
	}
	members := block[3*m:]
	for i := 0; i < len(items); {
		j := i + 1
		for j < len(items) && items[j].level == items[i].level {
			j++
		}
		rank := int32(len(r.ranks))
		group := members[i:j:j]
		for m := i; m < j; m++ {
			group[m-i] = items[m].obj
			r.object[items[m].obj] = rank
		}
		r.ranks = append(r.ranks, group)
		i = j
	}
	ix.sc.ranks = r.ranks
	r.setParentRanks(g, ix)
	return r
}

// setParentRanks sets the first and last rank of every container from the
// ranks of the objects in it.
func (r *objectRanks) setParentRanks(g *d2graph.Graph, ix *objectIndex) {
	for i, obj := range g.Objects {
		if obj.IsContainer() {
			continue
		}
		rank := orZero(r.object[ix.at[i]])
		// update all ancestor's min/max ranks
		for parent := ix.parent[ix.at[i]]; parent != ix.root && ix.objs[parent] != nil; parent = ix.parent[parent] {
			if start := r.startingParent[parent]; start < 0 || rank < start {
				r.startingParent[parent] = rank
			}
			if end := r.endingParent[parent]; end < 0 || rank > end {
				r.endingParent[parent] = rank
			}
		}
	}
}

// getRanksMapOrder is getRanks for a graph in which some object's rank
// coordinate is NaN, computed the way the original computed it. The
// original grouped the objects in a map keyed by the coordinate. A NaN key
// equals no key, so each object with a NaN coordinate made a key of its
// own that no lookup finds: a rank without objects, and the object itself
// got no rank (it reads as rank 0). The keys were sorted with sort.Slice,
// to which NaN is neither less nor greater than anything, so where the
// empty ranks landed, and in what order the ranks around them came, depended
// on the order of the keys (see map_order.go). The ranks are numbered here
// by ranging over the same map, filled in the same order, and sorting its
// keys the same way.
func getRanksMapOrder(g *d2graph.Graph, ix *objectIndex, isHorizontal bool, orders mapOrders) objectRanks {
	alignedObjects := make(map[float64][]*d2graph.Object)
	for _, obj := range g.Objects {
		if !obj.IsContainer() {
			if !isHorizontal {
				y := math.Ceil(obj.TopLeft.Y + obj.Height/2)
				alignedObjects[y] = append(alignedObjects[y], obj)
			} else {
				x := math.Ceil(obj.TopLeft.X + obj.Width/2)
				alignedObjects[x] = append(alignedObjects[x], obj)
			}
		}
	}

	var levels []float64
	if orders != nil {
		levels = orders.levels(alignedObjects)
	} else {
		levels = make([]float64, 0, len(alignedObjects))
		for l := range alignedObjects {
			levels = append(levels, l)
		}
	}
	sort.Slice(levels, func(i, j int) bool {
		return levels[i] < levels[j]
	})

	m := len(ix.objs)
	block := make([]int32, 3*m)
	for k := range block {
		block[k] = -1
	}
	r := objectRanks{
		ranks:          make([][]int32, 0, len(levels)),
		object:         block[:m:m],
		startingParent: block[m : 2*m : 2*m],
		endingParent:   block[2*m:],
	}
	for i, l := range levels {
		var rank []int32
		for _, obj := range alignedObjects[l] {
			k, _ := ix.lookup(obj)
			r.object[k] = int32(i)
			rank = append(rank, k)
		}
		r.ranks = append(r.ranks, rank)
	}
	r.setParentRanks(g, ix)
	return r
}

// ancestorPositions collects, for one rank, the position every ancestor of
// the rank's objects must reach on one side. A parent queued several times
// (once per child in the rank in the earlier version) is processed once:
// processing it again only repeated the same Min or Max over the same values
// and queued its parent again.
type ancestorPositions struct {
	stamp []int32
	pos   []float64
	keys  []int32 // in the order the ancestors were first reached
	level []int32
	next  []int32
}

// newAncestorPositionsIn returns the buffers for the starting and the ending
// ancestors of n objects, in a few blocks of the scratch's storage. The lists
// are capped slices; one that outgrows its share is moved out of the block by
// append. The stamps start at zero; the positions are written before they
// are read.
func newAncestorPositionsIn(sc *layoutScratch, n int) (starting, ending ancestorPositions) {
	stamps := grow(sc.ancestorInts, 8*n)
	clear(stamps[:2*n])
	pos := grow(sc.ancestorPos, 2*n)
	sc.ancestorInts, sc.ancestorPos = stamps, pos
	part := func(i int) []int32 { return stamps[i*n : i*n : (i+1)*n] }
	starting = ancestorPositions{stamp: stamps[:n:n], pos: pos[:n:n], keys: part(2), level: part(3), next: part(4)}
	ending = ancestorPositions{stamp: stamps[n : 2*n : 2*n], pos: pos[n:], keys: part(5), level: part(6), next: part(7)}
	return starting, ending
}

func (a *ancestorPositions) reset() {
	a.keys = a.keys[:0]
}

// adjustRankSpacing makes room between ranks for container padding and
// child margins, shifting everything past each rank boundary.
//
// The adjustments at one rank are applied to the starting (ending) ancestors
// in increasing (decreasing) order of their target position, ancestors with
// equal positions in the order of the original's map (sortAncestors).
func adjustRankSpacing(g *d2graph.Graph, ix *objectIndex, rankSep float64, isHorizontal bool) {
	adjustRankSpacingWith(g, ix, rankSep, isHorizontal, nil)
}

// adjustRankSpacingWith is adjustRankSpacing; orders, when not nil, replaces
// the iteration of the maps the order of ties comes from (tests).
func adjustRankSpacingWith(g *d2graph.Graph, ix *objectIndex, rankSep float64, isHorizontal bool, orders mapOrders) {
	r := getRanks(g, ix, isHorizontal, orders)
	if len(r.ranks) == 0 {
		return
	}
	starting, ending := newAncestorPositionsIn(ix.sc, len(ix.objs))
	stamp := int32(0)

	// shifting bottom rank down first, then moving up to next rank
	for rank := int32(len(r.ranks)) - 1; rank >= 0; rank-- {
		starting.reset()
		ending.reset()
		starting.level = starting.level[:0]
		ending.level = ending.level[:0]
		for _, obj := range r.ranks[rank] {
			parent := ix.parent[obj]
			if parent == ix.root {
				continue
			}
			if r.endingParent[parent] >= 0 && r.endingParent[parent] == rank {
				ending.level = append(ending.level, parent)
			}
			if r.startingParent[parent] >= 0 && r.startingParent[parent] == rank {
				starting.level = append(starting.level, parent)
			}
		}

		stamp++
		for len(starting.level) > 0 {
			starting.next = starting.next[:0]
			for _, parent := range starting.level {
				if starting.stamp[parent] == stamp {
					continue
				}
				starting.stamp[parent] = stamp
				starting.keys = append(starting.keys, parent)
				po := ix.objs[parent]
				_, padding := ix.spacing(parent)
				startPosition := math.Inf(1)
				var position float64
				if isHorizontal {
					paddingIncrease := math.Max(0, padding.Left-rankSep/2)
					position = po.TopLeft.X - paddingIncrease
				} else {
					paddingIncrease := math.Max(0, padding.Top-rankSep/2)
					position = po.TopLeft.Y - paddingIncrease
				}
				startPosition = math.Min(startPosition, position)
				for _, child := range ix.children(parent) {
					if cr := r.object[child]; cr >= 0 {
						if cr != rank {
							continue
						}
					} else {
						if orZero(r.startingParent[child]) != rank {
							continue
						}
					}
					margin, _ := ix.spacing(child)
					co := ix.objs[child]
					if isHorizontal {
						position = co.TopLeft.X - margin.Left - padding.Left
					} else {
						position = co.TopLeft.Y - margin.Top - padding.Top
					}
					startPosition = math.Min(startPosition, position)
				}
				starting.pos[parent] = startPosition
				if ix.parent[parent] != ix.root {
					starting.next = append(starting.next, ix.parent[parent])
				}
			}
			starting.level, starting.next = starting.next, starting.level
		}

		for len(ending.level) > 0 {
			ending.next = ending.next[:0]
			for _, parent := range ending.level {
				if ending.stamp[parent] == stamp {
					continue
				}
				ending.stamp[parent] = stamp
				ending.keys = append(ending.keys, parent)
				po := ix.objs[parent]
				_, padding := ix.spacing(parent)
				endPosition := math.Inf(-1)
				var position float64
				if isHorizontal {
					position = po.TopLeft.X + po.Width + padding.Right - rankSep/2.
				} else {
					position = po.TopLeft.Y + po.Height + padding.Bottom - rankSep/2.
				}
				endPosition = math.Max(endPosition, position)
				for _, child := range ix.children(parent) {
					if cr := r.object[child]; cr >= 0 {
						if cr != rank {
							continue
						}
					} else {
						if orZero(r.endingParent[child]) != rank {
							continue
						}
					}
					margin, _ := ix.spacing(child)
					co := ix.objs[child]
					if isHorizontal {
						position = co.TopLeft.X + co.Width + margin.Right + padding.Right
					} else {
						position = co.TopLeft.Y + co.Height + margin.Bottom + padding.Bottom
					}
					endPosition = math.Max(endPosition, position)
				}
				ending.pos[parent] = endPosition
				if ix.parent[parent] != ix.root {
					ending.next = append(ending.next, ix.parent[parent])
				}
			}
			ending.level, ending.next = ending.next, ending.level
		}

		// adjust starting ancestors top-down, ending ancestors bottom-up
		sortAncestors(ix, &starting, &ending, orders)

		for _, ancestor := range ending.keys {
			ao := ix.objs[ancestor]
			var position float64
			if isHorizontal {
				position = ao.TopLeft.X + ao.Width
			} else {
				position = ao.TopLeft.Y + ao.Height
			}
			endDelta := ending.pos[ancestor] - position
			if endDelta > 0 {
				for i, obj := range g.Objects {
					if !obj.IsContainer() {
						continue
					}
					k := ix.at[i]
					start := orZero(r.startingParent[k])
					end := orZero(r.endingParent[k])
					if start <= rank && rank <= end {
						if isHorizontal && position <= obj.TopLeft.X+obj.Width {
							obj.Width += endDelta
						} else if !isHorizontal &&
							position <= obj.TopLeft.Y+obj.Height {
							obj.Height += endDelta
						}
					}
				}
				shiftDown(g, position, endDelta, isHorizontal)
			}
		}

		for _, ancestor := range starting.keys {
			ao := ix.objs[ancestor]
			var position float64
			if isHorizontal {
				position = ao.TopLeft.X
			} else {
				position = ao.TopLeft.Y
			}
			startDelta := position - starting.pos[ancestor]
			if startDelta > 0 {
				for i, obj := range g.Objects {
					if !obj.IsContainer() {
						continue
					}
					k := ix.at[i]
					start := orZero(r.startingParent[k])
					end := orZero(r.endingParent[k])
					if start <= rank && rank <= end {
						if isHorizontal && obj.TopLeft.X <= position {
							obj.Width += startDelta
						} else if !isHorizontal && obj.TopLeft.Y <= position {
							obj.Height += startDelta
						}
					}
				}
				shiftUp(g, position, startDelta, isHorizontal)
			}
		}
	}
}

// sortAncestors sorts one rank's starting ancestors by increasing and its
// ending ancestors by decreasing target position (the keys of starting and
// ending, in the order the rank first reached them), the way the original
// ordered them. The original filled a map from the starting ancestors to
// their positions and then one for the ending ancestors, each ancestor
// inserted with an infinite position and then assigned its position, in the
// order the rank first reached them; then it ranged over the starting map
// and over the ending map and sorted their keys with sort.Slice.
//
// When a side's positions are distinct and none is NaN, every order of its
// keys sorts to the same result, which a stable sort gives. When either side
// has ties, the order of its tied ancestors came from its map's iteration
// and from sort.Slice, so both maps are made, filled, ranged over and sorted
// as the original did (see map_order.go). The other side's map is part of
// it: under TinyGo, every map creation, growth and iteration start draws
// from one random stream whose nearby draws are related, so the order in
// which a map is iterated depends on the draws between its creation and its
// iteration, which in the original were the other map's. Only TinyGo tests
// check that, and no CI job runs them: crossRankShifter.originalMaps gives
// the command to run after changing these maps.
func sortAncestors(ix *objectIndex, starting, ending *ancestorPositions, orders mapOrders) {
	first := append(append(ix.sc.ancestorOrder[:0], starting.keys...), ending.keys...)
	ix.sc.ancestorOrder = first
	startingFirst, endingFirst := first[:len(starting.keys)], first[len(starting.keys):]
	startingTied := sortStableByPosition(starting.keys, starting.pos, false)
	endingTied := sortStableByPosition(ending.keys, ending.pos, true)
	if !startingTied && !endingTied {
		if orders != nil {
			orders.ancestors(nil)
			orders.ancestors(nil)
		}
		return
	}

	startingPositions := ancestorMap(ix, startingFirst, starting.pos, math.Inf(1))
	endingPositions := ancestorMap(ix, endingFirst, ending.pos, math.Inf(-1))
	var startingOrder, endingOrder []*d2graph.Object
	if orders != nil {
		if startingTied {
			startingOrder = orders.ancestors(startingPositions)
		} else {
			orders.ancestors(nil)
		}
		if endingTied {
			endingOrder = orders.ancestors(endingPositions)
		} else {
			orders.ancestors(nil)
		}
	} else {
		startingOrder = ancestorMapOrder(startingPositions, startingTied)
		endingOrder = ancestorMapOrder(endingPositions, endingTied)
	}
	if startingTied {
		sortAncestorMapOrder(ix, starting.keys, startingOrder, startingPositions, false)
	}
	if endingTied {
		sortAncestorMapOrder(ix, ending.keys, endingOrder, endingPositions, true)
	}
}

// ancestorLess is the original's comparison of two target positions:
// increasing for the starting ancestors, decreasing for the ending ones.
func ancestorLess(a, b float64, descending bool) bool {
	if descending {
		return b < a
	}
	return a < b
}

// sortStableByPosition sorts keys stably by their positions pos and reports
// whether two of them tie (neither is before the other, NaN included).
func sortStableByPosition(keys []int32, pos []float64, descending bool) (tied bool) {
	xsort.StableFunc(keys, func(a, b int32) int {
		return lessToCompare(ancestorLess(pos[a], pos[b], descending), ancestorLess(pos[b], pos[a], descending))
	})
	for i := 1; i < len(keys); i++ {
		if !ancestorLess(pos[keys[i-1]], pos[keys[i]], descending) {
			return true
		}
	}
	return false
}

// ancestorMap fills a map the way the original filled each side's map: every
// ancestor inserted with the position inf (+Inf for the starting side, -Inf
// for the ending side) and then assigned its position, in the order the rank
// first reached them. Under gc, an assignment to a key the map holds leaves
// the map as it is; TinyGo checks its load factor before it looks the key
// up, so the assignment right after an insert can grow the map, as the
// original's did.
func ancestorMap(ix *objectIndex, firstReached []int32, pos []float64, inf float64) map[*d2graph.Object]float64 {
	positions := make(map[*d2graph.Object]float64)
	for _, k := range firstReached {
		o := ix.objs[k]
		positions[o] = inf
		positions[o] = pos[k]
	}
	return positions
}

// ancestorMapOrder ranges over positions as the original did and returns its
// keys in the iteration's order when keep is set (only the iteration happens
// otherwise: the order of an untied side sorts to one result).
func ancestorMapOrder(positions map[*d2graph.Object]float64, keep bool) []*d2graph.Object {
	if !keep {
		for range positions {
		}
		return nil
	}
	order := make([]*d2graph.Object, 0, len(positions))
	for ancestor := range positions {
		order = append(order, ancestor)
	}
	return order
}

// sortAncestorMapOrder sorts order, the keys of positions in the order the
// map's iteration gave them, with sort.Slice and the original's comparison,
// and writes the result to keys.
func sortAncestorMapOrder(ix *objectIndex, keys []int32, order []*d2graph.Object, positions map[*d2graph.Object]float64, descending bool) {
	sort.Slice(order, func(i, j int) bool {
		iPos := positions[order[i]]
		jPos := positions[order[j]]
		return ancestorLess(iPos, jPos, descending)
	})
	for i, ancestor := range order {
		keys[i], _ = ix.lookup(ancestor)
	}
}

// lessToCompare turns the results of less(a, b) and less(b, a) into the
// result of a three-way comparison.
func lessToCompare(aLess, bLess bool) int {
	switch {
	case aLess:
		return -1
	case bLess:
		return 1
	default:
		return 0
	}
}
