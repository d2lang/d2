package routing

import (
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// routeLabelBoxes holds the node-label obstacles of one router's searches.
// Route flavors search concurrently against a shared graph that no flavor
// mutates, so a node's label box is the same in every search of a router.
// Independent rerouting considers placed node labels. A ranked placement can
// already avoid the caller's fixed outside labels and container headings.
type routeLabelBoxes struct {
	nodes  []*layoutgraph.Node
	boxes  []geo.Box
	bounds []routeLabelBoxBounds
	// edges is the OVG's edge list; spans, indexed like it, locate each
	// edge's hit list (the indices of the boxes segmentIntersectsBox reports
	// for it, in node order) in pool once computed.
	edges []*OVGEdge
	spans []routeLabelHitSpan
	pool  []int32
	// other holds hit lists of edges outside the OVG's edge list.
	other map[*OVGEdge][]int32
}

type routeLabelHitSpan struct {
	start int32
	// count is negative until the span is computed.
	count int32
}

// routeLabelBoxBounds are the box extents exactly as segmentIntersectsBox
// computes them, so its first rejection test can run without calling it.
type routeLabelBoxBounds struct {
	left, right, top, bottom float64
	// valid is false for a box without a top-left point, which
	// segmentIntersectsBox never reports.
	valid bool
}

func newRouteLabelBoxes(nodes []*layoutgraph.Node, edges []*OVGEdge, fixedOnly bool) *routeLabelBoxes {
	index := &routeLabelBoxes{edges: edges, spans: make([]routeLabelHitSpan, len(edges))}
	for i := range index.spans {
		index.spans[i].count = -1
	}
	for _, n := range nodes {
		if n.Label == nil || n.Label.Text == "" {
			continue
		}
		if fixedOnly && (!n.Label.PositionFixed() || !(n.IsContainer() || n.Label.Position.IsOutside())) {
			continue
		}
		box := geo.Box{
			TopLeft: n.LabelTopLeft(n.Label.Position, n.Label.Width, n.Label.Height),
			Width:   n.Label.Width,
			Height:  n.Label.Height,
		}
		var bounds routeLabelBoxBounds
		if box.TopLeft != nil {
			bounds = routeLabelBoxBounds{
				left:   math.Min(box.TopLeft.X, box.TopLeft.X+box.Width),
				right:  math.Max(box.TopLeft.X, box.TopLeft.X+box.Width),
				top:    math.Min(box.TopLeft.Y, box.TopLeft.Y+box.Height),
				bottom: math.Max(box.TopLeft.Y, box.TopLeft.Y+box.Height),
				valid:  true,
			}
		}
		index.nodes = append(index.nodes, n)
		index.boxes = append(index.boxes, box)
		index.bounds = append(index.bounds, bounds)
	}
	return index
}

// hitsFor returns the indices of the boxes e intersects according to
// segmentIntersectsBox. A box failing segmentIntersectsBox's own bounding-box
// rejection, computed from the same values, is skipped without the call.
func (index *routeLabelBoxes) hitsFor(e *OVGEdge) []int32 {
	if i := int(e.index); i < len(index.edges) && index.edges[i] == e {
		span := &index.spans[i]
		if span.count >= 0 {
			end := span.start + span.count
			return index.pool[span.start:end:end]
		}
		start := len(index.pool)
		index.pool = index.appendHits(index.pool, e)
		*span = routeLabelHitSpan{start: int32(start), count: int32(len(index.pool) - start)}
		return index.pool[start:len(index.pool):len(index.pool)]
	}
	if hits, ok := index.other[e]; ok {
		return hits
	}
	if index.other == nil {
		index.other = make(map[*OVGEdge][]int32)
	}
	hits := index.appendHits(nil, e)
	index.other[e] = hits
	return hits
}

func (index *routeLabelBoxes) appendHits(hits []int32, e *OVGEdge) []int32 {
	first, second := e.From.Point, e.To.Point
	if first == nil || second == nil {
		return hits
	}
	minX, maxX := math.Min(first.X, second.X), math.Max(first.X, second.X)
	minY, maxY := math.Min(first.Y, second.Y), math.Max(first.Y, second.Y)
	for i := range index.boxes {
		bounds := &index.bounds[i]
		if !bounds.valid {
			continue
		}
		if maxX < bounds.left || bounds.right < minX || maxY < bounds.top || bounds.bottom < minY {
			continue
		}
		if segmentIntersectsBox(first, second, &index.boxes[i]) {
			hits = append(hits, int32(i))
		}
	}
	return hits
}
