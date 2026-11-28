package d2lib

import (
	"math"
	"slices"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2sequence"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/shape"
)

// Keep routes in the same coordinate system as the moving cells. Straight
// links track their shape or table-row ports. Routed links retain their bends.
type gridMotionEdges struct {
	pairs   map[string]*gridEdgePair
	removed []*gridEdgePair
}

type gridEdgeLayout struct {
	edge     *d2target.Connection
	src, dst *d2target.Shape
	straight bool
	// Relative heights of named row ports. Ordinary shape ports use -1.
	srcRow, dstRow float64
}

type gridEdgePair struct {
	before, after gridEdgeLayout
	dstAnchor     string
}

func newGridMotionEdges(from, to *d2target.Diagram, fromGraph, toGraph *d2graph.Graph, moving map[string]bool) *gridMotionEdges {
	m := &gridMotionEdges{pairs: make(map[string]*gridEdgePair)}
	graphs := [2]*d2graph.Graph{fromGraph, toGraph}
	for i, board := range []*d2target.Diagram{from, to} {
		if len(board.Connections) == 0 {
			continue
		}
		// Lifelines have a synthetic destination with no rendered shape. Anchor
		// both route ports to their own actor, including during a subtree pop.
		lifelines := make(map[string]bool)
		rowEdges := make(map[string]*d2graph.Edge)
		if graph := graphs[i]; graph != nil {
			for _, edge := range graph.Edges {
				if edge.SrcTableColumnIndex != nil || edge.DstTableColumnIndex != nil {
					rowEdges[edge.AbsID()] = edge
				}
				if d2sequence.IsLifelineEnd(edge.Dst) {
					lifelines[edge.AbsID()] = true
				}
			}
		}
		shapes := gridEdgeShapeMap(board.Shapes)
		for j := range board.Connections {
			e := &board.Connections[j]
			if !moving[e.Src] && !moving[e.Dst] {
				continue
			}
			pair := m.pairs[e.ID]
			if pair == nil {
				pair = &gridEdgePair{dstAnchor: e.Dst}
				if lifelines[e.ID] {
					pair.dstAnchor = e.Src
				}
				m.pairs[e.ID] = pair
			}
			// A custom layout can return an incomplete route. Let normal
			// render-target validation report it on the authored endpoint.
			if len(e.Route) < 2 || e.IsCurve && (len(e.Route)-1)%3 != 0 || slices.Contains(e.Route, nil) {
				continue
			}
			layout := gridEdgeLayout{edge: e, src: shapes[e.Src], dst: shapes[pair.dstAnchor], srcRow: -1, dstRow: -1}
			if row := rowEdges[e.ID]; row != nil {
				if row.SrcTableColumnIndex != nil && layout.src != nil && layout.src.Height > 0 {
					layout.srcRow = (e.Route[0].Y - layout.src.RenderedPosition().Y) / float64(layout.src.Height)
				}
				if row.DstTableColumnIndex != nil && layout.dst != nil && layout.dst.Height > 0 {
					layout.dstRow = (e.Route[len(e.Route)-1].Y - layout.dst.RenderedPosition().Y) / float64(layout.dst.Height)
				}
			}
			layout.straight = layout.hasStraightPorts()
			if i == 0 {
				pair.before = layout
			} else {
				pair.after = layout
			}
		}
	}
	for _, e := range from.Connections {
		if pair := m.pairs[e.ID]; pair != nil && pair.before.edge != nil && pair.after.edge == nil {
			m.removed = append(m.removed, pair)
		}
	}
	return m
}

func gridEdgeShapeMap(shapes []d2target.Shape) map[string]*d2target.Shape {
	index := make(map[string]*d2target.Shape, len(shapes))
	for i := range shapes {
		index[shapes[i].ID] = &shapes[i]
	}
	return index
}

