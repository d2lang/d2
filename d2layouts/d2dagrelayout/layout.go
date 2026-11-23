package d2dagrelayout

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/d2lang/util-go/xdefer"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/internal/layoutguard"
	"github.com/d2lang/d2/d2layouts/internal/ranklabel"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/internal/geomath"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
	"github.com/d2lang/dagro"
)

const (
	MIN_RANK_SEP    = 60
	EDGE_LABEL_GAP  = 20
	DEFAULT_PADDING = 30.
	MIN_SPACING     = 10.
)

type ConfigurableOpts struct {
	NodeSep int `json:"nodesep"`
	EdgeSep int `json:"edgesep"`
}

var DefaultOpts = ConfigurableOpts{
	NodeSep: 60,
	EdgeSep: 20,
}

type dagreOpts struct {
	// for a top to bottom graph: ranksep is y spacing, nodesep is x spacing, edgesep is x spacing
	ranksep int
	// graph direction: tb (top to bottom)| bt | lr | rl
	rankdir string

	ConfigurableOpts
}

func DefaultLayout(ctx context.Context, g *d2graph.Graph) (err error) {
	return Layout(ctx, g, nil)
}

func Layout(ctx context.Context, g *d2graph.Graph, opts *ConfigurableOpts) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts == nil {
		opts = &DefaultOpts
	}
	defer xdefer.Errorf(&err, "failed to dagre layout")
	if err := layoutguard.CheckGraph(ctx, "dagre", g); err != nil {
		return err
	}

	rootAttrs := dagreOpts{
		ConfigurableOpts: ConfigurableOpts{
			EdgeSep: opts.EdgeSep,
			NodeSep: opts.NodeSep,
		},
	}
	isHorizontal := false
	switch g.Root.Direction.Value {
	case "down":
		rootAttrs.rankdir = "TB"
	case "right":
		rootAttrs.rankdir = "LR"
		isHorizontal = true
	case "left":
		rootAttrs.rankdir = "RL"
		isHorizontal = true
	case "up":
		rootAttrs.rankdir = "BT"
	default:
		rootAttrs.rankdir = "TB"
	}

	// set label and icon positions for dagre
	// Most objects get one position and some edges one label position.
	positions := stringArena{chunk: len(g.Objects) + len(g.Edges)}
	for _, obj := range g.Objects {
		positionLabelsIcons(obj, &positions)
	}

	maxLabelWidth := 0
	maxLabelHeight := 0
	for _, edge := range g.Edges {
		width := edge.LabelDimensions.Width
		height := edge.LabelDimensions.Height
		maxLabelWidth = go2.Max(maxLabelWidth, width)
		maxLabelHeight = go2.Max(maxLabelHeight, height)
	}

	if !isHorizontal {
		rootAttrs.ranksep = go2.Max(100, maxLabelHeight+40)
	} else {
		rootAttrs.ranksep = go2.Max(100, maxLabelWidth+40)
		// use existing config
		// rootAttrs.NodeSep = rootAttrs.EdgeSep
		// // configure vertical padding
		// rootAttrs.EdgeSep = maxLabelHeight + 40
		// Note: non-containers have both of these as padding (rootAttrs.NodeSep + rootAttrs.EdgeSep)
	}

	sc := getScratch()
	ix := sc.objectIndex(g)
	resolver := newEndpointResolver(g)
	// Every element is written below.
	resolvedSrc := grow(sc.resolvedSrc, len(g.Edges))
	resolvedDst := grow(sc.resolvedDst, len(g.Edges))
	sc.resolvedSrc, sc.resolvedDst = resolvedSrc, resolvedDst
	for i, edge := range g.Edges {
		resolvedSrc[i], resolvedDst[i] = resolver.resolve(edge)
	}
	sc.counts = grow(sc.counts, 2*len(g.Edges))
	sameWay, bothWays := countParallelEdgesInto(sc.counts, resolvedSrc, resolvedDst)

	topLefts := make([]geo.Point, ix.distinct)
	in, buildErr, readErr := dagroInput(g, ix, rootAttrs, resolvedSrc, resolvedDst, sameWay, bothWays)
	if buildErr != nil {
		return buildErr
	}
	sc.in = in
	if err := ctx.Err(); err != nil {
		return err
	}
	// dagro does not accept a context. Check immediately around the call; the
	// topology preflight above bounds inputs before entering this section.
	out := &sc.out
	if err := dagro.LayoutTyped(&sc.in, out); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for i, obj := range g.Objects {
		// dagre gives center of node. A repeated object gets the same
		// position at each of its occurrences.
		k := ix.at[i]
		n := &out.Nodes[i]
		topLefts[k] = geo.Point{X: math.Round(n.X - n.Width/2), Y: math.Round(n.Y - n.Height/2)}
		obj.TopLeft = &topLefts[k]
		obj.Width = math.Ceil(n.Width)
		obj.Height = math.Ceil(n.Height)
	}
	// The original found these errors reading the result, after it had set
	// every object's position (see dagroInput).
	if readErr != nil {
		return readErr
	}

	totalPoints := 0
	for i := range g.Edges {
		totalPoints += len(out.Edges[i].Points)
	}
	// Each route may also take a new start and end point (the box chops).
	routeValues := pointArena{chunk: totalPoints + 2*len(g.Edges)}
	routeRefs := pointRefArena{chunk: totalPoints}
	for i, edge := range g.Edges {
		dagrePoints := out.Edges[i].Points
		pointValues := routeValues.take(len(dagrePoints))
		points := routeRefs.take(len(dagrePoints))
		for i, point := range dagrePoints {
			pointValues[i] = geo.Point{X: point.X, Y: point.Y}
			if edge.SrcArrow && !edge.DstArrow {
				points[len(dagrePoints)-i-1] = &pointValues[i]
			} else {
				points[i] = &pointValues[i]
			}
		}
		// Dagre 3.1 self-loop routes contain repeated control points. Remove only
		// consecutive duplicates so downstream tangent calculations never see a
		// zero-length segment.
		points = deduplicateRoutePoints(points)
		if edge.Src == edge.Dst {
			// The self-loop controls describe the loop outside the node, but do not
			// include usable node-facing endpoints. Center sentinels give
			// TraceToShape a direction from which to chop the route to each border.
			center := edge.Src.Center()
			points = append([]*geo.Point{center.Copy()}, points...)
			points = append(points, center.Copy())
		}
		if len(points) < 2 {
			return fmt.Errorf("dagro returned edge %q without a usable route", edge.AbsID())
		}

		startIndex, endIndex := 0, len(points)-1
		start, end := points[startIndex], points[endIndex]

		// chop where edge crosses the source/target boxes since container edges were routed to a descendant
		if edge.Src != edge.Dst {
			// The intersections are fresh points; the last source one is
			// made only once it is known to be kept.
			var startAt geo.Point
			startFound := false
			for i := 1; i < len(points); i++ {
				segment := geo.Segment{Start: points[i-1], End: points[i]}
				if p, ok := firstBoxIntersection(edge.Src.Box, segment); ok {
					startAt, startFound = p, true
					startIndex = i - 1
				}

				if p, ok := firstBoxIntersection(edge.Dst.Box, segment); ok {
					end = &routeValues.take(1)[0]
					*end = geo.Point{X: p.X, Y: p.Y}
					endIndex = i
					break
				}
			}
			if startFound {
				start = &routeValues.take(1)[0]
				*start = geo.Point{X: startAt.X, Y: startAt.Y}
			}
		}
		points = points[startIndex : endIndex+1]
		points[0] = start
		points[len(points)-1] = end

		edge.Route = points
	}

	ix.topLefts = topLefts
	ix.closeOver()
	adjustRankSpacing(g, ix, float64(rootAttrs.ranksep), isHorizontal)
	adjustCrossRankSpacing(g, ix, float64(rootAttrs.ranksep), !isHorizontal)
	fitContainerPadding(g, ix, float64(rootAttrs.ranksep), isHorizontal)

	// Size the arenas for the paths of the routes as they are; tracing to the
	// shapes can only shorten a route, and an arena that runs out allocates
	// another chunk.
	curveSize, pathSize := 0, 0
	for _, edge := range g.Edges {
		along := curvePoints(len(edge.Route) - 1)
		curveSize += along
		pathSize += along + 2
	}
	curveValues := pointArena{chunk: curveSize}
	curveRefs := pointRefArena{chunk: pathSize}

	for _, edge := range g.Edges {
		points := edge.Route
		startIndex, endIndex := 0, len(points)-1
		start, end := points[startIndex], points[endIndex]

		// arrowheads can appear broken if segments are very short from dagre routing a point just outside the shape
		// to fix this, we try extending the previous segment into the shape instead of having a very short segment
		if startIndex+2 < len(points) {
			newStartingSegment := *geo.NewSegment(start, points[startIndex+1])
			if newStartingSegment.Length() < d2graph.MIN_SEGMENT_LEN {
				// we don't want a very short segment right next to the source because it will mess up the arrowhead
				// instead we want to extend the next segment into the shape border if possible
				nextStart := points[startIndex+1]
				nextEnd := points[startIndex+2]

				// Note: in other direction to extend towards source
				nextSegment := *geo.NewSegment(nextStart, nextEnd)
				v := nextSegment.ToVector()
				extendedStart := nextEnd.ToVector().Add(v.AddLength(d2graph.MIN_SEGMENT_LEN)).ToPoint()
				extended := *geo.NewSegment(nextEnd, extendedStart)

				if intersections := edge.Src.Box.Intersections(extended); len(intersections) > 0 {
					startIndex++
					points[startIndex] = intersections[0]
					start = points[startIndex]
				}
			}
		}
		if endIndex-2 >= 0 {
			newEndingSegment := *geo.NewSegment(end, points[endIndex-1])
			if newEndingSegment.Length() < d2graph.MIN_SEGMENT_LEN {
				// extend the prev segment into the shape border if possible
				prevStart := points[endIndex-2]
				prevEnd := points[endIndex-1]

				prevSegment := *geo.NewSegment(prevStart, prevEnd)
				v := prevSegment.ToVector()
				extendedEnd := prevStart.ToVector().Add(v.AddLength(d2graph.MIN_SEGMENT_LEN)).ToPoint()
				extended := *geo.NewSegment(prevStart, extendedEnd)

				if intersections := edge.Dst.Box.Intersections(extended); len(intersections) > 0 {
					endIndex--
					points[endIndex] = intersections[0]
					end = points[endIndex]
				}
			}
		}

		var originalSrcTL, originalDstTL *geo.Point
		// if the edge passes through 3d/multiple, use the offset box for tracing to border
		if srcDx, srcDy := edge.Src.GetModifierElementAdjustments(); srcDx != 0 || srcDy != 0 {
			if start.X > edge.Src.TopLeft.X+srcDx &&
				start.Y < edge.Src.TopLeft.Y+edge.Src.Height-srcDy {
				originalSrcTL = edge.Src.TopLeft.Copy()
				edge.Src.TopLeft.X += srcDx
				edge.Src.TopLeft.Y -= srcDy
			}
		}
		if dstDx, dstDy := edge.Dst.GetModifierElementAdjustments(); dstDx != 0 || dstDy != 0 {
			if end.X > edge.Dst.TopLeft.X+dstDx &&
				end.Y < edge.Dst.TopLeft.Y+edge.Dst.Height-dstDy {
				originalDstTL = edge.Dst.TopLeft.Copy()
				edge.Dst.TopLeft.X += dstDx
				edge.Dst.TopLeft.Y -= dstDy
			}
		}

		startIndex, endIndex = edge.TraceToShape(points, startIndex, endIndex)
		points = points[startIndex : endIndex+1]

		// build a curved path from the dagre route: the ends, and points along
		// segment i (from points[i] to points[i+1]) at .8 of the first, at .2,
		// .5 and .8 of segments 1 to n-3, and at .2 of the last.
		n := len(points) - 1
		var path []*geo.Point
		if sameRankPeers(edge) {
			// Dagro reserved a polyline track for this edge and its label.
			path = points
			edge.IsCurve = false
		} else if n > 1 {
			along := curveValues.take(curvePoints(n))
			path = curveRefs.take(len(along) + 2)[:0]
			path = append(path, points[0])
			along[0] = pointAlong(points[0], points[1], .8)
			path = append(path, &along[0])
			j := 1
			for i := 1; i < n-2; i++ {
				along[j] = pointAlong(points[i], points[i+1], .2)
				along[j+1] = pointAlong(points[i], points[i+1], .5)
				along[j+2] = pointAlong(points[i], points[i+1], .8)
				path = append(path, &along[j], &along[j+1], &along[j+2])
				j += 3
			}
			along[j] = pointAlong(points[n-1], points[n], .2)
			path = append(path, &along[j], points[n])
			edge.IsCurve = true
		} else {
			path = curveRefs.take(2)
			path[0], path[1] = points[0], points[n]
		}

		edge.Route = path
		if sameRankPeers(edge) {
			dockRankedTableColumns(g, edge)
		}
		// compile needs to assign edge label positions
		if edge.Label.Value != "" {
			edge.LabelPosition = positions.ptr(label.InsideMiddleCenter.String())
		}

		// undo 3d/multiple offset
		if originalSrcTL != nil {
			edge.Src.TopLeft.X = originalSrcTL.X
			edge.Src.TopLeft.Y = originalSrcTL.Y
		}
		if originalDstTL != nil {
			edge.Dst.TopLeft.X = originalDstTL.X
			edge.Dst.TopLeft.Y = originalDstTL.Y
		}
	}

	putScratch(sc)
	if len(in.RankGroups) > 0 {
		ranklabel.ClearOverlaps(g)
	}

	return nil
}

