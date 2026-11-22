package d2elklayout

// Copied from d2 e22f4d566 (d2layouts/d2elklayout/spatial_index.go and
// deleteBends in layout.go): the original
// bend cleanup, every identifier prefixed with orig, as the reference of
// TestDeleteBendsMatchesOriginal.

import (
	"math"
	"strings"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/shape"
)

const (
	origBendIndexCellSize = 128.0
	origBendIndexMaxCells = 256
)

type origSpatialBounds struct {
	minX float64
	minY float64
	maxX float64
	maxY float64
}

func origBoundsForSegment(segment geo.Segment) origSpatialBounds {
	return origSpatialBounds{
		minX: math.Min(segment.Start.X, segment.End.X),
		minY: math.Min(segment.Start.Y, segment.End.Y),
		maxX: math.Max(segment.Start.X, segment.End.X),
		maxY: math.Max(segment.Start.Y, segment.End.Y),
	}
}

func (bounds origSpatialBounds) expanded(amount float64) origSpatialBounds {
	bounds.minX -= amount
	bounds.minY -= amount
	bounds.maxX += amount
	bounds.maxY += amount
	return bounds
}

func (bounds origSpatialBounds) overlaps(other origSpatialBounds) bool {
	return bounds.minX <= other.maxX && other.minX <= bounds.maxX &&
		bounds.minY <= other.maxY && other.minY <= bounds.maxY
}

type origSpatialCell struct {
	x int
	y int
}

type origSpatialEntry struct {
	bounds origSpatialBounds
	cells  []origSpatialCell
	large  bool
}

// origSpatialIndex is an exact-query broad phase: it may return false positives,
// but every item whose bounds overlap the query is returned. Very large or
// non-finite entries are kept in a small fallback set instead of filling an
// unbounded number of grid cells.
type origSpatialIndex[T comparable] struct {
	cellSize float64
	maxCells int
	cells    map[origSpatialCell]map[T]struct{}
	large    map[T]struct{}
	entries  map[T]origSpatialEntry
	seen     map[T]uint64
	queryID  uint64
}

func origNewSpatialIndex[T comparable](capacity int) *origSpatialIndex[T] {
	return &origSpatialIndex[T]{
		cellSize: origBendIndexCellSize,
		maxCells: origBendIndexMaxCells,
		cells:    make(map[origSpatialCell]map[T]struct{}),
		large:    make(map[T]struct{}),
		entries:  make(map[T]origSpatialEntry, capacity),
		seen:     make(map[T]uint64, capacity),
	}
}

func (index *origSpatialIndex[T]) add(item T, bounds origSpatialBounds) {
	index.remove(item)
	cells, ok := index.coveredCells(bounds)
	entry := origSpatialEntry{bounds: bounds, cells: cells, large: !ok}
	index.entries[item] = entry
	if !ok {
		index.large[item] = struct{}{}
		return
	}
	for _, cell := range cells {
		items := index.cells[cell]
		if items == nil {
			items = make(map[T]struct{})
			index.cells[cell] = items
		}
		items[item] = struct{}{}
	}
}

func (index *origSpatialIndex[T]) remove(item T) {
	entry, ok := index.entries[item]
	if !ok {
		return
	}
	delete(index.entries, item)
	delete(index.seen, item)
	if entry.large {
		delete(index.large, item)
		return
	}
	for _, cell := range entry.cells {
		items := index.cells[cell]
		delete(items, item)
		if len(items) == 0 {
			delete(index.cells, cell)
		}
	}
}

func (index *origSpatialIndex[T]) query(bounds origSpatialBounds, visit func(T)) {
	cells, ok := index.coveredCells(bounds)
	if !ok {
		for item, entry := range index.entries {
			if entry.bounds.overlaps(bounds) {
				visit(item)
			}
		}
		return
	}

	index.queryID++
	if index.queryID == 0 {
		clear(index.seen)
		index.queryID = 1
	}
	queryID := index.queryID
	for _, cell := range cells {
		for item := range index.cells[cell] {
			if index.seen[item] == queryID {
				continue
			}
			index.seen[item] = queryID
			if index.entries[item].bounds.overlaps(bounds) {
				visit(item)
			}
		}
	}
	for item := range index.large {
		if index.seen[item] == queryID {
			continue
		}
		if index.entries[item].bounds.overlaps(bounds) {
			visit(item)
		}
	}
}

