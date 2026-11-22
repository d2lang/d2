package d2elklayout

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"unicode/utf8"

	elk "github.com/d2lang/elk-go"
)

// D2 used to send its ELK request to elk-go as JSON and read the response back
// from JSON. runELK gives elk-go the request through its typed Request
// instead: element by element, in the order in which elk-go imports a graph,
// with exactly the values elk.LayoutJSON decodes from json.Marshal of the
// ELKGraph (the same members, member order, numbers, strings and option
// values), so elk-go builds the same model and computes the same layout; the
// result is then copied into the ELKGraph as json.Unmarshal of LayoutJSON's
// response would copy it. Every effect of the two JSON round trips is
// reproduced:
//
//   - Members: fields without omitempty are always present (node, port and label
//     coordinates and sizes, label text, edge sources, targets and container,
//     the root's layoutOptions, the options elk.direction and
//     elk.spacing.nodeSelfLoop); omitempty fields are present only when
//     non-empty (slices of length 0, nil pointers, "" strings, 0 numbers and
//     false are left out).
//   - Option maps hold what elk-go's element decoders yield (they call
//     json.Unmarshal without UseNumber): strings, true, and float64 for
//     numbers, which is float64(n) for an int n; their member order is the
//     struct field order.
//   - Finite float64 values survive JSON exactly (shortest round-trip
//     formatting, -0 included), so they are passed unchanged. Strings are
//     passed as JSON gives them back (jsonText: json.Marshal writes each byte
//     that is not part of valid UTF-8 as U+FFFD).
//   - Failures: json.Marshal fails for a NaN or an infinity, and for nesting
//     beyond encoding/json's depth limit, before anything is laid out; such
//     requests return the error json.Marshal returns for them. A request
//     elk-go rejects (duplicate IDs, missing edge ends, ...) fails with the
//     error LayoutJSON gave, since the typed Request reports Layout(graph)'s
//     error for d2's requests.
//   - Results: a non-finite coordinate is encoded as null, which leaves the
//     decoded field unchanged: a node keeps its value and a point of a new
//     section stays 0. Sections and container are replaced only when the
//     response has them; a container whose ID JSON changed names no object
//     (see readBack). Ports and labels are not copied back: nothing reads
//     their layout.
//
// The request gives no edge sections: Layout builds every edge zeroed, so
// the JSON had none either (elk-go's JSON import would have read them). Nor
// does it give edge containers, which that import never reads.

// jsonSafeDepth is the node nesting up to which json.Marshal of a request
// cannot fail for depth: encoding/json fails beyond 10000 nested values, two
// or more per node level. runELK asks json.Marshal about deeper requests
// (rare: Layout's graph guard allows at most 1024 objects in g.Objects).
const jsonSafeDepth = 1000

// For tests: beforeELKHook sees every request runELK receives, and
// jsonOracle, when set, lays requests out instead of the typed path, the
// original's way through elk-go's JSON interface, as the reference the tests
// compare the typed path with (oracle_test.go).
var (
	beforeELKHook func(*ELKGraph)
	jsonOracle    func(context.Context, *ELKGraph, *elkGoRequest) error
)

// runELK lays out elkGraph in place with elk-go, through request (empty; see
// elkGoRequest.reserve).
func runELK(ctx context.Context, elkGraph *ELKGraph, request *elkGoRequest) error {
	if beforeELKHook != nil {
		request.sharedSet = false          // the hook may write the shared structs
		request.rootKey = rootOptionsKey{} // and the root's
		beforeELKHook(elkGraph)
	}
	if jsonOracle != nil {
		return jsonOracle(ctx, elkGraph, request)
	}
	typed := request.build(elkGraph)
	err := request.layOut(ctx, elkGraph, typed)
	typed.Release()
	return err
}