func sameRankPeers(edge *d2graph.Edge) bool {
	return edge.Src != edge.Dst && edge.Src.Rank != nil && edge.Src.Rank.Value != "" && edge.Dst.Rank != nil && edge.Src.Rank.Value == edge.Dst.Rank.Value
}

func deduplicateRoutePoints(points []*geo.Point) []*geo.Point {
	if len(points) < 2 {
		return points
	}
	deduplicated := points[:1]
	for _, point := range points[1:] {
		if !point.Equals(deduplicated[len(deduplicated)-1]) {
			deduplicated = append(deduplicated, point)
		}
	}
	return deduplicated
}

// countParallelEdgesInto returns, for every edge i, the number of edges from
// src[i] to dst[i] (sameWay), and that number plus, when src[i] != dst[i],
// the number of edges from dst[i] to src[i] (bothWays), storing them in
// counts, of length 2*len(src), every element of which it writes.
func countParallelEdgesInto(counts []int, src, dst []*d2graph.Object) (sameWay, bothWays []int) {
	sameWay, bothWays = counts[:len(src)], counts[len(src):]
	if len(src) <= 32 {
		for i := range src {
			same, reverse := 0, 0
			for j := range src {
				if src[j] == src[i] && dst[j] == dst[i] {
					same++
				}
				if src[i] != dst[i] && src[j] == dst[i] && dst[j] == src[i] {
					reverse++
				}
			}
			sameWay[i], bothWays[i] = same, same+reverse
		}
		return sameWay, bothWays
	}
	type endpointPair struct {
		src, dst *d2graph.Object
	}
	directed := make(map[endpointPair]int, len(src))
	for i := range src {
		directed[endpointPair{src: src[i], dst: dst[i]}]++
	}
	for i := range src {
		same := directed[endpointPair{src: src[i], dst: dst[i]}]
		both := same
		if src[i] != dst[i] {
			both += directed[endpointPair{src: dst[i], dst: src[i]}]
		}
		sameWay[i], bothWays[i] = same, both
	}
	return sameWay, bothWays
}

