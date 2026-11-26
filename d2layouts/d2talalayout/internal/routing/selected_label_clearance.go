package routing

import (
	"context"
	"math"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/labeling"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/xsort"
)

// labelInk indexes the same part of an edge before and after a trial. This
// preserves ownership when a board already has unrelated overlaps.
type labelInk struct {
	box  geo.Box
	text bool
}

func edgeLabelInk(e *layoutgraph.Edge) ([]labelInk, bool) {
	var ink []labelInk
	for _, box := range laneBoxes(e) {
		ink = append(ink, labelInk{box, true})
	}
	p := e.Points
	for i := 1; i < len(p); i++ {
		a, b := p[i-1], p[i]
		ink = append(ink, labelInk{box: geo.Box{TopLeft: geo.NewPoint(min(a.X, b.X)-1, min(a.Y, b.Y)-1), Width: math.Abs(b.X-a.X) + 2, Height: math.Abs(b.Y-a.Y) + 2}})
		if corner := fanCornerBounds(p, i); corner != nil {
			ink = append(ink, labelInk{box: *corner})
		}
	}
	last := len(p) - 1
	for _, end := range []struct {
		arrow     layoutgraph.Arrowhead
		tip, next *geo.Point
	}{
		{e.SourceArrowhead, p[0], p[1]}, {e.TargetArrowhead, p[last], p[last-1]},
	} {
		marker, ok := markerClearanceBox(e, end.arrow, end.tip, end.next)
		if !ok {
			return nil, false
		}
		if marker != nil {
			ink = append(ink, labelInk{box: *marker})
		}
	}
	return ink, true
}

func nodeTextBox(n *layoutgraph.Node) geo.Box {
	l := n.Label
	return geo.Box{TopLeft: n.LabelTopLeft(l.Position, l.Width, l.Height), Width: l.Width, Height: l.Height}
}

// A candidate must clear visible text without introducing another contact.
// In particular compare each marker, rounded corner, and painted shaft
// separately. Unrelated conflicts elsewhere on the board cannot pay for a move.
func textClearanceGain(before, after, obstacles []labelInk) int {
	if len(before) != len(after) {
		return -1
	}
	gain := 0
	for i, a := range after {
		b := before[i]
		// An endpoint annotation follows the changed route. It must also
		// clear its own opposite terminal and any newly adjacent turn.
		for j, other := range after[:i] {
			if (a.text || other.text) && overlapArea(a.box, other.box) > overlapArea(b.box, before[j].box) {
				return -1
			}
		}
		if *a.box.TopLeft == *b.box.TopLeft && a.box.Width == b.box.Width && a.box.Height == b.box.Height {
			continue
		}
		for _, o := range obstacles {
			was, now := overlapArea(b.box, o.box), overlapArea(a.box, o.box)
			if now > was {
				return -1
			}
			if was > 0 && now == 0 && (a.text || o.text) {
				gain++
			}
		}
	}
	return gain
}

func overlapArea(a, b geo.Box) float64 {
	if !a.Overlaps(b) {
		return 0
	}
	x := min(a.TopLeft.X+a.Width, b.TopLeft.X+b.Width) - max(a.TopLeft.X, b.TopLeft.X)
	y := min(a.TopLeft.Y+a.Height, b.TopLeft.Y+b.Height) - max(a.TopLeft.Y, b.TopLeft.Y)
	return x * y
}

// RefineOrdinaryLabelRoutes checks flat, rectangular drawings whose paint
// inventory fits a bounded local search. Keep the winning layout's bends,
// endpoint faces and directions when sliding a labeled terminal run.
func RefineOrdinaryLabelRoutes(ctx context.Context, g *layoutgraph.Graph) error {
	if len(g.Nodes) > 32 || len(g.Edges) > 32 || shortcutInputTooLarge(g) {
		return ctx.Err()
	}
	hasLabels := false
	var nodes []labelInk
	for _, n := range g.Nodes {
		if !ordinaryRectangle(n) || n.IsContainer() || n.Sequence != nil || n.Icon != nil {
			return ctx.Err()
		}
		nodes = append(nodes, labelInk{box: n.Box})
		if n.Label != nil && n.Label.Position != label.Unset {
			nodes = append(nodes, labelInk{nodeTextBox(n), true})
			hasLabels = hasLabels || n.Label.Position.IsOutside()
		}
	}
	for _, e := range g.Edges {
		if e.IsInvisible || e.IsCurve || e.HasTableColumn() || !shortcutOrthogonal(e.Points) ||
			e.Style.BorderRadius != nil {
			return ctx.Err()
		}
		hasLabels = hasLabels || e.SourceArrowheadLabel != nil || e.TargetArrowheadLabel != nil || e.IsLoop() && e.Label != nil
	}
	if !hasLabels {
		return ctx.Err()
	}
	ink := make([][]labelInk, len(g.Edges))
	for i, e := range g.Edges {
		var ok bool
		if ink[i], ok = edgeLabelInk(e); !ok {
			return ctx.Err()
		}
	}
	// Repair a singleton at its own corner before moving a neighboring port.
	for _, loops := range []bool{true, false} {
		for i, e := range g.Edges {
			if err := ctx.Err(); err != nil {
				return err
			}
			if e.IsLoop() != loops {
				continue
			}
			var candidates [][]*geo.Point
			if loops {
				candidates = labelLoopCorners(e)
			} else {
				candidates = labelPortLanes(e)
			}
			if len(candidates) == 0 {
				continue
			}
			obstacles := slices.Clone(nodes)
			for j, other := range ink {
				if i != j {
					obstacles = append(obstacles, other...)
				}
			}
			bestGain := 0
			bestLength := math.Inf(1)
			var best []*geo.Point
			var bestInk []labelInk
			for _, p := range candidates {
				if err := ctx.Err(); err != nil {
					return err
				}
				proposal := *e
				proposal.Points = p
				paint, ok := edgeLabelInk(&proposal)
				if !ok {
					continue
				}
				gain := textClearanceGain(ink[i], paint, obstacles)
				if length := shortcutLength(p); gain > 0 && (gain > bestGain || gain == bestGain && length < bestLength) {
					bestGain, bestLength, best, bestInk = gain, length, p, paint
				}
			}
			if best != nil {
				e.Points, ink[i] = best, bestInk
			}
		}
	}
	return ctx.Err()
}

