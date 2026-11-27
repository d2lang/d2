package routing

import (
	"context"
	"errors"
	"math"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

type arrivalTrack struct {
	target     *layoutgraph.Node
	face       geo.Orientation
	track      float64
	increasing bool
}

func ordinaryArrival(e *layoutgraph.Edge) (arrivalTrack, bool) {
	if e == nil || e.From == nil || e.To == nil || len(e.Points) != 4 || e.IsInvisible || e.IsLoop() || e.IsCurve ||
		e.HasTableColumn() || e.Label != nil || e.SourceArrowheadLabel != nil || e.TargetArrowheadLabel != nil ||
		e.TargetArrowhead != layoutgraph.TriangleArrowhead || e.HasArrowTo(e.From) || !shortcutOrthogonal(e.Points) {
		return arrivalTrack{}, false
	}
	if !ordinaryRectangle(e.From) || !ordinaryRectangle(e.To) {
		return arrivalTrack{}, false
	}
	p := e.Points
	face := fanFace(e.To, p[3], p[2])
	if face == geo.NONE || fanFace(e.From, p[0], p[1]) != face.GetOpposite() {
		return arrivalTrack{}, false
	}
	axis := face.IsVertical()
	return arrivalTrack{e.To, face, channelCoordinate(p[1], !axis), channelCoordinate(p[2], axis) > channelCoordinate(p[1], axis)}, true
}

// RefineOrdinaryFanLanes splits coincident two-bend arrivals without moving any
// shape. Only selected routes can move; other routes remain obstacles, including
// existing connections restored by a nested layout.
func RefineOrdinaryFanLanes(ctx context.Context, g *layoutgraph.Graph, selected []*layoutgraph.Edge) error {
	if ctx == nil || g == nil {
		return layoutgraph.Validate(ctx, "RefineOrdinaryFanLanes", g)
	}
	if len(selected) < 2 || len(selected) > len(g.Edges) || shortcutInputTooLarge(g) || g.IsRootHierarchy ||
		slices.ContainsFunc(g.Nodes, func(n *layoutgraph.Node) bool { return n != nil && (n.ForceHierarchy || n.Hierarchy != nil) }) {
		return ctx.Err()
	}
	var groups [][]*layoutgraph.Edge
	index := map[arrivalTrack]int{}
	for _, e := range selected {
		key, ok := ordinaryArrival(e)
		if !ok {
			continue
		}
		i, found := index[key]
		if !found {
			i = len(groups)
			index[key] = i
			groups = append(groups, nil)
		}
		groups[i] = append(groups[i], e)
	}
	// All proposed tracks must overlap before this pass. Two doglegs with
	// disjoint middle segments simply happen to use the same coordinate.
	groups = slices.DeleteFunc(groups, func(es []*layoutgraph.Edge) bool {
		if len(es) < 2 || len(es) > 8 {
			return true
		}
		key, _ := ordinaryArrival(es[0])
		lo, hi := math.Inf(-1), math.Inf(1)
		for _, e := range es {
			a, b := channelCoordinate(e.Points[1], key.face.IsVertical()), channelCoordinate(e.Points[2], key.face.IsVertical())
			lo = max(lo, min(a, b))
			hi = min(hi, max(a, b))
		}
		return hi-lo < 2*fanCornerRadius
	})
	if len(groups) == 0 {
		return ctx.Err()
	}
	err := runAtomicRouteStage(ctx, "RefineOrdinaryFanLanes", g, selected, maxRouteStageWorkUnits, func(guard *routeWorkGuard) error {
		env, err := hierarchyRouteObstacles(g, guard)
		if err != nil || env == nil {
			return err
		}
		for _, es := range groups {
			if err := env.splitArrivalTrack(es); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, errRouteStageWorkLimit) {
		return ctx.Err()
	}
	return err
}

func (env fanObstacles) splitArrivalTrack(es []*layoutgraph.Edge) error {
	key, _ := ordinaryArrival(es[0])
	axis := key.face.IsVertical()
	if err := stableSortRouteValues(es, func(a, b *layoutgraph.Edge) bool {
		x, y := channelCoordinate(a.Points[0], axis), channelCoordinate(b.Points[0], axis)
		if x == y {
			return channelCoordinate(a.Points[3], axis) < channelCoordinate(b.Points[3], axis)
		}
		return x < y
	}, env.guard); err != nil {
		return err
	}

	sourceCount := map[*layoutgraph.Node]int{}
	moving := map[*layoutgraph.Edge]bool{}
	for _, e := range es {
		sourceCount[e.From]++
		moving[e] = true
	}
	// Each lane checks all stationary ink. Check the moving lanes against
	// one another at their proposed positions, not their old shared track.
	stationary := &layoutgraph.Graph{Nodes: env.g.Nodes}
	for _, other := range env.g.Edges {
		if !moving[other] {
			stationary.Edges = append(stationary.Edges, other)
		}
	}
	env.g = stationary
	var proposed [][]*geo.Point
	before, after := 0., 0.
	// Nest the turns. For a downward route turning right, the leftmost
	// departure takes the lowest track; reflecting either axis flips the order.
	normal := channelCoordinate(es[0].Points[3], !axis) - channelCoordinate(es[0].Points[0], !axis)
	order := math.Copysign(1, normal)
	if key.increasing {
		order = -order
	}
	lastTarget := math.Inf(-1)
	lo, hi := fanFaceSpan(key.target, key.face)
	// When all tips coincide, retain the existing port as an anchor and
	// spread toward the leaves on a wide face. An already distinct set of
	// tips instead packs from the near end to leave room around captions.
	anchor := channelCoordinate(es[0].Points[3], axis)
	sharedTip := true
	for _, e := range es[1:] {
		sharedTip = sharedTip && channelCoordinate(e.Points[3], axis) == anchor
	}
	pitch := min(3*fanMarkerPitch, (hi-lo-fanMarkerPitch)/float64(len(es)-1))
	if pitch < fanMarkerPitch {
		return nil
	}
	sourceRank := map[*layoutgraph.Node]int{}
	for i, e := range es {
		p := e.Points
		rank, count := sourceRank[e.From], sourceCount[e.From]
		sourceRank[e.From]++
		src := channelCoordinate(p[0], axis)
		if count > 1 {
			low, high := fanFaceSpan(e.From, key.face)
			width := float64(count-1) * fanMarkerPitch
			firstSlot := max(low+fanMarkerPitch/2, min(src-width/2, high-fanMarkerPitch/2-width))
			src = firstSlot + float64(rank)*fanMarkerPitch
			if src > high-fanMarkerPitch/2 {
				return nil
			}
		}
		track := key.track + order*fanCornerRadius*(float64(i)-float64(len(es)-1)/2)
		// Start on the side nearer the sources. Allow a few offsets inside
		// each slot to clear captions while leaving room for later arrivals.
		low := max(lo+fanMarkerPitch/2, lastTarget+fanMarkerPitch)
		high := hi - fanMarkerPitch/2 - float64(len(es)-1-i)*fanMarkerPitch
		if high < low {
			return nil
		}
		desired := high
		if key.increasing {
			desired = low
		}
		if sharedTip {
			steps := i
			if key.increasing {
				steps -= len(es) - 1
			}
			desired = max(low, min(high, anchor+float64(steps)*pitch))
		}
		start := atCorridorCoordinate(p[0], axis, src)
		first := atCorridorCoordinate(start, !axis, track)
		var candidate []*geo.Point
		for _, offset := range []float64{0, 4, 8, 12, 16, 24, 32} {
			dst := desired - offset
			if key.increasing {
				dst = desired + offset
			}
			if dst < low || dst > high {
				continue
			}
			end := atCorridorCoordinate(p[3], axis, dst)
			last := atCorridorCoordinate(end, !axis, track)
			points := []*geo.Point{start, first, last, end}
			sign := math.Copysign(1, normal)
			if (dst > src) != key.increasing || math.Abs(dst-src) < 2*fanCornerRadius ||
				sign*(track-channelCoordinate(start, !axis)) < 2*fanCornerRadius ||
				sign*(channelCoordinate(end, !axis)-track) < 2*fanCornerRadius {
				continue
			}
			clear, err := env.arrivalLaneClear(e, points)
			if err != nil {
				return err
			}
			if !clear {
				continue
			}
			candidate = points
			lastTarget = dst
			break
		}
		if candidate == nil {
			return nil
		}
		for _, prev := range proposed {
			for j := 1; j < len(prev); j++ {
				for k := 1; k < len(candidate); k++ {
					if shortcutSegmentsMeet(prev[j-1], prev[j], candidate[k-1], candidate[k]) {
						return nil
					}
				}
			}
		}
		proposed = append(proposed, candidate)
		before += shortcutLength(p)
		after += shortcutLength(candidate)
	}
	if after <= before+shortcutEpsilon {
		for i, e := range es {
			e.Points = proposed[i]
		}
	}
	return nil
}

func (env fanObstacles) arrivalLaneClear(e *layoutgraph.Edge, points []*geo.Point) (bool, error) {
	// Ancestors can surround one or both endpoints. Keep each bend in the
	// same region of every ancestor so all entries and exits retain their sides.
	var obstacles layoutgraph.Graph
	obstacles.Edges = env.g.Edges
	for _, n := range env.g.Nodes {
		if err := env.guard.step(); err != nil {
			return false, err
		}
		if n == e.From || n == e.To {
			continue
		}
		if n.IsContainer() && (e.From.IsDescendantOf(n) || e.To.IsDescendantOf(n)) {
			if !n.Shape.IsRectangular() {
				return false, nil
			}
			bottom := geo.NewPoint(n.TopLeft.X+n.Width, n.TopLeft.Y+n.Height)
			for i, p := range points {
				b := e.Points[i]
				if (p.X < n.TopLeft.X) != (b.X < n.TopLeft.X) || (p.X > bottom.X) != (b.X > bottom.X) ||
					(p.Y < n.TopLeft.Y) != (b.Y < n.TopLeft.Y) || (p.Y > bottom.Y) != (b.Y > bottom.Y) {
					return false, nil
				}
			}
			continue
		}
		obstacles.Nodes = append(obstacles.Nodes, n)
	}
	env.g = &obstacles
	return clearHierarchyCorridor(&env, e, points)
}