// curvePoints is the number of points a curved path adds between the ends
// of a route of n segments: one on the first and one on the last segment,
// and three on each segment between the first two and the last two.
func curvePoints(n int) int {
	switch {
	case n <= 1:
		return 0
	case n <= 3:
		return 2
	default:
		return 2 + 3*(n-3)
	}
}

// pointAlong returns the value of p.AddVector(p.VectorTo(q).Multiply(t)),
// without the three slices. Those methods stored every difference and
// product in a slice before using it, so each was rounded on its own; the
// explicit conversion keeps that rounding, because it forbids the compiler
// to fuse the multiplication and the addition into one FMA instruction.
//
// It is not inlined. When both operands of an addition are NaN, the sum
// carries the NaN of the operand the compiled instruction takes first. In
// the original, Layout's inlined Vector.Add loops always took the scaled
// difference first on arm64 (FADDD); inlined into Layout here, some of these
// additions took p's coordinate first, while the one compiled body of
// pointAlong takes the scaled difference first in both (checked in the
// assembly; TestPointAlongNaNs). The operand order of a commutative
// instruction is the compiler's choice per function and architecture (on
// amd64 the original's ADDSD took p's coordinate first, and this body does
// so only for X), so only arm64, the platform the results are checked on,
// is matched. Results that are not NaN have the same bits either way.
//
//go:noinline
func pointAlong(p, q *geo.Point, t float64) geo.Point {
	dx := q.X - p.X
	dy := q.Y - p.Y
	return geo.Point{X: p.X + float64(dx*t), Y: p.Y + float64(dy*t)}
}

