package d2lib

import (
	"image"
	"math"
	"slices"
	"sort"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
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
	pivot               geo.Point
	rebound             float64
	start, contact, end float64
	labelBox            *d2target.MotionBox
	labelAt             *geo.Point
}

type boardTrace struct {
	start, end float64
	backwards  bool
	points     []*geo.Point // walked from the familiar end
	lengths    []float64    // cumulative polyline length at each point
	minDraw    float64      // length needed to paint a partial route and its marker
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
			birth.end = float64(level)
		}
		// Fixed inside captions can be shown as soon as the body covers their ink.
		// Border captions and icons depend on the moving silhouette.
		pos := label.FromString(s.LabelPosition)
		if pos == label.Unset && (s.Language == "markdown" || s.Language == "latex") {
			pos = label.InsideMiddleCenter
		}
		if s.Label != "" && !integralBoardText(s) && s.Icon == nil && !pos.IsOutside() && !pos.IsBorder() && s.Width > 0 && s.Height > 0 {
			birth.labelBox = &d2target.MotionBox{Point: s.Pos, Width: s.Width, Height: s.Height}
			birth.labelAt = s.LabelTopLeft(pos, s.Geometry().GetInnerBox())
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
				end = start + .50
				break
			}
		}
		wire := newBoardTrace(e, backwards, start, end)
		if wire == nil {
			return nil
		}
		m.wires[e.ID] = wire
		m.end = max(m.end, end)
	}
	if m.end == 0 {
		return nil
	}
	for id, birth := range m.nodes {
		if e := arrival[id]; e != nil {
			wire := m.wires[e.ID]
			birth.start, birth.contact = wire.pickup(), wire.end
			m.nodes[id] = birth
		}
	}
	// Only grow past the authored box after reserving the extra space.
	// Large edits still get the damped arrival within the ordinary footprint.
	if len(next.Shapes) <= 40 && len(next.Connections) <= 48 {
		for _, n := range g.Objects {
			id := n.AbsID()
			if birth, ok := m.nodes[id]; ok {
				birth.rebound = m.popClearance(n, next, g, shapes, arrival[id], birth)
				m.nodes[id] = birth
			}
		}
	}
	return m
}

// Precompute arc lengths and the minimum visible stroke for both diagram
// arrivals and sequence messages. Points are ordered in the draw direction.
func newBoardTrace(e *d2target.Connection, backwards bool, start, end float64) *boardTrace {
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
	srcWidth, _ := e.SrcArrow.Dimensions(e.RenderedStrokeWidth())
	dstWidth, _ := e.DstArrow.Dimensions(e.RenderedStrokeWidth())
	return &boardTrace{
		start: start, end: end, backwards: backwards, points: points, lengths: lengths,
		minDraw: max(srcWidth, dstWidth, 2*e.RenderedStrokeWidth()),
	}
}

// Start opening the node over the last six pixels of the incoming stroke.
// On short routes keep the first 90% of the trace to the wire itself. Search
// the eased arc length once, including any slowdown through a final bend.
func (w *boardTrace) pickup() float64 {
	length := w.lengths[len(w.lengths)-1]
	target := max(length-6, .90*length, w.minDraw)
	if target >= length {
		return w.end // The marker first fits on the complete wire.
	}
	lo, hi := 0., 1.
	for range 20 {
		mid := (lo + hi) / 2
		if w.travel(mid)*length < target {
			lo = mid
		} else {
			hi = mid
		}
	}
	return w.start + (w.end-w.start)*hi
}

func (b boardBirth) progress(now float64) float64 {
	if b.contact == 0 {
		return gridPhase(now, b.start, b.end)
	}
	// At contact the body is already opening, even when a short route or a
	// sparse sample schedule puts the first visible wire and node on one frame.
	const atContact = .24
	if now < b.start {
		return 0
	}
	if now < b.contact {
		overlap := b.contact - b.start
		u := (now - b.start) / overlap
		// Match the opening speed on both sides of contact. A shorter
		// overlap on a long wire can otherwise make the node brake abruptly.
		endSlope := (1 - atContact) * overlap / (b.end - b.contact)
		return u * (atContact + (1-u)*(atContact-endSlope))
	}
	return atContact + (1-atContact)*gridPhase(now, b.contact, b.end)
}