// layOut lays out typed, built from elkGraph, and copies the layout into
// elkGraph, failing where the original's JSON round trip failed.
func (r *elkGoRequest) layOut(ctx context.Context, elkGraph *ELKGraph, typed *elk.Request) error {
	if r.marshalMayFail {
		// The original's first step was json.Marshal(elkGraph).
		if _, err := json.Marshal(elkGraph); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// ELK does not accept a context. Check immediately around the call; the
	// topology preflight bounds inputs before entering this section.
	if err := typed.Layout(); err != nil {
		return fmt.Errorf("native ELK layout failed: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.readBack(typed)
	return nil
}

// elkGoRequest gives an ELKGraph to elk-go as a typed Request and copies
// the layout back. Its storage is kept across Layout calls (inside
// elkScratch): release empties it.
type elkGoRequest struct {
	// marshalMayFail is set when json.Marshal of the request may fail (a
	// non-finite number, nesting deeper than jsonSafeDepth).
	marshalMayFail bool
	// counts size the next elk.Request.
	nodes, ports, labels, edgeCount int

	// rootID, nodes (by elk.NodeRef; the root is nil) and edges (by
	// elk.EdgeRef) are the ELKGraph's elements in the request.
	rootID   string
	nodeList []*ELKNode
	edgeList []*ELKEdge

	// shared are option structs that elements of the ELKGraph share by
	// pointer, one per static option set, holding its source.
	shared [staticOptionSetCount]elkOpts
	// sharedIntact is true while every shared struct still holds its
	// source: then a pointer to one selects its static set without
	// comparing the struct.
	sharedIntact bool
	// sharedSet is true while the shared structs hold their sources with
	// nothing having been able to write them since they were set: only a
	// beforeELKHook and the tests' JSON oracle, decoding a response through
	// the ELKGraph's option pointers, can, and both clear it.
	sharedSet bool
	// recent are the option sets prepared last for this request.
	recent []elkPreparedSet
	pairs  []elkOptionPair
	keys   []string
	values []any
	// rootSets are root option sets prepared for earlier requests, by
	// their pairs; they outlive release. (Other elements' sets rarely
	// repeat across requests.)
	rootSets optionCache
	// rootKey identifies the content of the root's option struct of this
	// request when its generation is not 0 (Layout sets it).
	rootKey rootOptionsKey

	// sections and bends hold the result copied back into the ELKGraph.
	sections lists[ELKEdgeSection]
	bends    lists[ELKPoint]

	// containerRefs holds per ELK edge, once laid out, the node named as its
	// container (elk.NodeRef; -1 when the container was left as it was).
	containerRefs []int32
}

// elkPreparedSet is an option struct prepared for the request: elements
// whose option structs are equal share it (elk-go stores the same values for
// them).
type elkPreparedSet struct {
	source  elkOpts
	options *elk.Options
}

// reserve sizes the storage for a request of the given element counts and
// sets the shared option structs; the request must be empty.
func (r *elkGoRequest) reserve(nodes, ports, labels, edges int) {
	r.nodes, r.ports, r.labels, r.edgeCount = nodes+1, ports, labels, edges
	r.sections.reserve(0)
	r.bends.reserve(0)
	if !r.sharedSet {
		for i := range r.shared {
			r.shared[i] = staticOptionSets()[i].source
		}
		r.sharedSet = true
	}
}

// release empties the request for reuse.
func (r *elkGoRequest) release() {
	clear(r.nodeList)
	r.nodeList = r.nodeList[:0]
	clear(r.edgeList)
	r.edgeList = r.edgeList[:0]
	clear(r.recent)
	r.recent = r.recent[:0]
	clear(r.pairs)
	r.pairs = r.pairs[:0]
	clear(r.values)
	r.values = r.values[:0]
	r.keys = r.keys[:0]
	r.rootID = ""
	r.rootKey = rootOptionsKey{}
	r.sections.release()
	r.bends.release()
	r.containerRefs = r.containerRefs[:0]
}

// retainedBytes bounds the storage the request keeps once released (see
// elkScratch.retainedBytes).
func (r *elkGoRequest) retainedBytes() int {
	bytes := pointerBytes*(cap(r.nodeList)+cap(r.edgeList)) + preparedSetBytes*cap(r.recent) +
		optionPairBytes*cap(r.pairs) + stringBytes*cap(r.keys) + anyBytes*cap(r.values) +
		edgeSectionBytes*r.sections.capacity() + elkPointBytes*r.bends.capacity() + 4*cap(r.containerRefs)
	for i := range r.rootSets.sets {
		bytes += optionPairBytes * cap(r.rootSets.sets[i].pairs)
	}
	return bytes
}

// build adds g's elements to a new elk-go Request: every element as elk-go
// imports the corresponding element of g's JSON, in its import order.
func (r *elkGoRequest) build(g *ELKGraph) *elk.Request {
	r.marshalMayFail = false
	r.sharedIntact = true
	if !r.sharedSet {
		sources := staticOptionSets()
		for i := range r.shared {
			if r.shared[i] != sources[i].source {
				r.sharedIntact = false
			}
		}
	}
	typed := elk.NewRequest(r.nodes, r.ports, r.labels, r.edgeCount)
	r.rootID = jsonText(g.ID)
	r.nodeList = append(r.nodeList[:0], nil)
	root := typed.Root(r.rootID, r.preparedIn(typed, g.LayoutOptions, &r.rootSets))
	for _, child := range g.Children {
		if child != nil {
			r.node(typed, child, root, 1)
		}
	}
	for _, edge := range g.Edges {
		if edge != nil {
			r.edge(typed, edge, root)
		}
	}
	return typed
}

// jsonText is s as JSON gives it back: json.Marshal writes each byte that is
// not part of valid UTF-8 as U+FFFD.
func jsonText(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	encoded, _ := json.Marshal(s)
	var decoded string
	_ = json.Unmarshal(encoded, &decoded)
	return decoded
}

// jsonTexts is list with jsonText of every string (list itself when they are
// all valid).
func jsonTexts(list []string) []string {
	for i, s := range list {
		if !utf8.ValidString(s) {
			texts := append([]string(nil), list...)
			for j := i; j < len(texts); j++ {
				texts[j] = jsonText(texts[j])
			}
			return texts
		}
	}
	return list
}

func (r *elkGoRequest) checkNumbers(values ...float64) {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			r.marshalMayFail = true
		}
	}
}

// node adds n, a child of parent at the given depth below the root, with
// its ports, labels and children.
func (r *elkGoRequest) node(typed *elk.Request, n *ELKNode, parent elk.NodeRef, depth int) {
	if depth > jsonSafeDepth {
		r.marshalMayFail = true
	}
	r.checkNumbers(n.X, n.Y, n.Width, n.Height)
	ref := typed.Node(parent, jsonText(n.ID), n.X, n.Y, n.Width, n.Height, r.prepared(typed, n.LayoutOptions))
	r.nodeList = append(r.nodeList, n)
	for _, port := range n.Ports {
		if port != nil {
			r.checkNumbers(port.X, port.Y, port.Width, port.Height)
			typed.Port(ref, jsonText(port.ID), port.X, port.Y, port.Width, port.Height, r.prepared(typed, port.LayoutOptions))
		}
	}
	for _, label := range n.Labels {
		if label != nil {
			r.checkNumbers(label.X, label.Y, label.Width, label.Height)
			typed.NodeLabel(ref, jsonText(label.Text), label.X, label.Y, label.Width, label.Height, r.prepared(typed, label.LayoutOptions))
		}
	}
	for _, child := range n.Children {
		if child != nil {
			r.node(typed, child, ref, depth+1)
		}
	}
}

// edge adds e, an edge of the root, with its labels.
func (r *elkGoRequest) edge(typed *elk.Request, e *ELKEdge, root elk.NodeRef) {
	ref := typed.Edge(root, jsonText(e.ID), jsonTexts(e.Sources), jsonTexts(e.Targets), nil)
	r.edgeList = append(r.edgeList, e)
	for _, label := range e.Labels {
		if label != nil {
			r.checkNumbers(label.X, label.Y, label.Width, label.Height)
			typed.EdgeLabel(ref, jsonText(label.Text), label.X, label.Y, label.Width, label.Height, r.prepared(typed, label.LayoutOptions))
		}
	}
}

// prepared returns the options elk-go imports for opts (nil: none).
func (r *elkGoRequest) prepared(typed *elk.Request, opts *elkOpts) *elk.Options {
	return r.preparedIn(typed, opts, nil)
}

// preparedIn is prepared, also looking for option sets of earlier requests
// in cache (when not nil).
func (r *elkGoRequest) preparedIn(typed *elk.Request, opts *elkOpts, cache *optionCache) *elk.Options {
	if opts == nil {
		return nil
	}
	static := preparedStaticSets()
	if r.sharedIntact {
		for i := range r.shared {
			if opts == &r.shared[i] {
				return static[i]
			}
		}
	}
	// Every static source has these two fields empty (staticOptionSets
	// checks it), so a struct with either set equals none of them.
	if opts.Thoroughness == 0 && opts.FixedAlignment == "" {
		sources := staticOptionSets()
		for i := range sources {
			if sources[i].source == *opts {
				return static[i]
			}
		}
	}
	for i := len(r.recent) - 1; i >= 0; i-- {
		// Equal structs have equal minimum sizes, the field that differs
		// most between containers.
		if r.recent[i].source.NodeSizeMinimum == opts.NodeSizeMinimum && r.recent[i].source == *opts {
			return r.recent[i].options
		}
	}
	var key optionsKey
	if cache != nil {
		// The root: the set found by rootKey (see optionCache.byRoot) is the
		// one the comparison of the whole struct below would find. The
		// root's struct is not recorded in recent; only an equal struct
		// could take it from there, and that one would get an equal set.
		if r.rootKey.generation != 0 && cache.keyed != nil && cache.byRoot == r.rootKey {
			return cache.keyed
		}
		key = keyOf(opts)
		if cache.keyed != nil && cache.key == key {
			cache.byRoot = r.rootKey
			return cache.keyed
		}
	}
	var ok bool
	r.pairs, _, ok = appendOptionPairs(r.pairs[:0], opts)
	if !ok {
		r.marshalMayFail = true
	}
	// The root's set is kept across requests (see optionCache) when the
	// request can be laid out and the struct's texts are short.
	keep := cache != nil && ok && opts.textBytes() <= maxOwnedOptionText
	var options *elk.Options
	if keep {
		options = cache.find(r.pairs)
	}
	if options == nil {
		if keep {
			// Kept sets are prepared from copies of the strings, so that
			// they refer to nothing of the caller's.
			ownPairs(r.pairs)
		}
		r.keys, r.values = r.keys[:0], r.values[:0]
		for _, pair := range r.pairs {
			r.keys = append(r.keys, pair.key)
			r.values = append(r.values, pair.value)
		}
		if keep {
			// Cached sets outlive the request: prepared on their own.
			options = elk.PrepareOptions(r.keys, r.values)
			cache.add(r.pairs, options)
		} else {
			options = typed.PrepareOptions(r.keys, r.values)
		}
	}
	if keep {
		key.own()
		cache.key, cache.keyed, cache.byRoot = key, options, r.rootKey
	}
	if len(r.recent) == optionSetsKept {
		copy(r.recent, r.recent[1:])
		r.recent = r.recent[:optionSetsKept-1]
	}
	r.recent = append(r.recent, elkPreparedSet{source: *opts, options: options})
	return options
}

// optionCache holds option sets prepared for earlier requests with the
// pairs they were prepared from. Equal pairs (the same keys, and values of
// the same type and bits) prepare equal options, since preparing is a
// function of the keys and values (and of elk-go's option registry, which is
// fixed), so a set found here is the one a new preparation would give.
//
// The cache outlives requests in the scratch (a slot that garbage collection
// never empties), so it keeps nothing of the caller's: the pairs, their sets
// and key hold copies of their strings, and key's pointers are markers.
type optionCache struct {
	sets [8]cachedOptions
	next int
	// keyed is the set for the struct last looked up, by key: what
	// appendOptionPairs reads of it, so equal keys give equal pairs.
	key   optionsKey
	keyed *elk.Options
	// byRoot is the rootKey of the struct keyed was last found or made for
	// (when its generation is not 0). An equal rootKey of the same scratch
	// means an equal struct: the same graph options memo result (whose
	// generation changes whenever it changes, and whose pointees the memo
	// checks), the same self-loop spacing and direction, and the same values
	// of the scratch-owned pointees (rootLayoutOptions: the edge-node spacing
	// it sets is a constant; the edge-edge spacing is the value of the
	// options' *EdgeEdgeSpacing, which the memo does not check, or 50); so its
	// key equals key.
	byRoot rootOptionsKey
}

// rootOptionsKey identifies the content of a request's root option struct
// (see optionCache.byRoot).
type rootOptionsKey struct {
	generation uint64
	selfLoop   int
	direction  Direction
	edgeEdge   int
}

// optionsKey is everything appendOptionPairs reads of an option struct: its
// fields (of its pointers, whether they are set: see markPointers) and the
// values its pointers point to (numbers by their bits).
type optionsKey struct {
	opts     elkOpts
	pointees [7]uint64
}

func keyOf(opts *elkOpts) optionsKey {
	key := optionsKey{opts: *opts}
	c := &opts.ConfigurableOpts
	for i, value := range [...]*float64{c.NodeNode, c.EdgeNode, c.EdgeEdge, c.ForceRepulsion,
		c.StressDesiredEdgeLength, c.RadialRadius} {
		if value != nil {
			key.pointees[i] = math.Float64bits(*value)
		}
	}
	if c.EdgeEdgeSpacing != nil {
		key.pointees[6] = uint64(*c.EdgeEdgeSpacing)
	}
	key.opts.ConfigurableOpts.markPointers()
	return key
}

// own replaces the key's strings by strings of its own (see ownString).
func (key *optionsKey) own() {
	for _, field := range key.opts.stringFields() {
		ownString(field)
	}
}

// stringFields returns pointers to every string field of o, its
// ConfigurableOpts' included.
func (o *elkOpts) stringFields() [21]*string {
	c := &o.ConfigurableOpts
	return [...]*string{&o.FixedAlignment, (*string)(&o.Direction), &o.HierarchyHandling, &o.ConsiderModelOrder,
		&o.CycleBreakingStrategy, &o.SelfLoopDistribution, &o.LayerConstraint, &o.NodeSizeConstraints,
		&o.ContentAlignment, &o.NodeSizeMinimum, (*string)(&o.PortSide), &o.PortConstraints,
		&c.EdgeRouting, &c.WrappingStrategy, &c.ForceModel, &c.MrTreeSearchOrder,
		&c.MrTreeWeighting, &c.RadialWedgeCriteria, &c.RadialCompactor, &c.Algorithm, &c.Padding}
}

// textBytes is the length of o's strings together.
func (o *elkOpts) textBytes() int {
	total := 0
	for _, field := range o.stringFields() {
		total += len(*field)
	}
	return total
}

// ownPairs replaces the string values of pairs by strings of their own
// (see ownString), boxed as boxedString boxes them.
func ownPairs(pairs []elkOptionPair) {
	for i := range pairs {
		if text, isString := pairs[i].value.(string); isString {
			ownString(&text)
			pairs[i].value = boxedString(text)
		}
	}
}

type cachedOptions struct {
	pairs   []elkOptionPair
	options *elk.Options
}

func (cache *optionCache) find(pairs []elkOptionPair) *elk.Options {
	for i := range cache.sets {
		if set := &cache.sets[i]; set.options != nil && samePairs(set.pairs, pairs) {
			return set.options
		}
	}
	return nil
}

func (cache *optionCache) add(pairs []elkOptionPair, options *elk.Options) {
	set := &cache.sets[cache.next]
	cache.next = (cache.next + 1) % len(cache.sets)
	set.pairs = append(set.pairs[:0], pairs...)
	set.options = options
}

func samePairs(a, b []elkOptionPair) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].key != b[i].key || !sameOptionValue(a[i].value, b[i].value) {
			return false
		}
	}
	return true
}