// firstBoxIntersection returns b.Intersections(s)[0] and whether there is
// one, as a value: without the slice, the later intersections or a point on
// the heap until the caller keeps one.
func firstBoxIntersection(b *geo.Box, s geo.Segment) (geo.Point, bool) {
	x, y, ok := geomath.FirstBoxIntersection(b.TopLeft.X, b.TopLeft.Y, b.Width, b.Height, s.Start.X, s.Start.Y, s.End.X, s.End.Y)
	return geo.Point{X: x, Y: y}, ok
}

type containerEndpoints struct {
	head, tail *d2graph.Object
}

type endpointResolver struct {
	g          *d2graph.Graph
	containers map[*d2graph.Object]containerEndpoints
}

func newEndpointResolver(g *d2graph.Graph) *endpointResolver {
	return &endpointResolver{
		g:          g,
		containers: make(map[*d2graph.Object]containerEndpoints),
	}
}

func (r *endpointResolver) head(container *d2graph.Object) *d2graph.Object {
	return r.endpoints(container).head
}

func (r *endpointResolver) tail(container *d2graph.Object) *d2graph.Object {
	return r.endpoints(container).tail
}

func (r *endpointResolver) endpoints(container *d2graph.Object) containerEndpoints {
	endpoints, ok := r.containers[container]
	if !ok {
		endpoints.head, endpoints.tail = getLongestEdgeChainEndpoints(r.g, container)
		r.containers[container] = endpoints
	}
	return endpoints
}

func (r *endpointResolver) resolve(edge *d2graph.Edge) (*d2graph.Object, *d2graph.Object) {
	// dagre doesn't work with edges to containers so we connect container edges to their first child instead (going all the way down)
	// we will chop the edge where it intersects the container border so it only shows the edge from the container
	src := edge.Src
	for len(src.Children) > 0 && src.Class == nil && src.SQLTable == nil {
		// We want to get the bottom node of sources, setting its rank higher than all children
		src = r.tail(src)
	}
	dst := edge.Dst
	for len(dst.Children) > 0 && dst.Class == nil && dst.SQLTable == nil {
		dst = r.head(dst)
	}
	if edge.SrcArrow && !edge.DstArrow {
		// for `b <- a`, edge.Edge is `a -> b` and we expect this routing result
		src, dst = dst, src
	}
	return src, dst
}

type containerTopology struct {
	container   *d2graph.Object
	hasIncoming map[*d2graph.Object]bool
	outgoing    map[*d2graph.Object][]*d2graph.Object
}

func newContainerTopology(g *d2graph.Graph, container *d2graph.Object) *containerTopology {
	topology := &containerTopology{
		container:   container,
		hasIncoming: make(map[*d2graph.Object]bool, len(container.ChildrenArray)),
		outgoing:    make(map[*d2graph.Object][]*d2graph.Object, len(container.ChildrenArray)+1),
	}
	for _, edge := range g.Edges {
		src := inContainer(edge.Src, container)
		dst := inContainer(edge.Dst, container)
		if src == nil || dst == nil {
			continue
		}

		// The original head test counts every edge from anywhere in the
		// container to a direct child's subtree, including internal edges.
		if dst != container {
			topology.hasIncoming[dst] = true
		}

		// Preserve edge order and duplicates: the breadth-first walk's rank
		// updates are order-sensitive even though the graph is usually acyclic.
		if src != dst {
			topology.outgoing[src] = append(topology.outgoing[src], dst)
		}
		// A container can enter the old traversal when an edge terminates on
		// it. From there, every edge whose source is inside it is reachable.
		if src != container && dst != container {
			topology.outgoing[container] = append(topology.outgoing[container], dst)
		}
	}
	return topology
}

func (t *containerTopology) longestChainEndpoints() (*d2graph.Object, *d2graph.Object) {
	rank := make(map[*d2graph.Object]int)
	chainLength := make(map[*d2graph.Object]int)

	for _, obj := range t.container.ChildrenArray {
		if t.hasIncoming[obj] {
			continue
		}
		rank[obj] = 1
		chainLength[obj] = 1
		queue := []*d2graph.Object{obj}
		visited := make(map[*d2graph.Object]struct{})
		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]
			if _, ok := visited[curr]; ok {
				continue
			}
			visited[curr] = struct{}{}
			for _, child := range t.outgoing[curr] {
				if rank[curr]+1 > rank[child] {
					rank[child] = rank[curr] + 1
					chainLength[obj] = go2.Max(chainLength[obj], rank[child])
				}
				queue = append(queue, child)
			}
		}
	}

	maxChainLength := int(math.MinInt32)
	maxRank := int(math.MinInt32)
	for _, obj := range t.container.ChildrenArray {
		maxChainLength = go2.Max(maxChainLength, chainLength[obj])
		maxRank = go2.Max(maxRank, rank[obj])
	}

	var heads, tails []*d2graph.Object
	for _, obj := range t.container.ChildrenArray {
		if rank[obj] == 1 && chainLength[obj] == maxChainLength {
			heads = append(heads, obj)
		}
		if rank[obj] == maxRank {
			tails = append(tails, obj)
		}
	}

	head := t.container.ChildrenArray[0]
	if len(heads) > 0 {
		head = heads[len(heads)/2]
	}
	return head, tails[len(tails)/2]
}

func getLongestEdgeChainEndpoints(g *d2graph.Graph, container *d2graph.Object) (*d2graph.Object, *d2graph.Object) {
	return newContainerTopology(g, container).longestChainEndpoints()
}

func inContainer(obj, container *d2graph.Object) *d2graph.Object {
	if obj == nil {
		return nil
	}
	if obj == container {
		return obj
	}
	if obj.Parent == container {
		return obj
	}
	return inContainer(obj.Parent, container)
}

