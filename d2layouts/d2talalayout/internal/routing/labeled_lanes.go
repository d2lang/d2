package routing

import (
	"math"
	"slices"
	"sort"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/labeling"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

// Work on a single parallel bundle whose entire obstacle inventory is known.
func CanRefineLabeledLanes(g *layoutgraph.Graph) bool {
	if len(g.Nodes) != 2 || len(g.Edges) < 2 || len(g.Edges) > 16 {
		return false
	}
	for _, n := range g.Nodes {
		if n.IsContainer() || n.IsInvisible || n.Is3D || n.IsMultiple || n.Icon != nil || n.Sequence != nil ||
			(n.Label != nil && n.Label.Position.IsOutside()) ||
			(n.ShapeType() != "" && n.ShapeType() != shape.SQUARE_TYPE && n.ShapeType() != shape.REAL_SQUARE_TYPE) {
			return false
		}
	}
	reciprocal := len(g.Edges) == 2 && g.Edges[0].From == g.Edges[1].To && g.Edges[0].To == g.Edges[1].From
	labeled := false
	for _, e := range g.Edges {
		if e.IsLoop() || e.IsInvisible || e.IsCurve || e.HasTableColumn() || (len(e.Points) != 2 && !(reciprocal && len(e.Points) == 4)) ||
			e.Style.StrokeWidth != nil || e.Style.BorderRadius != nil {
			return false
		}
		labeled = labeled || e.Label != nil || e.SourceArrowheadLabel != nil || e.TargetArrowheadLabel != nil
	}
	return labeled
}

type labeledLane struct {
	edge   *layoutgraph.Edge
	slot   float64
	labels []geo.Box
}

func routeLabelBox(e *layoutgraph.Edge, includePaint bool) geo.Box {
	l := e.Label
	p := e.LabelTopLeft(l.Position, l.Width, l.Height)
	padX, padY := 0., 0.
	if includePaint {
		// SVG masks extend 2px past the text on either side, including for
		// off-edge labels. A filled label paints a larger background.
		padX = 2
		if e.Style.Fill != nil && e.Style.Fill.Value != "" {
			padX, padY = 4, 3
		}
	}
	return geo.Box{TopLeft: geo.NewPoint(p.X-padX, p.Y-padY), Width: l.Width + 2*padX, Height: l.Height + 2*padY}
}

func laneBoxes(e *layoutgraph.Edge) []geo.Box {
	var boxes []geo.Box
	if e.Label != nil && e.Label.Position != label.Unset {
		boxes = append(boxes, routeLabelBox(e, true))
	}
	for _, target := range []bool{false, true} {
		if end := labeling.PositionArrowheadLabel(e, target, e.Points); end != nil {
			boxes = append(boxes, end.Box)
		}
	}
	return boxes
}

func boxInterval(b geo.Box, x bool) (float64, float64) {
	if x {
		return b.TopLeft.X, b.TopLeft.X + b.Width
	}
	return b.TopLeft.Y, b.TopLeft.Y + b.Height
}

// RefineLabeledLanes packs up to sixteen straight routes on the common part of
// two facing sides. End labels may additionally exchange lanes on a reciprocal
// pair. All geometry is checked before installing replacement routes.
func RefineLabeledLanes(g *layoutgraph.Graph) {
	if !CanRefineLabeledLanes(g) {
		return
	}
	for _, e := range g.Edges {
		if len(e.Points) != 2 {
			straightenLabeledReciprocal(g, e)
			return
		}
	}
	first := g.Edges[0]
	x := first.SourcePort().X == first.TargetPort().X // transverse coordinate
	flow := func(p *geo.Point) float64 { return channelCoordinate(p, !x) }
	start, end := flow(first.SourcePort()), flow(first.TargetPort())
	if start == end {
		return
	}
	lo, hi := boxInterval(first.From.Box, x)
	l, r := boxInterval(first.To.Box, x)
	lo, hi = max(lo, l), min(hi, r)
	var lanes []labeledLane
	for _, e := range g.Edges {
		a, b := e.SourcePort(), e.TargetPort()
		slot := channelCoordinate(a, x)
		if slot != channelCoordinate(b, x) || min(flow(a), flow(b)) != min(start, end) ||
			max(flow(a), flow(b)) != max(start, end) {
			return
		}
		for _, n := range []*layoutgraph.Node{e.From, e.To} {
			tip, next := fanEndpoint(e, n)
			near, far := boxInterval(n.Box, !x)
			if (flow(tip) != near || flow(next) >= near) && (flow(tip) != far || flow(next) <= far) {
				return
			}
			if _, ok := hierarchyMarkerBox(e, n, tip, next); !ok {
				return
			}
		}
		lanes = append(lanes, labeledLane{edge: e, slot: slot, labels: laneBoxes(e)})
	}
	sort.SliceStable(lanes, func(i, j int) bool { return lanes[i].slot < lanes[j].slot })
	slots := make([]float64, len(lanes))
	for i, lane := range lanes {
		slots[i] = lane.slot
	}
	if laneLabelsClear(lanes, slots, x, g) {
		return
	}
	if len(lanes) == 2 && lanes[0].edge.From != lanes[1].edge.From {
		slots[0], slots[1] = slots[1], slots[0]
		if laneLabelsClear(lanes, slots, x, g) {
			installLanes(lanes, slots, x)
			return
		}
	}
	// The projected label intervals constrain the distance between every pair
	// of shafts. A reverse sweep bounds each lane by its successors, then a
	// forward sweep keeps it as close to its incumbent position as possible.
	n := len(lanes)
	const padding = 2.0
	gaps := make([][]float64, n)
	left, right := lo, hi
	for i, a := range lanes {
		gaps[i] = make([]float64, n)
		for _, box := range a.labels {
			u, v := boxInterval(box, x)
			left = min(left, u)
			right = max(right, v)
		}
		for j := i + 1; j < n; j++ {
			b := lanes[j]
			gap := fanMarkerPitch
			for _, box := range a.labels {
				_, v := boxInterval(box, x)
				gap = max(gap, v-a.slot+padding)
			}
			for _, box := range b.labels {
				u, _ := boxInterval(box, x)
				gap = max(gap, b.slot-u+padding)
			}
			for _, p := range a.labels {
				_, pmax := boxInterval(p, x)
				p0, p1 := boxInterval(p, !x)
				for _, q := range b.labels {
					qmin, _ := boxInterval(q, x)
					q0, q1 := boxInterval(q, !x)
					if min(p1, q1)+padding > max(p0, q0) {
						gap = max(gap, pmax-a.slot-qmin+b.slot+padding)
					}
				}
			}
			gaps[i][j] = math.Ceil(gap)
		}
	}
	// Find the least outward growth that leaves the bundle feasible.
	lower := make([]float64, n)
	pack := func(growth int) bool {
		for i := n - 1; i >= 0; i-- {
			a := lanes[i]
			minimum, upper := lo+fanMarkerPitch/2+padding, hi-fanMarkerPitch/2-padding
			for _, box := range a.labels {
				u, v := boxInterval(box, x)
				minimum = max(minimum, left-float64(growth)-u+a.slot)
				upper = min(upper, right+float64(growth)-v+a.slot)
			}
			for j := i + 1; j < n; j++ {
				upper = min(upper, slots[j]-gaps[i][j])
			}
			lower[i] = math.Ceil(minimum)
			slots[i] = math.Floor(upper)
			if slots[i] < lower[i] {
				return false
			}
		}
		for i, a := range lanes {
			minimum := lower[i]
			for j := 0; j < i; j++ {
				minimum = max(minimum, slots[j]+gaps[j][i])
			}
			slots[i] = min(slots[i], max(minimum, math.Round(a.slot)))
		}
		return true
	}
	if hi <= lo {
		return
	}
	maxGrowth := int(math.Ceil(hi - lo))
	growth := sort.Search(maxGrowth+1, pack)
	if growth <= maxGrowth && pack(growth) && laneLabelsClear(lanes, slots, x, g) {
		installLanes(lanes, slots, x)
	}
}

func moveLane(lane labeledLane, slot float64, x bool) []*geo.Point {
	points := slices.Clone(lane.edge.Points)
	for i, p := range points {
		q := *p
		if x {
			q.X = slot
		} else {
			q.Y = slot
		}
		points[i] = &q
	}
	return points
}

func installLanes(lanes []labeledLane, slots []float64, x bool) {
	for i, a := range lanes {
		if a.slot != slots[i] {
			a.edge.Points = moveLane(a, slots[i], x)
		}
	}
}

func laneLabelsClear(lanes []labeledLane, slots []float64, x bool, g *layoutgraph.Graph) bool {
	type obstacle struct {
		owner int
		box   geo.Box
	}
	var text, ink []obstacle
	for i, a := range lanes {
		e := *a.edge
		e.Points = moveLane(a, slots[i], x)
		for _, b := range laneBoxes(&e) {
			text = append(text, obstacle{i, b})
		}
		p, q := e.SourcePort(), e.TargetPort()
		ink = append(ink, obstacle{i, *geo.NewBox(geo.NewPoint(min(p.X, q.X)-1, min(p.Y, q.Y)-1), math.Abs(p.X-q.X)+2, math.Abs(p.Y-q.Y)+2)})
		for _, node := range []*layoutgraph.Node{e.From, e.To} {
			tip, next := fanEndpoint(&e, node)
			if marker, _ := hierarchyMarkerBox(&e, node, tip, next); marker != nil {
				ink = append(ink, obstacle{i, *marker})
			}
		}
	}
	for i, t := range text {
		for _, other := range text[:i] {
			if t.box.Overlaps(other.box) {
				return false
			}
		}
		for _, other := range ink {
			if t.owner != other.owner && t.box.Overlaps(other.box) {
				return false
			}
		}
		for _, node := range g.Nodes {
			if t.box.Overlaps(node.Box) {
				return false
			}
		}
	}
	return true
}