func labelLoopCorners(e *layoutgraph.Edge) [][]*geo.Point {
	if len(e.Points) != 5 || shortcutBends(e.Points) != 3 || e.Label == nil && e.SourceArrowheadLabel == nil && e.TargetArrowheadLabel == nil {
		return nil
	}
	for _, other := range e.From.Edges {
		if other != e && other.IsLoop() {
			return nil
		}
	}
	c := e.From.Box.Center()
	var candidates [][]*geo.Point
	for _, flip := range [][2]bool{{false, true}, {true, false}, {true, true}} {
		p := make([]*geo.Point, len(e.Points))
		for i, old := range e.Points {
			p[i] = old.Copy()
			if flip[0] {
				p[i].X = 2*c.X - old.X
			}
			if flip[1] {
				p[i].Y = 2*c.Y - old.Y
			}
		}
		candidates = append(candidates, p)
		// A turn near a long annotation can consume the space beside the
		// final approach. Give that side one more corner radius if needed,
		// keeping both attachment points and their directions.
		for _, target := range []bool{false, true} {
			first, elbow := 0, 1
			if target {
				first, elbow = 4, 3
			}
			if target && e.TargetArrowheadLabel == nil || !target && e.SourceArrowheadLabel == nil {
				continue
			}
			x := p[first].Y == p[elbow].Y
			lane := channelCoordinate(p[elbow], x)
			lane += math.Copysign(fanCornerRadius, lane-channelCoordinate(p[first], x))
			extended := slices.Clone(p)
			extended[elbow] = atCorridorCoordinate(p[elbow], x, lane)
			extended[2] = atCorridorCoordinate(p[2], x, lane)
			candidates = append(candidates, extended)
		}
	}
	return candidates
}

func labelPortLanes(e *layoutgraph.Edge) [][]*geo.Point {
	var candidates [][]*geo.Point
	for _, n := range []*layoutgraph.Node{e.From, e.To} {
		tip, next := fanEndpoint(e, n)
		face := fanFace(n, tip, next)
		if face == geo.NONE {
			continue
		}
		x := face.IsVertical()
		slot := channelCoordinate(tip, x)
		var slots []float64
		if n.Label != nil && n.Label.Position.IsOutside() {
			box := nodeTextBox(n)
			lo, hi := boxInterval(box, x)
			perpendicularLo, perpendicularHi := boxInterval(box, !x)
			a, b := channelCoordinate(tip, !x), channelCoordinate(next, !x)
			if slot >= lo-fanMarkerPitch/2 && slot <= hi+fanMarkerPitch/2 && min(a, b) < perpendicularHi && max(a, b) > perpendicularLo {
				slots = append(slots, lo-fanMarkerPitch/2-1, hi+fanMarkerPitch/2+1)
			}
		}
		own := labeling.PositionArrowheadLabel(e, n == e.To, e.Points)
		if own != nil {
			a, b := boxInterval(own.Box, x)
			// Only one other label needs to establish each boundary. A
			// disjoint label or the unlabeled end of an edge adds no trial.
			for _, other := range n.Edges {
				if e == other || other.IsLoop() {
					continue
				}
				text := labeling.PositionArrowheadLabel(other, n == other.To, other.Points)
				if text == nil || !own.Box.Overlaps(text.Box) {
					continue
				}
				lo, hi := boxInterval(text.Box, x)
				slots = append(slots, slot+lo-b-label.PADDING, slot+hi-a+label.PADDING)
			}
		}
		xsort.Ordered(slots)
		for _, s := range slices.Compact(slots) {
			lane := math.Round(s)
			lo, hi := fanFaceSpan(n, face)
			if lane < lo+fanMarkerPitch/2 || lane > hi-fanMarkerPitch/2 {
				continue
			}
			p := slices.Clone(e.Points)
			first, second := 0, 1
			if n == e.To {
				first, second = len(p)-1, len(p)-2
			}
			p[first], p[second] = atCorridorCoordinate(p[first], x, lane), atCorridorCoordinate(p[second], x, lane)
			if len(p) == 2 {
				other := e.From
				if n == e.From {
					other = e.To
				}
				otherFace := fanFace(other, p[second], p[first])
				lo, hi = fanFaceSpan(other, otherFace)
				if otherFace == geo.NONE || lane < lo+fanMarkerPitch/2 || lane > hi-fanMarkerPitch/2 {
					continue
				}
			} else {
				i := 2*second - first
				if !sameRouteDirection(e.Points[second], e.Points[i], p[second], p[i]) {
					continue
				}
			}
			candidates = append(candidates, p)
		}
	}
	return candidates
}