func positionLabelsIcons(obj *d2graph.Object, positions *stringArena) {
	if obj.Icon != nil && obj.IconPosition == nil {
		if len(obj.ChildrenArray) > 0 {
			obj.IconPosition = positions.ptr(label.OutsideTopLeft.String())
			if obj.LabelPosition == nil {
				obj.LabelPosition = positions.ptr(label.OutsideTopRight.String())
				return
			}
		} else if obj.SQLTable != nil || obj.Class != nil || obj.Language != "" {
			obj.IconPosition = positions.ptr(label.OutsideTopLeft.String())
		} else {
			obj.IconPosition = positions.ptr(label.InsideMiddleCenter.String())
		}
	}
	if obj.HasLabel() && obj.LabelPosition == nil {
		if len(obj.ChildrenArray) > 0 {
			obj.LabelPosition = positions.ptr(label.OutsideTopCenter.String())
		} else if obj.HasOutsideBottomLabel() {
			obj.LabelPosition = positions.ptr(label.OutsideBottomCenter.String())
		} else if obj.Icon != nil {
			obj.LabelPosition = positions.ptr(label.InsideTopCenter.String())
		} else {
			obj.LabelPosition = positions.ptr(label.InsideMiddleCenter.String())
		}
		if float64(obj.LabelDimensions.Width) > obj.Width || float64(obj.LabelDimensions.Height) > obj.Height {
			if len(obj.ChildrenArray) > 0 {
				obj.LabelPosition = positions.ptr(label.OutsideTopCenter.String())
			} else {
				obj.LabelPosition = positions.ptr(label.OutsideBottomCenter.String())
			}
		}
	}
}

// shift everything down by distance if it is at or below start position
func shiftDown(g *d2graph.Graph, start, distance float64, isHorizontal bool) {
	if isHorizontal {
		for _, edge := range g.Edges {
			first, last := edge.Route[0], edge.Route[len(edge.Route)-1]
			if start <= first.X {
				onStaticSrc := first.X == edge.Src.TopLeft.X+edge.Src.Width && edge.Src.TopLeft.X < start
				if !onStaticSrc {
					// src is not shifting and we are on src so don't shift
					first.X += distance
				}
			}
			if start <= last.X {
				onStaticDst := last.X == edge.Dst.TopLeft.X+edge.Dst.Width && edge.Dst.TopLeft.X < start
				if !onStaticDst {
					last.X += distance
				}
			}
			for i := 1; i < len(edge.Route)-1; i++ {
				p := edge.Route[i]
				if p.X < start {
					continue
				}
				p.X += distance
			}
		}
		for _, obj := range g.Objects {
			if obj.TopLeft.X < start {
				continue
			}
			obj.TopLeft.X += distance
		}
	} else {
		for _, edge := range g.Edges {
			first, last := edge.Route[0], edge.Route[len(edge.Route)-1]
			if start <= first.Y {
				onStaticSrc := first.Y == edge.Src.TopLeft.Y+edge.Src.Height && edge.Src.TopLeft.Y < start
				if !onStaticSrc {
					// src is not shifting and we are on src so don't shift
					first.Y += distance
				}
			}
			if start <= last.Y {
				onStaticDst := last.Y == edge.Dst.TopLeft.Y+edge.Dst.Height && edge.Dst.TopLeft.Y < start
				if !onStaticDst {
					last.Y += distance
				}
			}
			for i := 1; i < len(edge.Route)-1; i++ {
				p := edge.Route[i]
				if p.Y < start {
					continue
				}
				p.Y += distance
			}
		}
		for _, obj := range g.Objects {
			if obj.TopLeft.Y < start {
				continue
			}
			obj.TopLeft.Y += distance
		}
	}
}

func shiftUp(g *d2graph.Graph, start, distance float64, isHorizontal bool) {
	if isHorizontal {
		for _, edge := range g.Edges {
			first, last := edge.Route[0], edge.Route[len(edge.Route)-1]
			if first.X <= start {
				onStaticSrc := first.X == edge.Src.TopLeft.X && start < edge.Src.TopLeft.X+edge.Src.Width
				if !onStaticSrc {
					// src is not shifting and we are on src so don't shift
					first.X -= distance
				}
			}
			if last.X <= start {
				onStaticDst := last.X == edge.Dst.TopLeft.X && start < edge.Dst.TopLeft.X+edge.Dst.Width
				if !onStaticDst {
					last.X -= distance
				}
			}
			for i := 1; i < len(edge.Route)-1; i++ {
				p := edge.Route[i]
				if start < p.X {
					continue
				}
				p.X -= distance
			}
		}
		for _, obj := range g.Objects {
			if start < obj.TopLeft.X {
				continue
			}
			obj.TopLeft.X -= distance
		}
	} else {
		for _, edge := range g.Edges {
			first, last := edge.Route[0], edge.Route[len(edge.Route)-1]
			if first.Y <= start {
				// don't shift first edge point if src is not shifting and we are on src
				onStaticSrc := first.Y == edge.Src.TopLeft.Y && start < edge.Src.TopLeft.Y+edge.Src.Height
				if !onStaticSrc {
					first.Y -= distance
				}
			}
			if last.Y <= start {
				onStaticDst := last.Y == edge.Dst.TopLeft.Y && start < edge.Dst.TopLeft.Y
				if !onStaticDst {
					last.Y -= distance
				}
			}
			for i := 1; i < len(edge.Route)-1; i++ {
				p := edge.Route[i]
				// for _, p := range edge.Route {
				if start < p.Y {
					continue
				}
				p.Y -= distance
			}
		}
		for _, obj := range g.Objects {
			if start < obj.TopLeft.Y {
				continue
			}
			obj.TopLeft.Y -= distance
		}
	}
}

func fitContainerPadding(g *d2graph.Graph, ix *objectIndex, rankSep float64, isHorizontal bool) {
	// The children of an indexed object are its ChildrenArray, in order.
	for _, obj := range ix.children(ix.root) {
		fitPadding(ix, obj)
	}
}

