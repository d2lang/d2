package routing

import (
	"context"
	"errors"
	"math"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/shape"
	"github.com/d2lang/d2/lib/xsort"
)

// A default 2px-stroke D2 triangle is 10 by 12px. Reserve a 16px square
// on the approach side, allowing for its border and clearance from neighbors.
const fanMarkerPitch = 16.0
const fanCornerRadius = 10.0 // D2’s default connection border radius.

func hierarchyMarkerBox(e *layoutgraph.Edge, n *layoutgraph.Node, tip, next *geo.Point) (*geo.Box, bool) {
	return markerClearanceBox(e, e.ArrowheadTo(n), tip, next)
}

func markerClearanceBox(e *layoutgraph.Edge, arrow layoutgraph.Arrowhead, tip, next *geo.Point) (*geo.Box, bool) {
	if e.Style.StrokeWidth != nil && e.Style.StrokeWidth.Value != "2" {
		return nil, false
	}
	switch arrow {
	case "", layoutgraph.NoArrowhead:
		return nil, true
	case layoutgraph.TriangleArrowhead:
	default:
		return nil, false
	}
	dx, dy := next.X-tip.X, next.Y-tip.Y
	x, y := tip.X, tip.Y
	switch {
	case dx == 0 && dy != 0:
		x -= fanMarkerPitch / 2
		if dy < 0 {
			y -= fanMarkerPitch
		}
	case dy == 0 && dx != 0:
		y -= fanMarkerPitch / 2
		if dx < 0 {
			x -= fanMarkerPitch
		}
	default:
		return nil, false
	}
	return geo.NewBox(geo.NewPoint(x, y), fanMarkerPitch, fanMarkerPitch), true
}

// RefineHierarchyFanPorts operates on the selected, fully routed graph. Try
// ordering existing slots before creating new ones. Crowded faces can often
// lose their crossings even when there is no room to separate every marker.
func RefineHierarchyFanPorts(ctx context.Context, g *layoutgraph.Graph) error {
	if ctx != nil && g != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(g.Nodes) > 256 || len(g.Edges) > 256 {
			return nil
		}
		points := 0
		for _, e := range g.Edges {
			if e != nil {
				points += len(e.Points)
			}
			if points > 1024 {
				return nil
			}
		}
	}
	err := runAtomicRouteStage(ctx, "RefineHierarchyFanPorts", g, nil, maxRouteStageWorkUnits, func(guard *routeWorkGuard) error {
		return refineHierarchyFanPorts(g, guard)
	})
	if errors.Is(err, errRouteStageWorkLimit) {
		return nil
	}
	return err
}

type fanObstacles struct {
	g      *layoutgraph.Graph
	labels []geo.Box
	guard  *routeWorkGuard
}

func refineHierarchyFanPorts(g *layoutgraph.Graph, guard *routeWorkGuard) error {
	if !slices.ContainsFunc(g.Nodes, func(n *layoutgraph.Node) bool { return n.Hierarchy != nil && len(n.Edges) >= 2 }) {
		return nil
	}
	env, err := hierarchyRouteObstacles(g, guard)
	if err != nil || env == nil {
		return err
	}
	for _, n := range g.Nodes {
		if !automaticFanFace(n) || len(n.Edges) < 2 {
			continue
		}
		for _, face := range []geo.Orientation{geo.Top, geo.Right, geo.Bottom, geo.Left} {
			if err := env.refineFace(n, face); err != nil {
				return err
			}
		}
	}
	for _, e := range g.Edges {
		if err := env.slideStraightFanLane(e); err != nil {
			return err
		}
	}
	return nil
}