// sameOptionValue compares the values appendOptionPairs makes (strings,
// true, float64), numbers by their bits (-0 is not 0 to the parser).
func sameOptionValue(a, b any) bool {
	switch x := a.(type) {
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case float64:
		y, ok := b.(float64)
		return ok && math.Float64bits(x) == math.Float64bits(y)
	}
	return false
}

// readBack copies the layout of typed into the ELKGraph's nodes and edges
// as json.Unmarshal of the JSON response would. The original then looked
// each container up by the ID the response gave: a container whose ID JSON
// changed (not valid UTF-8) is no object's, and its ref is 0 (no object).
func (r *elkGoRequest) readBack(typed *elk.Request) {
	for ref := 1; ref < len(r.nodeList); ref++ {
		n := r.nodeList[ref]
		x, y, width, height := typed.NodeLayout(elk.NodeRef(ref))
		setFromJSONNumber(&n.X, &x)
		setFromJSONNumber(&n.Y, &y)
		setFromJSONNumber(&n.Width, &width)
		setFromJSONNumber(&n.Height, &height)
	}
	for ref, e := range r.edgeList {
		edge := elk.EdgeRef(ref)
		if count := typed.Sections(edge); count > 0 {
			sections := r.sections.make(count)
			for i := range sections {
				start, end, bends := typed.Section(edge, i)
				sections[i].Start, sections[i].End = pointFromJSON(start), pointFromJSON(end)
				if bends > 0 {
					points := r.bends.make(bends)
					for j := range points {
						points[j] = pointFromJSON(typed.Bend(edge, i, j))
					}
					sections[i].BendPoints = points
				}
			}
			e.Sections = sections
		}
		// Container IDs are node IDs, which are all strings here.
		ref := int32(-1)
		if container, ok := typed.Container(edge); ok {
			ref = int32(container)
			if container == 0 {
				e.Container = r.rootID
			} else {
				id := r.nodeList[container].ID
				e.Container = jsonText(id)
				if e.Container != id {
					ref = 0 // no object has the ID JSON gives back
				}
			}
		}
		r.containerRefs = append(r.containerRefs, ref)
	}
}

