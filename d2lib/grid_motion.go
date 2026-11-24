package d2lib

import (
	"math"
	"slices"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
)

// A grid is laid out at each authored endpoint. Plan the transition using full
// D2 IDs so a reorder, a label edit, and a new occupant of a slot stay distinct.
type gridMotion struct {
	items                    map[string]*gridMotionItem
	removed                  []d2target.Shape
	hasEnter, hasExit, moves bool
	edges                    *gridMotionEdges
}

type gridMotionItem struct {
	from, to   *d2target.Shape
	arcX, arcY float64
	track      *gridShuffleTrack
	carrier    *gridMotionItem
	// A whole entering or leaving subtree scales around its outermost new
	// cell, following the nearest surviving parent as the grid rearranges.
	pivot  *d2target.Shape
	parent *gridMotionItem
}

type gridNode struct {
	shape        *d2target.Shape
	parent       *gridNode
	inGrid, cell bool
}

type gridSlot struct {
	parent string
	x, y   int
}

func (n *gridNode) slot() gridSlot {
	s := gridSlot{x: n.shape.Pos.X, y: n.shape.Pos.Y}
	if n.parent != nil {
		s.parent = n.parent.shape.ID
		s.x -= n.parent.shape.Pos.X
		s.y -= n.parent.shape.Pos.Y
	}
	return s
}

func gridNodes(board *d2target.Diagram, graph *d2graph.Graph) map[string]*gridNode {
	if graph == nil || graph.Root == nil || board == nil {
		return nil
	}
	hasGrid := graph.Root.IsGridDiagram()
	for _, obj := range graph.Objects {
		if obj.IsGridDiagram() {
			hasGrid = true
			break
		}
	}
	if !hasGrid {
		return nil
	}
	nodes := gridShapeNodes(board)
	var walk func(obj *d2graph.Object, parent *gridNode, prefix string, inGrid bool)
	walk = func(obj *d2graph.Object, parent *gridNode, prefix string, inGrid bool) {
		for _, child := range obj.ChildrenArray {
			id := prefix + child.ID
			node := nodes[id]
			if node == nil {
				continue
			}
			node.parent, node.cell = parent, obj.IsGridDiagram()
			node.inGrid = inGrid || child.IsGridDiagram()
			walk(child, node, id+".", node.inGrid)
		}
	}
	walk(graph.Root, nil, "", graph.Root.IsGridDiagram())
	return nodes
}

