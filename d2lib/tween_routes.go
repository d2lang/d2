package d2lib

import (
	"context"
	"math"
	"slices"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// Cap geometry with expensive intersections and the number of alternatives
// compared between neighboring samples. The native route is always retained.
const (
	maxTweenRoutePoints  = 128
	maxTweenRouteChoices = 80
)

// Layout engines choose routes for individual graphs. Two equally good static
// routes can be far apart on screen. Choose a path through the sampled layouts
// while both authored endpoints and each frame's obstacle geometry are known.
type tweenRouteFrame struct {
	board  *d2target.Diagram
	shapes map[string]*d2target.Shape
	edges  map[string]*d2target.Connection
	pairs  map[[2]string]int
}

func indexTweenRoutes(board *d2target.Diagram) tweenRouteFrame {
	f := tweenRouteFrame{
		board:  board,
		shapes: gridEdgeShapeMap(board.Shapes),
		edges:  make(map[string]*d2target.Connection, len(board.Connections)),
		pairs:  make(map[[2]string]int),
	}
	for i := range board.Connections {
		c := &board.Connections[i]
		f.edges[c.ID] = c
		f.pairs[[2]string{min(c.Src, c.Dst), max(c.Src, c.Dst)}]++
	}
	return f
}

type tweenRouteSample struct {
	frame *d2target.Diagram
	edge  *d2target.Connection
	nodes tweenRouteShapePair
}

func (s tweenRouteSample) setRoute(route []*geo.Point) {
	if slices.EqualFunc(s.edge.Route, route, (*geo.Point).Equals) {
		return
	}
	s.edge.BorderRadius = tweenRouteCornerRadius(s.edge, route, s.nodes)
	s.edge.Route = route
}

func (f tweenRouteFrame) sample(c *d2target.Connection) (tweenRouteSample, bool) {
	e := f.edges[c.ID]
	if e == nil || e.Src != c.Src || e.Dst != c.Dst || e.IsCurve || e.MotionClip != nil || e.MotionTip != "" {
		return tweenRouteSample{}, false
	}
	// Keep parallel and reciprocal wires in the lanes assigned together by layout.
	if f.pairs[[2]string{min(e.Src, e.Dst), max(e.Src, e.Dst)}] != 1 || !validTweenRoute(e.Route) {
		return tweenRouteSample{}, false
	}
	src, dst := f.shapes[e.Src], f.shapes[e.Dst]
	if src == dst || !tweenRoutableShape(src) || !tweenRoutableShape(dst) {
		return tweenRouteSample{}, false
	}
	return tweenRouteSample{f.board, e, tweenRouteShapePair{src, dst}}, true
}

// Evenly spaced landmarks distinguish both port jumps and changes in
// interior bends, independent of how many vertices a layout engine emitted.
type routeLandmarks [13]geo.Point

func measureTweenRoute(route []*geo.Point) (p routeLandmarks) {
	p[0], p[len(p)-1] = *route[0], *route[len(route)-1]
	length := geo.Route(route).Length()
	segment, start := 1, 0.0
	for i := 1; i < len(p)-1; i++ {
		at := length * float64(i) / float64(len(p)-1)
		for segment < len(route) {
			a, b := route[segment-1], route[segment]
			l := math.Hypot(b.X-a.X, b.Y-a.Y)
			if l > 0 && (start+l >= at || segment == len(route)-1) {
				t := (at - start) / l
				p[i] = geo.Point{X: a.X + t*(b.X-a.X), Y: a.Y + t*(b.Y-a.Y)}
				break
			}
			start += l
			segment++
		}
		if segment == len(route) {
			p[i] = p[len(p)-1]
		}
	}
	return p
}

func (a routeLandmarks) displacement2(b routeLandmarks) float64 {
	d := 0.0
	for i, p := range a {
		x, y := p.X-b[i].X, p.Y-b[i].Y
		d = max(d, x*x+y*y)
	}
	return d
}

func tweenNodeMotion(a, b *d2target.Shape) float64 {
	x, y := float64(b.Pos.X-a.Pos.X), float64(b.Pos.Y-a.Pos.Y)
	return math.Hypot(max(math.Abs(x), math.Abs(x+float64(b.Width-a.Width))), max(math.Abs(y), math.Abs(y+float64(b.Height-a.Height))))
}

func stabilizeTweenRoutes(ctx context.Context, previous, board *d2target.Diagram, budget *int) error {
	if len(board.Connections) == 0 || len(board.TweenFrames) == 0 || *budget <= 0 {
		return nil
	}
	indexCost := gridFrameElements(board)
	if previous != nil {
		indexCost += gridFrameElements(previous)
	}
	for _, f := range board.TweenFrames {
		indexCost += gridFrameElements(f)
	}
	if indexCost > *budget {
		return ctx.Err()
	}
	*budget -= indexCost
	frames := make([]tweenRouteFrame, 0, len(board.TweenFrames)+1)
	for _, f := range board.TweenFrames {
		frames = append(frames, indexTweenRoutes(f))
	}
	frames = append(frames, indexTweenRoutes(board))
	var prev tweenRouteFrame
	if previous != nil {
		prev = indexTweenRoutes(previous)
	}
	for i := range board.Connections {
		if err := ctx.Err(); err != nil {
			return err
		}
		c := &board.Connections[i]
		track := make([]tweenRouteSample, 0, len(frames))
		for _, f := range frames {
			s, ok := f.sample(c)
			if !ok || (len(track) > 0 && (s.nodes.src.Type != track[0].nodes.src.Type || s.nodes.dst.Type != track[0].nodes.dst.Type)) {
				break
			}
			track = append(track, s)
		}
		if len(track) != len(frames) {
			continue
		}
		prior, hasPrior := prev.sample(c)
		// With interrupted playback the first numeric sample defines the
		// start, regardless of what was on the screen before this step.
		canProjectBack := previous != nil && !hasPrior && prev.edges[c.ID] == nil
		stabilizeTweenEdge(track, prior, hasPrior, canProjectBack, budget)
		if *budget <= 0 {
			break
		}
	}
	return ctx.Err()
}

type tweenRouteChoice struct {
	route      []*geo.Point
	points     routeLandmarks
	peak, cost float64
	parent     int
}

func stabilizeTweenEdge(track []tweenRouteSample, prior tweenRouteSample, hasPrior, canProjectBack bool, budget *int) {
	last := len(track) - 1
	choices := make([][]tweenRouteChoice, len(track))
	lastSample := track[0]
	prevPoints := measureTweenRoute(lastSample.edge.Route)
	if hasPrior {
		lastSample, prevPoints = prior, measureTweenRoute(prior.edge.Route)
	}
	nativePeak := 0.0
	unstable := false
	for i, s := range track {
		points := measureTweenRoute(s.edge.Route)
		nativePeak = max(nativePeak, points.displacement2(prevPoints))
		srcMotion := tweenNodeMotion(lastSample.nodes.src, s.nodes.src)
		dstMotion := tweenNodeMotion(lastSample.nodes.dst, s.nodes.dst)
		// A fast source can legitimately move most of a long wire. Its
		// motion does not excuse an arrowhead teleporting on a fixed target.
		jump := func(at int) float64 { return math.Hypot(points[at].X-prevPoints[at].X, points[at].Y-prevPoints[at].Y) }
		if jump(0) > 4+1.5*srcMotion || jump(len(points)-1) > 4+1.5*dstMotion ||
			points.displacement2(prevPoints) > math.Pow(4+1.5*max(srcMotion, dstMotion), 2) {
			unstable = true
		}
		choices[i] = []tweenRouteChoice{{route: s.edge.Route, points: points}}
		lastSample, prevPoints = s, points
	}
	if !unstable {
		return
	}

	// Prefer one continuous correspondence between the authored layouts. A
	// clear morph whose speed follows the boxes needs no alternative lanes.
	start := track[0]
	if hasPrior {
		start = prior
	}
	if canProjectBack {
		start = track[last]
	}
	direct := make([][]*geo.Point, last)
	complete := true
	for i := 0; i < last; i++ {
		fraction := float64(i) / float64(last)
		if canProjectBack {
			fraction = 1
		}
		direct[i] = tweenRouteCandidate(*start.edge, *track[last].edge, start.nodes, track[last].nodes, track[i].nodes, fraction)
	}

	add := func(i int, r []*geo.Point) bool {
		if len(r) < 2 || len(r) > maxTweenRoutePoints {
			return false
		}
		s := track[i]
		for _, c := range choices[i] {
			if slices.EqualFunc(r, c.route, (*geo.Point).Equals) {
				return true
			}
		}
		if len(choices[i]) >= maxTweenRouteChoices {
			return false
		}
		// A candidate is compared with each choice in the two neighboring
		// frames as well as against the ink in its own frame.
		cost := len(r) * (1 + len(s.frame.Shapes) + len(s.frame.Connections))
		if i > 0 {
			cost += len(choices[i-1])
		}
		cost += len(choices[i+1])
		if cost > *budget {
			*budget = 0
			return false
		}
		*budget -= cost
		if !tweenRouteClear(s.frame, s.edge, r, s.nodes) {
			return false
		}
		choices[i] = append(choices[i], tweenRouteChoice{route: r, points: measureTweenRoute(r)})
		return true
	}
	prevPoints = measureTweenRoute(start.edge.Route)
	lastSample = start
	for i := 0; i <= last; i++ {
		r := track[last].edge.Route
		if i < last {
			r = direct[i]
			if !add(i, r) {
				complete = false
				continue
			}
		}
		points := measureTweenRoute(r)
		d := max(tweenNodeMotion(lastSample.nodes.src, track[i].nodes.src), tweenNodeMotion(lastSample.nodes.dst, track[i].nodes.dst))
		if (i > 0 || hasPrior) && points.displacement2(prevPoints) > min(nativePeak, math.Pow(6+2*d, 2)) {
			complete = false
		}
		prevPoints, lastSample = points, track[i]
	}
	if complete {
		for i, r := range direct {
			track[i].setRoute(r)
		}
		return
	}
	// A blocked port can often slide along the same face. Vary the
	// neighboring frames too so the wire can move aside before contact.
	for i, r := range direct {
		if i == 0 && !canProjectBack {
			continue
		}
		if len(r) == 0 {
			r = track[i].edge.Route
		}
		for variant := range tweenRoutePortAlternates(r, track[i].nodes) {
			if *budget <= 0 {
				break
			}
			add(i, variant)
		}
	}

	// Fit the whole transition first. Shorter spans offer genuine native
	// detours when a moving obstacle makes the end-to-end route infeasible.
	var span func(int, int)
	span = func(lo, hi int) {
		if hi-lo < 1 || *budget <= 0 {
			return
		}
		start := track[lo]
		first := lo + 1
		if lo == 0 && hasPrior {
			start = prior
			first = 0
		}
		for i := first; i < hi && *budget > 0; i++ {
			s := track[i]
			add(i, tweenRouteCandidate(*start.edge, *track[hi].edge, start.nodes, track[hi].nodes, s.nodes, float64(i-lo)/float64(hi-lo)))
		}
		if hi-lo > 2 {
			mid := (lo + hi) / 2
			span(lo, mid)
			span(mid, hi)
		}
	}
	span(0, last)

	if hasPrior && sameSlot(prior.nodes.src, track[0].nodes.src) && sameSlot(prior.nodes.dst, track[0].nodes.dst) {
		// Time zero is the preceding held layout unless something newly
		// present in this frame obstructs that route.
		for j, c := range choices[0] {
			if slices.EqualFunc(c.route, prior.edge.Route, (*geo.Point).Equals) {
				choices[0] = choices[0][j : j+1]
				break
			}
		}
	}
	chooseTweenRoutes(track, choices, prior, hasPrior)
}

// Minimize the largest jump, using cumulative motion to break ties. Every
// native sample is feasible, even if no continuous route clears an obstacle.
func chooseTweenRoutes(track []tweenRouteSample, choices [][]tweenRouteChoice, prior tweenRouteSample, hasPrior bool) {
	last := len(track) - 1
	var before routeLandmarks
	if hasPrior {
		before = measureTweenRoute(prior.edge.Route)
	}
	for i := range choices {
		for j := range choices[i] {
			c := &choices[i][j]
			if i == 0 {
				if hasPrior {
					c.peak = c.points.displacement2(before)
					c.cost = c.peak
				}
				continue
			}
			c.peak, c.cost = math.Inf(1), math.Inf(1)
			for k := range choices[i-1] {
				p := &choices[i-1][k]
				jump := c.points.displacement2(p.points)
				peak, cost := max(p.peak, jump), p.cost+jump
				if peak < c.peak || (peak == c.peak && cost < c.cost) {
					c.peak, c.cost, c.parent = peak, cost, k
				}
			}
		}
	}
	selected := choices[last][0].parent
	for i := last - 1; i >= 0; i-- {
		c := choices[i][selected]
		track[i].setRoute(c.route)
		selected = c.parent
	}
}