func (w *boardTrace) travel(t float64) float64 {
	distance := boardStroke(t)
	if len(w.points) <= 2 {
		return distance
	}
	total := w.lengths[len(w.lengths)-1]
	at := distance * total
	i := sort.SearchFloat64s(w.lengths, at)
	if i > 0 && at-w.lengths[i-1] < w.lengths[i]-at {
		i--
	}
	if i == 0 || i == len(w.points)-1 {
		return distance
	}
	a, b, c := w.points[i-1], w.points[i], w.points[i+1]
	left, right := w.lengths[i]-w.lengths[i-1], w.lengths[i+1]-w.lengths[i]
	// A tiny jog or an almost straight join should read as one stroke.
	if min(left, right) < 8 || (b.X-a.X)*(c.X-b.X)+(b.Y-a.Y)*(c.Y-b.Y) > math.Sqrt2/2*left*right {
		return distance
	}
	return boardCorner(distance, w.lengths[i]/total, min(.10, min(left, right)/(2*total)))
}

// A short scale rebound around the arrival port is limited to five pixels.
// With a tight footprint use a critically damped approach, settling from below.
func (b boardBirth) pop(t float64) (scale, opacity float64) {
	alpha := gridEase(min(1, t/.32))
	if b.rebound == 0 {
		r := 1 - t
		return 1 - .68*r*r*(1+7*t)*math.Exp(-5*t), alpha
	}
	const peak = .59
	if t < peak {
		return .32 + (.68+b.rebound)*boardStroke(t/peak), alpha
	}
	return 1 + b.rebound*(1-gridEase((t-peak)/(1-peak))), alpha
}

func (m *boardReveal) popClearance(n *d2graph.Object, d *d2target.Diagram, g *d2graph.Graph, shapes map[string]*d2target.Shape, arrival *d2target.Connection, b boardBirth) float64 {
	s := shapes[n.AbsID()]
	// Decorated shapes can extend beyond the body. Their contained arrival
	// doesn't need a painted-silhouette approximation.
	if s.Type != d2target.ShapeRectangle || s.Shadow || s.ThreeDee || s.Multiple || s.Icon != nil || s.Language != "" ||
		s.LabelPosition != "" && label.FromString(s.LabelPosition) != label.InsideMiddleCenter ||
		s.LabelWidth > s.Width || s.LabelHeight > s.Height || s.Width < 1 || s.Height < 1 {
		return 0
	}
	body := image.Rect(s.Pos.X, s.Pos.Y, s.Pos.X+s.Width, s.Pos.Y+s.Height)
	if b.pivot.X < float64(body.Min.X) || b.pivot.X > float64(body.Max.X) || b.pivot.Y < float64(body.Min.Y) || b.pivot.Y > float64(body.Max.Y) {
		return 0
	}
	extra := min(.045, 5/float64(max(s.Width, s.Height)))
	// Include the growing stroke, pixel rounding, and a second entrant's
	// five-pixel rebound when two leaves are introduced together.
	pad := int(math.Ceil(extra*float64(max(s.Width, s.Height))+(1+extra)*s.RenderedStrokeWidth()/2)) + 2
	area := body.Inset(-pad)
	for _, other := range g.Objects {
		if n == other {
			continue
		}
		o := shapes[other.AbsID()]
		if o == nil {
			return 0
		}
		if n.IsDescendantOf(other) {
			if !o.CanDrawOutline() {
				return 0
			}
			r := image.Rect(o.Pos.X, o.Pos.Y, o.Pos.X+o.Width, o.Pos.Y+o.Height).Inset(2 + int(math.Ceil(o.RenderedStrokeWidth()/2)))
			if o.Label != "" {
				switch label.FromString(o.LabelPosition) {
				case label.InsideTopCenter:
					r.Min.Y += o.LabelHeight + 2*label.PADDING
				case label.InsideBottomCenter:
					r.Max.Y -= o.LabelHeight + 2*label.PADDING
				default:
					return 0
				}
			}
			if !area.In(r) {
				return 0
			}
		} else {
			neighbor := *d
			neighbor.Shapes = []d2target.Shape{*o}
			neighbor.Connections = nil
			lo, hi := neighbor.BoundingBox()
			reserved := image.Rect(lo.X, lo.Y, hi.X, hi.Y)
			if _, entering := m.nodes[o.ID]; entering {
				reserved = reserved.Inset(-6)
			}
			if area.Overlaps(reserved) {
				return 0
			}
		}
	}
	for i := range d.Connections {
		e := &d.Connections[i]
		if e.Opacity == 0 {
			continue
		}
		if len(e.Route) < 2 || slices.Contains(e.Route, nil) {
			return 0
		}
		if e == arrival && len(e.Route) == 2 {
			other := e.Route[0]
			if e.Src == s.ID {
				other = e.Route[1]
			}
			if b.pivot.X == float64(body.Min.X) && other.X < b.pivot.X || b.pivot.X == float64(body.Max.X) && other.X > b.pivot.X ||
				b.pivot.Y == float64(body.Min.Y) && other.Y < b.pivot.Y || b.pivot.Y == float64(body.Max.Y) && other.Y > b.pivot.Y {
				continue
			}
		}
		if w := m.wires[e.ID]; w != nil && w.start >= b.end {
			continue
		}
		if e.Icon != nil || e.Label != "" || e.SrcLabel != nil || e.DstLabel != nil || e.Tooltip != "" || e.Link != "" {
			return 0
		}
		r := area.Inset(-int(math.Ceil(boardArrowReach(e))))
		expanded := *s
		expanded.Pos = d2target.Point{X: r.Min.X, Y: r.Min.Y}
		expanded.Width, expanded.Height = r.Dx(), r.Dy()
		if transferWireCrosses(e, 0, 0, s, &expanded) {
			return 0
		}
	}
	return extra
}