func newGridMotion(from, to *d2target.Diagram, fromGraph, toGraph *d2graph.Graph) *gridMotion {
	if from == nil || to == nil {
		return nil
	}
	old, next := gridNodes(from, fromGraph), gridNodes(to, toGraph)
	if old == nil && next == nil {
		return nil
	}
	// A step can turn the first grid on or the last grid off. Its existing
	// shapes still have identities on the side with no grid.
	if old == nil {
		old = gridShapeNodes(from)
	}
	if next == nil {
		next = gridShapeNodes(to)
	}
	m := &gridMotion{items: make(map[string]*gridMotionItem)}
	occupied := make(map[gridSlot]*gridNode)
	for i := range from.Shapes {
		n := old[from.Shapes[i].ID]
		if n.cell {
			occupied[n.slot()] = n
		}
	}
	for i := range to.Shapes {
		id := to.Shapes[i].ID
		a, b := old[id], next[id]
		if !b.inGrid && (a == nil || !a.inGrid) {
			continue
		}
		item := &gridMotionItem{to: b.shape}
		if a == nil {
			m.hasEnter = true
		} else {
			item.from = a.shape
			if a.shape.Pos == b.shape.Pos && a.shape.Width == b.shape.Width && a.shape.Height == b.shape.Height {
				continue
			}
			m.moves = true
			// A takeover of a still-present sibling's slot gets an arc.
			// Opposing movements take opposite sides; ordinary reflow is straight.
			if a.cell && b.cell {
				if other := occupied[b.slot()]; other != nil && other != a && next[other.shape.ID] != nil {
					item.arcX, item.arcY = gridShuffleArc(a, b)
				}
			}
		}
		m.items[id] = item
	}
	for i := range from.Shapes {
		s := &from.Shapes[i]
		if next[s.ID] == nil && old[s.ID].inGrid {
			m.items[s.ID] = &gridMotionItem{from: s}
			m.removed = append(m.removed, *s)
			m.hasExit = true
		}
	}
	if len(m.items) == 0 {
		return nil
	}
	planGridShuffles(old, next, m.items)
	// A descendant follows its parent's detour as well as its own local
	// reflow. Absolute endpoint positions already include linear parent motion.
	carriers := make(map[*gridNode]*gridMotionItem)
	var carrier func(*gridNode) *gridMotionItem
	carrier = func(n *gridNode) *gridMotionItem {
		if n == nil {
			return nil
		}
		if item, ok := carriers[n]; ok {
			return item
		}
		item := carrier(n.parent)
		if own := m.items[n.shape.ID]; own != nil && own.from != nil && own.to != nil {
			own.carrier = item
			item = own
		}
		carriers[n] = item
		return item
	}
	for id, item := range m.items {
		switch {
		case item.from == nil:
			item.bubbleOrigin(next[id], old, m.items)
		case item.to == nil:
			item.bubbleOrigin(old[id], next, m.items)
		default:
			carrier(next[id])
		}
	}
	moving := make(map[string]bool, len(m.items))
	for id := range m.items {
		moving[id] = true
	}
	m.edges = newGridMotionEdges(from, to, moving)
	return m
}

func gridShapeNodes(board *d2target.Diagram) map[string]*gridNode {
	nodes := make(map[string]*gridNode, len(board.Shapes))
	for i := range board.Shapes {
		s := &board.Shapes[i]
		nodes[s.ID] = &gridNode{shape: s}
	}
	return nodes
}

func (item *gridMotionItem) bubbleOrigin(n *gridNode, other map[string]*gridNode, items map[string]*gridMotionItem) {
	for n.parent != nil && n.parent.inGrid && other[n.parent.shape.ID] == nil {
		n = n.parent
	}
	item.pivot = n.shape
	if n.parent != nil {
		item.parent = items[n.parent.shape.ID]
	}
}

func gridShuffleArc(from, to *gridNode) (float64, float64) {
	a, b := from.slot(), to.slot()
	dx, dy := float64(b.x-a.x), float64(b.y-a.y)
	distance := math.Hypot(dx, dy)
	if distance == 0 {
		return 0, 0
	}
	nx, ny := -dy/distance, dx/distance
	size := math.Abs(nx)*float64(from.shape.Width) + math.Abs(ny)*float64(from.shape.Height)
	height := math.Min(size*.58, distance*.85)
	return nx * height, ny * height
}

func (m *gridMotion) sample(base *d2target.Diagram, fraction float64) *d2target.Diagram {
	result := *base
	result.Layers, result.Scenarios, result.Steps, result.TweenFrames = nil, nil, nil, nil
	result.TweenDurationMS = 0
	result.Shapes = make([]d2target.Shape, 0, len(base.Shapes)+len(m.removed))
	start, end := 0., 1.
	if m.hasExit {
		start = .16
	}
	if m.hasEnter {
		end = .82
	}
	travel := gridEase(gridPhase(fraction, start, end))
	// Let a departing cell clear its slot before a replacement opens. If
	// there are only additions or only removals, use the whole duration.
	enterStart, exitEnd := 0., 1.
	if m.moves || m.hasExit {
		enterStart = .40
	}
	if m.moves || m.hasEnter {
		exitEnd = .52
	}
	enter := gridPhase(fraction, enterStart, 1)
	exit := 1 - gridPhase(fraction, 0, exitEnd)
	enterOpacity := gridEase(min(1, enter/.50))
	exitOpacity := gridEase(min(1, exit/.50))
	for _, shape := range base.Shapes {
		item := m.items[shape.ID]
		switch {
		case item == nil:
		case item.from == nil:
			if enter == 0 {
				continue
			}
			shape = item.bubble(shape, enter, enterOpacity, travel)
		case item.to != nil:
			shape.Pos = item.position(travel)
			shape.Width = gridLerpInt(item.from.Width, item.to.Width, travel)
			shape.Height = gridLerpInt(item.from.Height, item.to.Height, travel)
		default:
			// A sampled base may already contain this departing item. The
			// ordered removal list below supplies its old board appearance.
			continue
		}
		result.Shapes = append(result.Shapes, shape)
	}
	if exit > 0 {
		for _, s := range m.removed {
			result.Shapes = append(result.Shapes, m.items[s.ID].bubble(s, exit, exitOpacity, travel))
		}
	}
	result.Connections = m.edges.sample(base.Connections, result.Shapes, travel, enterOpacity, exitOpacity)
	return &result
}

