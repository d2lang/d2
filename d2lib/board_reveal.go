package d2lib

import (
	"math"
	"slices"
	"sort"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// Trace new connections outward from familiar objects. A new leaf grows
// around its incoming port, so the arriving wire stays attached throughout
// the pop. Additional branches can then grow from that leaf.
type boardReveal struct {
	nodes map[string]boardBirth
	wires map[string]*boardTrace
	end   float64
}

type boardBirth struct {
	pivot      geo.Point
	start, end float64
}

type boardTrace struct {
	start, end float64
	backwards  bool
	points     []*geo.Point // walked from the familiar end
	lengths    []float64    // cumulative polyline length at each point
}

func planBoardReveal(prev, next *d2target.Diagram, g *d2graph.Graph) *boardReveal {
	if len(next.Shapes) > 256 || len(next.Connections) > 256 || len(prev.Shapes) > len(next.Shapes) || len(prev.Connections) > len(next.Connections) {
		return nil
	}
	shapes := gridEdgeShapeMap(next.Shapes)
	depth := make(map[string]int, len(shapes))
	var queue []string
	for i := range prev.Shapes {
		s := &prev.Shapes[i]
		if n := shapes[s.ID]; n == nil || n.Type != s.Type || !sameSlot(s, n) {
			return nil
		}
		depth[s.ID] = 0
		queue = append(queue, s.ID)
	}
	oldEdges := make(map[string]*d2target.Connection, len(prev.Connections))
	for i := range prev.Connections {
		oldEdges[prev.Connections[i].ID] = &prev.Connections[i]
	}
	added := make([]*d2target.Connection, 0, len(next.Connections)-len(prev.Connections))
	links := make(map[string][]*d2target.Connection)
	common := 0
	for i := range next.Connections {
		e := &next.Connections[i]
		if old := oldEdges[e.ID]; old != nil {
			if e.Src != old.Src || e.Dst != old.Dst || e.IsCurve != old.IsCurve ||
				!slices.EqualFunc(e.Route, old.Route, func(a, b *geo.Point) bool { return a != nil && b != nil && *a == *b }) {
				return nil
			}
			common++
			continue
		}
		if e.Opacity == 0 {
			continue
		}
		if e.IsCurve || len(e.Route) < 2 || slices.Contains(e.Route, nil) || shapes[e.Src] == nil || shapes[e.Dst] == nil {
			return nil
		}
		added = append(added, e)
		links[e.Src] = append(links[e.Src], e)
		links[e.Dst] = append(links[e.Dst], e)
	}
	if common != len(oldEdges) {
		return nil
	}
	// Choose one incoming route per leaf in native edge order. For two or
	// more new tiers, finish each pop before extending the next tier's wire.
	arrival := make(map[string]*d2target.Connection)
	for i := 0; i < len(queue); i++ {
		id := queue[i]
		for _, e := range links[id] {
			to := e.Dst
			if id == to {
				to = e.Src
			}
			if _, seen := depth[to]; seen {
				continue
			}
			depth[to] = depth[id] + 1
			arrival[to] = e
			queue = append(queue, to)
		}
	}
	m := &boardReveal{nodes: make(map[string]boardBirth), wires: make(map[string]*boardTrace)}
	for _, n := range g.Objects {
		if n.IsContainer() {
			continue
		}
		id := n.AbsID()
		s := shapes[id]
		if s == nil || s.Opacity == 0 {
			continue
		}
		level, connected := depth[id]
		if connected && level == 0 {
			continue
		}
		birth := boardBirth{pivot: gridEdgeCenter(s), end: .5}
		if e := arrival[id]; e != nil {
			port := e.Route[len(e.Route)-1]
			if e.Src == id {
				port = e.Route[0]
			}
			birth.pivot = *port
			birth.start = float64(level-1) + .58
			birth.end = float64(level)
		}
		m.nodes[id] = birth
		m.end = max(m.end, birth.end)
	}
	for _, e := range added {
		backwards := e.DstArrow == d2target.NoArrowhead && e.SrcArrow != d2target.NoArrowhead
		start := max(m.nodes[e.Src].end, m.nodes[e.Dst].end)
		end := start + .8
		for _, id := range []string{e.Src, e.Dst} {
			if arrival[id] == e {
				backwards = e.Src == id
				start = float64(depth[id] - 1)
				end = start + .60
				break
			}
		}
		points := e.Route
		if backwards {
			points = slices.Clone(points)
			slices.Reverse(points)
		}
		lengths := make([]float64, len(points))
		for i := 1; i < len(points); i++ {
			lengths[i] = lengths[i-1] + math.Hypot(points[i].X-points[i-1].X, points[i].Y-points[i-1].Y)
			if !(lengths[i] > lengths[i-1]) || math.IsInf(lengths[i], 0) {
				return nil
			}
		}
		m.wires[e.ID] = &boardTrace{start, end, backwards, points, lengths}
		m.end = max(m.end, end)
	}
	if m.end == 0 {
		return nil
	}
	return m
}

func (m *boardReveal) apply(frame *d2target.Diagram, progress float64) {
	now := progress * m.end
	for i := range frame.Shapes {
		s := &frame.Shapes[i]
		if birth, ok := m.nodes[s.ID]; ok && now < birth.end {
			ratio := gridPhase(now, birth.start, birth.end)
			scale, alpha := gridPop(ratio)
			x := birth.pivot.X + (float64(s.Pos.X)-birth.pivot.X)*scale
			y := birth.pivot.Y + (float64(s.Pos.Y)-birth.pivot.Y)*scale
			*s = scaleMotionShape(*s, x, y, scale, alpha)
			// SVG draws link and tooltip badges in a separate appendix layer.
			// Attach them once the node settles at its final size and location.
			s.Tooltip, s.Link, s.PrettyLink = "", "", ""
		}
	}
	result := make([]d2target.Connection, 0, len(frame.Connections))
	for _, e := range frame.Connections {
		if wire := m.wires[e.ID]; wire != nil {
			t := gridPhase(now, wire.start, wire.end)
			if t < 1 {
				path := wire.prefix(gridEase(t))
				if len(path) < 2 {
					continue
				}
				e.Route = path
			}
			// Labels and badges use the full route for placement.
			if t < 1 || now < max(m.nodes[e.Src].end, m.nodes[e.Dst].end) {
				e.Label, e.Icon, e.SrcLabel, e.DstLabel = "", nil, nil, nil
			}
			if t < 1 {
				revealed := gridEase(t) * wire.lengths[len(wire.lengths)-1]
				// Do not let a full-size marker overwhelm a tiny first segment.
				w1, _ := e.SrcArrow.Dimensions(e.RenderedStrokeWidth())
				w2, _ := e.DstArrow.Dimensions(e.RenderedStrokeWidth())
				if revealed < max(w1, w2, 2*e.RenderedStrokeWidth()) {
					continue
				}
			}
		}
		result = append(result, e)
	}
	frame.Connections = result
}

func (w *boardTrace) prefix(fraction float64) []*geo.Point {
	if fraction <= 0 {
		return nil
	}
	at := fraction * w.lengths[len(w.lengths)-1]
	i := sort.SearchFloat64s(w.lengths, at)
	if i == 0 {
		return nil
	}
	if i == len(w.lengths) {
		i--
	}
	path := slices.Clone(w.points[:i])
	end := w.points[i]
	if at < w.lengths[i] {
		end = w.points[i-1].Interpolate(end, (at-w.lengths[i-1])/(w.lengths[i]-w.lengths[i-1]))
	}
	if *path[len(path)-1] != *end {
		path = append(path, end)
	}
	if w.backwards {
		slices.Reverse(path)
	}
	return path
}