func fitPadding(ix *objectIndex, k int32) {
	obj := ix.objs[k]
	// Note: there's no shape-specific padding/placement in dagre yet
	// (Both tests are side-effect free; the cheap one runs first.)
	if !obj.IsContainer() {
		return
	}
	dslShape := strings.ToLower(obj.Shape.Value)
	shapeType := d2target.DSL_SHAPE_TO_SHAPE_TYPE[dslShape]
	if shapeType != shape.SQUARE_TYPE {
		return
	}
	for _, child := range ix.children(k) {
		fitPadding(ix, child)
	}

	// we will compute a perfectly fit innerBox merging our padding with children's margin,
	// but we need to add padding and margin together if an outside child label will overlap with our inside label
	_, padding := ix.spacing(k)
	padding.Top = math.Max(padding.Top, DEFAULT_PADDING)
	padding.Bottom = math.Max(padding.Bottom, DEFAULT_PADDING)
	padding.Left = math.Max(padding.Left, DEFAULT_PADDING)
	padding.Right = math.Max(padding.Right, DEFAULT_PADDING)

	// where we are (current*) vs where we want to fit each side to (inner*)
	currentTop := obj.TopLeft.Y
	currentBottom := obj.TopLeft.Y + obj.Height
	currentLeft := obj.TopLeft.X
	currentRight := obj.TopLeft.X + obj.Width

	innerTop := math.Inf(1)
	innerBottom := math.Inf(-1)
	innerLeft := math.Inf(1)
	innerRight := math.Inf(-1)

	// we create boxes for our inside label and icon, and will check against overlaps with any internal boxes
	var labelPosition, iconPosition label.Position
	var labelBox, iconBox *geo.Box
	if obj.HasLabel() && obj.LabelPosition != nil {
		labelPosition = label.FromString(*obj.LabelPosition)
		switch labelPosition {
		case label.InsideTopLeft, label.InsideTopCenter, label.InsideTopRight,
			label.InsideBottomLeft, label.InsideBottomCenter, label.InsideBottomRight,
			label.InsideMiddleLeft, label.InsideMiddleRight:
			labelTL := obj.GetLabelTopLeft()
			if labelTL != nil {
				labelBox = geo.NewBox(labelTL, float64(obj.LabelDimensions.Width)+2*label.PADDING, float64(obj.LabelDimensions.Height))
			}
		}
	}
	if obj.HasIcon() && obj.IconPosition != nil {
		iconPosition = label.FromString(*obj.IconPosition)
		switch iconPosition {
		case label.InsideTopLeft, label.InsideTopCenter, label.InsideTopRight,
			label.InsideBottomLeft, label.InsideBottomCenter, label.InsideBottomRight,
			label.InsideMiddleLeft, label.InsideMiddleRight:
			iconTL := obj.GetIconTopLeft()
			if iconTL != nil {
				iconBox = geo.NewBox(iconTL, d2target.MAX_ICON_SIZE, d2target.MAX_ICON_SIZE)
			}
		}
	}

	// update the inner positions for children's margin and collect the outside boxes that we cannot overlap with
	var innerBoxes []geo.Box
	for _, c := range ix.children(k) {
		child := ix.objs[c]
		margin, _ := ix.spacing(c)
		dx, dy := child.GetModifierElementAdjustments()

		if labelBox != nil || iconBox != nil {
			var childLabelBox *geo.Box
			var childLabelPosition, childIconPosition label.Position
			if child.HasLabel() && child.LabelPosition != nil {
				childLabelPosition = label.FromString(*child.LabelPosition)
				if childLabelPosition.IsOutside() {
					childLabelTL := child.GetLabelTopLeft()

					childLabelBox = geo.NewBox(
						childLabelTL,
						float64(child.LabelDimensions.Width),
						float64(child.LabelDimensions.Height),
					)
					innerBoxes = append(innerBoxes, *childLabelBox)
				}
			}
			if child.HasIcon() && child.IconPosition != nil {
				childIconPosition = label.FromString(*child.IconPosition)
				if childIconPosition.IsOutside() {
					childIconTL := child.GetIconTopLeft()

					childIconBox := geo.NewBox(childIconTL, d2target.MAX_ICON_SIZE, d2target.MAX_ICON_SIZE)
					innerBoxes = append(innerBoxes, *childIconBox)
				}
			}
		}

		innerTop = math.Min(innerTop, child.TopLeft.Y-dy-math.Max(margin.Top, padding.Top))
		innerBottom = math.Max(innerBottom, child.TopLeft.Y+child.Height+math.Max(margin.Bottom, padding.Bottom))
		innerLeft = math.Min(innerLeft, child.TopLeft.X-math.Max(margin.Left, padding.Left))
		innerRight = math.Max(innerRight, child.TopLeft.X+child.Width+dx+math.Max(margin.Right, padding.Right))
	}

	// collect edge label boxes and update inner box for internal edges
	for _, edge := range obj.Graph.Edges {
		if !edge.Src.IsDescendantOf(obj) || !edge.Dst.IsDescendantOf(obj) {
			continue
		}
		// check internal edge + their labels
		if edge.Label.Value != "" {
			labelPosition := label.InsideMiddleCenter
			if edge.LabelPosition != nil {
				labelPosition = label.FromString(*edge.LabelPosition)
			}
			labelWidth := float64(edge.LabelDimensions.Width)
			labelHeight := float64(edge.LabelDimensions.Height)
			point, _ := labelPosition.GetPointOnRoute(edge.Route, 2, 0, labelWidth, labelHeight)

			if labelBox != nil || iconBox != nil {
				innerBoxes = append(innerBoxes, geo.Box{TopLeft: point, Width: labelWidth, Height: labelHeight})
			}

			innerTop = math.Min(innerTop, point.Y-padding.Top)
			innerBottom = math.Max(innerBottom, point.Y+labelHeight+padding.Bottom)
			innerLeft = math.Min(innerLeft, point.X-padding.Left)
			innerRight = math.Max(innerRight, point.X+labelWidth+padding.Right)
		}
		for _, point := range edge.Route {
			innerTop = math.Min(innerTop, point.Y-padding.Top)
			innerBottom = math.Max(innerBottom, point.Y+padding.Bottom)
			innerLeft = math.Min(innerLeft, point.X-padding.Left)
			innerRight = math.Max(innerRight, point.X+padding.Right)
		}
	}

	// how much do we need to shrink each side
	topDelta := innerTop - currentTop
	bottomDelta := currentBottom - innerBottom
	leftDelta := innerLeft - currentLeft
	rightDelta := currentRight - innerRight

	if topDelta > 0 || bottomDelta > 0 || leftDelta > 0 || rightDelta > 0 {
		var leftOverlap, rightOverlap, topOverlap, bottomOverlap float64
		var labelSide, iconSide geo.Orientation
		if labelBox != nil {
			switch labelPosition {
			case label.InsideTopLeft, label.InsideTopCenter, label.InsideTopRight:
				labelSide = geo.Top
			case label.InsideBottomLeft, label.InsideBottomCenter, label.InsideBottomRight:
				labelSide = geo.Bottom
			case label.InsideMiddleLeft:
				labelSide = geo.Left
			case label.InsideMiddleRight:
				labelSide = geo.Right
			default:
				labelSide = geo.NONE
			}
			// move labelBox to its position with the merged delta and check for overlaps
			switch labelSide {
			case geo.Top:
				if topDelta > 0 {
					labelBox.TopLeft.Y += topDelta
				}
			case geo.Bottom:
				if bottomDelta > 0 {
					labelBox.TopLeft.Y -= bottomDelta
				}
			case geo.Left:
				if leftDelta > 0 {
					labelBox.TopLeft.X += leftDelta
				}
			case geo.Right:
				if rightDelta > 0 {
					labelBox.TopLeft.X -= rightDelta
				}
			}
			switch labelSide {
			case geo.Top:
				if topDelta > 0 {
					for _, box := range innerBoxes {
						if labelBox.Overlaps(box) {
							dy := labelBox.TopLeft.Y + labelBox.Height - box.TopLeft.Y
							topOverlap = go2.Max(topOverlap, dy)
						}
					}
				}
			case geo.Bottom:
				if bottomDelta > 0 {
					for _, box := range innerBoxes {
						if labelBox.Overlaps(box) {
							dy := box.TopLeft.Y + box.Height - labelBox.TopLeft.Y
							bottomOverlap = go2.Max(bottomOverlap, dy)
						}
					}
				}
			case geo.Left:
				if leftDelta > 0 {
					for _, box := range innerBoxes {
						if labelBox.Overlaps(box) {
							dx := labelBox.TopLeft.X + labelBox.Width - box.TopLeft.X
							leftOverlap = go2.Max(leftOverlap, dx)
						}
					}
				}
			case geo.Right:
				if rightDelta > 0 {
					for _, box := range innerBoxes {
						if labelBox.Overlaps(box) {
							dx := box.TopLeft.X + box.Width - labelBox.TopLeft.X
							rightOverlap = go2.Max(rightOverlap, dx)
						}
					}
				}
			}
		}
		if iconBox != nil {
			switch iconPosition {
			case label.InsideTopLeft, label.InsideTopCenter, label.InsideTopRight:
				iconSide = geo.Top
			case label.InsideBottomLeft, label.InsideBottomCenter, label.InsideBottomRight:
				iconSide = geo.Bottom
			case label.InsideMiddleLeft:
				iconSide = geo.Left
			case label.InsideMiddleRight:
				iconSide = geo.Right
			default:
				iconSide = geo.NONE
			}
			// move iconBox to its position with the merged delta and check for overlaps
			switch iconSide {
			case geo.Top:
				if topDelta > 0 {
					iconBox.TopLeft.Y += topDelta
				}
			case geo.Bottom:
				if bottomDelta > 0 {
					iconBox.TopLeft.Y -= bottomDelta
				}
			case geo.Left:
				if leftDelta > 0 {
					iconBox.TopLeft.X += leftDelta
				}
			case geo.Right:
				if rightDelta > 0 {
					iconBox.TopLeft.X -= rightDelta
				}
			}
			switch iconSide {
			case geo.Top:
				if topDelta > 0 {
					for _, box := range innerBoxes {
						if iconBox.Overlaps(box) {
							dy := iconBox.TopLeft.Y + iconBox.Height - box.TopLeft.Y
							topOverlap = go2.Max(topOverlap, dy)
						}
					}
				}
			case geo.Bottom:
				if bottomDelta > 0 {
					for _, box := range innerBoxes {
						if iconBox.Overlaps(box) {
							dy := box.TopLeft.Y + box.Height - iconBox.TopLeft.Y
							bottomOverlap = go2.Max(bottomOverlap, dy)
						}
					}
				}
			case geo.Left:
				if leftDelta > 0 {
					for _, box := range innerBoxes {
						if iconBox.Overlaps(box) {
							dx := iconBox.TopLeft.X + iconBox.Width - box.TopLeft.X
							leftOverlap = go2.Max(leftOverlap, dx)
						}
					}
				}
			case geo.Right:
				if rightDelta > 0 {
					for _, box := range innerBoxes {
						if iconBox.Overlaps(box) {
							dx := box.TopLeft.X + box.Width - iconBox.TopLeft.X
							rightOverlap = go2.Max(rightOverlap, dx)
						}
					}
				}
			}
		}

		if leftOverlap > 0 {
			leftDelta -= leftOverlap + MIN_SPACING
		}
		if rightOverlap > 0 {
			rightDelta -= rightOverlap + MIN_SPACING
		}
		if topOverlap > 0 {
			topDelta -= topOverlap + MIN_SPACING
		}
		if bottomOverlap > 0 {
			bottomDelta -= bottomOverlap + MIN_SPACING
		}
	}

	if 0 < topDelta {
		topDelta = adjustDeltaForEdges(obj, currentTop, topDelta, false)
		if 0 < topDelta {
			adjustEdges(obj, currentTop, topDelta, false)
			obj.TopLeft.Y += topDelta
			obj.Height -= topDelta
		}
	}
	if 0 < bottomDelta {
		bottomDelta = adjustDeltaForEdges(obj, currentBottom, -bottomDelta, false)
		if 0 < bottomDelta {
			adjustEdges(obj, currentBottom, -bottomDelta, false)
			obj.Height -= bottomDelta
		}
	}
	if 0 < leftDelta {
		leftDelta = adjustDeltaForEdges(obj, currentLeft, leftDelta, true)
		if 0 < leftDelta {
			adjustEdges(obj, currentLeft, leftDelta, true)
			obj.TopLeft.X += leftDelta
			obj.Width -= leftDelta
		}
	}
	if 0 < rightDelta {
		rightDelta = adjustDeltaForEdges(obj, currentRight, -rightDelta, true)
		if 0 < rightDelta {
			adjustEdges(obj, currentRight, -rightDelta, true)
			obj.Width -= rightDelta
		}
	}
}