func (index *origSpatialIndex[T]) coveredCells(bounds origSpatialBounds) ([]origSpatialCell, bool) {
	if !origFiniteBounds(bounds) {
		return nil, false
	}
	minX := math.Floor(bounds.minX / index.cellSize)
	maxX := math.Floor(bounds.maxX / index.cellSize)
	minY := math.Floor(bounds.minY / index.cellSize)
	maxY := math.Floor(bounds.maxY / index.cellSize)
	// Keep float-to-int conversion safe on every supported target, including
	// wasm32. Leave one unit of headroom so the inclusive loops below cannot
	// overflow when incrementing their final coordinate.
	const safeCellCoordinate = float64(1<<31 - 2)
	if math.Abs(minX) > safeCellCoordinate || math.Abs(maxX) > safeCellCoordinate ||
		math.Abs(minY) > safeCellCoordinate || math.Abs(maxY) > safeCellCoordinate {
		return nil, false
	}
	width := maxX - minX + 1
	height := maxY - minY + 1
	if width <= 0 || height <= 0 || width > float64(index.maxCells) ||
		height > float64(index.maxCells) || width*height > float64(index.maxCells) {
		return nil, false
	}
	cells := make([]origSpatialCell, 0, int(width*height))
	for y := int(minY); y <= int(maxY); y++ {
		for x := int(minX); x <= int(maxX); x++ {
			cells = append(cells, origSpatialCell{x: x, y: y})
		}
	}
	return cells, true
}

func origFiniteBounds(bounds origSpatialBounds) bool {
	return !math.IsNaN(bounds.minX) && !math.IsNaN(bounds.minY) &&
		!math.IsNaN(bounds.maxX) && !math.IsNaN(bounds.maxY) &&
		!math.IsInf(bounds.minX, 0) && !math.IsInf(bounds.minY, 0) &&
		!math.IsInf(bounds.maxX, 0) && !math.IsInf(bounds.maxY, 0)
}

type origIndexedEdgeSegment struct {
	edge    *d2graph.Edge
	segment geo.Segment
}

type origBendCollisionIndex struct {
	objectBuffer float64
	edgeBuffer   float64
	objects      *origSpatialIndex[*d2graph.Object]
	segments     *origSpatialIndex[*origIndexedEdgeSegment]
	edgeSegments map[*d2graph.Edge][]*origIndexedEdgeSegment
}

func origNewBendCollisionIndex(g *d2graph.Graph, opts LayeredOptions) *origBendCollisionIndex {
	index := &origBendCollisionIndex{
		objects:      origNewSpatialIndex[*d2graph.Object](len(g.Objects)),
		segments:     origNewSpatialIndex[*origIndexedEdgeSegment](len(g.Edges) * 3),
		edgeSegments: make(map[*d2graph.Edge][]*origIndexedEdgeSegment, len(g.Edges)),
	}
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
	for _, obj := range g.Objects {
		bounds := origSpatialBounds{
			minX: obj.TopLeft.X,
			minY: obj.TopLeft.Y,
			maxX: obj.TopLeft.X + obj.Width,
			maxY: obj.TopLeft.Y + obj.Height,
		}.expanded(index.objectBuffer)
		index.objects.add(obj, bounds)
	}
	for _, edge := range g.Edges {
		index.updateEdge(edge)
	}
	return index
}

func (index *origBendCollisionIndex) updateEdge(edge *d2graph.Edge) {
	previous := index.edgeSegments[edge]
	for _, segment := range previous {
		index.segments.remove(segment)
	}
	segmentCount := max(0, len(edge.Route)-1)
	segments := previous[:0]
	if cap(segments) < segmentCount {
		segments = make([]*origIndexedEdgeSegment, 0, segmentCount)
	}
	for i := 0; i+1 < len(edge.Route); i++ {
		var segment *origIndexedEdgeSegment
		if i < len(previous) {
			segment = previous[i]
		} else {
			segment = &origIndexedEdgeSegment{}
		}
		segment.edge = edge
		segment.segment = geo.Segment{Start: edge.Route[i], End: edge.Route[i+1]}
		segments = append(segments, segment)
		index.segments.add(segment, origBoundsForSegment(segment.segment))
	}
	for i := len(segments); i < len(previous); i++ {
		previous[i] = nil
	}
	index.edgeSegments[edge] = segments
}

func (index *origBendCollisionIndex) countObjectIntersects(src, dst *d2graph.Object, segment geo.Segment) int {
	count := 0
	index.objects.query(origBoundsForSegment(segment), func(obj *d2graph.Object) {
		if obj == src || obj == dst {
			return
		}
		if obj.Intersects(segment, index.objectBuffer) {
			count++
		}
	})
	return count
}

func (index *origBendCollisionIndex) countEdgeIntersects(sourceEdge *d2graph.Edge, segment geo.Segment) (int, int, int, int) {
	isHorizontal := math.Ceil(segment.Start.Y) == math.Ceil(segment.End.Y)
	crossingsCount := 0
	overlapsCount := 0
	closeOverlapsCount := 0
	touchingCount := 0
	queryBounds := origBoundsForSegment(segment).expanded(index.edgeBuffer)
	index.segments.query(queryBounds, func(other *origIndexedEdgeSegment) {
		if other.edge == sourceEdge {
			return
		}
		otherSegment := other.segment
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
	})
	return crossingsCount, overlapsCount, closeOverlapsCount, touchingCount
}