func (m *gridMotionEdges) sample(base []d2target.Connection, shapes []d2target.Shape, travel, enter, exit float64, visibility map[string]float64) []d2target.Connection {
	if len(m.pairs) == 0 {
		return base
	}
	alpha := func(e *d2target.Connection, dstAnchor string, fallback float64) float64 {
		a, src := visibility[e.Src]
		b, dst := visibility[dstAnchor]
		switch {
		case src && dst:
			return min(a, b)
		case src:
			return a
		case dst:
			return b
		default:
			return fallback
		}
	}
	current := gridEdgeShapeMap(shapes)
	result := make([]d2target.Connection, 0, len(base)+len(m.removed))
	for _, e := range base {
		pair := m.pairs[e.ID]
		if pair == nil {
			result = append(result, e)
			continue
		}
		if pair.after.edge == nil { // Added below in the old board's order.
			continue
		}
		if pair.before.edge == nil {
			e.Opacity *= alpha(&e, pair.dstAnchor, enter)
		}
		if pair.sample(&e, current, travel) {
			result = append(result, e)
		}
	}
	if exit > 0 || len(visibility) != 0 {
		for _, pair := range m.removed {
			e := *pair.before.edge
			e.Opacity *= alpha(&e, pair.dstAnchor, exit)
			if pair.sample(&e, current, travel) {
				result = append(result, e)
			}
		}
	}
	return result
}

// A link between details shares their growing viewport. For an external link,
// expose the route only as its attachment comes into view.
func gridEdgeRevealed(e *d2target.Connection, src, dst *d2target.MotionClip) bool {
	if src == nil && dst == nil {
		return true
	}
	if src != nil && dst != nil && *src == *dst {
		e.MotionClip = src
		return true
	}
	portAlpha := func(c *d2target.MotionClip, p *geo.Point) float64 {
		if c == nil {
			return 1
		}
		x, y := p.X-float64(c.X), p.Y-float64(c.Y)
		return gridEase(min(1, max(0, min(x, y, float64(c.Width)-x, float64(c.Height)-y)/5)))
	}
	e.Opacity *= min(portAlpha(src, e.Route[0]), portAlpha(dst, e.Route[len(e.Route)-1]))
	return e.Opacity > 0
}

// Infer direct routing only when we can reproduce both authored endpoints.
// Labels, off-center lanes and custom routers may use a different attachment.
func (l gridEdgeLayout) hasStraightPorts() bool {
	if l.src == nil || l.dst == nil || l.src == l.dst || l.edge.IsCurve || len(l.edge.Route) != 2 {
		return false
	}
	a, b := straightGridPorts(l.src, l.dst, l.srcRow, l.dstRow)
	start, end := l.edge.Route[0], l.edge.Route[1]
	return a != nil && math.Hypot(a.X-start.X, a.Y-start.Y) <= 1.5 && math.Hypot(b.X-end.X, b.Y-end.Y) <= 1.5
}

// When a detour switches table faces, interpolating the two sets of bends
// pulls the row attachment through the table. Use the detour on the side
// currently occupied by the peer, moving its ports with their shapes.
func (l gridEdgeLayout) switchesRowFace(next gridEdgeLayout) bool {
	if l.srcRow >= 0 && next.srcRow >= 0 {
		a := l.edge.Route[0].X - gridEdgeCenter(l.src).X
		b := next.edge.Route[0].X - gridEdgeCenter(next.src).X
		if a*b < 0 {
			return true
		}
	}
	if l.dstRow >= 0 && next.dstRow >= 0 {
		a := l.edge.Route[len(l.edge.Route)-1].X - gridEdgeCenter(l.dst).X
		b := next.edge.Route[len(next.edge.Route)-1].X - gridEdgeCenter(next.dst).X
		return a*b < 0
	}
	return false
}