// chunks hands out pointers to zeroed values from blocks that never move.
type chunks[T any] struct {
	block []T
	next  int
}

func (c *chunks[T]) new() *T {
	if len(c.block) == 0 {
		c.block = make([]T, max(c.next, 8))
		c.next = 2 * len(c.block)
	}
	value := &c.block[0]
	c.block = c.block[1:]
	return value
}

// elkOptionSet is a converted option struct: its options map and member
// order, as elk-go decodes them from JSON.
type elkOptionSet struct {
	source  elkOpts
	options elk.LayoutOptions
	keys    []string
}

type elkOptionPair struct {
	key   string
	value any
}

// optionSetsKept is how many recent option sets are compared for reuse.
const optionSetsKept = 4

// The static option sets, in the order of staticOptionSets.
const (
	leafOptionSet = iota
	edgeLabelOptionSet
	eastPortOptionSet
	westPortOptionSet
	southPortOptionSet
	northPortOptionSet
	sqlLeafOptionSet
	staticOptionSetCount
)

// staticOptionSets returns the option sets of leaves, edge labels and ports,
// built on first use (not at package initialisation: many d2 processes never
// lay out with ELK) and shared by every request (elk-go only reads them).
func staticOptionSets() []elkOptionSet {
	staticSources.once.Do(func() { staticSources.sets = makeStaticOptionSets() })
	return staticSources.sets
}

