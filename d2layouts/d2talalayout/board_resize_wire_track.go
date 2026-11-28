package d2talalayout

import (
	"context"
	"math"
	"slices"
	"strconv"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// Project the two held sets of lanes onto the entire transition. Starting
// each projection at its held route lets a temporary detour disappear as
// soon as the moving obstacle clears; it cannot accumulate extra elbows.
func routeResizeSequence(ctx context.Context, start, end *d2graph.Graph, plans []map[*d2graph.Object]*geo.Box) bool {
	frames := end.TweenFrames
	if len(frames) < 2 {
		return true
	}
	first, last := resizeObjectIndex(start), resizeObjectIndex(end)
	anchors := func(i int) map[string]*d2graph.Object {
		if i*2 < len(frames) {
			return first
		}
		return last
	}
	entry, exit := resizeWirePoses(start), resizeWirePoses(end)
	forward, backward := make([]map[string]resizeWirePose, len(frames)+1), make([]map[string]resizeWirePose, len(frames)+1)
	forward[0], backward[0], forward[len(frames)], backward[len(frames)] = entry, entry, exit, exit
	if entry == nil || exit == nil {
		return false
	}
	from, to := newResizeReference(ctx, start), newResizeReference(ctx, end)
	if from == nil || to == nil {
		return false
	}
	for i := 1; i < len(frames); i++ {
		if ctx.Err() != nil {
			return false
		}
		if !from.route(ctx, frames[i], plans[i], anchors(i)) {
			return false
		}
		forward[i] = resizeWirePoses(frames[i])
		if !to.route(ctx, frames[i], plans[i], anchors(i)) {
			return false
		}
		backward[i] = resizeWirePoses(frames[i])
		if forward[i] == nil || backward[i] == nil {
			return false
		}
	}

	// The two tracks contain the same scene geometry. Each wire can change
	// its family of routes once, at the sample where shaft, tips and caption
	// can make the least visible movement together.
	byFrame := make([]map[string]*d2graph.Edge, len(frames))
	for i := 1; i < len(frames); i++ {
		byFrame[i] = continuityEdges(frames[i])
	}
	// Parallel and reciprocal connections share the router's port assignments.
	// Change them together so one wire cannot occupy the other's lane.
	groups := make(map[[2]string][]string)
	for _, wire := range start.Edges {
		src, dst := wire.Src.AbsID(), wire.Dst.AbsID()
		g := [2]string{min(src, dst), max(src, dst)}
		groups[g] = append(groups[g], wire.AbsID())
	}
	for _, ids := range groups {
		if ctx.Err() != nil {
			return false
		}
		cut := resizeWireCut(ids, forward, backward)
		for i := 1; i < cut; i++ {
			for _, id := range ids {
				wire := byFrame[i][id]
				pose := forward[i][id]
				wire.Route, wire.LabelPosition, wire.LabelPercentage = pose.route, pose.label, pose.percentage
			}
		}
	}
	return true
}

type resizeWirePose struct {
	route      []*geo.Point
	label      *string
	percentage *float64
	landmarks  [11]geo.Point
	tangents   [2]geo.Vector
	opacity    float64
}

func resizeWirePoses(g *d2graph.Graph) map[string]resizeWirePose {
	poses := make(map[string]resizeWirePose, len(g.Edges))
	for _, e := range g.Edges {
		route := geo.Route(e.Route)
		p := resizeWirePose{route: route, label: e.LabelPosition, percentage: e.LabelPercentage, opacity: 1}
		if e.Style.Opacity != nil {
			p.opacity, _ = strconv.ParseFloat(e.Style.Opacity.Value, 64)
		}
		for i := 1; i < len(route); i++ {
			if route[i].Equals(route[i-1]) {
				route = slices.CompactFunc(slices.Clone(route), func(a, b *geo.Point) bool { return a.Equals(b) })
				break
			}
		}
		if len(route) < 2 {
			return nil
		}
		length := route.Length()
		for i := 0; i < 10; i++ {
			a, _ := route.GetPointAtDistance(float64(i) * length / 9)
			p.landmarks[i] = *a
		}
		p.landmarks[10] = p.landmarks[5]
		if e.LabelPosition != nil && e.Label.Value != "" {
			fraction := 0.
			if e.LabelPercentage != nil {
				fraction = *e.LabelPercentage
			}
			caption, _ := label.FromString(*e.LabelPosition).GetPointOnRoute(route, 2, fraction, float64(e.LabelDimensions.Width), float64(e.LabelDimensions.Height))
			if caption != nil {
				p.landmarks[10] = *caption
			}
		}
		n := len(route)
		p.tangents = [2]geo.Vector{route[0].VectorTo(route[1]).Unit(), route[n-2].VectorTo(route[n-1]).Unit()}
		poses[e.AbsID()] = p
	}
	return poses
}

func resizeWireJump(a, b resizeWirePose) float64 {
	weight := min(a.opacity, b.opacity)
	if weight <= 0 {
		return 0
	}
	largest := 0.
	for i, q := range a.landmarks {
		p := b.landmarks[i]
		largest = max(largest, math.Hypot(p.X-q.X, p.Y-q.Y))
	}
	// Changing the angle of a tip is visible even on a short final run.
	for i, t := range a.tangents {
		u := b.tangents[i]
		largest += 32 * (1 - min(1, t[0]*u[0]+t[1]*u[1]))
	}
	return weight * largest * largest
}

func resizeWireCut(ids []string, f, b []map[string]resizeWirePose) int {
	jump := func(a, b map[string]resizeWirePose) float64 {
		sum := 0.
		for _, id := range ids {
			sum += resizeWireJump(a[id], b[id])
		}
		return sum
	}
	last := len(f) - 1
	// Near a held board, keep its still-clear corridor. A small
	// preference avoids paying two intermediate jumps simply to divide
	// one necessary switch between substantially different port faces.
	preference := func(i int) float64 {
		return 0.15 * jump(f[i], b[i]) * float64(last-2*i) / float64(last)
	}
	right := 0.
	for i := 1; i < last; i++ {
		right += max(0., preference(i))
	}
	for i := 2; i <= last; i++ {
		right += jump(b[i-1], b[i])
	}
	left, best, cut := 0., math.Inf(1), 1
	for i := 1; i <= last; i++ {
		cost := left + jump(f[i-1], b[i]) + right
		if cost < best || cost == best && math.Abs(float64(i*2-last)) < math.Abs(float64(cut*2-last)) {
			best, cut = cost, i
		}
		if i < last {
			left += jump(f[i-1], f[i]) + max(0., -preference(i))
			right -= jump(b[i], b[i+1]) + max(0., preference(i))
		}
	}
	return cut
}