func adjustDeltaForEdges(obj *d2graph.Object, objPosition, delta float64, isHorizontal bool) (newMagnitude float64) {
	isOnCollapsingSide := func(p *geo.Point) bool {
		var position float64
		if isHorizontal {
			position = p.X
		} else {
			position = p.Y
		}
		if geo.PrecisionCompare(position, objPosition, 1) == 0 {
			return false
		}
		// check for edges on side corners
		var isOnSide bool
		if isHorizontal {
			if geo.PrecisionCompare(p.Y, obj.TopLeft.Y, 1) == 0 ||
				geo.PrecisionCompare(p.Y, obj.TopLeft.Y+obj.Height, 1) == 0 {
				isOnSide = true
			}
		} else {
			if geo.PrecisionCompare(p.X, obj.TopLeft.X, 1) == 0 ||
				geo.PrecisionCompare(p.X, obj.TopLeft.X+obj.Width, 1) == 0 {
				isOnSide = true
			}
		}
		if !isOnSide {
			return false
		}
		buffer := MIN_SPACING
		var isInRange bool
		if delta > 0 {
			if objPosition <= position && position <= objPosition+delta+buffer {
				isInRange = true
			}
		} else {
			if objPosition+delta-buffer <= position && position <= objPosition {
				isInRange = true
			}
		}
		return isInRange
	}
	hasEdgeOnCollapsingSide := false
	outermost := objPosition + delta
	for _, edge := range obj.Graph.Edges {
		if edge.Src == obj {
			p := edge.Route[0]
			if isOnCollapsingSide(p) {
				hasEdgeOnCollapsingSide = true
				var position float64
				if isHorizontal {
					position = p.X
				} else {
					position = p.Y
				}
				if delta < 0 {
					outermost = math.Max(outermost, position)
				} else {
					outermost = math.Min(outermost, position)
				}
			}
		}
		if edge.Dst == obj {
			p := edge.Route[len(edge.Route)-1]
			if isOnCollapsingSide(p) {
				hasEdgeOnCollapsingSide = true
				var position float64
				if isHorizontal {
					position = p.X
				} else {
					position = p.Y
				}
				if delta < 0 {
					outermost = math.Max(outermost, position)
				} else {
					outermost = math.Min(outermost, position)
				}
			}
		}
	}
	newMagnitude = math.Abs(delta)
	if hasEdgeOnCollapsingSide {
		// only reduce to outermost + DEFAULT_PADDING
		if delta < 0 {
			newMagnitude = math.Max(0, objPosition-(outermost+DEFAULT_PADDING))
		} else {
			newMagnitude = math.Max(0, (outermost-DEFAULT_PADDING)-objPosition)
		}
	}
	return newMagnitude
}