var staticSources struct {
	once sync.Once
	sets []elkOptionSet
}

func makeStaticOptionSets() []elkOptionSet {
	var sets []elkOptionSet
	for _, source := range [staticOptionSetCount]elkOpts{
		leafOptionSet:      {SelfLoopDistribution: "EQUALLY"},
		edgeLabelOptionSet: {InlineEdgeLabels: true},
		eastPortOptionSet:  {PortSide: East},
		westPortOptionSet:  {PortSide: West},
		southPortOptionSet: {PortSide: South},
		northPortOptionSet: {PortSide: North},
		sqlLeafOptionSet:   {SelfLoopDistribution: "EQUALLY", PortConstraints: "FIXED_POS"},
	} {
		pairs, _, ok := appendOptionPairs(nil, &source)
		if !ok || source.Thoroughness != 0 || source.FixedAlignment != "" {
			panic("static ELK option set rejected")
		}
		set := elkOptionSet{source: source, options: make(elk.LayoutOptions, len(pairs)), keys: make([]string, len(pairs))}
		for i, pair := range pairs {
			set.keys[i] = pair.key
			set.options[pair.key] = pair.value
		}
		sets = append(sets, set)
	}
	return sets
}

// preparedStaticSets returns the static option sets prepared for elk-go,
// prepared on first use and shared by every request (elk-go only reads
// them).
func preparedStaticSets() *[staticOptionSetCount]*elk.Options {
	staticPrepared.once.Do(func() {
		sets := staticOptionSets()
		for i := range sets {
			set := &sets[i]
			values := make([]any, len(set.keys))
			for j, key := range set.keys {
				values[j] = set.options[key]
			}
			staticPrepared.sets[i] = elk.PrepareOptions(set.keys, values)
		}
	})
	return &staticPrepared.sets
}