// The clearance checks model rectilinear shafts, ordinary triangle markers
// and positioned labels. Unmodeled shapes of ink or missing owners veto the trial.
func hierarchyRouteObstacles(g *layoutgraph.Graph, guard *routeWorkGuard) (*fanObstacles, error) {
	for _, e := range g.Edges {
		if err := guard.step(); err != nil {
			return nil, err
		}
		if e.IsLoop() || e.IsCurve || !shortcutOrthogonal(e.Points) ||
			e.Style.BorderRadius != nil && e.Style.BorderRadius.Value != "10" {
			return nil, nil
		}
		for _, n := range []*layoutgraph.Node{e.From, e.To} {
			p, q := fanEndpoint(e, n)
			if _, ok := hierarchyMarkerBox(e, n, p, q); !ok {
				return nil, nil
			}
		}
	}
	snapshot, err := captureRouteMutations(g, nil, guard)
	if err != nil {
		return nil, err
	}
	closed, err := channelInventoryIsClosed(g, snapshot, guard)
	if !closed || err != nil {
		return nil, err
	}
	labels, err := shortcutLabelBoxes(g, guard, true)
	if err != nil {
		return nil, err
	}
	return &fanObstacles{g, labels, guard}, nil
}

func automaticFanFace(n *layoutgraph.Node) bool {
	return n.Hierarchy != nil && !n.IsContainer() && !n.IsInvisible && !n.Is3D && !n.IsMultiple &&
		(n.ShapeType() == "" || n.ShapeType() == shape.SQUARE_TYPE || n.ShapeType() == shape.REAL_SQUARE_TYPE)
}

func freeFanEdge(e *layoutgraph.Edge) bool {
	return !e.IsInvisible && !e.IsLoop() && !e.HasTableColumn() && e.Label == nil &&
		e.SourceArrowheadLabel == nil && e.TargetArrowheadLabel == nil && e.From.Hierarchy != nil && e.From.Hierarchy == e.To.Hierarchy
}

func fanEndpoint(e *layoutgraph.Edge, n *layoutgraph.Node) (*geo.Point, *geo.Point) {
	if e.From == n {
		return e.Points[0], e.Points[1]
	}
	i := len(e.Points) - 1
	return e.Points[i], e.Points[i-1]
}

func fanFace(n *layoutgraph.Node, p, q *geo.Point) geo.Orientation {
	switch {
	case p.X == q.X && p.Y == n.TopLeft.Y && q.Y < p.Y:
		return geo.Top
	case p.X == q.X && p.Y == n.TopLeft.Y+n.Height && q.Y > p.Y:
		return geo.Bottom
	case p.Y == q.Y && p.X == n.TopLeft.X && q.X < p.X:
		return geo.Left
	case p.Y == q.Y && p.X == n.TopLeft.X+n.Width && q.X > p.X:
		return geo.Right
	}
	return geo.NONE
}

func fanFaceSpan(n *layoutgraph.Node, face geo.Orientation) (float64, float64) {
	if face.IsVertical() {
		return n.TopLeft.X, n.TopLeft.X + n.Width
	}
	return n.TopLeft.Y, n.TopLeft.Y + n.Height
}

type fanLane struct {
	edge            *layoutgraph.Edge
	tip, elbow, end geo.Point
	positive        bool
	distance        float64
}

