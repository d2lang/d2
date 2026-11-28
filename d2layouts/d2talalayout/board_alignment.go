package d2talalayout

import (
	"context"
	"math"
	"strings"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/xsort"
)

type boardAnchor struct {
	old, current *d2graph.Object
}

func alignBoard(ctx context.Context, previous, current *d2graph.Graph) (bool, error) {
	anchors := boardAnchors(previous, current)
	if len(anchors) == 0 {
		return false, nil
	}
	bounds := boardOffsetBounds(current.Objects, current.Edges)
	dx, dy := selectBoardOffset(anchors, bounds)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	aligned := dx != 0 || dy != 0
	var undo func()
	if aligned {
		undo = shiftBoardObjects(current.Objects, current.Edges, dx, dy)
	} else {
		for _, a := range anchors {
			if difference := anchorOffset(a); difference.X == 0 && difference.Y == 0 {
				aligned = true
				break
			}
		}
	}
	if improveBoardGroups(ctx, previous, current, anchors) {
		return true, nil
	}
	// Local routing may observe cancellation after the rigid alignment.
	// Restore the exact incoming pointers before reporting an interrupted edit.
	if err := ctx.Err(); err != nil {
		if undo != nil {
			undo()
		}
		return false, err
	}
	return aligned, nil
}

// Leave authored origins and special-purpose grid and sequence playback alone.
// Bound both the topology walk and the work used to compare candidate offsets.
func boardCanAlign(g *d2graph.Graph) bool { return validBoardGeometry(g, false) }

func validBoardGeometry(g *d2graph.Graph, allowConstantNear bool) bool {
	r := g.Root
	if !g.IsWholeLayout() || r == nil || r.Parent != nil || r.Top != nil || r.Left != nil || r.NearKey != nil || r.WidthAttr != nil || r.HeightAttr != nil ||
		r.GridRows != nil || r.GridColumns != nil || r.IsSequenceDiagram() || g.Legend != nil ||
		len(g.Objects) == 0 || len(g.Objects) > 256 || len(g.Edges) > 256 {
		return false
	}
	// Each non-root object must occur exactly once in the tree and the
	// flattened list. Translation visits the list once, including containers.
	seen := make(map[*d2graph.Object]int, len(g.Objects)+1)
	seen[r] = 1
	var visit func(*d2graph.Object) bool
	visit = func(n *d2graph.Object) bool {
		if len(n.ChildrenArray) != len(n.Children) {
			return false
		}
		for _, child := range n.ChildrenArray {
			if child == nil || child.Parent != n || seen[child] != 0 || len(seen) > len(g.Objects) || n.Children[strings.ToLower(child.ID)] != child {
				return false
			}
			seen[child] = 1
			if !visit(child) {
				return false
			}
		}
		return true
	}
	if !visit(r) || len(seen) != len(g.Objects)+1 {
		return false
	}
	for _, n := range g.Objects {
		if seen[n] != 1 || n == r || n.Top != nil || n.Left != nil || n.NearKey != nil && !(allowConstantNear && n.Parent == r && n.Graph == g && n.IsConstantNear()) ||
			n.IsGridDiagram() || n.IsSequenceDiagram() || n.Box == nil || !boardLeafPoint(n.TopLeft) ||
			!boardLeafDimension(n.Width) || !boardLeafDimension(n.Height) {
			return false
		}
		seen[n] = 2
	}
	points := 0
	edgeSeen := make(map[*d2graph.Edge]bool, len(g.Edges))
	for _, e := range g.Edges {
		if e == nil || edgeSeen[e] || seen[e.Src] != 2 || seen[e.Dst] != 2 || len(e.Route) < 2 || len(e.Route) > 4096-points {
			return false
		}
		edgeSeen[e] = true
		points += len(e.Route)
		for _, p := range e.Route {
			if !boardLeafPoint(p) {
				return false
			}
		}
	}
	return canShiftBoardObjects(g.Objects, g.Edges, 0, 0)
}