var staticPrepared struct {
	once sync.Once
	sets [staticOptionSetCount]*elk.Options
}

// knownOption returns value converted to an interface without allocating (a
// constant converts statically) when it is one of the option strings d2
// sends most, else nil.
func knownOption(value string) any {
	switch value {
	case "":
		return ""
	case "BALANCED":
		return "BALANCED"
	case "DOWN":
		return "DOWN"
	case "UP":
		return "UP"
	case "RIGHT":
		return "RIGHT"
	case "LEFT":
		return "LEFT"
	case "INCLUDE_CHILDREN":
		return "INCLUDE_CHILDREN"
	case "NODES_AND_EDGES":
		return "NODES_AND_EDGES"
	case "NONE":
		return "NONE"
	case "GREEDY_MODEL_ORDER":
		return "GREEDY_MODEL_ORDER"
	case "GREEDY":
		return "GREEDY"
	case "MINIMUM_SIZE":
		return "MINIMUM_SIZE"
	case "H_CENTER V_CENTER":
		return "H_CENTER V_CENTER"
	case "EQUALLY":
		return "EQUALLY"
	case "FIXED_POS":
		return "FIXED_POS"
	case "layered":
		return "layered"
	case "[top=50,left=50,bottom=50,right=50]":
		return "[top=50,left=50,bottom=50,right=50]"
	}
	return nil
}

