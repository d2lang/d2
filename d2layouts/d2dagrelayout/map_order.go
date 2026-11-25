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
// Go randomizes map iteration: gc starts at a random slot (and group), and
// in a loop that adds entries visits those added to a map of up to 8
// entries if it has not passed their slot yet, but never those added after
// the map outgrew its first group; TinyGo starts at a random bucket and
// slot. So for some inputs the original's result changes from run to run,
// and for many others it is always the same only because of how a small map
// is iterated. These steps therefore range over maps of the same types,
// made without a size hint and filled with the same keys in the same order
// as the original's maps, so that each runtime iterates them by the same
// rules as the original's. They do so only where the order can change the
// result; where every order gives the same result they use the order they
// like.
//
// "The same order" includes every write. TinyGo checks a map's load factor
// before it looks a key up, so an assignment to a key the map already holds
// can grow the map; the original assigned every ancestor's position right
// after inserting it, and so do these maps.
//
// Under gc this gives the original's distribution of results: gc draws each
// map's hash seed and the start of each iteration from per-thread ChaCha8
// state, so how a map is iterated depends only on its own history, and each
// of these steps repeats the history of the original's map.
//
// Under TinyGo it gives the same set of results, but not always with the
// same frequencies. One xorshift32 stream, seeded when the program starts,
// feeds every map creation, growth and iteration start of the program, and
// the low bits of nearby numbers of that stream are related, so how the
// keys of a map that grew are laid out and where its iteration starts
// depend on how many numbers other maps drew in between; TinyGo also hashes
// a pointer key by its bytes, so the layout depends on the objects'
// addresses. The steps repeat the draws around their own maps: the original
// made both ancestor maps of a rank before it ranged over either
// (sortAncestors does the same), and made, filled and wrote other maps
// between creating a shift's map of seen objects and ranging over it
// (crossRankShifter.originalMaps repeats that under TinyGo; under gc these
// draws change no distribution, so the other maps are not made there). They
// do not repeat the draws of the original's maps between different steps
// (other shifts, other ranks, getRanks' other maps, the dagre engine's own
// maps), so on an input whose result depends on map order, how often each
// result comes can differ from the original's. The original's own
// frequencies on such an input change with what the program ran before (in
// d2's wasm build, the diagrams the same instance laid out earlier), so
// under TinyGo there is no fixed distribution to match.
// TestSpacingPassDistribution checks single spacing passes against the
// original's under TinyGo, each run starting a random number of draws
// apart.
//
// mapOrders lets tests replace these ranges with the orders a recorded run
// of the original took, to compare the two deterministically. Production
// code passes nil.
type mapOrders interface {
	// growPass is called once for every shift of adjustCrossRankSpacing,
	// after the shift's first processQueue. When the grow pass may grow a
	// container, seen is the map of the shift's seen objects, which the
	// pass ranges over, and growPass calls visit for every object it
	// visits; seen receives the objects that are seen meanwhile, and
	// inserted returns all keys of seen in the order they were inserted.
	// With shadowMaps, writes are the writes into the original's maps of
	// the shift that were repeated before the pass (originalMaps). seen,
	// visit, inserted and writes are nil when the pass cannot grow anything
	// (and so changes nothing in any order).
	growPass(seen map[*d2graph.Object]struct{}, visit func(*d2graph.Object), inserted func() []*d2graph.Object, writes []int32)
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