// A one-bend face consists of parallel exits and perpendicular turns to the
// leaves. Within each turning direction, the longer exit must leave farther
// from those leaves. This orders even the lanes that currently share a port.
func (env fanObstacles) refineFace(n *layoutgraph.Node, face geo.Orientation) error {
	var lanes []fanLane
	var slots []float64
	marked := false
	for _, e := range n.Edges {
		if err := env.guard.step(); err != nil {
			return err
		}
		p, q := fanEndpoint(e, n)
		if fanFace(n, p, q) != face {
			continue
		}
		if !freeFanEdge(e) || len(e.Points) != 3 {
			return nil
		}
		_, _, end := pointsAt(e, n)
		t := channelCoordinate(p, face.IsVertical())
		u := channelCoordinate(end, face.IsVertical())
		if u == t || channelCoordinate(end, !face.IsVertical()) != channelCoordinate(q, !face.IsVertical()) {
			return nil
		}
		if len(lanes) > 0 && (e.ArrowheadTo(n) != lanes[0].edge.ArrowheadTo(n) || !e.EquivalentStyles(lanes[0].edge)) {
			return nil
		}
		lanes = append(lanes, fanLane{e, *p, *q, *end, u > t, math.Abs(q.X-p.X) + math.Abs(q.Y-p.Y)})
		slots = append(slots, t)
		marked = e.HasArrowTo(n)
	}
	if len(lanes) < 2 {
		return nil
	}
	xsort.Ordered(slots)
	repeated := false
	for i := 1; i < len(slots); i++ {
		repeated = repeated || slots[i] == slots[i-1]
	}
	if err := stableSortRouteValues(lanes, func(a, b fanLane) bool {
		if a.positive != b.positive {
			return !a.positive
		}
		if a.positive {
			return a.distance > b.distance
		}
		return a.distance < b.distance
	}, env.guard); err != nil {
		return err
	}
	// Coincident final tracks in the same turning direction would require
	// separating both ends of a route. Leave them to the general router.
	for i := 1; i < len(lanes); i++ {
		if lanes[i].positive == lanes[i-1].positive && lanes[i].distance == lanes[i-1].distance {
			return nil
		}
	}
	original := make([]float64, len(lanes))
	for i, l := range lanes {
		original[i] = channelCoordinate(&l.tip, face.IsVertical())
	}
	if !repeated && slices.Equal(slots, original) {
		return nil
	}
	candidates := [][]float64{slots}
	lo, hi := fanFaceSpan(n, face)
	gap := fanMarkerPitch / 2
	if marked {
		gap = fanMarkerPitch
	}
	width := float64(len(lanes)-1) * gap
	if repeated && width+fanMarkerPitch <= hi-lo {
		start := lo + (hi-lo-width)/2
		if lanes[0].positive {
			start = hi - fanMarkerPitch/2 - width
		} else if !lanes[len(lanes)-1].positive {
			start = lo + fanMarkerPitch/2
		}
		spaced := make([]float64, len(slots))
		for i := range spaced {
			spaced[i] = start + float64(i)*gap
		}
		candidates = append(candidates, spaced)
	}
	// Evaluate in order of preference. If the distinct allocation cannot fit
	// safely, a permutation retains the original ports, including duplicates.
	for j := len(candidates) - 1; j >= 0; j-- {
		proposed := candidates[j]
		if slices.Equal(original, proposed) {
			continue
		}
		ok, err := env.fanStripClear(n, face, lanes, proposed, marked)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		for i, l := range lanes {
			points := slices.Clone(l.edge.Points)
			tip, elbow := l.tip, l.elbow
			if face.IsVertical() {
				tip.X, elbow.X = proposed[i], proposed[i]
			} else {
				tip.Y, elbow.Y = proposed[i], proposed[i]
			}
			idx := 0
			if l.edge.To == n {
				idx = 2
			}
			points[idx], points[1] = &tip, &elbow
			l.edge.Points = points
		}
		return nil
	}
	return nil
}

