package d2dagrelayout

import (
	"strconv"
	"sync"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

// nodeID is the dagro node id of the object at position i of g.Objects,
// strconv.Itoa(i). strconv returns the ids below 100 without allocating;
// the ids of the other positions the layout guard admits are made once, the
// first time a graph has that many objects, and shared.
func nodeID(i int) string {
	if i < 100 {
		return strconv.Itoa(i)
	}
	if i < len(nodeIDs) {
		nodeIDsOnce.Do(func() {
			for i := range nodeIDs {
				nodeIDs[i] = strconv.Itoa(i)
			}
		})
		return nodeIDs[i]
	}
	return strconv.Itoa(i)
}

var (
	nodeIDsOnce sync.Once
	nodeIDs     [1024]string
)

// objectIndex numbers the objects of one layout call so that per-object
// state can live in slices instead of maps keyed by pointer.
//
// objs starts with the distinct objects of g.Objects in order. The dagro node
// of a repeated object is the id of its last position, as with objectMapper.
// closeOver then appends every other object the post-processing can reach
// through parents, children and edge endpoints (g.Root, and in a malformed
// graph objects missing from g.Objects, or nil), so that every pointer the
// pointer-based algorithms followed has an index.
type objectIndex struct {
	sc    *layoutScratch // storage of this call
	g     *d2graph.Graph
	objs  []*d2graph.Object
	index map[*d2graph.Object]int32
	// at[i] is the index of g.Objects[i].
	at []int32
	// lastAt[k] is the last position of objs[k] in g.Objects, -1 for objects
	// that are not in g.Objects.
	lastAt []int32
	// distinct is the number of distinct objects in g.Objects.
	distinct int

	// Set by closeOver.
	root int32
	// parent[k] is the index of objs[k].Parent, or -1 when objs[k] is nil.
	parent []int32
	// The children of objs[k], in ChildrenArray order, are
	// childList[childStart[k]:childStart[k+1]].
	childStart []int32
	childList  []int32
	// edgeSrc[i] and edgeDst[i] are the indices of g.Edges[i].Src and .Dst.
	edgeSrc []int32
	edgeDst []int32

	spacings []objectSpacing

	// topLefts holds the positions Layout gave the distinct objects of
	// g.Objects (TopLeft of objs[k] is &topLefts[k] unless an object shares
	// another's Box).
	topLefts []geo.Point
}

// objectIndex returns the index of g in the scratch's storage.
func (sc *layoutScratch) objectIndex(g *d2graph.Graph) *objectIndex {
	n := len(g.Objects)
	ix := &sc.ix
	*ix = objectIndex{
		sc:     sc,
		g:      g,
		objs:   ix.objs[:0],
		at:     grow(ix.at, n),
		lastAt: ix.lastAt[:0],
	}
	if n+2 > smallIndex {
		ix.index = make(map[*d2graph.Object]int32, n+2)
	}
	for i, obj := range g.Objects {
		k, ok := ix.lookup(obj)
		if !ok {
			k = ix.insert(obj)
		}
		ix.at[i] = k
		ix.lastAt[k] = int32(i)
	}
	ix.distinct = len(ix.objs)
	return ix
}

// smallIndex is the number of objects up to which a lookup scans objs
// instead of building and hashing into a map.
const smallIndex = 32

// lookup returns the index of obj, if it has one.
func (ix *objectIndex) lookup(obj *d2graph.Object) (int32, bool) {
	if ix.index != nil {
		k, ok := ix.index[obj]
		return k, ok
	}
	for k, o := range ix.objs {
		if o == obj {
			return int32(k), true
		}
	}
	return 0, false
}

// insert appends obj, which has no index yet, as an object that is not in
// g.Objects.
func (ix *objectIndex) insert(obj *d2graph.Object) int32 {
	k := int32(len(ix.objs))
	ix.objs = append(ix.objs, obj)
	ix.lastAt = append(ix.lastAt, -1)
	if ix.index != nil {
		ix.index[obj] = k
	} else if len(ix.objs) > smallIndex {
		ix.index = make(map[*d2graph.Object]int32, 2*len(ix.objs))
		for i, o := range ix.objs {
			ix.index[o] = int32(i)
		}
	}
	return k
}

// nodeIDOf returns the dagro node id of obj, or "" when obj is not in
// g.Objects (objectMapper.ToID).
func (ix *objectIndex) nodeIDOf(obj *d2graph.Object) string {
	k, ok := ix.lookup(obj)
	if !ok || ix.lastAt[k] < 0 {
		return ""
	}
	return nodeID(int(ix.lastAt[k]))
}

// add returns the index of obj, appending it if it has none.
func (ix *objectIndex) add(obj *d2graph.Object) int32 {
	if k, ok := ix.lookup(obj); ok {
		return k
	}
	return ix.insert(obj)
}

// closeOver indexes g.Root, the edge endpoints and, transitively, the parents
// and children of every indexed object.
func (ix *objectIndex) closeOver() {
	g := ix.g
	ix.root = ix.add(g.Root)
	e := len(g.Edges)
	// Usually only g.Root and its nil parent join the objects. The slices are
	// capped, so if more objects join, append moves them out of the block.
	m := len(ix.objs) + 1
	// Every element is written before it is read.
	block := grow(ix.sc.indexBlock, 2*e+3*m+1)
	ix.sc.indexBlock = block
	ix.edgeSrc = block[:e:e]
	ix.edgeDst = block[e : 2*e : 2*e]
	rest := block[2*e:]
	ix.parent = rest[:0:m]
	ix.childStart = rest[m : m : 2*m+1]
	ix.childList = rest[2*m+1 : 2*m+1 : 3*m+1]
	for i, e := range g.Edges {
		ix.edgeSrc[i] = ix.add(e.Src)
		ix.edgeDst[i] = ix.add(e.Dst)
	}
	for k := 0; k < len(ix.objs); k++ {
		obj := ix.objs[k]
		ix.childStart = append(ix.childStart, int32(len(ix.childList)))
		if obj == nil {
			ix.parent = append(ix.parent, -1)
			continue
		}
		ix.parent = append(ix.parent, ix.add(obj.Parent))
		for _, child := range obj.ChildrenArray {
			ix.childList = append(ix.childList, ix.add(child))
		}
	}
	ix.childStart = append(ix.childStart, int32(len(ix.childList)))
}

func (ix *objectIndex) children(k int32) []int32 {
	return ix.childList[ix.childStart[k]:ix.childStart[k+1]]
}

type objectSpacing struct {
	margin, padding geo.Spacing
	known           bool
}

// spacing returns objs[k].Spacing(). It depends only on the object's label,
// icon, shape and style and on its label and icon positions, none of which
// change after positionLabelsIcons, so it is computed once per object.
func (ix *objectIndex) spacing(k int32) (margin, padding geo.Spacing) {
	if ix.spacings == nil {
		ix.spacings = zeroed(ix.sc.spacingsBuf, len(ix.objs))
		ix.sc.spacingsBuf = ix.spacings
	}
	s := &ix.spacings[k]
	if !s.known {
		s.margin, s.padding = ix.objs[k].Spacing()
		s.known = true
	}
	return s.margin, s.padding
}
