package d2elklayout

import (
	"math"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

const (
	bendIndexCellSize = 128.0
	// bendIndexMaxCells bounds the cells of a grid; a larger extent gets
	// larger cells.
	bendIndexMaxCells = 4096
)

type spatialBounds struct {
	minX float64
	minY float64
	maxX float64
	maxY float64
}

func boundsForSegment(segment geo.Segment) spatialBounds {
	return spatialBounds{
		minX: math.Min(segment.Start.X, segment.End.X),
		minY: math.Min(segment.Start.Y, segment.End.Y),
		maxX: math.Max(segment.Start.X, segment.End.X),
		maxY: math.Max(segment.Start.Y, segment.End.Y),
	}
}

func (bounds spatialBounds) expanded(amount float64) spatialBounds {
	bounds.minX -= amount
	bounds.minY -= amount
	bounds.maxX += amount
	bounds.maxY += amount
	return bounds
}

func (bounds spatialBounds) overlaps(other spatialBounds) bool {
	return bounds.minX <= other.maxX && other.minX <= bounds.maxX &&
		bounds.minY <= other.maxY && other.minY <= bounds.maxY
}

func finiteBounds(bounds spatialBounds) bool {
	return !math.IsNaN(bounds.minX) && !math.IsNaN(bounds.minY) &&
		!math.IsNaN(bounds.maxX) && !math.IsNaN(bounds.maxY) &&
		!math.IsInf(bounds.minX, 0) && !math.IsInf(bounds.minY, 0) &&
		!math.IsInf(bounds.maxX, 0) && !math.IsInf(bounds.maxY, 0)
}

// regular reports whether bounds are finite and not inverted, so that the
// cells between their corners hold every point they overlap.
func regular(bounds spatialBounds) bool {
	return finiteBounds(bounds) && bounds.minX <= bounds.maxX && bounds.minY <= bounds.maxY
}

// gridIndex is an exact broad phase: collect returns every live item whose
// bounds overlap the query bounds, once each, and no other item. Items with
// regular bounds are kept in the cells of a uniform grid over the extent the
// index was reset to; a coordinate outside the extent counts in the nearest
// border cell. That is conservative: cell coordinates are monotone in the
// coordinates, so an item and a query that share a point both cover that
// point's cell. Other items (non-finite or inverted bounds) are checked by
// every query, and a query without regular bounds checks every item. Its
// storage is reused by the next reset.
type gridIndex struct {
	originX, originY float64
	cellSize         float64
	cols, rows       int
	cells            [][]int32
	// usedCells are the cells holding items, emptied by reset.
	usedCells []int
	irregular []int32
	bounds    []spatialBounds
	alive     []bool
	seen      []uint32
	stamp     uint32
}

// reset empties the index and lays its grid over extent.
func (grid *gridIndex) reset(extent spatialBounds) {
	for _, cell := range grid.usedCells {
		grid.cells[cell] = grid.cells[cell][:0]
	}
	grid.usedCells = grid.usedCells[:0]
	grid.irregular = grid.irregular[:0]
	grid.bounds = grid.bounds[:0]
	grid.alive = grid.alive[:0]
	grid.seen = grid.seen[:0]
	grid.stamp = 0
	grid.originX, grid.originY = 0, 0
	grid.cellSize = bendIndexCellSize
	grid.cols, grid.rows = 1, 1
	width, height := extent.maxX-extent.minX, extent.maxY-extent.minY
	if regular(extent) && !math.IsInf(width, 0) && !math.IsInf(height, 0) {
		size := float64(bendIndexCellSize)
		for (math.Floor(width/size)+1)*(math.Floor(height/size)+1) > bendIndexMaxCells {
			size *= 2
		}
		grid.originX, grid.originY = extent.minX, extent.minY
		grid.cellSize = size
		grid.cols, grid.rows = int(math.Floor(width/size))+1, int(math.Floor(height/size))+1
	}
	if n := grid.cols * grid.rows; cap(grid.cells) < n {
		grid.cells = append(grid.cells[:cap(grid.cells)], make([][]int32, n-cap(grid.cells))...)
	}
	grid.cells = grid.cells[:grid.cols*grid.rows]
}

func cellCoordinate(value, origin, size float64, count int) int {
	cell := math.Floor((value - origin) / size)
	if !(cell > 0) {
		return 0
	}
	if cell >= float64(count-1) {
		return count - 1
	}
	return int(cell)
}

// add indexes an item with bounds and returns its id (ids count up from 0).
func (grid *gridIndex) add(bounds spatialBounds) int32 {
	id := int32(len(grid.bounds))
	grid.bounds = append(grid.bounds, bounds)
	grid.alive = append(grid.alive, true)
	grid.seen = append(grid.seen, 0)
	if !regular(bounds) {
		grid.irregular = append(grid.irregular, id)
		return id
	}
	c0, c1 := cellCoordinate(bounds.minX, grid.originX, grid.cellSize, grid.cols), cellCoordinate(bounds.maxX, grid.originX, grid.cellSize, grid.cols)
	r0, r1 := cellCoordinate(bounds.minY, grid.originY, grid.cellSize, grid.rows), cellCoordinate(bounds.maxY, grid.originY, grid.cellSize, grid.rows)
	for row := r0; row <= r1; row++ {
		for col := c0; col <= c1; col++ {
			cell := row*grid.cols + col
			if len(grid.cells[cell]) == 0 {
				grid.usedCells = append(grid.usedCells, cell)
			}
			grid.cells[cell] = append(grid.cells[cell], id)
		}
	}
	return id
}

// remove drops item id from every later query.
func (grid *gridIndex) remove(id int32) {
	grid.alive[id] = false
}

// collect appends to found the live items whose bounds overlap bounds.
func (grid *gridIndex) collect(bounds spatialBounds, found []int32) []int32 {
	if !regular(bounds) {
		for id := range grid.bounds {
			if grid.alive[id] && grid.bounds[id].overlaps(bounds) {
				found = append(found, int32(id))
			}
		}
		return found
	}
	grid.stamp++
	if grid.stamp == 0 {
		clear(grid.seen)
		grid.stamp = 1
	}
	stamp := grid.stamp
	c0, c1 := cellCoordinate(bounds.minX, grid.originX, grid.cellSize, grid.cols), cellCoordinate(bounds.maxX, grid.originX, grid.cellSize, grid.cols)
	r0, r1 := cellCoordinate(bounds.minY, grid.originY, grid.cellSize, grid.rows), cellCoordinate(bounds.maxY, grid.originY, grid.cellSize, grid.rows)
	for row := r0; row <= r1; row++ {
		for col := c0; col <= c1; col++ {
			for _, id := range grid.cells[row*grid.cols+col] {
				if grid.seen[id] == stamp {
					continue
				}
				grid.seen[id] = stamp
				if grid.alive[id] && grid.bounds[id].overlaps(bounds) {
					found = append(found, id)
				}
			}
		}
	}
	for _, id := range grid.irregular {
		if grid.alive[id] && grid.bounds[id].overlaps(bounds) {
			found = append(found, id)
		}
	}
	return found
}

type bendCollisionIndex struct {
	objectBuffer float64
	edgeBuffer   float64

	// objects holds one entry per distinct object (its last bounds), and
	// segments the current route segments of each distinct edge, by the
	// position of its first occurrence in edges.
	objects  []linearObject
	edges    []*d2graph.Edge
	position map[*d2graph.Edge]int
	// edgeAt, when Layout gives it, holds per edge its state, whose first
	// listing's position replaces position.
	edgeAt   []*elkEdgeState
	segments [][]linearSegment

	// grids, when not nil, index the same entries for large graphs; small
	// ones are scanned (see linearIndexLimit).
	grids *bendGrids
}

// linearIndexLimit is the number of objects plus route segments up to which
// the index scans its lists: building the grids costs more than scanning
// them. Both forms visit exactly the entries whose recorded bounds overlap a
// query, and visits are only counted, so their results are equal.
const linearIndexLimit = 256

type linearObject struct {
	obj    *d2graph.Object
	bounds spatialBounds
}

type linearSegment struct {
	segment geo.Segment
	bounds  spatialBounds
}

// bendGrids are the grids of a large index: object item i is objects[i];
// segment items are listed in segmentItems, and segmentIDs holds the items of
// each edge position's current segments. Kept across Layout calls.
type bendGrids struct {
	objectGrid   gridIndex
	segmentGrid  gridIndex
	segmentItems []segmentItem
	segmentIDs   [][]int32
	found        []int32

	// The index's lists (any graph size), kept across calls.
	objects  []linearObject
	segments [][]linearSegment
	block    []linearSegment
	// states and edgeAt are Layout's object and edge states for the call
	// (nil outside Layout); stamp numbers the calls.
	states map[*d2graph.Object]*elkObject
	edgeAt []*elkEdgeState
	stamp  uint64

	// built is set when a call built the grids (only then do the cells' and
	// segmentIDs' lists grow); listBytes is their storage, as release last
	// measured it.
	built     bool
	listBytes int
}

// release drops what points into a graph (segments, objects, states),
// keeping the storage.
func (grids *bendGrids) release() {
	clear(grids.segmentItems)
	grids.segmentItems = grids.segmentItems[:0]
	clear(grids.objects)
	grids.objects = grids.objects[:0]
	for i := range grids.segments {
		clear(grids.segments[i][:cap(grids.segments[i])])
	}
	clear(grids.segments)
	grids.segments = grids.segments[:0]
	clear(grids.block)
	grids.states, grids.edgeAt = nil, nil
	if grids.built {
		// The lists only grow in calls that build the grids: measured here
		// once per such call instead of at every release.
		grids.listBytes = grids.objectGrid.cellListBytes() + grids.segmentGrid.cellListBytes()
		for _, ids := range grids.segmentIDs[:cap(grids.segmentIDs)] {
			grids.listBytes += 4 * cap(ids)
		}
		grids.built = false
	}
}

type segmentItem struct {
	position int
	segment  geo.Segment
}

// retainedBytes bounds the storage the grids keep once released (see
// elkScratch.retainedBytes; listBytes is current after release).
func (grids *bendGrids) retainedBytes() int {
	return grids.objectGrid.retainedBytes() + grids.segmentGrid.retainedBytes() + grids.listBytes +
		segmentItemBytes*cap(grids.segmentItems) + sliceBytes*cap(grids.segmentIDs) + 4*cap(grids.found) +
		linearObjectBytes*cap(grids.objects) + sliceBytes*cap(grids.segments) + linearSegmentBytes*cap(grids.block)
}

// retainedBytes bounds the storage the grid keeps, but for its cells'
// lists (cellListBytes).
func (grid *gridIndex) retainedBytes() int {
	return sliceBytes*cap(grid.cells) + pointerBytes*cap(grid.usedCells) + 4*cap(grid.irregular) +
		boundsBytes*cap(grid.bounds) + cap(grid.alive) + 4*cap(grid.seen)
}

// cellListBytes is the storage of the grid's cells' lists.
func (grid *gridIndex) cellListBytes() int {
	bytes := 0
	for _, cell := range grid.cells[:cap(grid.cells)] {
		bytes += 4 * cap(cell)
	}
	return bytes
}

// newBendCollisionIndexWith builds the index scanning lists (scan) or with
// grids, whose storage comes from grids when it is not nil.
func newBendCollisionIndexWith(g *d2graph.Graph, opts LayeredOptions, scan bool, grids *bendGrids) *bendCollisionIndex {
	index := &bendCollisionIndex{}
	index.objectBuffer = float64(edge_node_spacing) - 1
	if opts.EdgeNode != nil {
		index.objectBuffer = math.Max(0, *opts.EdgeNode-1)
	}
	// The bend cleanup historically checked for parallel edges within twice
	// the default 10 unit edge spacing. Keep that tolerance proportional.
	index.edgeBuffer = 20
	if opts.EdgeEdge != nil {
		index.edgeBuffer = 2 * *opts.EdgeEdge
	}
	total := 0
	for _, edge := range g.Edges {
		total += max(0, len(edge.Route)-1)
	}
	var block []linearSegment
	if grids != nil {
		// Lists kept across calls (emptied on release).
		index.objects = grids.objects[:0]
		index.segments = resized(grids.segments, len(g.Edges))
		block = resized(grids.block, total)
		grids.stamp++
	} else {
		index.objects = make([]linearObject, 0, len(g.Objects))
		index.segments = make([][]linearSegment, len(g.Edges))
		block = make([]linearSegment, total)
	}
	index.edges = g.Edges
	if grids != nil && grids.edgeAt != nil {
		index.edgeAt = grids.edgeAt
	} else {
		index.position = make(map[*d2graph.Edge]int, len(g.Edges))
	}
	// One entry per object, with its last bounds: duplicates are found by
	// the objects' states when Layout gave them (stamped per call), else
	// (and for objects without one) by a map.
	var objectSlot map[*d2graph.Object]int
	for _, obj := range g.Objects {
		entry := linearObject{obj: obj, bounds: objectBounds(obj, index.objectBuffer)}
		var state *elkObject
		if grids != nil && grids.states != nil {
			state = grids.states[obj]
		}
		if state != nil {
			if state.bendStamp == grids.stamp {
				index.objects[state.bendSlot] = entry
				continue
			}
			state.bendStamp, state.bendSlot = grids.stamp, len(index.objects)
		} else {
			if objectSlot == nil {
				objectSlot = make(map[*d2graph.Object]int)
			}
			if slot, seen := objectSlot[obj]; seen {
				index.objects[slot] = entry
				continue
			}
			objectSlot[obj] = len(index.objects)
		}
		index.objects = append(index.objects, entry)
	}
	for i, edge := range g.Edges {
		if index.position != nil {
			if _, seen := index.position[edge]; !seen {
				index.position[edge] = i
			}
		}
		// Room for the edge's current segments; updateEdge appends there.
		n := max(0, len(edge.Route)-1)
		index.segments[i] = block[:0:n]
		block = block[n:]
	}
	if !scan {
		if grids == nil {
			grids = &bendGrids{}
		}
		index.grids = grids
		grids.built = true
		objectExtent := spatialBounds{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		for i := range index.objects {
			objectExtent = union(objectExtent, index.objects[i].bounds)
		}
		grids.objectGrid.reset(objectExtent)
		for i := range index.objects {
			grids.objectGrid.add(index.objects[i].bounds)
		}
		segmentExtent := spatialBounds{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		for _, edge := range g.Edges {
			for i := 0; i+1 < len(edge.Route); i++ {
				segmentExtent = union(segmentExtent, boundsForSegment(geo.Segment{Start: edge.Route[i], End: edge.Route[i+1]}))
			}
		}
		grids.segmentGrid.reset(segmentExtent)
		clear(grids.segmentItems)
		grids.segmentItems = grids.segmentItems[:0]
		for i := range grids.segmentIDs {
			grids.segmentIDs[i] = grids.segmentIDs[i][:0]
		}
		if cap(grids.segmentIDs) < len(g.Edges) {
			grids.segmentIDs = append(grids.segmentIDs[:cap(grids.segmentIDs)], make([][]int32, len(g.Edges)-cap(grids.segmentIDs))...)
		}
		grids.segmentIDs = grids.segmentIDs[:len(g.Edges)]
	}
	for i, edge := range g.Edges {
		index.updateEdgeAt(i, edge)
	}
	if grids != nil {
		grids.objects, grids.segments = index.objects, index.segments
		grids.block = block[:cap(block)]
	}
	return index
}

// resized returns list with length n, all zero, reusing its storage.
func resized[T any](list []T, n int) []T {
	if cap(list) < n {
		return make([]T, n)
	}
	list = list[:n]
	clear(list)
	return list
}

// union is the smallest bounds holding a and b, ignoring b when it is not
// regular (such items are not kept in cells).
func union(a, b spatialBounds) spatialBounds {
	if !regular(b) {
		return a
	}
	return spatialBounds{math.Min(a.minX, b.minX), math.Min(a.minY, b.minY), math.Max(a.maxX, b.maxX), math.Max(a.maxY, b.maxY)}
}

func objectBounds(obj *d2graph.Object, buffer float64) spatialBounds {
	return spatialBounds{
		minX: obj.TopLeft.X,
		minY: obj.TopLeft.Y,
		maxX: obj.TopLeft.X + obj.Width,
		maxY: obj.TopLeft.Y + obj.Height,
	}.expanded(buffer)
}

// updateEdgeAt is updateEdge for g.Edges[i], edge.
func (index *bendCollisionIndex) updateEdgeAt(i int, edge *d2graph.Edge) {
	if index.edgeAt != nil {
		index.updateSegments(index.edgeAt[i].first, edge)
		return
	}
	index.updateEdge(edge)
}

func (index *bendCollisionIndex) updateEdge(edge *d2graph.Edge) {
	if index.edgeAt != nil {
		// Positions come from the edges' states: find edge's (any edge
		// of g is in edges).
		for i, other := range index.edges {
			if other == edge {
				index.updateSegments(index.edgeAt[i].first, edge)
				return
			}
		}
	}
	position, ok := index.position[edge]
	if !ok {
		// Not one of the graph's edges: give it a slot of its own.
		position = len(index.edges)
		index.edges = append(index.edges[:position:position], edge)
		index.position[edge] = position
		index.segments = append(index.segments, nil)
		if index.grids != nil {
			index.grids.segmentIDs = append(index.grids.segmentIDs, nil)
		}
	}
	index.updateSegments(position, edge)
}

// updateSegments records the current segments of edge, at position.
func (index *bendCollisionIndex) updateSegments(position int, edge *d2graph.Edge) {
	segments := index.segments[position][:0]
	for i := 0; i+1 < len(edge.Route); i++ {
		segment := geo.Segment{Start: edge.Route[i], End: edge.Route[i+1]}
		segments = append(segments, linearSegment{segment: segment, bounds: boundsForSegment(segment)})
	}
	index.segments[position] = segments
	if grids := index.grids; grids != nil {
		ids := grids.segmentIDs[position]
		for _, id := range ids {
			grids.segmentGrid.remove(id)
		}
		ids = ids[:0]
		for i := range segments {
			ids = append(ids, grids.segmentGrid.add(segments[i].bounds))
			grids.segmentItems = append(grids.segmentItems, segmentItem{position: position, segment: segments[i].segment})
		}
		grids.segmentIDs[position] = ids
	}
}

func (index *bendCollisionIndex) countObjectIntersects(src, dst *d2graph.Object, segment geo.Segment) int {
	count := 0
	visit := func(obj *d2graph.Object) {
		if obj == src || obj == dst {
			return
		}
		if obj.Intersects(segment, index.objectBuffer) {
			count++
		}
	}
	queryBounds := boundsForSegment(segment)
	if grids := index.grids; grids != nil {
		grids.found = grids.objectGrid.collect(queryBounds, grids.found[:0])
		for _, id := range grids.found {
			visit(index.objects[id].obj)
		}
		return count
	}
	for i := range index.objects {
		if index.objects[i].bounds.overlaps(queryBounds) {
			visit(index.objects[i].obj)
		}
	}
	return count
}

func (index *bendCollisionIndex) countEdgeIntersects(sourceEdge *d2graph.Edge, segment geo.Segment) (int, int, int, int) {
	isHorizontal := math.Ceil(segment.Start.Y) == math.Ceil(segment.End.Y)
	crossingsCount := 0
	overlapsCount := 0
	closeOverlapsCount := 0
	touchingCount := 0
	queryBounds := boundsForSegment(segment).expanded(index.edgeBuffer)
	visit := func(edge *d2graph.Edge, otherSegment geo.Segment) {
		if edge == sourceEdge {
			return
		}
		otherIsHorizontal := math.Ceil(otherSegment.Start.Y) == math.Ceil(otherSegment.End.Y)
		if isHorizontal == otherIsHorizontal {
			if segment.Overlaps(otherSegment, !isHorizontal, 0.) {
				if isHorizontal {
					if math.Abs(segment.Start.Y-otherSegment.Start.Y) < index.edgeBuffer {
						overlapsCount++
						if math.Abs(segment.Start.Y-otherSegment.Start.Y) < index.edgeBuffer/2 {
							closeOverlapsCount++
							if math.Abs(segment.Start.Y-otherSegment.Start.Y) < 1. {
								touchingCount++
							}
						}
					}
				} else {
					if math.Abs(segment.Start.X-otherSegment.Start.X) < index.edgeBuffer {
						overlapsCount++
						if math.Abs(segment.Start.X-otherSegment.Start.X) < index.edgeBuffer/2 {
							closeOverlapsCount++
							// Preserve the existing predicate, including its use of Y
							// for vertical touching segments.
							if math.Abs(segment.Start.Y-otherSegment.Start.Y) < 1. {
								touchingCount++
							}
						}
					}
				}
			}
		} else if segment.Intersects(otherSegment) {
			crossingsCount++
		}
	}
	if grids := index.grids; grids != nil {
		grids.found = grids.segmentGrid.collect(queryBounds, grids.found[:0])
		for _, id := range grids.found {
			item := &grids.segmentItems[id]
			visit(index.edges[item.position], item.segment)
		}
		return crossingsCount, overlapsCount, closeOverlapsCount, touchingCount
	}
	for position, segments := range index.segments {
		for i := range segments {
			if segments[i].bounds.overlaps(queryBounds) {
				visit(index.edges[position], segments[i].segment)
			}
		}
	}
	return crossingsCount, overlapsCount, closeOverlapsCount, touchingCount
}
