package d2dagrelayout

import (
	"github.com/d2lang/d2/d2graph"
)

// Three steps of the spacing passes took an order from ranging over a Go map
// in the original (e22f4d566), and their results can depend on it:
//
//   - the cross-rank grow pass ranged over the map of a shift's seen objects
//     while the containers it grew added objects to that map
//     (crossRankShifter.growContainers);
//   - adjustRankSpacing ranged over the maps from one rank's starting and
//     ending ancestors to their target positions and sorted the keys with
//     sort.Slice, so ancestors with equal (or NaN) positions came in an order
//     taken from the map (sortAncestors);
//   - getRanks ranged over the map from rank coordinates to objects; an
//     object with a NaN coordinate was a key of its own, whose place among
//     the sorted coordinates came from the map's order (getRanksMapOrder).
//
// Go randomizes map iteration, so for some diagrams the original's layout
// changed from run to run, and in D2.js it could change between builds.
// These steps take one fixed order instead, which gc's iteration of the
// original's maps could take wherever the maps hold at most mapGroupSlots
// keys, so that there a layout that never changed stays the same. gc starts
// an iteration at a random slot. A map of at most mapGroupSlots keys is one
// group, whose slots hold the keys in insertion order, so an iteration that
// starts at the first slot takes them in that order, together with keys
// inserted meanwhile while they still fit in the group (not those inserted
// after the map outgrew it, nor any into a map that started out larger).
// The steps take that order: the seen objects in the order they were seen,
// the ancestors in the order the rank first reached them (sort.Slice keeps
// the order of equal keys among up to 12), and the coordinates in the order
// the objects first had them.
//
// mapOrders lets tests replace these orders with the ones a recorded run of
// the original took, to compare the two (TestSpacingPassesMatchLegacy).
// Production code passes nil.
type mapOrders interface {
	// growPass is called once for every shift of adjustCrossRankSpacing,
	// after the shift's first processQueue. When the grow pass may grow a
	// container, seen is the map of the shift's seen objects, which the
	// pass ranges over, and growPass calls visit for every object it
	// visits; seen receives the objects that are seen meanwhile, and
	// inserted returns all keys of seen in the order they were inserted.
	// seen, visit and inserted are nil when the pass cannot grow anything
	// (and so changes nothing in any order).
	growPass(seen map[*d2graph.Object]struct{}, visit func(*d2graph.Object), inserted func() []*d2graph.Object)
	// ancestors returns the keys of positions, the target positions of one
	// rank's starting or ending ancestors, in the order to sort them from.
	// It is called for every rank, starting ancestors first, with nil
	// when every order sorts to the same result.
	ancestors(positions map[*d2graph.Object]float64) []*d2graph.Object
	// levels returns the keys of aligned, the objects of each rank
	// coordinate, in the order to sort them from. It is called once for
	// every getRanks, with nil when no coordinate is NaN.
	levels(aligned map[float64][]*d2graph.Object) []float64
}

// mapGroupSlots is the number of keys a gc map holds in one group, before it
// grows into a table of groups.
const mapGroupSlots = 8