// These renderers lay out the content and its body together. Introduce the
// authored block in one frame so code, equations and table rows stay legible.
func integralBoardText(s *d2target.Shape) bool {
	return s.Type == d2target.ShapeText || s.Type == d2target.ShapeCode ||
		s.Type == d2target.ShapeClass || s.Type == d2target.ShapeSQLTable ||
		s.Language != "" && s.Language != "markdown" && s.Language != "latex"
}

func (b boardBirth) sampleShape(final d2target.Shape, now float64) d2target.Shape {
	scale, alpha := b.pop(b.progress(now))
	if integralBoardText(&final) {
		if alpha < .5 {
			final.Opacity = 0
		}
		return final
	}
	x := b.pivot.X + (float64(final.Pos.X)-b.pivot.X)*scale
	y := b.pivot.Y + (float64(final.Pos.Y)-b.pivot.Y)*scale
	s := scaleMotionShape(final, x, y, scale, alpha)
	s.Text, s.MotionScale = final.Text, final.MotionScale
	if s.Label == "" {
		return s
	}
	if alpha == 1 {
		if sameSlot(&s, &final) {
			return s
		}
		if b.labelAt != nil {
			// An overshooting body already covers the authored silhouette,
			// including labels intentionally placed against its edge.
			if scale >= 1 {
				s.MotionLabelBox = b.labelBox
				return s
			}
			room := s.Geometry().GetInnerBox()
			p := b.labelAt
			pad := math.Max(label.PADDING, s.RenderedStrokeWidth()/2)
			if p.X >= room.TopLeft.X+pad && p.Y >= room.TopLeft.Y+pad &&
				p.X+float64(s.LabelWidth) <= room.TopLeft.X+room.Width-pad &&
				p.Y+float64(s.LabelHeight) <= room.TopLeft.Y+room.Height-pad {
				s.MotionLabelBox = b.labelBox
				return s
			}
		}
	}
	s.Label = ""
	return s
}

func (m *boardReveal) apply(frame *d2target.Diagram, progress float64) {
	now := progress * m.end
	for i := range frame.Shapes {
		s := &frame.Shapes[i]
		if birth, ok := m.nodes[s.ID]; ok && now < birth.end {
			final := *s
			*s = birth.sampleShape(final, now)
			// The appendix draws badges separately from the body. Attach them
			// at rest, including the first visible frame of a text or code card.
			if birth.rebound > 0 || s.Opacity != final.Opacity || !sameSlot(s, &final) {
				s.Tooltip, s.Link, s.PrettyLink = "", "", ""
				if final.Link != "" && final.Language == "markdown" {
					// The outer link suppresses links inside the Markdown. Keep
					// that caption with its link so a nested target never flashes.
					s.Label = ""
				}
			}
		}
	}
	result := make([]d2target.Connection, 0, len(frame.Connections))
	for _, e := range frame.Connections {
		if wire := m.wires[e.ID]; wire != nil {
			t := gridPhase(now, wire.start, wire.end)
			lengthFraction := wire.travel(t)
			if t < 1 {
				path := wire.prefix(lengthFraction)
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
				revealed := lengthFraction * wire.lengths[len(wire.lengths)-1]
				// Do not let a full-size marker overwhelm a tiny first segment.
				if revealed < wire.minDraw {
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
