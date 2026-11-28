package d2talalayout

import (
	"context"
	"math"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/labeling"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/quality"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/routing"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/shape"
)

// routeResizeBoard keeps the preceding board's wires and captions on a proposed
// placement. Only connections that lose an attachment or acquire an obstruction
// need the router. Work on a private graph until the whole board is ready.
func routeResizeBoard(ctx context.Context, previous, current *d2graph.Graph, plan map[*d2graph.Object]*geo.Box) bool {
	oldD2 := continuityEdges(previous)
	for _, e := range current.Edges {
		old := oldD2[e.AbsID()]
		if e.IsCurve || old == nil || old.IsCurve {
			return false // The clearance inspection only certifies polygonal wires.
		}
	}
	before, trial := layoutgraph.NewGraph(), layoutgraph.NewGraph()
	if _, err := translateGraph(ctx, previous, before, true); err != nil {
		return false
	}
	bindings, err := translateGraph(ctx, current, trial, true)
	if err != nil {
		return false
	}
	nodes := make(map[layoutgraph.EntityID]*layoutgraph.Node, len(trial.Nodes))
	oldNodes := make(map[layoutgraph.EntityID]*layoutgraph.Node, len(before.Nodes))
	for _, n := range before.Nodes {
		oldNodes[n.ID] = n
	}
	for _, n := range trial.Nodes {
		nodes[n.ID] = n
	}
	for _, o := range current.Objects {
		n := nodes[bindings.objectIDs[o]]
		old := oldNodes[n.ID]
		b := plan[o]
		if old == nil || b == nil || !boardLeafPoint(b.TopLeft) || !boardLeafDimension(b.Width) || !boardLeafDimension(b.Height) {
			return false
		}
		n.Box = *b.Copy()
		if n.Label != nil && old.Label != nil {
			n.Label.Position = old.Label.Position
		}
		if n.Icon != nil && old.Icon != nil {
			n.Icon.Position = old.Icon.Position
		}
	}
	oldEdges := make(map[layoutgraph.EntityID]*layoutgraph.Edge, len(before.Edges))
	for _, e := range before.Edges {
		oldEdges[e.ID] = e
	}
	needsRoute := make(map[layoutgraph.EntityID]bool)
	unplaced := make(map[*layoutgraph.Edge]bool)
	changedLabels := make(map[layoutgraph.EntityID]bool)
	for _, e := range trial.Edges {
		old := oldEdges[e.ID]
		if old == nil || len(old.Points) < 2 {
			return false
		}
		if e.Label != nil && old.Label != nil {
			e.Label.Position, e.LabelPercentage = old.Label.Position, old.LabelPercentage
		}
		if route := resizeBoardWire(old, e); route != nil {
			e.Points = route
			if resizeBoardLabel(old, e) {
				changedLabels[e.ID] = true
			}
		} else {
			needsRoute[e.ID] = true
			// A stale ordinary layout on a different arrangement is unrelated
			// to the trial. Inspect the scene without this wire until routed.
			unplaced[e] = e.IsInvisible
			e.IsInvisible = true
		}
	}
	if len(trial.Edges) != 0 {
		baseline, err := quality.InspectLoopClearance(ctx, before)
		if err != nil {
			return false
		}
		candidate, err := quality.InspectLoopClearance(ctx, trial)
		collisions := make(map[layoutgraph.EntityID]bool)
		obscuredLabels := make(map[layoutgraph.EntityID]bool)
		if err != nil || !candidate.CollectObstructedRoutes(baseline, collisions, obscuredLabels) {
			return false
		}
		if len(obscuredLabels) != 0 {
			var labels []*layoutgraph.Edge
			for _, e := range trial.Edges {
				if obscuredLabels[e.ID] {
					changedLabels[e.ID] = true
					labels = append(labels, e)
				}
			}
			if err := labeling.PlaceNewEdges(ctx, trial, labels); err != nil {
				return false
			}
			candidate, err = quality.InspectLoopClearance(ctx, trial)
		}
		if err != nil || !candidate.CollectObstructedRoutes(baseline, needsRoute, nil) {
			return false
		}
		for edge, invisible := range unplaced {
			edge.IsInvisible = invisible
		}
		if len(needsRoute) != 0 {
			var changed []*layoutgraph.Edge
			for _, e := range trial.Edges {
				if needsRoute[e.ID] {
					changed = append(changed, e)
				}
			}
			if err := routing.RouteEdges(ctx, trial, changed); err != nil {
				return false
			}
		}
	}
	patch, err := buildRoutePatch(ctx, current, bindings, trial.Edges)
	if err != nil || ctx.Err() != nil {
		return false
	}
	for _, o := range current.Objects {
		n := nodes[bindings.objectIDs[o]]
		o.Box = plan[o]
		if n.Label != nil && o.LabelPosition != nil {
			o.LabelPosition = new(n.Label.Position.String())
		}
		if n.Icon != nil && o.IconPosition != nil {
			o.IconPosition = new(n.Icon.Position.String())
		}
	}
	// Reversing a source-only arrow for layout mirrors its label percentage.
	// Preserve the original value (including nil) on a carried wire.
	for _, update := range patch.edges {
		if update.label == nil || needsRoute[bindings.edgeIDs[update.edge]] || changedLabels[bindings.edgeIDs[update.edge]] {
			continue
		}
		old := oldD2[update.edge.AbsID()]
		update.label.position, update.label.percentage = nil, nil
		if old.LabelPosition != nil {
			update.label.position = new(*old.LabelPosition)
		}
		if old.LabelPercentage != nil {
			update.label.percentage = new(*old.LabelPercentage)
		}
	}
	patch.apply()
	return true
}

