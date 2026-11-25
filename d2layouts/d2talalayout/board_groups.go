package d2talalayout

import (
	"context"
	"image"
	"math"
	"slices"
	"strings"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/quality"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/routing"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// Position familiar descendants before their ancestors. A group always moves
// with its contents, even when a newly expanded sibling displaces it.
type boardGroup struct {
	root     *d2graph.Object
	members  []*d2graph.Object
	retained int
}

type boardPlacement struct {
	old   map[string]*d2graph.Object
	moved map[*d2graph.Object][]boardAnchor
	plan  map[*d2graph.Object]*geo.Box
	// Try alternate sides of the familiar siblings when the nearest free slot routes poorly.
	side     geo.Orientation
	compact  bool
	ordinary *d2graph.Object
}

func improveBoardGroups(ctx context.Context, previous, current *d2graph.Graph, anchors []boardAnchor) bool {
	if !groupBoardSupported(current) || !groupBoardSupported(previous) || ctx.Err() != nil {
		return false
	}
	moved, movedWires := movedBoardPanels(previous, current)
	// With every landmark already in place, the only remaining improvement
	// is to restore the internal spacing of a transferred panel.
	if len(moved) == 0 && !slices.ContainsFunc(anchors, func(a boardAnchor) bool {
		return anchorOffset(a) != (geo.Point{})
	}) {
		return false
	}
	old := make(map[string]*d2graph.Object, len(previous.Objects))
	for _, n := range previous.Objects {
		old[strings.ToLower(n.AbsID())] = n
	}
	var tried [][][4]float64
	flows := boardFlow(current)
	candidates := []boardPlacement{{side: geo.NONE}}
	for _, side := range []geo.Orientation{geo.NONE, geo.Right, geo.Bottom, geo.Left, geo.Top} {
		candidates = append(candidates, boardPlacement{side: side, compact: true})
	}
	// If retaining every member gives poor routes, let one panel use its new
	// internal layout while the surrounding groups keep their landmarks.
	for _, root := range current.Root.ChildrenArray {
		if len(candidates) == 10 {
			break
		}
		if root.IsContainer() {
			candidates = append(candidates, boardPlacement{side: geo.NONE, compact: true, ordinary: root})
		}
	}
	for _, p := range candidates {
		p.moved = moved
		if ctx.Err() != nil {
			return false
		}
		p.old, p.plan = old, make(map[*d2graph.Object]*geo.Box, len(current.Objects))
		p.group(current.Root)
		if !advanceBoardFlow(current, p.plan, flows) || !p.valid(current) || !betterBoardPlan(anchors, p.plan, p.moved) {
			continue
		}
		// Different sides can yield the same plan, especially after deleting
		// a node. Inspect and route each distinct geometry only once.
		snapshot := make([][4]float64, 0, len(current.Objects))
		for _, n := range current.Objects {
			b := p.plan[n]
			snapshot = append(snapshot, [4]float64{b.TopLeft.X, b.TopLeft.Y, b.Width, b.Height})
		}
		if slices.ContainsFunc(tried, func(other [][4]float64) bool { return slices.Equal(snapshot, other) }) {
			continue
		}
		tried = append(tried, snapshot)
		if routeBoardPlan(ctx, previous, current, p.plan, movedWires) {
			return true
		}
	}
	return false
}

func (p *boardPlacement) group(root *d2graph.Object) boardGroup {
	isRoot := root.Parent == nil
	g := boardGroup{root: root}
	var peer *d2graph.Object
	if pairs := p.moved[root]; len(pairs) > 0 {
		for _, a := range pairs {
			g.members = append(g.members, a.current)
			p.plan[a.current] = a.old.Box.Copy()
		}
		desired, was := root.Center(), p.plan[root].Center()
		dx, dy := int(math.Round(desired.X-was.X)), int(math.Round(desired.Y-was.Y))
		p.shift(g, image.Pt(dx, dy))
		return g
	}
	if !isRoot {
		g.members = append(g.members, root)
		peer = p.old[strings.ToLower(root.AbsID())]
		p.plan[root] = root.Box.Copy()
	}
	if len(root.ChildrenArray) == 0 {
		if peer != nil && !peer.IsContainer() && peer.Width == root.Width && peer.Height == root.Height {
			p.plan[root] = peer.Box.Copy()
			g.retained = 1
		}
		return g
	}

	var established, added []boardGroup
	var fresh, moving image.Rectangle
	var anchors []boardAnchor
	for _, n := range root.ChildrenArray {
		child := p.group(n)
		g.members = append(g.members, child.members...)
		g.retained += child.retained
		fresh = fresh.Union(boardOuterPixels(n.Box))
		if child.retained == 0 {
			added = append(added, child)
			moving = moving.Union(boardOuterPixels(p.plan[n]))
		} else {
			established = append(established, child)
			if old := p.old[strings.ToLower(n.AbsID())]; old != nil {
				anchors = append(anchors, boardAnchor{old, n})
			}
		}
	}
	// Keep an intact transferred team rigid when relaxing its destination.
	transferred := slices.ContainsFunc(g.members, func(n *d2graph.Object) bool { return len(p.moved[n]) > 0 })
	if root == p.ordinary && g.retained > 0 && !transferred {
		var anchors []boardAnchor
		for _, n := range g.members {
			p.plan[n] = n.Box.Copy()
			old := p.old[strings.ToLower(n.AbsID())]
			if !n.IsContainer() && old != nil && !old.IsContainer() {
				anchors = append(anchors, boardAnchor{old, n})
			}
		}
		dx, dy := chooseBoardOffset(anchors)
		p.shift(g, image.Pt(int(dx), int(dy)))
		g.retained = 1
		return g
	}
	if len(established) == 0 && !transferred {
		return g
	}
	if len(established) == 0 {
		established, added = added, nil
	}
	slices.SortStableFunc(established, func(a, b boardGroup) int { return b.retained - a.retained })
	var occupied []image.Rectangle
	var content image.Rectangle
	for _, child := range established {
		r := boardOuterPixels(p.plan[child.root])
		// Already-placed siblings may have had a smaller gap than newly
		// placed ones. Keep it when their existing outlines remain clear.
		if slices.ContainsFunc(occupied, func(o image.Rectangle) bool { return o.Inset(-4).Overlaps(r) }) {
			offset := packBoardRect(r, occupied, 36)
			p.shift(child, offset)
			r = r.Add(offset)
		}
		occupied = append(occupied, r)
		content = content.Union(r)
	}
	if len(added) > 0 {
		dx, dy := chooseBoardOffset(anchors)
		desired := image.Pt(int(dx), int(dy))
		switch p.side {
		case geo.Right:
			desired.X = content.Max.X + 40 - moving.Min.X
		case geo.Left:
			desired.X = content.Min.X - 40 - moving.Max.X
		case geo.Top:
			desired.Y = content.Min.Y - 40 - moving.Max.Y
		case geo.Bottom:
			desired.Y = content.Max.Y + 40 - moving.Min.Y
		}
		offset := desired.Add(packBoardRect(moving.Add(desired), occupied, 40))
		for _, child := range added {
			p.shift(child, offset)
			content = content.Union(boardOuterPixels(p.plan[child.root]))
		}
	}
	if !isRoot {
		frame := boardOuterPixels(root.Box)
		// Preserve the ordinary layout's room for titles, borders and ports.
		required := image.Rectangle{Min: content.Min.Sub(fresh.Min.Sub(frame.Min)), Max: content.Max.Add(frame.Max.Sub(fresh.Max))}
		if peer != nil && peer.IsContainer() && (!p.compact || len(root.ChildrenArray) == len(peer.ChildrenArray)) && peer.Label.Value == root.Label.Value && peer.LabelDimensions == root.LabelDimensions {
			prior := boardOuterPixels(peer.Box)
			if required.In(prior) {
				p.plan[root] = peer.Box.Copy()
				return g
			}
			required = required.Union(prior)
		}
		if required.Dx() < root.LabelDimensions.Width+16 {
			required.Max.X = required.Min.X + root.LabelDimensions.Width + 16
		}
		p.plan[root] = geo.NewBox(geo.NewPoint(float64(required.Min.X), float64(required.Min.Y)), float64(required.Dx()), float64(required.Dy()))
	}
	return g
}

func (p *boardPlacement) shift(group boardGroup, offset image.Point) {
	for _, n := range group.members {
		p.plan[n].TopLeft.X += float64(offset.X)
		p.plan[n].TopLeft.Y += float64(offset.Y)
	}
}

func (p *boardPlacement) valid(g *d2graph.Graph) bool {
	plan := p.plan
	for _, n := range g.Objects {
		b := plan[n]
		end := geo.NewPoint(b.TopLeft.X+b.Width, b.TopLeft.Y+b.Height)
		if !boardLeafPoint(b.TopLeft) || !boardLeafPoint(end) ||
			!boardLeafDimension(b.Width) || !boardLeafDimension(b.Height) {
			return false
		}
		if n.Parent != g.Root && (!plan[n.Parent].Contains(b.TopLeft) || !plan[n.Parent].Contains(end)) {
			return false
		}
	}
	// A rigid transfer must also be rigid after the exporter's truncation.
	for _, pairs := range p.moved {
		root := pairs[0]
		dx := int(plan[root.current].TopLeft.X) - int(root.old.TopLeft.X)
		dy := int(plan[root.current].TopLeft.Y) - int(root.old.TopLeft.Y)
		for _, a := range pairs {
			if int(plan[a.current].TopLeft.X)-int(a.old.TopLeft.X) != dx ||
				int(plan[a.current].TopLeft.Y)-int(a.old.TopLeft.Y) != dy {
				return false
			}
		}
	}

	return true
}

// An explicit root direction applies to branches at the root. Nested rows
// and columns can use a different axis inside those branches.
type boardFlowEdge struct {
	from, to  *d2graph.Object
	direction geo.Orientation
}

func (e boardFlowEdge) progress(plan map[*d2graph.Object]*geo.Box) float64 {
	from, to := e.from.Box, e.to.Box
	if plan != nil {
		from, to = plan[e.from], plan[e.to]
	}
	a, b := from.Center(), to.Center()
	switch e.direction {
	case geo.Right:
		return b.X - a.X
	case geo.Left:
		return a.X - b.X
	case geo.Top:
		return a.Y - b.Y
	default:
		return b.Y - a.Y
	}
}

func boardFlow(g *d2graph.Graph) []boardFlowEdge {
	if g.Root.Direction.Value == "" {
		return nil
	}
	dir := parseDirection(g.Root.Direction.Value)
	var edges []boardFlowEdge
	for _, e := range g.Edges {
		if e.SrcArrow == e.DstArrow {
			continue
		}
		a, b := e.Src, e.Dst
		if e.SrcArrow {
			a, b = b, a
		}
		for a.Parent != g.Root {
			a = a.Parent
		}
		for b.Parent != g.Root {
			b = b.Parent
		}
		f := boardFlowEdge{a, b, dir}
		if a != b && f.progress(nil) > 0 {
			edges = append(edges, f)
		}
	}
	return edges
}

// A new dependency can require one downstream branch to give way. Move that
// branch along its authored flow, retaining all descendants as one unit.
func advanceBoardFlow(g *d2graph.Graph, plan map[*d2graph.Object]*geo.Box, edges []boardFlowEdge) bool {
	for range len(g.Root.ChildrenArray) + 1 {
		changed := false
		for _, e := range edges {
			d := e.progress(plan)
			if d >= 0 {
				continue
			}
			advance := int(math.Ceil(-d + min(24, e.progress(nil))))
			unit := image.Point{}
			switch e.direction {
			case geo.Right:
				unit.X = 1
			case geo.Left:
				unit.X = -1
			case geo.Top:
				unit.Y = -1
			default:
				unit.Y = 1
			}
			origin := boardOuterPixels(plan[e.to])
			// Advancing strictly in one direction passes each obstacle at most once.
			for range len(g.Root.ChildrenArray) {
				at := origin.Add(unit.Mul(advance))
				next := advance
				for _, other := range g.Root.ChildrenArray {
					if other == e.to {
						continue
					}
					r := boardOuterPixels(plan[other]).Inset(-24)
					if !at.Overlaps(r) {
						continue
					}
					switch e.direction {
					case geo.Right:
						next = max(next, r.Max.X-origin.Min.X)
					case geo.Left:
						next = max(next, origin.Max.X-r.Min.X)
					case geo.Top:
						next = max(next, origin.Max.Y-r.Min.Y)
					case geo.Bottom:
						next = max(next, r.Max.Y-origin.Min.Y)
					}
				}
				if next == advance {
					break
				}
				advance = next
			}
			offset := unit.Mul(advance)
			for _, n := range g.Objects {
				parent := n
				for parent.Parent != g.Root {
					parent = parent.Parent
				}
				if parent == e.to {
					plan[n].TopLeft.X += float64(offset.X)
					plan[n].TopLeft.Y += float64(offset.Y)
				}
			}
			changed = true
		}
		if !changed {
			return true
		}
	}
	return false
}

func groupBoardSupported(g *d2graph.Graph) bool {
	if len(g.Objects) > 40 || len(g.Edges) > 48 || !supportsRefinementStyles(g) ||
		g.Root.Shape.Value != "" && g.Root.Shape.Value != d2target.ShapeRectangle {
		return false
	}
	for _, n := range g.Objects {
		if n.Shape.Value != "" && n.Shape.Value != d2target.ShapeRectangle && n.Shape.Value != d2target.ShapeSquare ||
			n.Direction.Value != "" || n.Language != "" || n.ZIndex != 0 || continuityHidden(n.Style) ||
			n.IsContainer() && (n.WidthAttr != nil || n.HeightAttr != nil || n.Shape.Value == d2target.ShapeSquare) ||
			n.LabelDimensions.Width > int(n.Width) || n.LabelDimensions.Height > int(n.Height) {
			return false
		}
		if n.LabelPosition != nil {
			p := label.FromString(*n.LabelPosition)
			if n.IsContainer() {
				if p != label.InsideTopCenter && p != label.InsideBottomCenter {
					return false
				}
			} else if p != label.InsideMiddleCenter {
				return false
			}
		}
	}
	for _, e := range g.Edges {
		if e.Label.Value != "" || e.IsCurve || e.SrcArrowhead != nil || e.DstArrowhead != nil ||
			e.Src == e.Dst || continuityHidden(e.Style) {
			return false
		}
	}
	return true
}

func boardOuterPixels(b *geo.Box) image.Rectangle {
	return image.Rect(int(math.Floor(b.TopLeft.X)), int(math.Floor(b.TopLeft.Y)), int(math.Ceil(b.TopLeft.X+b.Width)), int(math.Ceil(b.TopLeft.Y+b.Height)))
}

// Project the desired origin onto the clearance boundaries of surrounding
// rectangles. The closest clear intersection also handles L-shaped gaps.
func packBoardRect(rect image.Rectangle, obstacles []image.Rectangle, gap int) image.Point {
	xs, ys := []int{0}, []int{0}
	for _, r := range obstacles {
		r = r.Inset(-gap)
		xs = append(xs, r.Min.X-rect.Max.X, r.Max.X-rect.Min.X)
		ys = append(ys, r.Min.Y-rect.Max.Y, r.Max.Y-rect.Min.Y)
	}
	best := image.Point{}
	cost := math.Inf(1)
	for _, x := range xs {
		for _, y := range ys {
			d := float64(x)*float64(x) + float64(y)*float64(y)
			if d >= cost {
				continue
			}
			shift := image.Pt(x, y)
			at := rect.Add(shift)
			if !slices.ContainsFunc(obstacles, func(r image.Rectangle) bool { return r.Inset(-gap).Overlaps(at) }) {
				best, cost = shift, d
				if d == 0 {
					return best
				}
			}
		}
	}
	return best
}

func betterBoardPlan(anchors []boardAnchor, plan map[*d2graph.Object]*geo.Box, moved map[*d2graph.Object][]boardAnchor) bool {
	var oldFixed, newFixed int
	var oldTravel, newTravel float64
	for _, a := range anchors {
		o := a.old
		dist := func(b *geo.Box) float64 {
			return math.Abs(float64(int(b.TopLeft.X)-int(o.TopLeft.X))+float64(int(b.Width)-int(o.Width))/2) +
				math.Abs(float64(int(b.TopLeft.Y)-int(o.TopLeft.Y))+float64(int(b.Height)-int(o.Height))/2)
		}
		base, trial := dist(a.current.Box), dist(plan[a.current])
		oldTravel += base
		newTravel += trial
		if base == 0 {
			oldFixed++
		}
		if trial == 0 {
			newFixed++
		}
	}
	if newFixed < oldFixed || newTravel > oldTravel {
		return false
	}
	if newFixed > oldFixed || newTravel < oldTravel {
		return true
	}
	// Surrounding landmarks can already be fixed while a transferred team's
	// internal spacing changes. A rigid copy can improve that tie.
	for _, pairs := range moved {
		root := pairs[0]
		dx, dy := root.current.TopLeft.X-root.old.TopLeft.X, root.current.TopLeft.Y-root.old.TopLeft.Y
		for _, a := range pairs {
			if a.current.TopLeft.X-a.old.TopLeft.X != dx || a.current.TopLeft.Y-a.old.TopLeft.Y != dy ||
				a.current.Width != a.old.Width || a.current.Height != a.old.Height {
				return true
			}
		}
	}
	return false
}

func routeBoardPlan(ctx context.Context, previous, current *d2graph.Graph, plan map[*d2graph.Object]*geo.Box, moved map[*d2graph.Edge]*d2graph.Edge) bool {
	trial := layoutgraph.NewGraph()
	bindings, err := translateGraph(ctx, current, trial, true)
	if err != nil {
		return false
	}
	baseline, err := quality.Inspect(ctx, trial)
	if err != nil {
		return false
	}
	nodes := make(map[layoutgraph.EntityID]*layoutgraph.Node, len(trial.Nodes))
	for _, n := range trial.Nodes {
		nodes[n.ID] = n
	}
	var fresh, planned image.Rectangle
	for _, o := range current.Objects {
		n := nodes[bindings.objectIDs[o]]
		b := plan[o]
		n.Box = *b.Copy()
		fresh = fresh.Union(boardOuterPixels(o.Box))
		planned = planned.Union(boardOuterPixels(b))
	}
	if int64(planned.Dx())*int64(planned.Dy()) > 2*int64(fresh.Dx())*int64(fresh.Dy()) {
		return false
	}
	oldEdges := continuityEdges(previous)
	edges := make(map[layoutgraph.EntityID]*layoutgraph.Edge, len(trial.Edges))
	for _, e := range trial.Edges {
		edges[e.ID] = e
	}
	var changed []*layoutgraph.Edge
	for _, e := range current.Edges {
		edge := edges[bindings.edgeIDs[e]]
		from, to := plan[e.Src], plan[e.Dst]
		preserve := func(ref *d2graph.Edge) bool {
			if ref == nil || ref.Src.Width != from.Width || ref.Src.Height != from.Height || ref.Dst.Width != to.Width || ref.Dst.Height != to.Height ||
				!boardPortTouches(ref.Src.Box, ref.Route[0]) || !boardPortTouches(ref.Dst.Box, ref.Route[len(ref.Route)-1]) {
				return false
			}
			x, y := from.TopLeft.X-ref.Src.TopLeft.X, from.TopLeft.Y-ref.Src.TopLeft.Y
			// Reuse a path only if its endpoints undergo the same shift in
			// world coordinates and in the integer boxes used by the exporter.
			if x != to.TopLeft.X-ref.Dst.TopLeft.X || y != to.TopLeft.Y-ref.Dst.TopLeft.Y ||
				x != float64(int(from.TopLeft.X)-int(ref.Src.TopLeft.X)) || y != float64(int(from.TopLeft.Y)-int(ref.Src.TopLeft.Y)) ||
				x != float64(int(to.TopLeft.X)-int(ref.Dst.TopLeft.X)) || y != float64(int(to.TopLeft.Y)-int(ref.Dst.TopLeft.Y)) {
				return false
			}
			edge.Points = translatedBoardRoute(ref.Route, x, y)
			if edge.ReversedForLayout {
				slices.Reverse(edge.Points)
			}
			return true
		}
		if !preserve(oldEdges[e.AbsID()]) && !preserve(moved[e]) && !preserve(e) {
			changed = append(changed, edge)
		}
	}
	acceptable := func() bool {
		for _, e := range trial.Edges {
			if len(e.Points) < 2 || !boardPortTouches(&e.From.Box, e.SourcePort()) || !boardPortTouches(&e.To.Box, e.TargetPort()) {
				return false
			}
		}
		m, err := quality.Inspect(ctx, trial)
		return err == nil && m.NodeOverlaps <= baseline.NodeOverlaps && m.RouteObstructions <= baseline.RouteObstructions &&
			m.TextOcclusions <= baseline.TextOcclusions && m.InteriorCrossings <= baseline.InteriorCrossings && m.Crossings <= baseline.Crossings &&
			m.RouteLength <= baseline.RouteLength*1.6+1
	}
	if len(changed) > 0 && routing.RouteEdges(ctx, trial, changed) != nil {
		return false
	}
	if !acceptable() {
		if len(changed) == len(trial.Edges) || routing.RouteEdges(ctx, trial, trial.Edges) != nil || !acceptable() {
			return false
		}
	}
	for _, e := range trial.Edges {
		for _, p := range e.Points {
			if !boardLeafPoint(p) {
				return false
			}
		}
	}
	patch, err := buildRoutePatch(ctx, current, bindings, trial.Edges)
	if err != nil || ctx.Err() != nil {
		return false
	}
	for _, o := range current.Objects {
		o.Box = plan[o]
	}
	patch.apply()
	return true
}