// Full scoped IDs carry identity across edits. If no surviving leaf shares
// a path, match a reparented leaf only when its local ID is unique on both
// boards and its label and shape still agree. Repeated “db” or “api” names
// in sibling containers do not imply that two services are the same.
func boardAnchors(previous, current *d2graph.Graph) []boardAnchor {
	old := make(map[string]*d2graph.Object, len(previous.Objects))
	oldLocal := make(map[string]*d2graph.Object, len(previous.Objects))
	newLocal := make(map[string]int, len(current.Objects))
	for _, n := range previous.Objects {
		old[strings.ToLower(n.AbsID())] = n
		k := strings.ToLower(n.ID)
		if _, exists := oldLocal[k]; exists {
			oldLocal[k] = nil
		} else {
			oldLocal[k] = n
		}
	}
	for _, n := range current.Objects {
		newLocal[strings.ToLower(n.ID)]++
	}
	var leaves, moved, containers []boardAnchor
	for _, n := range current.Objects {
		if !visibleBoardAnchor(n, current) {
			continue
		}
		peer := old[strings.ToLower(n.AbsID())]
		if peer != nil {
			if !visibleBoardAnchor(peer, previous) || n.IsContainer() != peer.IsContainer() {
				continue
			}
			if n.IsContainer() {
				containers = append(containers, boardAnchor{peer, n})
			} else {
				leaves = append(leaves, boardAnchor{peer, n})
			}
			continue
		}
		key := strings.ToLower(n.ID)
		peer = oldLocal[key]
		if newLocal[key] == 1 && peer != nil && !peer.IsContainer() && !n.IsContainer() &&
			peer.Label.Value == n.Label.Value && peer.Shape.Value == n.Shape.Value && visibleBoardAnchor(peer, previous) {
			moved = append(moved, boardAnchor{peer, n})
		}
	}
	if len(leaves) > 0 {
		return leaves
	}
	if len(moved) > 0 {
		return moved
	}
	return containers
}

func visibleBoardAnchor(n *d2graph.Object, g *d2graph.Graph) bool {
	if continuityHidden(n.Style) {
		return false
	}
	// Plain text has no rendered box, even with the default stroke and fill.
	plainText := n.Shape.Value == d2target.ShapeText && n.Language == ""
	if n.Icon != nil || !plainText && !unpaintedBody(n, g) {
		return true
	}
	_, alpha := paintAlphaRange(n.Style.FontColor)
	return alpha != 0 && strings.TrimSpace(n.Label.Value) != ""
}

// Use the center of the exported box, or the top of a table when appended
// fields leave all existing row baselines and column ports in place.
func anchorOffset(a boardAnchor) geo.Point {
	old, current := a.old, a.current
	dy := float64(int(old.TopLeft.Y) - int(current.TopLeft.Y))
	stableRowPositions := current.SQLTable.AppendsRowsTo(old.SQLTable, int(old.Height), int(current.Height)) ||
		old.SQLTable.AppendsRowsTo(current.SQLTable, int(current.Height), int(old.Height))
	if int(old.Width) != int(current.Width) || !stableRowPositions {
		dy += float64(int(old.Height)-int(current.Height)) / 2
	}
	return geo.Point{
		X: float64(int(old.TopLeft.X)-int(current.TopLeft.X)) + float64(int(old.Width)-int(current.Width))/2,
		Y: dy,
	}
}

func chooseBoardOffset(anchors []boardAnchor) (dx, dy float64) {
	return selectBoardOffset(anchors, fullBoardOffsetRange())
}