// All changed geometry lies in this strip between the hub and its elbows.
// Foreign geometry intersecting the strip is a conservative veto. This avoids
// introducing a crossing, even when a nearby edge lies outside the hierarchy.
func (env fanObstacles) fanStripClear(n *layoutgraph.Node, face geo.Orientation, lanes []fanLane, slots []float64, marked bool) (bool, error) {
	tlo, thi, nlo, nhi := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	ignore := make(map[*layoutgraph.Edge]bool, len(lanes))
	delta := 0.
	beforeShared, afterShared := 0., 0.
	if err := env.guard.add(uint64(len(lanes) * len(lanes))); err != nil {
		return false, err
	}
	for i, l := range lanes {
		t, u, v := channelCoordinate(&l.tip, face.IsVertical()), channelCoordinate(&l.end, face.IsVertical()), slots[i]
		// Equal-radius turns on ordered lanes stay nested. A short exit or
		// final leg can clip that corner or its neighboring arrowhead.
		if l.distance < fanCornerRadius || min(math.Abs(u-v), math.Abs(u-t)) < 2*fanCornerRadius ||
			marked && l.distance < fanMarkerPitch {
			return false, nil
		}
		for j, m := range lanes[:i] {
			if channelCoordinate(&m.tip, face.IsVertical()) == t {
				beforeShared += min(m.distance, l.distance)
			}
			if slots[j] == v {
				afterShared += min(m.distance, l.distance)
			}
		}
		if (u-v)*(u-t) <= 0 {
			return false, nil
		}
		// Keep enough room on the last leg for the unchanged far marker.
		if l.edge.HasArrowTo(n.Adjacent(l.edge)) && math.Abs(u-v) < min(fanMarkerPitch, math.Abs(u-t)) {
			return false, nil
		}
		delta += math.Abs(u-v) - math.Abs(u-t)
		tlo = min(tlo, t, v)
		thi = max(thi, t, v)
		a, b := channelCoordinate(&l.tip, !face.IsVertical()), channelCoordinate(&l.elbow, !face.IsVertical())
		nlo = min(nlo, a, b)
		nhi = max(nhi, a, b)
		ignore[l.edge] = true
	}
	if delta > shortcutEpsilon || afterShared > beforeShared+shortcutEpsilon {
		return false, nil
	}
	padding := fanCornerRadius + 1 // rounded elbow and half of its stroke
	tlo -= padding
	thi += padding
	x, y, w, h := nlo, tlo, nhi-nlo, thi-tlo
	if face.IsVertical() {
		x, y, w, h = tlo, nlo, thi-tlo, nhi-nlo
	}
	strip := geo.NewBox(geo.NewPoint(x, y), w, h)
	for _, l := range lanes {
		box, _ := hierarchyMarkerBox(l.edge, n.Adjacent(l.edge), &l.end, &l.elbow)
		if box != nil && box.Overlaps(*strip) {
			return false, nil
		}
	}
	return env.clear(strip, n, nil, ignore)
}

func (env fanObstacles) clear(strip *geo.Box, a, b *layoutgraph.Node, ignore map[*layoutgraph.Edge]bool) (bool, error) {
	for _, n := range env.g.Nodes {
		if err := env.guard.step(); err != nil {
			return false, err
		}
		if n != a && n != b && n.Box.Overlaps(*strip) {
			// Containing walls stay untouched when the entire changed strip is inside.
			if n.IsContainer() && a.IsDescendantOf(n) && (b == nil || b.IsDescendantOf(n)) &&
				strip.TopLeft.X > n.TopLeft.X && strip.TopLeft.Y > n.TopLeft.Y &&
				strip.TopLeft.X+strip.Width < n.TopLeft.X+n.Width && strip.TopLeft.Y+strip.Height < n.TopLeft.Y+n.Height {
				continue
			}
			return false, nil
		}
	}
	for _, box := range env.labels {
		if err := env.guard.step(); err != nil {
			return false, err
		}
		if box.Overlaps(*strip) {
			return false, nil
		}
	}
	for _, e := range env.g.Edges {
		if err := env.guard.add(uint64(len(e.Points) + 2)); err != nil {
			return false, err
		}
		if ignore[e] {
			continue
		}
		for i := 1; i < len(e.Points); i++ {
			if segmentIntersectsBox(e.Points[i-1], e.Points[i], strip) {
				return false, nil
			}
			if corner := fanCornerBounds(e.Points, i); corner != nil && corner.Overlaps(*strip) {
				return false, nil
			}
		}
		for _, n := range []*layoutgraph.Node{e.From, e.To} {
			p, q := fanEndpoint(e, n)
			box, _ := hierarchyMarkerBox(e, n, p, q)
			if box != nil && box.Overlaps(*strip) {
				return false, nil
			}
		}
	}
	return true, nil
}

