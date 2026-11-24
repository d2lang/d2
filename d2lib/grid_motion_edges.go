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

// Keep routes in the same coordinate system as the moving cells. Grid-to-grid
// links are straight, center-to-center links clipped to the two shape outlines.
// Routed links to other parts of the diagram keep their authored bends.
type gridMotionEdges struct {
	pairs   map[string]*gridEdgePair
	removed []*gridEdgePair
}

type gridEdgeLayout struct {
	edge     *d2target.Connection
	src, dst *d2target.Shape
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
		if graph := graphs[i]; graph != nil {
			for _, edge := range graph.Edges {
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
			layout := gridEdgeLayout{e, shapes[e.Src], shapes[pair.dstAnchor]}
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

// A distance of roughly one pixel accounts for integer target shape
// bounds versus subpixel layout ports. Preserve off-center and named ports.
func (l gridEdgeLayout) direct() bool {
	if l.src == nil || l.dst == nil || l.src == l.dst || len(l.edge.Route) != 2 {
		return false
	}
	a, b := gridEdgeCenter(l.src), gridEdgeCenter(l.dst)
	dx, dy := b.X-a.X, b.Y-a.Y
	length := math.Hypot(dx, dy)
	if length == 0 {
		return false
	}
	for _, p := range l.edge.Route {
		if math.Abs((p.X-a.X)*dy-(p.Y-a.Y)*dx) > 1.5*length {
			return false
		}
	}
	return true
}

func gridEdgeCenter(s *d2target.Shape) geo.Point {
	return geo.Point{X: float64(s.Pos.X) + float64(s.Width)/2, Y: float64(s.Pos.Y) + float64(s.Height)/2}
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
	return shape.TraceToShapeBorder(outline, border, geo.NewPoint(c.X+dx*2*ratio, c.Y+dy*2*ratio))
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
	if old.direct() && next.direct() {
		a, b := gridEdgeCenter(src), gridEdgeCenter(dst)
		dx, dy := b.X-a.X, b.Y-a.Y
		if dx == 0 && dy == 0 {
			return false
		}
		start, end := gridEdgeBorder(src, dx, dy), gridEdgeBorder(dst, -dx, -dy)
		if (end.X-start.X)*dx+(end.Y-start.Y)*dy <= 0 {
			// These two cells currently overlap; no exposed shaft is visible.
			return false
		}
		e.Route, e.IsCurve = []*geo.Point{start, end}, false
		return true
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
		dx, dy := float64(current.Pos.X-old.Pos.X), float64(current.Pos.Y-old.Pos.Y)
		if old.Width > 0 {
			dx += (p.X - float64(old.Pos.X)) * float64(current.Width-old.Width) / float64(old.Width)
		}
		if old.Height > 0 {
			dy += (p.Y - float64(old.Pos.Y)) * float64(current.Height-old.Height) / float64(old.Height)
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