// knownString returns the known option string with value's text (and false
// for other texts): a string that refers to nothing of the caller's.
func knownString(value string) (string, bool) {
	known, ok := knownOption(value).(string)
	return known, ok
}

// boxedString is any(value), without allocating for the known option
// strings.
func boxedString(value string) any {
	if known := knownOption(value); known != nil {
		return known
	}
	return value
}

// boxedFloat is any(value), without allocating for the numbers d2's options
// hold by default (a constant converts to an interface statically; == does
// not tell 0 from -0, so the zero case checks the sign).
func boxedFloat(value float64) any {
	switch value {
	case 0:
		if !math.Signbit(value) {
			return float64(0)
		}
	case 8:
		return float64(8)
	case 40:
		return float64(40)
	case 50:
		return float64(50)
	case 70:
		return float64(70)
	}
	return value
}

// appendOptionPairs appends the members json.Marshal writes for o, in its
// order, with the values elk-go's decoder gives them. mask has bit i set
// when member i is present; ok is false when json.Marshal rejects a value (a
// NaN or infinite number, which is left out).
func appendOptionPairs(pairs []elkOptionPair, o *elkOpts) (_ []elkOptionPair, mask uint64, ok bool) {
	b := optionPairs{pairs: pairs, ok: true}
	b.str("elk.layered.nodePlacement.bk.fixedAlignment", o.FixedAlignment, true)
	b.integer("elk.layered.thoroughness", o.Thoroughness, true)
	b.str("elk.direction", string(o.Direction), false)
	b.str("elk.hierarchyHandling", o.HierarchyHandling, true)
	b.boolean("elk.edgeLabels.inline", o.InlineEdgeLabels)
	b.str("elk.layered.considerModelOrder.strategy", o.ConsiderModelOrder, true)
	b.str("elk.layered.cycleBreaking.strategy", o.CycleBreakingStrategy, true)
	b.str("elk.layered.edgeRouting.selfLoopDistribution", o.SelfLoopDistribution, true)
	b.str("elk.layered.layering.layerConstraint", o.LayerConstraint, true)
	b.str("elk.nodeSize.constraints", o.NodeSizeConstraints, true)
	b.str("elk.contentAlignment", o.ContentAlignment, true)
	b.str("elk.nodeSize.minimum", o.NodeSizeMinimum, true)
	b.str("elk.port.side", string(o.PortSide), true)
	b.str("elk.portConstraints", o.PortConstraints, true)

	c := &o.ConfigurableOpts
	b.float("elk.spacing.nodeNode", c.NodeNode)
	b.float("elk.spacing.edgeNode", c.EdgeNode)
	b.float("elk.spacing.edgeEdge", c.EdgeEdge)
	b.str("elk.edgeRouting", c.EdgeRouting, true)
	b.str("elk.layered.wrapping.strategy", c.WrappingStrategy, true)
	b.boolean("elk.layered.crossingMinimization.forceNodeModelOrder", c.ForceNodeModelOrder)
	b.boolean("elk.layered.feedbackEdges", c.FeedbackEdges)
	b.boolean("elk.layered.mergeEdges", c.MergeEdges)

	b.str("elk.force.model", c.ForceModel, true)
	b.float("elk.force.repulsion", c.ForceRepulsion)
	b.float("elk.stress.desiredEdgeLength", c.StressDesiredEdgeLength)
	b.str("elk.mrtree.searchOrder", c.MrTreeSearchOrder, true)
	b.str("elk.mrtree.weighting", c.MrTreeWeighting, true)
	b.float("elk.radial.radius", c.RadialRadius)
	b.str("elk.radial.wedgeCriteria", c.RadialWedgeCriteria, true)
	b.str("elk.radial.compactor", c.RadialCompactor, true)

	b.str("elk.algorithm", c.Algorithm, true)
	b.integer("spacing.nodeNodeBetweenLayers", c.NodeSpacing, true)
	b.str("elk.padding", c.Padding, true)
	b.integer("spacing.edgeNodeBetweenLayers", c.EdgeNodeSpacing, true)
	if c.EdgeEdgeSpacing != nil {
		b.integer("spacing.edgeEdgeBetweenLayers", *c.EdgeEdgeSpacing, false)
	} else {
		b.member++
	}
	b.integer("elk.spacing.nodeSelfLoop", c.SelfLoopSpacing, false)
	return b.pairs, b.mask, b.ok
}

