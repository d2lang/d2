package d2elklayout

import (
	"iter"
	"math"
	"strconv"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/util-go/go2"
)

// A loop can have only a few pixels of straight line between its rounded
// corners. Keep inline labels on long runs, and look for space beside a loop
// when its current label would cover a bend.
func clearSelfLoopLabelBends(g *d2graph.Graph) {
	var obstacles *loopLabelObstacles
	for _, edge := range g.Edges {
		if edge.Src != edge.Dst || edge.Label.Value == "" || edge.LabelPosition == nil || len(edge.Route) < 3 ||
			!label.FromString(*edge.LabelPosition).IsOnEdge() {
			continue
		}
		c := loopLabelConnection(g, edge)
		current := loopLabelBounds(c)
		coversBend := false
		walkLoopLabelPath(c, func(bounds spatialBounds, bend bool) {
			coversBend = coversBend || bend && bounds.overlaps(current)
		})
		if !coversBend {
			continue
		}
		if obstacles == nil {
			obstacles = newLoopLabelObstacles(g)
		}

		obstacles.place(g, edge, c)
	}
}

func loopLabelConnection(g *d2graph.Graph, edge *d2graph.Edge) *d2target.Connection {
	c := d2target.BaseConnection()
	c.Route, c.IsCurve = edge.Route, edge.IsCurve
	c.LabelWidth, c.LabelHeight = edge.LabelDimensions.Width, edge.LabelDimensions.Height
	c.Icon = edge.Icon
	if edge.LabelPosition != nil {
		c.LabelPosition = *edge.LabelPosition
	}
	if edge.LabelPercentage != nil {
		c.LabelPercentage = *edge.LabelPercentage
	}
	if edge.Style.StrokeWidth != nil {
		c.StrokeWidth, _ = strconv.Atoi(edge.Style.StrokeWidth.Value)
	}
	if g.Theme != nil && g.Theme.SpecialRules.NoCornerRadius {
		c.BorderRadius = 0
	}
	if edge.Style.BorderRadius != nil {
		c.BorderRadius, _ = strconv.ParseFloat(edge.Style.BorderRadius.Value, 64)
	}
	return c
}

func loopLabelBounds(c *d2target.Connection) spatialBounds {
	p := c.GetLabelTopLeft()
	// The text mask extends two pixels past each end. An accompanying
	// icon occupies the space to the left of the text.
	b := spatialBounds{p.X - 2, p.Y, p.X + float64(c.LabelWidth) + 2, p.Y + float64(c.LabelHeight)}
	if c.Icon != nil {
		b.minX -= d2target.DEFAULT_ICON_SIZE + d2target.CONNECTION_ICON_LABEL_GAP
		extra := math.Max(0, (d2target.DEFAULT_ICON_SIZE-float64(c.LabelHeight))/2)
		b.minY -= extra
		b.maxY += extra
	}
	return b
}

func walkLoopLabelPath(c *d2target.Connection, visit func(spatialBounds, bool)) {
	// Use the same corner geometry as rendering. The control-point bounds
	// enclose the whole cubic, even when two short bends form one curve.
	// Keeping the full terminal segments also reserves the attachments.
	halfStroke := float64(c.StrokeWidth) / 2
	var previous, zero geo.Point
	for cmd := range c.PathCommands(&zero, &zero) {
		switch cmd.Kind {
		case 'L':
			visit(boundsForSegment(geo.Segment{Start: &previous, End: &cmd.End}).expanded(halfStroke), false)
		case 'C', 'S':
			visitLoopBend(&previous, &cmd.Control1, &cmd.Control2, &cmd.End, 2, func(b spatialBounds) {
				visit(b.expanded(halfStroke), true)
			})
		}
		previous = cmd.End
	}
}

// Subdivision tightens the conservative bounds. The control points of a
// tight U-turn or a nearby elbow can reach well into the gap between loops.
func visitLoopBend(a, b, c, d *geo.Point, depth int, visit func(spatialBounds)) {
	if depth == 0 {
		visit(union(boundsForSegment(geo.Segment{Start: a, End: b}), boundsForSegment(geo.Segment{Start: c, End: d})))
		return
	}
	ab, bc, cd := a.Interpolate(b, 0.5), b.Interpolate(c, 0.5), c.Interpolate(d, 0.5)
	left, right := ab.Interpolate(bc, 0.5), bc.Interpolate(cd, 0.5)
	mid := left.Interpolate(right, 0.5)
	visitLoopBend(a, ab, left, mid, depth-1, visit)
	visitLoopBend(mid, right, cd, d, depth-1, visit)
}

type loopLabelObstacles struct {
	grid gridIndex
	// The first entries are objects, so a containing ancestor can be
	// distinguished from a shape the label must avoid.
	objects  []*d2graph.Object
	labelIDs map[*d2graph.Edge]int32
	labels   map[int32]*d2graph.Edge
	straight map[int32]*d2graph.Edge
	hits     []int32
}