// A candidate can keep one or more centers fixed, or minimize the total
// Manhattan movement at the coordinate medians. Prefer more fixed centers
// within the original movement budget, then less travel and a smaller shift.
func selectBoardOffset(anchors []boardAnchor, bounds offsetRange) (dx, dy float64) {
	if len(anchors) == 0 {
		return 0, 0
	}
	xs, ys := make([]float64, 0, len(anchors)), make([]float64, 0, len(anchors))
	offsets := make([]geo.Point, 0, len(anchors)+1)
	var original float64
	for _, a := range anchors {
		p := anchorOffset(a)
		offsets = append(offsets, p)
		xs, ys = append(xs, p.X), append(ys, p.Y)
		original += math.Abs(p.X) + math.Abs(p.Y)
	}
	if original == 0 {
		return 0, 0
	}
	xsort.Ordered(xs)
	xsort.Ordered(ys)
	// For an even number of anchors the entire interval between the two
	// medians has the same travel. Prefer its point closest to zero.
	mid := func(values []float64) float64 {
		return max(values[(len(values)-1)/2], min(0, values[len(values)/2]))
	}
	// Resizing an odd-width box can put the optimal center at a half
	// pixel. Check both neighboring grid points on each axis.
	mx, my := mid(xs), mid(ys)
	candidates := []geo.Point{{}}
	for _, x := range []float64{math.Floor(mx), math.Ceil(mx)} {
		for _, y := range []float64{math.Floor(my), math.Ceil(my)} {
			candidates = append(candidates, geo.Point{X: x, Y: y})
		}
	}
	for _, p := range offsets {
		if p.X == math.Trunc(p.X) && p.Y == math.Trunc(p.Y) {
			candidates = append(candidates, p)
		}
	}
	bestFixed, bestTravel := -1, original
	for _, p := range candidates {
		x := max(bounds.minX, min(bounds.maxX, p.X))
		y := max(bounds.minY, min(bounds.maxY, p.Y))
		fixed, total := 0, 0.
		for _, q := range offsets {
			if q.X == x && q.Y == y {
				fixed++
			}
			total += math.Abs(q.X-x) + math.Abs(q.Y-y)
		}
		if total > original {
			continue
		}
		shift, bestShift := math.Abs(x)+math.Abs(y), math.Abs(dx)+math.Abs(dy)
		if fixed > bestFixed || fixed == bestFixed && (total < bestTravel || total == bestTravel &&
			(shift < bestShift || shift == bestShift && (x < dx || x == dx && y < dy))) {
			bestFixed, bestTravel, dx, dy = fixed, total, x, y
		}
	}
	return dx, dy
}

type offsetRange struct{ minX, maxX, minY, maxY float64 }

func fullBoardOffsetRange() offsetRange {
	return offsetRange{-2 * limits.MaxGraphSize, 2 * limits.MaxGraphSize, -2 * limits.MaxGraphSize, 2 * limits.MaxGraphSize}
}

// The exporter truncates each box origin toward zero. Crossing zero with a
// fractional coordinate would move the shape by a different integer offset
// from its route, even if their continuous coordinates all moved together.
func boardOffsetBounds(nodes []*d2graph.Object, edges []*d2graph.Edge) offsetRange {
	r := fullBoardOffsetRange()
	include := func(value, extent float64, shape bool, lo, hi *float64) {
		*lo = max(*lo, math.Ceil(-limits.MaxGraphSize-value))
		*hi = min(*hi, math.Floor(limits.MaxGraphSize-value-extent))
		if shape && math.Trunc(value) != value {
			if value > 0 {
				*lo = max(*lo, -math.Floor(value))
			} else {
				*hi = min(*hi, -math.Ceil(value))
			}
		}
	}
	for _, n := range nodes {
		include(n.TopLeft.X, n.Width, true, &r.minX, &r.maxX)
		include(n.TopLeft.Y, n.Height, true, &r.minY, &r.maxY)
	}
	for _, e := range edges {
		for _, p := range e.Route {
			include(p.X, 0, false, &r.minX, &r.maxX)
			include(p.Y, 0, false, &r.minY, &r.maxY)
		}
	}
	return r
}

func canShiftBoardObjects(nodes []*d2graph.Object, edges []*d2graph.Edge, dx, dy float64) bool {
	r := boardOffsetBounds(nodes, edges)
	return dx == math.Trunc(dx) && dy == math.Trunc(dy) && dx >= r.minX && dx <= r.maxX && dy >= r.minY && dy <= r.maxY
}

func shiftBoardObjects(nodes []*d2graph.Object, edges []*d2graph.Edge, dx, dy float64) (undo func()) {
	boxes := make([]*geo.Box, len(nodes))
	routes := make([][]*geo.Point, len(edges))
	for i, n := range nodes {
		boxes[i] = n.Box
		n.Box = geo.NewBox(geo.NewPoint(n.TopLeft.X+dx, n.TopLeft.Y+dy), n.Width, n.Height)
	}
	for i, edge := range edges {
		routes[i] = edge.Route
		edge.Route = translatedBoardRoute(edge.Route, dx, dy)
	}
	return func() {
		for i, n := range nodes {
			n.Box = boxes[i]
		}
		for i, e := range edges {
			e.Route = routes[i]
		}
	}
}