// The SVG draws a rounded turn inside the quadrant between its two legs.
// A short intermediate segment can combine both turns into a single cubic;
// its control points can also enter the adjacent quadrant.
func fanCornerBounds(points []*geo.Point, i int) *geo.Box {
	if i <= 0 || i >= len(points)-1 {
		return nil
	}
	p, c, q := points[i-1], points[i], points[i+1]
	if p.X == q.X || p.Y == q.Y {
		return nil
	}
	r := fanCornerRadius
	shortBefore := i > 1 && math.Abs(p.X-c.X)+math.Abs(p.Y-c.Y) < 2*r
	shortAfter := i < len(points)-2 && math.Abs(q.X-c.X)+math.Abs(q.Y-c.Y) < 2*r
	if shortBefore || shortAfter {
		return geo.NewBox(geo.NewPoint(c.X-r-1, c.Y-r-1), 2*r+2, 2*r+2)
	}
	x, y := c.X, c.Y
	if p.X < c.X || q.X < c.X {
		x -= r
	}
	if p.Y < c.Y || q.Y < c.Y {
		y -= r
	}
	return geo.NewBox(geo.NewPoint(x-1, y-1), r+2, r+2)
}

// Slide a straight delivery only when another arrow arrives through exactly
// the same port. A free midpoint of the overlapping rectangular faces must
// clear every other route, node, label and marker along the entire corridor.
func (env fanObstacles) slideStraightFanLane(e *layoutgraph.Edge) error {
	if len(e.Points) != 2 || !freeFanEdge(e) || !automaticFanFace(e.From) || !automaticFanFace(e.To) {
		return nil
	}
	if !e.HasArrowTo(e.To) || len(e.To.Edges) < 2 {
		return nil
	}
	p, q := e.Points[0], e.Points[1]
	face := fanFace(e.From, p, q)
	if face == geo.NONE || fanFace(e.To, q, p) != face.GetOpposite() {
		return nil
	}
	lo, hi := fanFaceSpan(e.From, face)
	toLo, toHi := fanFaceSpan(e.To, face)
	lo, hi = max(lo, toLo), min(hi, toHi)
	if hi-lo < fanMarkerPitch {
		return nil
	}
	slots := []float64{lo, hi}
	shared := false
	for _, n := range []*layoutgraph.Node{e.From, e.To} {
		side := face
		if n == e.To {
			side = face.GetOpposite()
		}
		for _, other := range n.Edges {
			if err := env.guard.step(); err != nil {
				return err
			}
			if other == e {
				continue
			}
			a, b := fanEndpoint(other, n)
			if fanFace(n, a, b) != side {
				continue
			}
			t := channelCoordinate(a, side.IsVertical())
			if t > lo && t < hi {
				slots = append(slots, t)
			}
			shared = shared || n == e.To && *a == *q && other.HasArrowTo(n)
		}
	}
	if !shared {
		return nil
	}
	xsort.Ordered(slots)
	for i := 1; i < len(slots); i++ {
		if slots[i]-slots[i-1] < fanMarkerPitch*2 {
			continue
		}
		t := (slots[i] + slots[i-1]) / 2
		a, b := *p, *q
		if face.IsVertical() {
			a.X, b.X = t, t
		} else {
			a.Y, b.Y = t, t
		}
		x, y := min(a.X, b.X), min(a.Y, b.Y)
		w, h := math.Abs(a.X-b.X), math.Abs(a.Y-b.Y)
		if face.IsVertical() {
			x -= fanMarkerPitch / 2
			w = fanMarkerPitch
		} else {
			y -= fanMarkerPitch / 2
			h = fanMarkerPitch
		}
		ok, err := env.clear(geo.NewBox(geo.NewPoint(x, y), w, h), e.From, e.To, map[*layoutgraph.Edge]bool{e: true})
		if err != nil {
			return err
		}
		if ok {
			e.Points = []*geo.Point{&a, &b}
			return nil
		}
	}
	return nil
}
