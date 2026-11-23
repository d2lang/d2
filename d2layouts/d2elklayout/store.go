package d2elklayout

import (
	"math"
	"strconv"
	"sync"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

// elkObject is what Layout keeps per object its walk reaches (see
// walkObjects): one per object, shared by an object reached twice, its
// per-visit fields holding the latest visit's values.
type elkObject struct {
	// visited is set once a visit of the object has run (only objects with
	// a Parent are visited); Layout's map by object had entries for exactly
	// these.
	visited bool
	node    *ELKNode
	margin  geo.Spacing
	absID   string

	// The edges' statistics: the object's degree and, for a parent, the
	// largest label of its children's labeled self loops.
	degree   objectDegree
	selfLoop selfLoopLabelSize
	// incident holds the edges that start or end at the object, in edge
	// order and once each per listing (counted first in count).
	incident []*d2graph.Edge
	count    int
	// subtree is the object's preorder position and the position after its
	// subtree, numbered from the root (0) on.
	subtree subtreeRange
	// bendSlot is the object's entry in deleteBendsWith's index in the call
	// numbered bendStamp.
	bendStamp uint64
	bendSlot  int
}

// elkVisit is one visit of Layout's walk: an object with a Parent, reached
// from parent (its walk parent).
type elkVisit struct {
	obj, parent *d2graph.Object
	state       *elkObject
}

// elkEdgeState is what Layout keeps per edge: its ELK edge and its index in
// g.Edges (the last listing's, for an edge listed twice, as a map by edge
// held), and the descendant shifter's stamp.
type elkEdgeState struct {
	elk   *ELKEdge
	index int
	seen  uint64
	// first is the index in g.Edges of the edge's first listing.
	first int
}

// walkObjects records Layout's walk of g: depth first from g.Root through
// ChildrenArray, every object reached that has a Parent visited with its walk
// parent (the root too when it has one). Every object reached gets one state
// in objects (the root only when visited), from states when new; each reach
// numbers it in preorder from the root (0) and records its subtree range,
// as the descendant shifter numbers objects. It appends the visits to visits
// and returns them, and the root's range.
func walkObjects(g *d2graph.Graph, objects map[*d2graph.Object]*elkObject, states *slab[elkObject], visits []elkVisit) ([]elkVisit, subtreeRange) {
	next := 0
	var reach func(obj, parent *d2graph.Object) subtreeRange
	reach = func(obj, parent *d2graph.Object) subtreeRange {
		start := next
		next++
		var state *elkObject
		if obj != g.Root || obj.Parent != nil {
			if state = objects[obj]; state == nil {
				state = states.new()
				objects[obj] = state
			}
		}
		if obj.Parent != nil {
			visits = append(visits, elkVisit{obj: obj, parent: parent, state: state})
		}
		for _, child := range obj.ChildrenArray {
			reach(child, obj)
		}
		r := subtreeRange{start: start, end: next}
		if state != nil {
			state.subtree = r
		}
		return r
	}
	rootRange := reach(g.Root, nil)
	return visits, rootRange
}

// visitedState is the state of obj if a visit of it has run, else nil (what
// Layout's map by object gave).
func visitedState(objects map[*d2graph.Object]*elkObject, obj *d2graph.Object) *elkObject {
	if state := objects[obj]; state != nil && state.visited {
		return state
	}
	return nil
}

// selfLoop is where the self-loop label maximum of obj is kept: the root's
// in the scratch, a reached object's in its state, else nowhere (nothing
// reads it).
func (scratch *elkScratch) selfLoop(g *d2graph.Graph, obj *d2graph.Object) *selfLoopLabelSize {
	if obj == g.Root {
		return &scratch.rootSelfLoop
	}
	if state := scratch.objectStates[obj]; state != nil {
		return &state.selfLoop
	}
	return nil
}

func (size *selfLoopLabelSize) of(isWidth bool) int {
	if isWidth {
		return size.width
	}
	return size.height
}

// collectEdgeStats is collectELKGraphStats into the states of the reached
// objects: the degrees and self-loop label maxima the walk reads (it reads
// them for visited objects and the root only).
func (scratch *elkScratch) collectEdgeStats(g *d2graph.Graph) {
	objects := scratch.objectStates
	for _, edge := range g.Edges {
		if state := objects[edge.Src]; state != nil {
			state.degree.outgoing++
		}
		if state := objects[edge.Dst]; state != nil {
			state.degree.incoming++
		}
		if edge.Src != edge.Dst || edge.Label.Value == "" || edge.Src.Parent == nil {
			continue
		}
		if size := scratch.selfLoop(g, edge.Src.Parent); size != nil {
			size.width = go2.Max(size.width, edge.LabelDimensions.Width)
			size.height = go2.Max(size.height, edge.LabelDimensions.Height)
		}
	}
}

// collectIncidentEdges gives every reached object the edges of g that start
// or end at it, in order and once per listing, in lists cut from one block
// (counted first). Lists of objects not reached are never read.
func collectIncidentEdges(g *d2graph.Graph, objects map[*d2graph.Object]*elkObject, lists *lists[*d2graph.Edge]) {
	for _, e := range g.Edges {
		if state := objects[e.Src]; state != nil {
			state.count++
		}
		if e.Dst != e.Src {
			if state := objects[e.Dst]; state != nil {
				state.count++
			}
		}
	}
	incident := func(obj *d2graph.Object, e *d2graph.Edge) {
		state := objects[obj]
		if state == nil {
			return
		}
		if state.incident == nil {
			state.incident = lists.makeCap(0, state.count)
		}
		state.incident = append(state.incident, e)
	}
	for _, e := range g.Edges {
		incident(e.Src, e)
		if e.Dst != e.Src {
			incident(e.Dst, e)
		}
	}
}

// elkGraphStore hands out the values one Layout call leaves in the d2 graph
// (points, label and icon positions) from blocks allocated per call instead
// of one allocation each. Every value is still a distinct variable.
type elkGraphStore struct {
	points  chunks[geo.Point]
	strings chunks[string]
}

// reserve sizes the first blocks for g.
func (store *elkGraphStore) reserve(g *d2graph.Graph) {
	edgeLabels := 0
	for _, edge := range g.Edges {
		if edge.Label.Value != "" {
			edgeLabels++
		}
	}
	// Object corners plus, per edge, its two ends and a few bends.
	store.points.next = len(g.Objects) + 4*len(g.Edges)
	// Label and icon positions of objects, label positions of edges.
	store.strings.next = 2*len(g.Objects) + edgeLabels
}

// stringPointer returns go2.Pointer(value).
func (store *elkGraphStore) stringPointer(value string) *string {
	pointer := store.strings.new()
	*pointer = value
	return pointer
}

// point returns geo.NewPoint(x, y).
func (store *elkGraphStore) point(x, y float64) *geo.Point {
	p := store.points.new()
	p.X, p.Y = x, y
	return p
}

// elkScratch holds the ELK request of one Layout call: the ELKGraph, its
// conversion to elk-go's values and the bookkeeping around them. Nothing of
// it is referenced once Layout has read the result back, so later calls
// reuse its storage: release zeroes every value that was handed out, which
// makes reused storage indistinguishable from new.
type elkScratch struct {
	objects    slab[elkObject]
	nodes      slab[ELKNode]
	opts       slab[elkOpts]
	labels     slab[ELKLabel]
	edges      slab[ELKEdge]
	ports      slab[ELKPort]
	nodeLists  lists[*ELKNode]
	labelLists lists[*ELKLabel]
	portLists  lists[*ELKPort]
	edgeLists  lists[*ELKEdge]
	idLists    lists[string]
	edgeRefs   lists[*d2graph.Edge]

	edgeStateValues slab[elkEdgeState]
	visits          []elkVisit
	edgeAt          []*elkEdgeState
	nodeObjects     []*d2graph.Object
	rootSelfLoop    selfLoopLabelSize

	request elkGoRequest

	// bendGrids is the storage of deleteBendsWith's collision grids.
	bendGrids bendGrids

	// The ELK graph, the root's option struct and the values option
	// pointers point to; zeroed on release.
	graph        ELKGraph
	rootOpts     elkOpts
	optionValues struct {
		rootEdgeNode, containerEdgeNode float64
		rootEdgeEdge, containerEdgeEdge int
	}
	// graphOptions and paddingTexts outlive release.
	graphOptions graphOptionsMemo
	paddingTexts paddingTexts

	// Layout's maps, emptied on release.
	objectStates map[*d2graph.Object]*elkObject
	edgeStates   map[*d2graph.Edge]*elkEdgeState

	// The sizes bound the entries each map's storage was made or grown for
	// (maps never shrink).
	objectStatesSize, edgeStatesSize int
}

// releasedScratch keeps released scratch storage for reuse: one in a slot
// that garbage collection does not empty, so that sequential layouts always
// find it (sync.Pool loses values at collections and when a goroutine
// changes processors), and more in a sync.Pool for concurrent layouts.
var releasedScratch struct {
	sync.Mutex
	slot *elkScratch
	pool sync.Pool
}

// acquireScratch returns empty scratch storage sized for g.
func acquireScratch(g *d2graph.Graph) *elkScratch {
	releasedScratch.Lock()
	scratch := releasedScratch.slot
	releasedScratch.slot = nil
	releasedScratch.Unlock()
	if scratch == nil {
		scratch, _ = releasedScratch.pool.Get().(*elkScratch)
	}
	if scratch == nil {
		scratch = new(elkScratch)
	}
	// A new scratch has no maps, nor has one whose maps release dropped.
	if scratch.objectStates == nil {
		scratch.objectStates = make(map[*d2graph.Object]*elkObject, len(g.Objects))
		scratch.objectStatesSize = len(g.Objects)
	}
	if scratch.edgeStates == nil {
		scratch.edgeStates = make(map[*d2graph.Edge]*elkEdgeState, len(g.Edges))
		scratch.edgeStatesSize = len(g.Edges)
	}
	labels, ports := 0, 0
	for _, obj := range g.Objects {
		if obj.HasLabel() {
			labels++
		}
		if obj.SQLTable != nil {
			ports += 2 * len(obj.SQLTable.Columns)
		}
	}
	for _, edge := range g.Edges {
		if edge.Label.Value != "" {
			labels++
		}
		if edge.SrcTableColumnIndex == nil && edge.Src.SQLTable != nil {
			ports++
		}
		if edge.DstTableColumnIndex == nil && edge.Dst.SQLTable != nil {
			ports++
		}
	}
	objects := len(g.Objects)
	scratch.objects.reserve(objects)
	scratch.edgeStateValues.reserve(len(g.Edges))
	scratch.nodes.reserve(objects)
	scratch.opts.reserve(objects)
	scratch.labels.reserve(labels)
	scratch.edges.reserve(len(g.Edges))
	scratch.ports.reserve(ports)
	scratch.nodeLists.reserve(objects)
	scratch.labelLists.reserve(labels)
	scratch.portLists.reserve(ports)
	scratch.edgeLists.reserve(len(g.Edges))
	scratch.idLists.reserve(2 * len(g.Edges))
	scratch.edgeRefs.reserve(2 * len(g.Edges))
	scratch.request.reserve(objects, ports, labels, len(g.Edges))
	return scratch
}

// Bounds of what a released scratch keeps for reuse.
const (
	// maxScratchBytes bounds the storage a released scratch keeps (see
	// retainedBytes): a scratch holding more is dropped, so that one large
	// layout does not keep its storage for the life of the process (the slot
	// is never emptied, and TinyGo's sync.Pool never drops anything).
	maxScratchBytes = 2 << 20
	// maxScratchMapEntries bounds the entries a kept map was made or grown
	// for: emptying a map costs time in proportion to its storage.
	maxScratchMapEntries = 1 << 12
)

// releaseScratch zeroes what scratch handed out and offers it for reuse.
// Nothing obtained from it may be used afterwards. A released scratch refers
// to nothing of the caller's (the caches that outlive calls keep copies) and
// keeps at most maxScratchBytes; a larger one is emptied and dropped. Layout
// calls it only when it returns: a call that panics drops its scratch.
func releaseScratch(scratch *elkScratch) {
	scratch.objects.release()
	scratch.nodes.release()
	scratch.opts.release()
	scratch.labels.release()
	scratch.edges.release()
	scratch.ports.release()
	scratch.nodeLists.release()
	scratch.labelLists.release()
	scratch.portLists.release()
	scratch.edgeLists.release()
	scratch.idLists.release()
	scratch.edgeRefs.release()
	scratch.request.release()
	scratch.bendGrids.release()
	scratch.graph = ELKGraph{}
	scratch.rootOpts = elkOpts{}
	scratch.edgeStateValues.release()
	clear(scratch.visits)
	scratch.visits = scratch.visits[:0]
	clear(scratch.edgeAt)
	scratch.edgeAt = scratch.edgeAt[:0]
	clear(scratch.nodeObjects)
	scratch.nodeObjects = scratch.nodeObjects[:0]
	scratch.rootSelfLoop = selfLoopLabelSize{}
	scratch.graphOptions.release()
	scratch.objectStates, scratch.objectStatesSize = emptiedMap(scratch.objectStates, scratch.objectStatesSize)
	scratch.edgeStates, scratch.edgeStatesSize = emptiedMap(scratch.edgeStates, scratch.edgeStatesSize)
	if scratch.retainedBytes() > maxScratchBytes {
		// Dropped, and emptied in case something (TinyGo's sync.Pool keeps
		// the items it handed out in its list) still refers to it.
		*scratch = elkScratch{}
		return
	}
	releasedScratch.Lock()
	if releasedScratch.slot == nil {
		releasedScratch.slot = scratch
		scratch = nil
	}
	releasedScratch.Unlock()
	if scratch != nil {
		releasedScratch.pool.Put(scratch)
	}
}

// emptiedMap empties m (whose storage was made or grown for size entries)
// for reuse, or drops it (nil) when its storage holds more than
// maxScratchMapEntries; it returns the map and its new size.
func emptiedMap[K comparable, V any](m map[K]V, size int) (map[K]V, int) {
	if size = max(size, len(m)); size > maxScratchMapEntries {
		return nil, 0
	}
	clear(m)
	return m, size
}

// Upper bounds of the bytes one element of the scratch's storage takes
// (64-bit sizes; TestScratchByteBounds checks them).
const (
	elkObjectBytes     = 160
	elkNodeBytes       = 128
	elkOptsBytes       = 456
	elkLabelBytes      = 56
	elkEdgeBytes       = 128
	elkPortBytes       = 56
	elkEdgeStateBytes  = 32
	elkVisitBytes      = 24
	edgeSectionBytes   = 56
	elkPointBytes      = 16
	preparedSetBytes   = 464
	optionPairBytes    = 32
	linearObjectBytes  = 40
	linearSegmentBytes = 48
	segmentItemBytes   = 24
	boundsBytes        = 32
	pointerBytes       = 8
	stringBytes        = 16
	sliceBytes         = 24
	anyBytes           = 16
	// A map entry (key and value) with its share of the map's tables.
	pointerEntryBytes = 48
)

// retainedBytes bounds the storage scratch keeps once released: everything
// whose size follows the requests' sizes. The caches that outlive calls
// (graphOptions, paddingTexts, the request's rootSets) are bounded by
// construction.
func (scratch *elkScratch) retainedBytes() int {
	bytes := elkObjectBytes*scratch.objects.capacity() + elkNodeBytes*scratch.nodes.capacity() +
		elkOptsBytes*scratch.opts.capacity() + elkLabelBytes*scratch.labels.capacity() +
		elkEdgeBytes*scratch.edges.capacity() + elkPortBytes*scratch.ports.capacity()
	bytes += pointerBytes * (scratch.nodeLists.capacity() + scratch.labelLists.capacity() + scratch.portLists.capacity() +
		scratch.edgeLists.capacity() + scratch.edgeRefs.capacity())
	bytes += stringBytes * scratch.idLists.capacity()
	bytes += elkEdgeStateBytes*scratch.edgeStateValues.capacity() + elkVisitBytes*cap(scratch.visits) +
		pointerBytes*(cap(scratch.edgeAt)+cap(scratch.nodeObjects))
	bytes += pointerEntryBytes * (scratch.objectStatesSize + scratch.edgeStatesSize)
	return bytes + scratch.request.retainedBytes() + scratch.bendGrids.retainedBytes()
}

// labelList returns []*ELKLabel{label}.
func (scratch *elkScratch) labelList(label *ELKLabel) []*ELKLabel {
	list := scratch.labelLists.make(1)
	list[0] = label
	return list
}

// idList returns []string{id}.
func (scratch *elkScratch) idList(id string) []string {
	list := scratch.idLists.make(1)
	list[0] = id
	return list
}

// slab hands out pointers to zero values from one block that is kept across
// calls. Past the block it allocates them one by one, and the next call's
// block is made large enough for this call's demand.
type slab[T any] struct {
	block []T // block[used:] is all zero
	used  int
	extra int // values allocated past the block in this call
	need  int // values the last released call took in all
}

// reserve makes room for n values (at least as many as the last call took);
// the slab must be empty.
func (s *slab[T]) reserve(n int) {
	n = max(n, s.need)
	if cap(s.block) < n {
		s.block = make([]T, n)
	}
	s.block = s.block[:cap(s.block)]
}

func (s *slab[T]) new() *T {
	if s.used < len(s.block) {
		value := &s.block[s.used]
		s.used++
		return value
	}
	s.extra++
	return new(T)
}

// release zeroes the values handed out and empties the slab.
func (s *slab[T]) release() {
	clear(s.block[:s.used])
	s.need = s.used + s.extra
	s.used, s.extra = 0, 0
}

func (s *slab[T]) capacity() int { return cap(s.block) }

// lists hands out zeroed slices, capped at their capacity, from one block
// that is kept across calls (sized like a slab's).
type lists[T any] struct {
	block []T // block[used:] is all zero
	used  int
	extra int
	need  int
}

// reserve makes room for n elements (at least as many as the last call
// took); the block must be empty.
func (l *lists[T]) reserve(n int) {
	n = max(n, l.need)
	if cap(l.block) < n {
		l.block = make([]T, n)
	}
	l.block = l.block[:cap(l.block)]
}

// make returns a slice of length n (an empty non-nil slice for 0).
func (l *lists[T]) make(n int) []T {
	return l.makeCap(n, n)
}

// makeCap returns a slice of length n and capacity c >= n.
func (l *lists[T]) makeCap(n, c int) []T {
	if c == 0 {
		return []T{}
	}
	if l.used+c > len(l.block) {
		l.extra += c
		return make([]T, n, c)
	}
	out := l.block[l.used : l.used+n : l.used+c]
	l.used += c
	return out
}

// release zeroes the elements handed out and empties the block.
func (l *lists[T]) release() {
	clear(l.block[:l.used])
	l.need = l.used + l.extra
	l.used, l.extra = 0, 0
}

func (l *lists[T]) capacity() int { return cap(l.block) }

// readPaddingCached is readPadding, with the parse of D2's default padding
// done once (readPadding is a pure function of its text).
func readPaddingCached(value string) (shapePadding, error) {
	if value == defaultPaddingText {
		return defaultPadding, nil
	}
	return readPadding(value)
}

const defaultPaddingText = "[top=50,left=50,bottom=50,right=50]"

// defaultPadding is readPadding(defaultPaddingText), which never fails
// (TestDefaultPaddingParse checks it); written out so that no parse runs at
// package initialisation.
var defaultPadding = shapePadding{top: 50, left: 50, bottom: 50, right: 50}

// paddingTexts remembers the texts of the last paddings formatted (a
// padding's String is a function of its four values, compared by bits).
type paddingTexts struct {
	entries [8]paddingText
	next    int
}

type paddingText struct {
	bits [4]uint64
	text string
}

func (texts *paddingTexts) text(padding shapePadding) string {
	bits := [4]uint64{math.Float64bits(padding.top), math.Float64bits(padding.left),
		math.Float64bits(padding.bottom), math.Float64bits(padding.right)}
	for i := range texts.entries {
		if entry := &texts.entries[i]; entry.text != "" && entry.bits == bits {
			return entry.text
		}
	}
	text := padding.String()
	texts.entries[texts.next] = paddingText{bits: bits, text: text}
	texts.next = (texts.next + 1) % len(texts.entries)
	return text
}

// nodeSizeMinimum is fmt.Sprintf("(%d, %d)", first, second).
func nodeSizeMinimum(first, second int) string {
	return "(" + strconv.Itoa(first) + ", " + strconv.Itoa(second) + ")"
}

// portID is fmt.Sprintf("%s.%s%s", absID, column, suffix).
func portID(absID, column, suffix string) string {
	return absID + "." + column + suffix
}