func adjustEdges(obj *d2graph.Object, objPosition, delta float64, isHorizontal bool) {
	adjust := func(p *geo.Point) {
		var position float64
		if isHorizontal {
			position = p.X
		} else {
			position = p.Y
		}
		if geo.PrecisionCompare(position, objPosition, 1) == 0 {
			if isHorizontal {
				p.X += delta
			} else {
				p.Y += delta
			}
		} else {
			// check side corners
			var isOnSide bool
			if isHorizontal {
				if geo.PrecisionCompare(p.Y, obj.TopLeft.Y, 1) == 0 ||
					geo.PrecisionCompare(p.Y, obj.TopLeft.Y+obj.Height, 1) == 0 {
					isOnSide = true
				}
			} else {
				if geo.PrecisionCompare(p.X, obj.TopLeft.X, 1) == 0 ||
					geo.PrecisionCompare(p.X, obj.TopLeft.X+obj.Width, 1) == 0 {
					isOnSide = true
				}
			}
			if isOnSide {
				var isInRange bool
				if delta > 0 {
					if objPosition < position && position < objPosition+delta {
						isInRange = true
					}
				} else {
					if objPosition+delta < position && position < objPosition {
						isInRange = true
					}
				}
				if isInRange {
					if isHorizontal {
						p.X = objPosition + delta
					} else {
						p.Y = objPosition + delta
					}
				}
			}
		}
	}

	for _, edge := range obj.Graph.Edges {
		if edge.Src == obj {
			adjust(edge.Route[0])
		}
		if edge.Dst == obj {
			adjust(edge.Route[len(edge.Route)-1])
		}
	}
}