// Preserve the old interior lanes. A rigid translation applies even to traced
// silhouettes and table row ports. For independently moving ends only adjust
// the first and last runs, and leave unsupported attachments to native routing.
func resizeBoardWire(old, current *layoutgraph.Edge) []*geo.Point {
	route := old.Points
	last := len(route) - 1
	port := func(a, b *layoutgraph.Node, p, next *geo.Point) *geo.Point {
		x, y := p.X+(b.TopLeft.X-a.TopLeft.X), p.Y+(b.TopLeft.Y-a.TopLeft.Y)
		if a.Width == b.Width && a.Height == b.Height {
			return geo.NewPoint(x, y)
		}
		if !resizeBoardFlatFace(b) || old.HasTableColumn() {
			return nil
		}
		// An unchanged lane on the same flat face stays put as the shape
		// grows. For a shrinking face, keep the port strictly on its span.
		if p.Y == next.Y {
			delta := p.X - a.TopLeft.X
			if math.Abs(delta-a.Width) <= 1 && next.X > p.X {
				x += b.Width - a.Width
			} else if math.Abs(delta) > 1 || next.X >= p.X {
				return nil
			}
			y = max(b.TopLeft.Y, min(b.TopLeft.Y+b.Height, y))
		} else if p.X == next.X {
			delta := p.Y - a.TopLeft.Y
			if math.Abs(delta-a.Height) <= 1 && next.Y > p.Y {
				y += b.Height - a.Height
			} else if math.Abs(delta) > 1 || next.Y >= p.Y {
				return nil
			}
			x = max(b.TopLeft.X, min(b.TopLeft.X+b.Width, x))
		} else {
			return nil
		}
		return geo.NewPoint(x, y)
	}
	start := port(old.From, current.From, route[0], route[1])
	end := port(old.To, current.To, route[last], route[last-1])
	if start == nil || end == nil {
		return nil
	}
	dx, dy := start.X-route[0].X, start.Y-route[0].Y
	if dx == end.X-route[last].X && dy == end.Y-route[last].Y {
		return translatedBoardRoute(route, dx, dy)
	}
	if old.IsLoop() {
		return nil
	}
	out := translatedBoardRoute(route, 0, 0)
	out[0], out[last] = start, end
	if last == 2 {
		// The growing endpoint can swallow the first elbow. The remaining
		// rail then ends on the adjacent face, on exactly the same lane.
		if p := resizeBoardCoveredElbow(current.From, start, end, route[0], route[1], route[2]); p != nil {
			return []*geo.Point{p, end}
		}
		if p := resizeBoardCoveredElbow(current.To, end, start, route[2], route[1], route[0]); p != nil {
			return []*geo.Point{start, p}
		}
	}
	if last == 1 && resizeBoardFlatFace(current.From) && resizeBoardFlatFace(current.To) && !old.HasTableColumn() {
		// Preserve the physical ports and their approach directions as opposing
		// faces slide apart. Reciprocal connections bend at opposite thirds.
		if route[0].X == route[1].X && start.X != end.X && (end.Y-start.Y)*(route[1].Y-route[0].Y) > 0 {
			y := start.Y + (end.Y-start.Y)/3
			return []*geo.Point{start, geo.NewPoint(start.X, y), geo.NewPoint(end.X, y), end}
		}
		if route[0].Y == route[1].Y && start.Y != end.Y && (end.X-start.X)*(route[1].X-route[0].X) > 0 {
			x := start.X + (end.X-start.X)/3
			return []*geo.Point{start, geo.NewPoint(x, start.Y), geo.NewPoint(x, end.Y), end}
		}
	}
	if last > 1 {
		if route[0].X == route[1].X {
			out[1].X = start.X
		} else if route[0].Y == route[1].Y {
			out[1].Y = start.Y
		} else {
			return nil
		}
		if route[last].X == route[last-1].X {
			out[last-1].X = end.X
		} else if route[last].Y == route[last-1].Y {
			out[last-1].Y = end.Y
		} else {
			return nil
		}
	}
	for i := 1; i <= last; i++ {
		ox, oy := route[i].X-route[i-1].X, route[i].Y-route[i-1].Y
		x, y := out[i].X-out[i-1].X, out[i].Y-out[i-1].Y
		if ox == 0 && x == 0 && y*oy > 0 || oy == 0 && y == 0 && x*ox > 0 {
			continue
		}
		return nil
	}
	return out
}