// Keep bends in the gap between the two moving faces. Points alongside a
// shape follow its horizontal movement and resize, including its row ports.
func (l gridEdgeLayout) rowGapTransform(src, dst *d2target.Shape) (mapX func(float64) float64, visible bool) {
	left, right := l.src, l.dst
	if left.RenderedPosition().X > right.RenderedPosition().X {
		left, right, src, dst = right, left, dst, src
	}
	lo, hi := left.RenderedPosition().X+float64(left.Width), right.RenderedPosition().X
	a, b := src.RenderedPosition(), dst.RenderedPosition()
	newLo, newHi := a.X+float64(src.Width), b.X
	if lo >= hi || left.Width <= 0 || right.Width <= 0 {
		return nil, true
	}
	if newLo >= newHi {
		// A row-to-row detour through this gap is buried while the cells cross.
		return nil, a.Y+float64(src.Height) <= b.Y || b.Y+float64(dst.Height) <= a.Y
	}
	return func(x float64) float64 {
		switch {
		case x < lo:
			return newLo + (x-lo)*float64(src.Width)/float64(left.Width)
		case x > hi:
			return newHi + (x-hi)*float64(dst.Width)/float64(right.Width)
		default:
			return newLo + (x-lo)*(newHi-newLo)/(hi-lo)
		}
	}, true
}

// Row ports stay on the facing table sides as cells pass each other. A regular
// shape faces the other endpoint, which can be a row above or below its center.
func straightGridPorts(src, dst *d2target.Shape, srcRow, dstRow float64) (start, end *geo.Point) {
	a, b := gridEdgeCenter(src), gridEdgeCenter(dst)
	facing := b.X - a.X
	if srcRow >= 0 {
		pos := src.RenderedPosition()
		a.X, a.Y = pos.X, pos.Y+srcRow*float64(src.Height)
		if facing > 0 {
			a.X += float64(src.Width)
		}
	}
	if dstRow >= 0 {
		pos := dst.RenderedPosition()
		b.X, b.Y = pos.X, pos.Y+dstRow*float64(dst.Height)
		if facing < 0 {
			b.X += float64(dst.Width)
		}
	}
	dx, dy := b.X-a.X, b.Y-a.Y
	if dx == 0 && dy == 0 || (srcRow >= 0 || dstRow >= 0) && dx*facing <= 0 {
		return nil, nil
	}
	start, end = &a, &b
	if srcRow < 0 {
		start = gridEdgeBorder(src, dx, dy)
	}
	if dstRow < 0 {
		end = gridEdgeBorder(dst, -dx, -dy)
	}
	if (end.X-start.X)*dx+(end.Y-start.Y)*dy <= 0 {
		return nil, nil // The outlines meet or overlap along this segment.
	}
	return start, end
}

// While the horizontal projections overlap, a row port can still reach a
// table in a different row through the clear strip between their boxes.
func gridRowCrossingRoute(src, dst *d2target.Shape, srcRow, dstRow float64) []*geo.Point {
	a, b := src.RenderedPosition(), dst.RenderedPosition()
	if srcRow < 0 && dstRow < 0 || a.X+float64(src.Width) < b.X || b.X+float64(dst.Width) < a.X {
		return nil
	}
	gapTop := min(a.Y+float64(src.Height), b.Y+float64(dst.Height))
	gapBottom := max(a.Y, b.Y)
	if gapTop >= gapBottom {
		return nil
	}
	midY := (gapTop + gapBottom) / 2
	lead := min(12., (gapBottom-gapTop)/2)
	leg := func(s *d2target.Shape, row, side float64) []*geo.Point {
		c := gridEdgeCenter(s)
		if row < 0 {
			port := gridEdgeBorder(s, 0, midY-c.Y)
			return []*geo.Point{port, geo.NewPoint(port.X, midY)}
		}
		port := geo.NewPoint(c.X+side*float64(s.Width)/2, s.RenderedPosition().Y+row*float64(s.Height))
		x := port.X + side*lead
		return []*geo.Point{port, geo.NewPoint(x, port.Y), geo.NewPoint(x, midY)}
	}
	side := 1.
	if gridEdgeCenter(src).X > gridEdgeCenter(dst).X {
		side = -1
	}
	first, last := leg(src, srcRow, side), leg(dst, dstRow, -side)
	slices.Reverse(last)
	if first[len(first)-1].Equals(last[0]) {
		last = last[1:]
	}
	return append(first, last...)
}