// origDeleteBends is a shim for ELK to delete unnecessary bends
// see https://github.com/terrastruct/d2/issues/1030
func origDeleteBends(g *d2graph.Graph, opts LayeredOptions, keepRoute map[*d2graph.Edge]bool) {
	hasCandidate := false
	for _, edge := range g.Edges {
		if edge.Src != edge.Dst && len(edge.Route) >= 4 && !keepRoute[edge] {
			hasCandidate = true
			break
		}
	}
	if !hasCandidate {
		return
	}
	collisionIndex := origNewBendCollisionIndex(g, opts)

	// Straightening a bend can slide the attachment along the shape border.
	// Keep ELK's shared input and output ports at their original positions.
	type endpointKey struct {
		object *d2graph.Object
		point  geo.Point
		source bool
	}
	var attachments map[endpointKey]int
	if opts.MergeEdges {
		attachments = make(map[endpointKey]int)
		for _, edge := range g.Edges {
			attachments[endpointKey{edge.Src, *edge.Route[0], true}]++
			attachments[endpointKey{edge.Dst, *edge.Route[len(edge.Route)-1], false}]++
		}
	}

	// Get rid of S-shapes at the source and the target
	// TODO there might be value in repeating this. removal of an S shape introducing another S shape that can still be removed
	for _, isSource := range []bool{true, false} {
		for ei, e := range g.Edges {
			if len(e.Route) < 4 || keepRoute[e] {
				continue
			}
			if e.Src == e.Dst {
				continue
			}
			var endpoint *d2graph.Object
			var start *geo.Point
			var corner *geo.Point
			var end *geo.Point

			var columnIndex *int
			if isSource {
				start = e.Route[0]
				corner = e.Route[1]
				end = e.Route[2]
				endpoint = e.Src
				columnIndex = e.SrcTableColumnIndex
			} else {
				start = e.Route[len(e.Route)-1]
				corner = e.Route[len(e.Route)-2]
				end = e.Route[len(e.Route)-3]
				endpoint = e.Dst
				columnIndex = e.DstTableColumnIndex
			}

			if opts.MergeEdges && attachments[endpointKey{endpoint, *start, isSource}] > 1 {
				continue
			}
			isHorizontal := math.Ceil(start.Y) == math.Ceil(corner.Y)
			dx, dy := endpoint.GetModifierElementAdjustments()

			// Make sure it's still attached
			switch {
			case columnIndex != nil:
				rowHeight := endpoint.Height / float64(len(endpoint.SQLTable.Columns)+1)
				rowCenter := endpoint.TopLeft.Y + rowHeight*float64(*columnIndex+1) + rowHeight/2

				// for row connections new Y coordinate should be within 1/3 row height from the row center
				if math.Abs(end.Y-rowCenter) > rowHeight/3 {
					continue
				}
			case isHorizontal:
				if end.Y <= endpoint.TopLeft.Y+10-dy {
					continue
				}
				if end.Y >= endpoint.TopLeft.Y+endpoint.Height-10 {
					continue
				}
			default:
				if end.X <= endpoint.TopLeft.X+10 {
					continue
				}
				if end.X >= endpoint.TopLeft.X+endpoint.Width-10+dx {
					continue
				}
			}

			var newStart *geo.Point
			if isHorizontal {
				newStart = geo.NewPoint(start.X, end.Y)
			} else {
				newStart = geo.NewPoint(end.X, start.Y)
			}

			endpointShape := shape.NewShape(d2target.DSL_SHAPE_TO_SHAPE_TYPE[strings.ToLower(endpoint.Shape.Value)], endpoint.Box)
			newStart = shape.TraceToShapeBorder(endpointShape, newStart, end)

			// Check that the new segment doesn't collide with anything new

			oldSegment := geo.NewSegment(start, corner)
			newSegment := geo.NewSegment(newStart, end)

			oldIntersects := collisionIndex.countObjectIntersects(e.Src, e.Dst, *oldSegment)
			newIntersects := collisionIndex.countObjectIntersects(e.Src, e.Dst, *newSegment)

			if newIntersects > oldIntersects {
				continue
			}

			oldCrossingsCount, oldOverlapsCount, oldCloseOverlapsCount, oldTouchingCount := collisionIndex.countEdgeIntersects(g.Edges[ei], *oldSegment)
			newCrossingsCount, newOverlapsCount, newCloseOverlapsCount, newTouchingCount := collisionIndex.countEdgeIntersects(g.Edges[ei], *newSegment)

			if newCrossingsCount > oldCrossingsCount {
				continue
			}
			if newOverlapsCount > oldOverlapsCount {
				continue
			}

			if newCloseOverlapsCount > oldCloseOverlapsCount {
				continue
			}
			if newTouchingCount > oldTouchingCount {
				continue
			}

			// commit
			if isSource {
				g.Edges[ei].Route = append(
					[]*geo.Point{newStart},
					e.Route[3:]...,
				)
			} else {
				g.Edges[ei].Route = append(
					e.Route[:len(e.Route)-3],
					newStart,
				)
			}
			collisionIndex.updateEdge(g.Edges[ei])
		}
	}

	// Get rid of ladders
	// ELK likes to do these for some reason
	// .   ┌─
	// . ┌─┘
	// . │
	// We want to transform these into L-shapes

	points := map[geo.Point]int{}
	for _, e := range g.Edges {
		for _, p := range e.Route {
			points[*p]++
		}
	}

	for ei, e := range g.Edges {
		if len(e.Route) < 6 || keepRoute[e] {
			continue
		}
		if e.Src == e.Dst {
			continue
		}

		for i := 1; i < len(e.Route)-3; i++ {
			before := e.Route[i-1]
			start := e.Route[i]
			corner := e.Route[i+1]
			end := e.Route[i+2]
			after := e.Route[i+3]

			if c, _ := points[*corner]; c > 1 {
				// If corner is shared with another edge, they merge
				continue
			}

			// S-shape on sources only concerned one segment, since the other was just along the bound of endpoint
			// These concern two segments

			var newCorner *geo.Point
			if math.Ceil(start.X) == math.Ceil(corner.X) {
				newCorner = geo.NewPoint(end.X, start.Y)
				// not ladder
				if (end.X > start.X) != (start.X > before.X) {
					continue
				}
				if (end.Y > start.Y) != (after.Y > end.Y) {
					continue
				}
			} else {
				newCorner = geo.NewPoint(start.X, end.Y)
				if (end.Y > start.Y) != (start.Y > before.Y) {
					continue
				}
				if (end.X > start.X) != (after.X > end.X) {
					continue
				}
			}

			oldS1 := geo.NewSegment(start, corner)
			oldS2 := geo.NewSegment(corner, end)

			newS1 := geo.NewSegment(start, newCorner)
			newS2 := geo.NewSegment(newCorner, end)

			// Check that the new segments doesn't collide with anything new
			oldIntersects := collisionIndex.countObjectIntersects(e.Src, e.Dst, *oldS1) + collisionIndex.countObjectIntersects(e.Src, e.Dst, *oldS2)
			newIntersects := collisionIndex.countObjectIntersects(e.Src, e.Dst, *newS1) + collisionIndex.countObjectIntersects(e.Src, e.Dst, *newS2)

			if newIntersects > oldIntersects {
				continue
			}

			oldCrossingsCount1, oldOverlapsCount1, oldCloseOverlapsCount1, oldTouchingCount1 := collisionIndex.countEdgeIntersects(g.Edges[ei], *oldS1)
			oldCrossingsCount2, oldOverlapsCount2, oldCloseOverlapsCount2, oldTouchingCount2 := collisionIndex.countEdgeIntersects(g.Edges[ei], *oldS2)
			oldCrossingsCount := oldCrossingsCount1 + oldCrossingsCount2
			oldOverlapsCount := oldOverlapsCount1 + oldOverlapsCount2
			oldCloseOverlapsCount := oldCloseOverlapsCount1 + oldCloseOverlapsCount2
			oldTouchingCount := oldTouchingCount1 + oldTouchingCount2

			newCrossingsCount1, newOverlapsCount1, newCloseOverlapsCount1, newTouchingCount1 := collisionIndex.countEdgeIntersects(g.Edges[ei], *newS1)
			newCrossingsCount2, newOverlapsCount2, newCloseOverlapsCount2, newTouchingCount2 := collisionIndex.countEdgeIntersects(g.Edges[ei], *newS2)
			newCrossingsCount := newCrossingsCount1 + newCrossingsCount2
			newOverlapsCount := newOverlapsCount1 + newOverlapsCount2
			newCloseOverlapsCount := newCloseOverlapsCount1 + newCloseOverlapsCount2
			newTouchingCount := newTouchingCount1 + newTouchingCount2

			if newCrossingsCount > oldCrossingsCount {
				continue
			}
			if newOverlapsCount > oldOverlapsCount {
				continue
			}

			if newCloseOverlapsCount > oldCloseOverlapsCount {
				continue
			}
			if newTouchingCount > oldTouchingCount {
				continue
			}

			// commit
			g.Edges[ei].Route = append(append(
				e.Route[:i],
				newCorner,
			),
				e.Route[i+3:]...,
			)
			collisionIndex.updateEdge(g.Edges[ei])
			break
		}
	}
}
