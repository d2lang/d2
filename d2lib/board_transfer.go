package d2lib

import (
	"math"
	"reflect"
	"slices"
	"strings"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// A change of scope changes every ID in a subtree. Animate the handoff only
// when one identifiable subtree and its wiring survive the edit. The sampled
// shapes use the actual previous and next layouts, including host frame sizes.
type boardTransfer struct {
	before     *d2target.Diagram
	shapes     map[string]*d2target.Shape // current ID -> previous shape
	moved      map[string]bool
	stays      map[string]*d2target.Connection // current ID -> old rigid wire
	oldStays   map[string]bool
	captionIDs map[string]string // shared host whose title changes sides
}

func newBoardTransfer(prev, next *d2target.Diagram, oldGraph, newGraph *d2graph.Graph) *boardTransfer {
	if prev == nil || next == nil || oldGraph == nil || newGraph == nil || !next.LayoutContinuity ||
		len(prev.Shapes) != len(next.Shapes) || len(next.Shapes) > 40 || len(prev.Connections) != len(next.Connections) || len(next.Connections) > 48 {
		return nil
	}
	oldObjects := make(map[string]*d2graph.Object, len(oldGraph.Objects))
	for _, o := range oldGraph.Objects {
		oldObjects[o.AbsID()] = o
	}
	var root *d2graph.Object
	for _, n := range newGraph.Objects {
		if oldObjects[n.AbsID()] != nil || (n.Parent != newGraph.Root && oldObjects[n.Parent.AbsID()] == nil) {
			continue
		}
		if root != nil {
			return nil
		}
		root = n
	}
	if root == nil {
		return nil
	}
	unique := func(nodes []*d2graph.Object, id string) *d2graph.Object {
		var match *d2graph.Object
		for _, n := range nodes {
			if strings.EqualFold(n.ID, id) {
				if match != nil {
					return nil
				}
				match = n
			}
		}
		return match
	}
	prior := unique(oldGraph.Objects, root.ID)
	if prior == nil || unique(newGraph.Objects, root.ID) != root {
		return nil
	}
	oldShapes, newShapes := gridEdgeShapeMap(prev.Shapes), gridEdgeShapeMap(next.Shapes)
	if newShapes[prior.AbsID()] != nil {
		return nil
	}
	p := &boardTransfer{before: prev, shapes: make(map[string]*d2target.Shape), moved: make(map[string]bool), stays: make(map[string]*d2target.Connection), oldStays: make(map[string]bool)}
	oldRoot, newRoot := oldShapes[prior.AbsID()], newShapes[root.AbsID()]
	if oldRoot == nil || newRoot == nil || oldRoot.Pos == newRoot.Pos {
		return nil
	}
	dx, dy := newRoot.Pos.X-oldRoot.Pos.X, newRoot.Pos.Y-oldRoot.Pos.Y
	var match func(*d2graph.Object, *d2graph.Object) bool
	match = func(a, b *d2graph.Object) bool {
		if a == nil || len(a.Children) != len(b.Children) {
			return false
		}
		x, y := oldShapes[a.AbsID()], newShapes[b.AbsID()]
		if x == nil || y == nil || x.Width != y.Width || x.Height != y.Height || y.Pos.X-x.Pos.X != dx || y.Pos.Y-x.Pos.Y != dy || x.LabelPosition != y.LabelPosition {
			return false
		}
		p.shapes[y.ID], p.moved[y.ID] = x, true
		for _, child := range b.ChildrenArray {
			if !match(a.Children[strings.ToLower(child.ID)], child) {
				return false
			}
		}
		return true
	}
	if !match(prior, root) {
		return nil
	}
	p.captionIDs = make(map[string]string)
	for i := range next.Shapes {
		b := &next.Shapes[i]
		a := p.shapes[b.ID]
		if a == nil {
			a = oldShapes[b.ID]
		}
		if a == nil || a.Type != d2target.ShapeRectangle || a.Icon != nil || a.Shadow || a.ThreeDee || a.Multiple || a.Link != "" || a.Tooltip != "" || len(a.Classes) != 0 || a.Language != "" {
			return nil
		}
		s := *a
		s.ID, s.Pos, s.Width, s.Height, s.LabelPosition = b.ID, b.Pos, b.Width, b.Height, b.LabelPosition
		if !reflect.DeepEqual(s, *b) {
			return nil
		}
		p.shapes[b.ID] = a
		if a.LabelPosition != b.LabelPosition && b.Label != "" {
			id := b.ID + ".__handoff_title"
			for oldShapes[id] != nil || newShapes[id] != nil {
				id += "_"
			}
			p.captionIDs[b.ID] = id
		}
	}
	// A service cannot travel through another service. Test overlapping
	// interiors over the whole linear trajectory; easing changes only time.
	hosts := make(map[string]bool)
	for _, child := range []*d2graph.Object{prior, root} {
		for n := child.Parent; n != nil; n = n.Parent {
			hosts[n.AbsID()] = true
		}
	}
	currentObjects := make(map[string]*d2graph.Object, len(newGraph.Objects))
	for _, n := range newGraph.Objects {
		currentObjects[n.AbsID()] = n
	}
	for i := range next.Shapes {
		a := &next.Shapes[i]
		oa, na := oldObjects[p.shapes[a.ID].ID], currentObjects[a.ID]
		if oa == nil || na == nil {
			return nil
		}
		for j := 0; j < i; j++ {
			b := &next.Shapes[j]
			ob, nb := oldObjects[p.shapes[b.ID].ID], currentObjects[b.ID]
			// The receiving and departing frames can pass each other as they
			// resize. Their contents stay above both backgrounds.
			if hosts[a.ID] && hosts[b.ID] || oa.IsDescendantOf(ob) || ob.IsDescendantOf(oa) ||
				na.IsDescendantOf(nb) || nb.IsDescendantOf(na) {
				continue
			}
			if motionBoxesCross(p.shapes[a.ID], a, p.shapes[b.ID], b) {
				return nil
			}
		}
	}
	type edgeKey struct {
		src, dst           string
		srcArrow, dstArrow bool
		index              int
	}
	oldEdges := make(map[edgeKey]*d2target.Connection, len(prev.Connections))
	edgesByID := make(map[string]*d2target.Connection, len(prev.Connections))
	for i := range prev.Connections {
		e := &prev.Connections[i]
		edgesByID[e.ID] = e
	}
	for _, e := range oldGraph.Edges {
		oldEdges[edgeKey{e.Src.AbsID(), e.Dst.AbsID(), e.SrcArrow, e.DstArrow, e.Index}] = edgesByID[e.AbsID()]
	}
	newEdges := make(map[string]*d2target.Connection, len(next.Connections))
	for i := range next.Connections {
		e := &next.Connections[i]
		newEdges[e.ID] = e
	}
	for _, g := range newGraph.Edges {
		b := newEdges[g.AbsID()]
		if b == nil || p.shapes[b.Src] == nil || p.shapes[b.Dst] == nil {
			return nil
		}
		a := oldEdges[edgeKey{p.shapes[b.Src].ID, p.shapes[b.Dst].ID, g.SrcArrow, g.DstArrow, g.Index}]
		if a == nil {
			return nil
		}
		s := *a
		s.ID, s.Src, s.Dst, s.Route = b.ID, b.Src, b.Dst, b.Route
		if !reflect.DeepEqual(s, *b) {
			return nil
		}
		rigid := len(a.Route) == len(b.Route) && len(a.Route) >= 2
		x := float64(newShapes[b.Src].Pos.X - p.shapes[b.Src].Pos.X)
		y := float64(newShapes[b.Src].Pos.Y - p.shapes[b.Src].Pos.Y)
		rigid = rigid && newShapes[b.Src].Width == p.shapes[b.Src].Width && newShapes[b.Src].Height == p.shapes[b.Src].Height &&
			newShapes[b.Dst].Width == p.shapes[b.Dst].Width && newShapes[b.Dst].Height == p.shapes[b.Dst].Height &&
			newShapes[b.Dst].Pos.X-p.shapes[b.Dst].Pos.X == int(x) && newShapes[b.Dst].Pos.Y-p.shapes[b.Dst].Pos.Y == int(y)
		if rigid {
			for j, point := range b.Route {
				if point == nil || a.Route[j] == nil || math.Abs(point.X-a.Route[j].X-x) > 1e-6 || math.Abs(point.Y-a.Route[j].Y-y) > 1e-6 {
					rigid = false
					break
				}
			}
		}
		if rigid {
			for i := range next.Shapes {
				n := &next.Shapes[i]
				old := p.shapes[n.ID]
				o1, o2 := oldObjects[old.ID], currentObjects[n.ID]
				if g.Src.IsDescendantOf(o2) || g.Dst.IsDescendantOf(o2) ||
					oldObjects[a.Src].IsDescendantOf(o1) || oldObjects[a.Dst].IsDescendantOf(o1) {
					continue
				}
				if transferWireCrosses(a, int(x), int(y), old, n) {
					rigid = false
					break
				}
			}
		}
		if rigid {
			p.stays[b.ID], p.oldStays[a.ID] = a, true
		} else if p.moved[b.Src] && p.moved[b.Dst] {
			return nil
		}
	}
	return p
}

// In the wire's moving coordinate system a translating route is fixed.
// The union of the endpoint boxes bounds every intermediate position and
// size of the obstacle, including instants between animation samples.
func transferWireCrosses(wire *d2target.Connection, dx, dy int, old, next *d2target.Shape) bool {
	if wire.Opacity == 0 || old.Opacity == 0 && next.Opacity == 0 {
		return false
	}
	endX, endY := next.Pos.X-dx, next.Pos.Y-dy
	if old.Pos.X == endX && old.Pos.Y == endY && old.Width == next.Width && old.Height == next.Height {
		return false
	}
	pad := 1 + (wire.RenderedStrokeWidth()+old.RenderedStrokeWidth())/2
	left, top := float64(min(old.Pos.X, endX))-pad, float64(min(old.Pos.Y, endY))-pad
	right, bottom := float64(max(old.Pos.X+old.Width, endX+next.Width))+pad, float64(max(old.Pos.Y+old.Height, endY+next.Height))+pad
	box := geo.NewBox(geo.NewPoint(left, top), right-left, bottom-top)
	if wire.IsCurve && (len(wire.Route)-1)%3 != 0 {
		return true
	}
	var last geo.Point
	for cmd := range wire.PathCommands(&geo.Point{}, &geo.Point{}) {
		switch cmd.Kind {
		case 'L':
			if box.Contains(&cmd.End) || box.Contains(&last) || box.Intersects(geo.Segment{Start: &last, End: &cmd.End}, 0) {
				return true
			}
		case 'C', 'S':
			// A cubic, including a rounded corner, stays inside the
			// bounds of its endpoints and control points.
			if max(last.X, cmd.End.X, cmd.Control1.X, cmd.Control2.X) >= left && min(last.X, cmd.End.X, cmd.Control1.X, cmd.Control2.X) <= right &&
				max(last.Y, cmd.End.Y, cmd.Control1.Y, cmd.Control2.Y) >= top && min(last.Y, cmd.End.Y, cmd.Control1.Y, cmd.Control2.Y) <= bottom {
				return true
			}
		}
		last = cmd.End
	}
	return false
}

// Both box edges move linearly, so intersection of the four open half-planes
// gives the exact time interval when the rectangles overlap.
func motionBoxesCross(a, b, c, d *d2target.Shape) bool {
	lo, hi := 0., 1.
	for _, s := range [][2]int{
		{a.Pos.X + a.Width - c.Pos.X, b.Pos.X + b.Width - d.Pos.X},
		{c.Pos.X + c.Width - a.Pos.X, d.Pos.X + d.Width - b.Pos.X},
		{a.Pos.Y + a.Height - c.Pos.Y, b.Pos.Y + b.Height - d.Pos.Y},
		{c.Pos.Y + c.Height - a.Pos.Y, d.Pos.Y + d.Height - b.Pos.Y},
	} {
		if s[0] <= 0 && s[1] <= 0 {
			return false
		}
		if s[0] <= 0 {
			lo = max(lo, float64(s[0])/float64(s[0]-s[1]))
		} else if s[1] <= 0 {
			hi = min(hi, float64(s[0])/float64(s[0]-s[1]))
		}
	}
	return lo < hi
}

func (p *boardTransfer) sample(base *d2target.Diagram, t float64) *d2target.Diagram {
	// Leave and reconnect at rest. A wire never points at a vacated port.
	const leave, arrive = .18, .78
	board := base
	if t < leave {
		board = p.before
	}
	frame := *board
	frame.Layers, frame.Scenarios, frame.Steps, frame.TweenFrames = nil, nil, nil, nil
	frame.TweenDurationMS, frame.TweenFrameEndsMS, frame.GridMotion = 0, nil, false
	frame.Shapes = slices.Clone(board.Shapes)
	frame.Connections = make([]d2target.Connection, 0, len(board.Connections))
	travel := gridEase(gridPhase(t, leave, arrive))
	if t >= leave {
		for i := range frame.Shapes {
			s := &frame.Shapes[i]
			old := p.shapes[s.ID]

			// Round the common displacement, so every member, even one on the
			// opposite side of world zero, travels by the same pixel.
			s.Pos = d2target.Point{X: old.Pos.X + int(math.Round(float64(s.Pos.X-old.Pos.X)*travel)), Y: old.Pos.Y + int(math.Round(float64(s.Pos.Y-old.Pos.Y)*travel))}
			s.Width, s.Height = gridLerpInt(old.Width, s.Width, travel), gridLerpInt(old.Height, s.Height, travel)
		}
	}
	// A host title can switch from its bottom to its top as it grows.
	// Dissolve that caption at rest instead of moving text over the children.
	if t > 0 && t < 1 {
		for i := range frame.Shapes {
			s := &frame.Shapes[i]
			id, changes := p.captionIDs[s.ID]
			if !changes {
				continue
			}
			caption := *s
			s.Label = ""
			s.LabelWidth, s.LabelHeight = 0, 0
			alpha := gridEase(gridPhase(t, arrive, 1))
			if t < leave {
				alpha = 1 - gridEase(t/leave)
			}
			if alpha > 0 {
				caption.ID = id
				caption.Fill, caption.Stroke, caption.FillPattern = "transparent", "transparent", ""
				caption.DoubleBorder = false
				caption.Opacity *= alpha
				frame.Shapes = append(frame.Shapes, caption)
			}
		}
	}
	for _, e := range board.Connections {
		if t < leave {
			if !p.oldStays[e.ID] {
				e.Opacity *= 1 - gridEase(t/leave)
			}
		} else if before := p.stays[e.ID]; before != nil {
			if travel < 1 && (before.Route[0].X != e.Route[0].X || before.Route[0].Y != e.Route[0].Y) {
				dx := math.Round((e.Route[0].X - before.Route[0].X) * travel)
				dy := math.Round((e.Route[0].Y - before.Route[0].Y) * travel)
				e.Route = make([]*geo.Point, len(before.Route))
				for i, pt := range before.Route {
					e.Route[i] = geo.NewPoint(pt.X+dx, pt.Y+dy)
				}
			}
		} else {
			e.Opacity *= gridEase(gridPhase(t, arrive, 1))
		}
		if e.Opacity > 0 {
			frame.Connections = append(frame.Connections, e)
		}
	}
	return &frame
}