func newLoopLabelObstacles(g *d2graph.Graph) *loopLabelObstacles {
	o := &loopLabelObstacles{
		objects:  g.Objects,
		labelIDs: make(map[*d2graph.Edge]int32),
		labels:   make(map[int32]*d2graph.Edge),
		straight: make(map[int32]*d2graph.Edge),
	}
	var extent spatialBounds
	for _, obj := range g.Objects {
		extent = union(extent, objectBounds(obj, 0))
	}
	o.grid.reset(extent)
	for _, obj := range g.Objects {
		o.grid.add(objectBounds(obj, 0))
	}
	for _, obj := range g.Objects {
		if obj.HasLabel() {
			if p := obj.GetLabelTopLeft(); p != nil {
				o.grid.add(spatialBounds{p.X, p.Y, p.X + float64(obj.LabelDimensions.Width), p.Y + float64(obj.LabelDimensions.Height)})
			}
		}
		if obj.HasIcon() {
			if p := obj.GetIconTopLeft(); p != nil {
				o.grid.add(spatialBounds{p.X, p.Y, p.X + d2target.MAX_ICON_SIZE, p.Y + d2target.MAX_ICON_SIZE})
			}
		}
	}
	for _, edge := range g.Edges {
		if len(edge.Route) < 2 {
			continue
		}
		c := loopLabelConnection(g, edge)
		walkLoopLabelPath(c, func(b spatialBounds, bend bool) {
			id := o.grid.add(b)
			if !bend && edge.Src == edge.Dst {
				o.straight[id] = edge
			}
		})
		if edge.Label.Value != "" && edge.LabelPosition != nil {
			id := o.grid.add(loopLabelBounds(c))
			if edge.Src == edge.Dst && label.FromString(*edge.LabelPosition).IsOnEdge() {
				o.labelIDs[edge], o.labels[id] = id, edge
			}
		}
	}
	return o
}

type loopLabelCandidate struct {
	position label.Position
	fraction float64
	bounds   spatialBounds
}

func (o *loopLabelObstacles) place(g *d2graph.Graph, edge *d2graph.Edge, c *d2target.Connection) {
	// Try moving only the affected label. If a sibling occupies its only
	// free space, see whether both labels can be placed together.
	type conflict struct {
		candidate loopLabelCandidate
		sibling   *d2graph.Edge
	}
	var conflicts []conflict
	for candidate := range loopLabelCandidates(edge, c, false) {
		sibling, ok := o.blocker(edge, candidate)
		if !ok {
			continue
		}
		if sibling == nil {
			o.move(edge, candidate)
			return
		}
		conflicts = append(conflicts, conflict{candidate, sibling})
	}
	for _, conflict := range conflicts {
		other := loopLabelConnection(g, conflict.sibling)
		for alt := range loopLabelCandidates(conflict.sibling, other, true) {
			if alt.bounds.overlaps(conflict.candidate.bounds) {
				continue
			}
			blocker, ok := o.blocker(conflict.sibling, alt)
			if ok && (blocker == nil || blocker == edge) {
				o.move(conflict.sibling, alt)
				o.move(edge, conflict.candidate)
				return
			}
		}
	}
}

func (o *loopLabelObstacles) move(edge *d2graph.Edge, candidate loopLabelCandidate) {
	edge.LabelPosition = go2.Pointer(candidate.position.String())
	edge.LabelPercentage = go2.Pointer(candidate.fraction)
	o.grid.remove(o.labelIDs[edge])
	// Already placed labels are fixed obstacles for subsequent searches.
	o.labelIDs[edge] = o.grid.add(candidate.bounds)
}

func loopLabelCandidates(edge *d2graph.Edge, c *d2target.Connection, allowInline bool) iter.Seq[loopLabelCandidate] {
	return func(yield func(loopLabelCandidate) bool) {
		// Try the middle of the return first, then points on either arm.
		// Prefer the side facing away from the owner when both sides are free.
		center := edge.Src.Box.Center()
		distance := func(b spatialBounds) float64 {
			return geo.EuclideanDistance(center.X, center.Y, (b.minX+b.maxX)/2, (b.minY+b.maxY)/2)
		}
		candidate := func(pos label.Position, fraction float64) loopLabelCandidate {
			c.LabelPosition, c.LabelPercentage = pos.String(), fraction
			return loopLabelCandidate{pos, fraction, loopLabelBounds(c)}
		}
		fractions := [...]float64{0.5, 0.4, 0.6, 0.3, 0.7, 0.2, 0.8, 0.1, 0.9}
		for _, fraction := range fractions {
			first := candidate(label.UnlockedTop, fraction)
			second := candidate(label.UnlockedBottom, fraction)
			if distance(second.bounds) > distance(first.bounds) {
				first, second = second, first
			}
			if !yield(first) || !yield(second) {
				return
			}
		}
		if allowInline {
			for _, fraction := range fractions {
				if !yield(candidate(label.UnlockedMiddle, fraction)) {
					return
				}
			}
		}
	}
}

// blocker returns the single sibling label that a candidate would displace.
// Any other obstacle rules out the candidate.
func (o *loopLabelObstacles) blocker(edge *d2graph.Edge, candidate loopLabelCandidate) (*d2graph.Edge, bool) {
	b := candidate.bounds
	inline := candidate.position.IsOnEdge()
	if !regular(b) {
		return nil, false
	}
	// Child loops must keep their label inside the parent, even if a
	// candidate would land entirely beyond the parent's border.
	owner := edge.Src
	if parent := owner.Parent; parent != nil && parent.Box != nil && parent.TopLeft != nil {
		p := objectBounds(parent, 0)
		if b.minX <= p.minX || b.maxX >= p.maxX || b.minY <= p.minY || b.maxY >= p.maxY {
			return nil, false
		}
	}
	var sibling *d2graph.Edge
	o.hits = o.grid.collect(b, o.hits[:0])
	for _, id := range o.hits {
		if int(id) < len(o.objects) && owner != o.objects[id] && owner.IsDescendantOf(o.objects[id]) {
			continue
		}
		if id == o.labelIDs[edge] {
			continue
		}
		if inline && o.straight[id] == edge {
			// An inline label may break its own straight run once.
			inline = false
			continue
		}
		if other := o.labels[id]; other != nil && other.Src == owner && sibling == nil {
			sibling = other
			continue
		}
		return nil, false
	}
	return sibling, true
}
