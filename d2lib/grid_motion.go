package d2lib

import (
	"math"
	"slices"
	"sort"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
)

// A grid is laid out at each authored endpoint. Plan the transition using full
// D2 IDs so a reorder, a label edit, and a new occupant of a slot stay distinct.
type gridMotion struct {
	items                    map[string]*gridMotionItem
	removed                  []d2target.Shape
	paintOrder               map[string]int
	hasEnter, hasExit, moves bool
	edges                    *gridMotionEdges
	bands                    map[string]gridBand
	folds                    map[string]*gridFold
	foldStart, foldEnd       float64
	details                  map[string]gridFoldDetail
	handoffs                 *gridHandoffs
}

type gridMotionItem struct {
	from, to *d2target.Shape
	// A whole entering or leaving subtree scales around its outermost new
	// cell, following the nearest surviving parent as the grid rearranges.
	pivot  *d2target.Shape
	parent *gridMotionItem
}

type gridNode struct {
	shape                                  *d2target.Shape
	parent                                 *gridNode
	children                               []*gridNode
	inGrid, gridCell, isGrid, containsGrid bool
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
			node.parent = parent
			node.isGrid = child.IsGridDiagram()
			// Constant-near objects are laid out separately from the root grid.
			detached := obj == graph.Root && child.IsConstantNear()
			node.gridCell = obj.IsGridDiagram() && !detached
			if parent != nil {
				parent.children = append(parent.children, node)
			}
			node.inGrid = node.isGrid || (inGrid && !detached)
			walk(child, node, id+".", node.inGrid)
			if parent != nil && (node.isGrid || node.containsGrid) {
				parent.containsGrid = true
			}
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
			if !sameSlot(a.shape, b.shape) {
				m.moves = true
			} else if len(to.TweenFrames) == 0 {
				// A paint sample can reflow an intermediate label and displace
				// a stationary neighbor. Pin it only when paint frames exist.
				continue
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
	if !m.moves && !m.hasEnter && !m.hasExit {
		return nil
	}
	m.bands = planGridBands(old, next)
	m.addGridSurroundings(old, next)
	m.planGridFolds(old, next)
	m.handoffs = detectGridHandoffs(old, next)
	// During a pure reorder, retain the original stacking order while cells
	// cross. The renderer still applies explicit z-indices and container depth.
	if m.moves && !m.hasEnter && !m.hasExit && len(from.Shapes) == len(to.Shapes) &&
		!slices.EqualFunc(from.Shapes, to.Shapes, func(a, b d2target.Shape) bool { return a.ID == b.ID }) {
		m.paintOrder = make(map[string]int, len(from.Shapes))
		for i, s := range from.Shapes {
			if next[s.ID] == nil { // An unrelated edit also changed membership.
				m.paintOrder = nil
				break
			}
			m.paintOrder[s.ID] = i
		}
	}
	for id, item := range m.items {
		switch {
		case item.from == nil:
			item.bubbleOrigin(next[id], old, m.items)
		case item.to == nil:
			item.bubbleOrigin(old[id], next, m.items)
		}
	}
	moving := make(map[string]bool, len(m.items))
	for id := range m.items {
		moving[id] = true
	}
	m.edges = newGridMotionEdges(from, to, fromGraph, toGraph, moving)
	return m
}

// A reflow can resize containers enclosing a grid and displace their neighbors.
func (m *gridMotion) addGridSurroundings(old, next map[string]*gridNode) {
	// External shapes follow the grid clock, including the earlier slide
	// during a band insertion when all the changing cells share that timing.
	layoutBand := gridBandNone
	for id, item := range m.items {
		if item.from != nil && item.to != nil && sameSlot(item.from, item.to) {
			continue
		}
		band := m.bands[id]
		if band == gridBandNone || (layoutBand != gridBandNone && layoutBand != band) {
			layoutBand = gridBandNone
			break
		}
		layoutBand = band
	}
	for id, b := range next {
		a := old[id]
		if a == nil || b.inGrid || a.inGrid || sameSlot(a.shape, b.shape) {
			continue
		}
		if !a.containsGrid && !b.containsGrid && (a.shape.Width != b.shape.Width || a.shape.Height != b.shape.Height) {
			continue
		}
		m.items[id] = &gridMotionItem{from: a.shape, to: b.shape}
		m.moves = true
		if layoutBand != gridBandNone {
			m.bands[id] = layoutBand
		}
	}
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

// popWindows gives membership changes their own short gestures within the step.
// A shuffle can take the full configured duration while a cell stays settled.
func (m *gridMotion) popWindows(durationMS int) (enterStart, enterEnd, exitEnd int) {
	if m.hasExit {
		exitEnd = min(160, durationMS)
		if m.hasEnter {
			exitEnd = min(exitEnd, (durationMS+1)/2)
		}
	}
	if m.hasEnter {
		switch {
		case m.moves:
			// Let the old occupants move out of the incoming cells.
			enterStart = durationMS / 2
		case m.hasExit:
			// A direct replacement overlaps only the tail of the exit.
			enterStart = 3 * exitEnd / 4
		}
		enterEnd = min(durationMS, enterStart+220)
	}
	return enterStart, enterEnd, exitEnd
}

func (m *gridMotion) sample(base *d2target.Diagram, elapsedMS, durationMS int) *d2target.Diagram {
	result := *base
	result.Layers, result.Scenarios, result.Steps, result.TweenFrames = nil, nil, nil, nil
	result.TweenDurationMS = 0
	result.TweenFrameEndsMS = nil
	result.Shapes = make([]d2target.Shape, 0, len(base.Shapes)+len(m.removed))
	fraction := float64(elapsedMS) / float64(durationMS)
	travel := gridEase(fraction)
	if len(m.folds) != 0 {
		travel = gridEase(gridPhase(fraction, m.foldStart, m.foldEnd))
	}
	elapsed := float64(elapsedMS)
	enterStart, enterEnd, exitEnd := m.popWindows(durationMS)
	// A connection alone can enter or leave during a shuffle. In that case
	// its fade follows the step clock alongside the moving endpoints.
	enterScale, exitScale := 1., 1.
	enterOpacity, exitOpacity := travel, gridEase(1-fraction)
	if m.hasEnter {
		enterScale, enterOpacity = gridPop(gridPhase(elapsed, float64(enterStart), float64(enterEnd)))
	}
	if m.hasExit {
		exitScale, exitOpacity = gridPop(1 - gridPhase(elapsed, 0, float64(exitEnd)))
	}
	// Edges involving a membership change follow that cell's visibility.
	var visibility map[string]float64
	if len(m.edges.pairs) != 0 {
		visibility = make(map[string]float64)
	}
	member := func(shape d2target.Shape, item *gridMotionItem, scale, alpha float64) (d2target.Shape, bool) {
		id := shape.ID
		if detail, ok := m.details[id]; ok {
			shape, alpha = detail.sample(shape, fraction, travel)
		} else {
			position := travel
			if band := m.bands[id]; band != gridBandNone {
				_, scale, alpha = band.sample(elapsedMS, durationMS)
			}
			if parent := item.parent; parent != nil && parent.to != nil && m.folds[parent.to.ID] == nil {
				if band := m.bands[parent.to.ID]; band != gridBandNone {
					position, _, _ = band.sample(elapsedMS, durationMS)
				}
			}
			if alpha > 0 {
				shape = item.bubble(shape, scale, alpha, position)
			}
		}
		if visibility != nil {
			visibility[id] = alpha
		}
		return shape, alpha > 0
	}
	for _, shape := range base.Shapes {
		item := m.items[shape.ID]
		switch {
		case item == nil:
		case item.from == nil:
			var visible bool
			shape, visible = member(shape, item, enterScale, enterOpacity)
			if !visible {
				continue
			}
		case item.to != nil:
			position := travel
			if fold := m.folds[shape.ID]; fold != nil {
				fold.caption(&shape, travel)
			} else if band := m.bands[shape.ID]; band != gridBandNone {
				position, _, _ = band.sample(elapsedMS, durationMS)
			}
			shape.Pos = item.position(position)
			shape.Width = gridLerpInt(item.from.Width, item.to.Width, position)
			shape.Height = gridLerpInt(item.from.Height, item.to.Height, position)
		default:
			continue
		}
		result.Shapes = append(result.Shapes, shape)
	}
	for _, s := range m.removed {
		if shape, visible := member(s, m.items[s.ID], exitScale, exitOpacity); visible {
			result.Shapes = append(result.Shapes, shape)
		}
	}
	if m.paintOrder != nil && fraction < 1 {
		sort.SliceStable(result.Shapes, func(i, j int) bool {
			return m.paintOrder[result.Shapes[i].ID] < m.paintOrder[result.Shapes[j].ID]
		})
	}
	result.Shapes = m.handoffs.apply(result.Shapes, elapsedMS, durationMS)
	if m.handoffs != nil && visibility != nil {
		t := gridEase(gridPhase(elapsed, 0, float64(min(gridHandoffMS, durationMS))))
		for _, pair := range m.handoffs.pairs {
			visibility[pair.from.ID] = 1 - t
			visibility[pair.to.ID] = t
		}
	}
	result.Connections = m.edges.sample(base.Connections, m.handoffs.edgeShapes(result.Shapes), travel, enterOpacity, exitOpacity, visibility)
	result.Connections = m.handoffs.bindEdges(result.Connections, elapsedMS, durationMS)
	return &result
}

// Interpolate in diagram coordinates. A nested item's two positions already
// include its parent's motion, so the same straight path works at any depth.
func (item *gridMotionItem) position(t float64) d2target.Point {
	a, b := item.from.Pos, item.to.Pos
	return d2target.Point{X: gridLerpInt(a.X, b.X, t), Y: gridLerpInt(a.Y, b.Y, t)}
}

func (item *gridMotionItem) bubble(s d2target.Shape, scale, opacity, travel float64) d2target.Shape {
	// A concurrent numeric tween can produce a separately laid-out paint
	// sample. Grow around the incoming board's slot even when that sample
	// places a neighbor at a different width. Removed cells already come
	// from the outgoing board.
	if item.to != nil {
		s.Pos, s.Width, s.Height = item.to.Pos, item.to.Width, item.to.Height
	}
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
	if s.Language == "latex" && scale != 1 {
		s.MotionScale = scale
	}
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

// Sample the same gesture forward for entry and backward for exit. Quadratic
// ease-out settles at full size. Fade only near the small end so the change
// in speed stays visible for most of the gesture.
func gridPop(visible float64) (scale, opacity float64) {
	remaining := 1 - visible
	return 1 - .60*remaining*remaining, gridEase(min(1, visible/.36))
}

func gridLerpInt(a, b int, t float64) int {
	return int(math.Round(float64(a) + (float64(b)-float64(a))*t))
}

func gridPhase(t, start, end float64) float64 { return max(0, min(1, (t-start)/(end-start))) }
func gridEase(t float64) float64              { return t * t * (3 - 2*t) }