func resizeBoardFlatFace(n *layoutgraph.Node) bool {
	return !n.Is3D && !n.IsMultiple && (n.ShapeType() == shape.SQUARE_TYPE || n.ShapeType() == shape.REAL_SQUARE_TYPE)
}

// If the original first run has disappeared inside this box, move the tip
// around the corner onto the remaining orthogonal run.
func resizeBoardCoveredElbow(n *layoutgraph.Node, tip, other, oldTip, bend, oldOther *geo.Point) *geo.Point {
	if !resizeBoardFlatFace(n) {
		return nil
	}
	if oldTip.Y == bend.Y && bend.X == oldOther.X && (other.X-tip.X)*(bend.X-oldTip.X) <= 0 && other.X >= n.TopLeft.X && other.X <= n.TopLeft.X+n.Width {
		if oldOther.Y > bend.Y && other.Y > n.TopLeft.Y+n.Height {
			return geo.NewPoint(other.X, n.TopLeft.Y+n.Height)
		}
		if oldOther.Y < bend.Y && other.Y < n.TopLeft.Y {
			return geo.NewPoint(other.X, n.TopLeft.Y)
		}
	}
	if oldTip.X == bend.X && bend.Y == oldOther.Y && (other.Y-tip.Y)*(bend.Y-oldTip.Y) <= 0 && other.Y >= n.TopLeft.Y && other.Y <= n.TopLeft.Y+n.Height {
		if oldOther.X > bend.X && other.X > n.TopLeft.X+n.Width {
			return geo.NewPoint(n.TopLeft.X+n.Width, other.Y)
		}
		if oldOther.X < bend.X && other.X < n.TopLeft.X {
			return geo.NewPoint(n.TopLeft.X, other.Y)
		}
	}
	return nil
}

// When a straight wire gains bends, its old arclength percentage can land on
// a transverse run and put the caption on the opposite side of the drawing.
// Keep its original side and longitudinal position on the parallel approach.
func resizeBoardLabel(old, edge *layoutgraph.Edge) bool {
	if old.Label == nil || edge.Label == nil || !old.Label.Position.IsUnlocked() || len(old.Points) != 2 || len(edge.Points) != 4 {
		return false
	}
	// The two approach runs divide the old straight line in the same
	// order. Count the newly inserted crosswise run only for a caption
	// that belonged beyond its bend.
	p := edge.Points
	first := math.Hypot(p[1].X-p[0].X, p[1].Y-p[0].Y)
	cross := math.Hypot(p[2].X-p[1].X, p[2].Y-p[1].Y)
	last := math.Hypot(p[3].X-p[2].X, p[3].Y-p[2].Y)
	along := (first + last) * old.LabelPercentage
	if along > first {
		along += cross
	}
	edge.LabelPercentage = along / (first + cross + last)
	return true
}