// optionPairs is appendOptionPairs' builder, called once per member of the
// option struct in member order. Its methods are not inlined: a call per
// member is a fraction of the code an inlined copy per member would be.
type optionPairs struct {
	pairs  []elkOptionPair
	mask   uint64
	member int
	ok     bool
}

func (b *optionPairs) add(key string, value any) {
	b.mask |= 1 << b.member
	b.pairs = append(b.pairs, elkOptionPair{key, value})
}

//go:noinline
func (b *optionPairs) str(key, value string, omitEmpty bool) {
	if !(omitEmpty && value == "") {
		b.add(key, boxedString(jsonText(value)))
	}
	b.member++
}

// integer adds an int member. JSON numbers in option maps decode to float64:
// elk-go's element decoders call json.Unmarshal without UseNumber. The
// decimal text of an int parses to float64(value), both rounding to nearest
// even.
//
//go:noinline
func (b *optionPairs) integer(key string, value int, omitEmpty bool) {
	if !(omitEmpty && value == 0) {
		b.add(key, boxedFloat(float64(value)))
	}
	b.member++
}

//go:noinline
func (b *optionPairs) boolean(key string, value bool) {
	if value {
		b.add(key, true)
	}
	b.member++
}

// float adds a *float64 member. A finite float64 is written as the shortest
// decimal that parses back to it, so it decodes unchanged (negative zero
// included); a NaN or an infinity makes json.Marshal fail.
//
//go:noinline
func (b *optionPairs) float(key string, value *float64) {
	if value != nil {
		if math.IsNaN(*value) || math.IsInf(*value, 0) {
			b.ok = false
		} else {
			b.add(key, boxedFloat(*value))
		}
	}
	b.member++
}

// setFromJSONNumber stores value as decoding its JSON encoding would: a
// missing member or null (NaN, infinities) leaves the field unchanged.
func setFromJSONNumber(field *float64, value *float64) {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return
	}
	*field = *value
}

func pointFromJSON(point elk.Point) ELKPoint {
	var out ELKPoint
	setFromJSONNumber(&out.X, &point.X)
	setFromJSONNumber(&out.Y, &point.Y)
	return out
}