func gridEdgeCenter(s *d2target.Shape) geo.Point {
	p := s.RenderedPosition()
	return geo.Point{X: p.X + float64(s.Width)/2, Y: p.Y + float64(s.Height)/2}
}

func gridEdgeBorder(s *d2target.Shape, dx, dy float64) *geo.Point {
	c := gridEdgeCenter(s)
	if s.Width <= 0 || s.Height <= 0 {
		return &c
	}
	ratio := 0.5 / max(math.Abs(dx)/float64(s.Width), math.Abs(dy)/float64(s.Height))
	border := geo.NewPoint(c.X+dx*ratio, c.Y+dy*ratio)
	box := geo.NewBox(geo.NewPoint(float64(s.Pos.X), float64(s.Pos.Y)), float64(s.Width), float64(s.Height))
	outline := shape.NewShape(d2target.DSL_SHAPE_TO_SHAPE_TYPE[s.Type], box)
	// Start beyond the rectangular border even when the cell boxes overlap.
	far := geo.NewPoint(c.X+dx*2*ratio, c.Y+dy*2*ratio)
	if offset := s.MotionOffset; offset != nil {
		border.X, border.Y = border.X-offset.X, border.Y-offset.Y
		far.X, far.Y = far.X-offset.X, far.Y-offset.Y
		port := shape.TraceToShapeBorder(outline, border, far)
		return geo.NewPoint(port.X+offset.X, port.Y+offset.Y)
	}
	return shape.TraceToShapeBorder(outline, border, far)
}

func (pair *gridEdgePair) sample(e *d2target.Connection, shapes map[string]*d2target.Shape, t float64) bool {
	src, dst := shapes[e.Src], shapes[pair.dstAnchor]
	return src != nil && dst != nil && e.Opacity > 0 && pair.route(e, src, dst, t) && gridEdgeRevealed(e, src.MotionClip, dst.MotionClip)
}