func (item *gridMotionItem) position(t float64) d2target.Point {
	a, b := item.from.Pos, item.to.Pos
	x, y := 0., 0.
	for it := item; it != nil; it = it.carrier {
		if it.track != nil {
			v := it.track.at(t)
			x += v.x
			y += v.y
		} else {
			bend := 4 * t * (1 - t)
			x += bend * it.arcX
			y += bend * it.arcY
		}
	}
	return d2target.Point{
		X: int(math.Round(float64(a.X) + (float64(b.X)-float64(a.X))*t + x)),
		Y: int(math.Round(float64(a.Y) + (float64(b.Y)-float64(a.Y))*t + y)),
	}
}

func (item *gridMotionItem) bubble(s d2target.Shape, t, opacity, travel float64) d2target.Shape {
	// Ease-out-back produces a single small overshoot and lands exactly at 1.
	u := t - 1
	scale := 1 + 2.45*u*u*u + 1.45*u*u
	p := item.pivot
	cx, cy := float64(p.Pos.X)+float64(p.Width)/2, float64(p.Pos.Y)+float64(p.Height)/2
	x := cx + (float64(s.Pos.X)-cx)*scale
	y := cy + (float64(s.Pos.Y)-cy)*scale
	if parent := item.parent; parent != nil && parent.from != nil && parent.to != nil {
		offset := parent.from.Pos
		if item.to != nil {
			offset = parent.to.Pos
		}
		pos := parent.position(travel)
		x += float64(pos.X - offset.X)
		y += float64(pos.Y - offset.Y)
	}
	s.Pos = d2target.Point{X: int(math.Round(x)), Y: int(math.Round(y))}
	scaled := func(v int) int {
		if v == 0 {
			return 0
		}
		return max(1, int(math.Round(float64(v)*scale)))
	}
	s.Width, s.Height = scaled(s.Width), scaled(s.Height)
	s.FontSize, s.LabelWidth, s.LabelHeight = scaled(s.FontSize), scaled(s.LabelWidth), scaled(s.LabelHeight)
	s.StrokeWidth, s.BorderRadius, s.IconBorderRadius = scaled(s.StrokeWidth), scaled(s.BorderRadius), scaled(s.IconBorderRadius)
	if len(s.Columns) > 0 && scale != 1 {
		s.Columns = slices.Clone(s.Columns)
		for i := range s.Columns {
			c := &s.Columns[i]
			c.Name.LabelWidth, c.Type.LabelWidth = scaled(c.Name.LabelWidth), scaled(c.Type.LabelWidth)
		}
	}
	s.Opacity *= opacity
	return s
}

func gridLerpInt(a, b int, t float64) int {
	return int(math.Round(float64(a) + (float64(b)-float64(a))*t))
}

func gridPhase(t, start, end float64) float64 { return max(0, min(1, (t-start)/(end-start))) }
func gridEase(t float64) float64              { return t * t * (3 - 2*t) }
