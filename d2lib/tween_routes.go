package d2lib

import (
	"context"
	"math"
	"slices"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// Bound the geometry used in intersection checks.
const (
	maxTweenRoutePoints     = 128
	maxTweenDetourTemplates = 8  // Native samples to try per obstruction.
	maxTweenFrameChoices    = 12 // Bounds the comparison with the next frame.
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
	s.edge.Route = make([]*geo.Point, len(route))
	for i, p := range route {
		s.edge.Route[i] = p.Copy()
	}
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

// A choice carries one router-produced path along with its endpoints. Switching
// templates is discrete: there is no intermediate geometry between two lanes.
type tweenRouteChoice struct {
	route                         []*geo.Point
	template                      int
	switches, portChanges, parent int
	faces                         [2]byte
	deviation                     float64
}

func stabilizeTweenEdge(track []tweenRouteSample, prior tweenRouteSample, hasPrior, incoming bool, budget *int) {
	last := len(track) - 1
	start := track[0]
	if hasPrior {
		start = prior
	}
	end := track[last]
	if incoming {
		start = end
	}
	// Leave stable native motion alone. A changing path between stationary
	// endpoints merits a check even when its drift is small in every frame.
	prev := start
	unstable := false
	for _, s := range track {
		if slices.EqualFunc(prev.edge.Route, s.edge.Route, (*geo.Point).Equals) {
			prev = s
			continue
		}
		a, b := measureTweenRoute(prev.edge.Route), measureTweenRoute(s.edge.Route)
		src := tweenNodeMotion(prev.nodes.src, s.nodes.src)
		dst := tweenNodeMotion(prev.nodes.dst, s.nodes.dst)
		n := len(a) - 1
		if src == 0 && dst == 0 || math.Hypot(a[0].X-b[0].X, a[0].Y-b[0].Y) > 4+1.5*src ||
			math.Hypot(a[n].X-b[n].X, a[n].Y-b[n].Y) > 4+1.5*dst || a.displacement2(b) > math.Pow(4+1.5*max(src, dst), 2) {
			unstable = true
			break
		}
		prev = s
	}
	if !unstable {
		return
	}

	const startID, endID = -2, -1
	choices := make([][]tweenRouteChoice, len(track))
	// Zero means neither authored lane is clear at this sample.
	heldChoices := make([]int, last)
	pinnedFirst := false
	for i, s := range track[:last] {
		startClear := !incoming && addTweenRouteChoice(&choices[i], startID, start, s, budget)
		if i == 0 && startClear && sameTweenOutline(start.nodes.src, s.nodes.src) && sameTweenOutline(start.nodes.dst, s.nodes.dst) {
			pinnedFirst = true
		} else {
			addTweenRouteChoice(&choices[i], endID, end, s, budget)
		}
		if *budget <= 0 {
			return
		}
		heldChoices[i] = len(choices[i])
	}

	// A moving obstacle sometimes blocks both held lanes. Sample native
	// detours across the interval: later lanes may already be clear at entry.
	// Allow them to continue on each side as long as their geometry is clear.
	for i := 0; i < last; {
		if heldChoices[i] > 0 {
			i++
			continue
		}
		j := i + 1
		for j < last && heldChoices[j] == 0 {
			j++
		}
		project := func(k int) {
			for _, dir := range []int{-1, 1} {
				first := k
				if dir < 0 {
					first--
				}
				for f := first; f >= 0 && f < last && !(f == 0 && pinnedFirst); f += dir {
					if len(choices[f]) >= maxTweenFrameChoices || !addTweenRouteChoice(&choices[f], k, track[k], track[f], budget) {
						break
					}
				}
			}
		}
		n := min(maxTweenDetourTemplates, j-i)
		for k := 0; k < n; k++ {
			project(i + k*(j-i-1)/max(1, n-1))
		}
		for k := i; k < j; k++ {
			if len(choices[k]) == 0 {
				project(k)
			}
		}
		if *budget <= 0 {
			return
		}
		i = j
	}
	choices[last] = []tweenRouteChoice{{route: end.edge.Route, template: endID, faces: tweenRouteFaces(end.edge.Route)}}

	chooseTweenRoutes(track, choices)
}

// Among paths with the fewest switches, prefer stable attachment sides, then
// the switch timing that most closely follows the native intermediate layouts.
func chooseTweenRoutes(track []tweenRouteSample, choices [][]tweenRouteChoice) {
	last := len(track) - 1
	for i := 1; i < len(choices); i++ {
		for j := range choices[i] {
			c := &choices[i][j]
			cost := c.deviation
			for k, p := range choices[i-1] {
				changes := p.switches
				if c.template != p.template {
					changes++
				}
				ports := p.portChanges
				for end, face := range c.faces {
					if face != 0 && p.faces[end] != 0 && face != p.faces[end] {
						ports++
					}
				}
				d := p.deviation + cost
				betterPorts := ports < c.portChanges || ports == c.portChanges && d < c.deviation
				if k == 0 || changes < c.switches || changes == c.switches && betterPorts {
					c.parent, c.switches, c.portChanges, c.deviation = k, changes, ports, d
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

func addTweenRouteChoice(choices *[]tweenRouteChoice, id int, from, s tweenRouteSample, budget *int) bool {
	cost := len(from.edge.Route) * (1 + len(s.frame.Shapes) + len(s.frame.Connections))
	if cost > *budget {
		*budget = 0
		return false
	}
	*budget -= cost
	r := transportTweenRoute(from, s)
	if r == nil || !tweenRouteClear(s.frame, s.edge, r, s.nodes) {
		return false
	}
	deviation := measureTweenRoute(s.edge.Route).displacement2(measureTweenRoute(r))
	*choices = append(*choices, tweenRouteChoice{route: r, template: id, faces: tweenRouteFaces(r), deviation: deviation})
	return true
}

// Orthogonal approach directions distinguish which side of a node a wire
// uses. Native oblique ports have no comparable cardinal direction.
func tweenRouteFaces(r []*geo.Point) [2]byte {
	side := func(a, b *geo.Point) byte {
		switch {
		case math.Abs(a.X-b.X) < 1e-7:
			if b.Y < a.Y {
				return 'T'
			}
			return 'B'
		case math.Abs(a.Y-b.Y) < 1e-7:
			if b.X < a.X {
				return 'L'
			}
			return 'R'
		}
		return 0
	}
	n := len(r) - 1
	return [2]byte{side(r[0], r[1]), side(r[n], r[n-1])}
}