func (pair *gridEdgePair) route(e *d2target.Connection, src, dst *d2target.Shape, t float64) bool {
	old, next := pair.before, pair.after
	if old.edge == nil || t == 1 {
		old = next
	}
	if next.edge == nil || t == 0 {
		next = old
	}
	if old.edge == nil || next.edge == nil || old.src == nil || old.dst == nil || next.src == nil || next.dst == nil {
		return false
	}
	// Custom layout engines can return an incomplete route. Let the normal
	// render-target validation report the endpoint instead of indexing it here.
	for _, layout := range []gridEdgeLayout{old, next} {
		n := len(layout.edge.Route)
		if n < 2 || (layout.edge.IsCurve && (n-1)%3 != 0) {
			return true
		}
	}
	if old.edge == next.edge && sameSlot(src, old.src) && sameSlot(dst, old.dst) {
		e.IsCurve, e.Route = old.edge.IsCurve, old.edge.Route
		return true
	}
	samePorts := (old.srcRow >= 0) == (next.srcRow >= 0) && (old.dstRow >= 0) == (next.dstRow >= 0)
	straight := old.straight && next.straight && samePorts
	switches := old.edge != next.edge && old.switchesRowFace(next)
	if straight || switches && samePorts {
		srcRow := old.srcRow + (next.srcRow-old.srcRow)*t
		dstRow := old.dstRow + (next.dstRow-old.dstRow)*t
		if straight {
			if start, end := straightGridPorts(src, dst, srcRow, dstRow); start != nil {
				e.Route, e.IsCurve = []*geo.Point{start, end}, false
				return true
			}
		}
		if crossing := gridRowCrossingRoute(src, dst, srcRow, dstRow); crossing != nil {
			e.Route, e.IsCurve = crossing, false
			return true
		}
		if straight {
			return false
		}
	}
	var mapRowX func(float64) float64
	if switches {
		before := gridEdgeCenter(old.dst).X - gridEdgeCenter(old.src).X
		after := gridEdgeCenter(next.dst).X - gridEdgeCenter(next.src).X
		useBefore := t < .5
		if before*after < 0 {
			useBefore = before*(gridEdgeCenter(dst).X-gridEdgeCenter(src).X) > 0
		}
		if useBefore {
			next = old
		} else {
			old = next
		}
		var visible bool
		mapRowX, visible = old.rowGapTransform(src, dst)
		if !visible {
			return false
		}
	}
	// Route points are authored in absolute coordinates. Move each port to
	// its relative location on the current cell, distributing the correction
	// along the route so intermediate bends and their labels travel with it.
	oldRoute, newRoute := old.edge.Route, next.edge.Route
	compatible := len(oldRoute) == len(newRoute) && old.edge.IsCurve == next.edge.IsCurve
	e.IsCurve = compatible && old.edge.IsCurve
	oldSrc, oldDst := old.portShifts(src, dst)
	newSrc, newDst := next.portShifts(src, dst)
	n := max(len(oldRoute), len(newRoute))
	points := make([]*geo.Point, 0, n)
	for i := 0; i < n; i++ {
		f := float64(i) / float64(n-1)
		a, b := old.pointAt(i, f, compatible), next.pointAt(i, f, compatible)
		a.X += oldSrc.X + (oldDst.X-oldSrc.X)*f
		a.Y += oldSrc.Y + (oldDst.Y-oldSrc.Y)*f
		b.X += newSrc.X + (newDst.X-newSrc.X)*f
		b.Y += newSrc.Y + (newDst.Y-newSrc.Y)*f
		p := a.Interpolate(&b, t)
		if mapRowX != nil {
			p.X = mapRowX(oldRoute[i].X)
		}
		if len(points) > 0 && p.Equals(points[len(points)-1]) {
			// An edge clipped to a shared point has no visible segment.
			// Cubics with a collapsed control leg use the remaining polyline.
			e.IsCurve = false
			continue
		}
		points = append(points, p)
	}
	e.Route = points
	return len(points) >= 2
}

func (l gridEdgeLayout) portShifts(src, dst *d2target.Shape) (geo.Point, geo.Point) {
	shift := func(p *geo.Point, old, current *d2target.Shape) geo.Point {
		before, after := old.RenderedPosition(), current.RenderedPosition()
		dx, dy := after.X-before.X, after.Y-before.Y
		if old.Width > 0 {
			dx += (p.X - before.X) * float64(current.Width-old.Width) / float64(old.Width)
		}
		if old.Height > 0 {
			dy += (p.Y - before.Y) * float64(current.Height-old.Height) / float64(old.Height)
		}
		return geo.Point{X: dx, Y: dy}
	}
	return shift(l.edge.Route[0], l.src, src), shift(l.edge.Route[len(l.edge.Route)-1], l.dst, dst)
}

// Different route topologies share a parameter space only for the transition.
// Evaluate cubics as curves, not as polylines through their control points.
func (l gridEdgeLayout) pointAt(i int, f float64, compatible bool) geo.Point {
	r := l.edge.Route
	if compatible {
		return *r[i]
	}
	last := len(r) - 1
	if f == 1 {
		return *r[last]
	}
	if !l.edge.IsCurve {
		u := f * float64(last)
		j := min(int(u), last-1)
		return *r[j].Interpolate(r[j+1], u-float64(j))
	}
	u := f * float64(last/3)
	j := min(int(u), last/3-1)
	v := u - float64(j)
	w := 1 - v
	k := j * 3
	return geo.Point{
		X: w*w*w*r[k].X + 3*w*w*v*r[k+1].X + 3*w*v*v*r[k+2].X + v*v*v*r[k+3].X,
		Y: w*w*w*r[k].Y + 3*w*w*v*r[k+1].Y + 3*w*v*v*r[k+2].Y + v*v*v*r[k+3].Y,
	}
}
