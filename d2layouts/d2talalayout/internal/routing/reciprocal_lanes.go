package routing

import (
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// A reciprocal edge can be routed around the outside of two boxes with an
// approach shorter than its endpoint text. Reuse its straight peer's facing
// sides if both labels and both shafts clear on a pair of parallel lanes.
func straightenLabeledReciprocal(g *layoutgraph.Graph, bent *layoutgraph.Edge) {
	if len(g.Edges) != 2 || !shortcutOrthogonal(bent.Points) || !isEdgeUShaped(bent) {
		return
	}
	straight := g.Edges[0]
	if bent == straight {
		straight = g.Edges[1]
	}
	if len(straight.Points) != 2 || !shortcutOrthogonal(straight.Points) || bent.From != straight.To || bent.To != straight.From {
		return
	}
	conflict := false
	for _, box := range laneBoxes(bent) {
		conflict = conflict || box.Overlaps(bent.From.Box) || box.Overlaps(bent.To.Box)
	}
	if !conflict {
		return
	}
	a, b := straight.SourcePort(), straight.TargetPort()
	face := fanFace(straight.From, a, b)
	if face == geo.NONE || fanFace(straight.To, b, a) != face.GetOpposite() {
		return
	}
	x := face.IsVertical()
	lo, hi := fanFaceSpan(straight.From, face)
	u, v := fanFaceSpan(straight.To, face.GetOpposite())
	lo, hi = math.Ceil(max(lo, u)+fanMarkerPitch/2), math.Floor(min(hi, v)-fanMarkerPitch/2)
	if hi-lo < fanMarkerPitch {
		return
	}
	slot := channelCoordinate(a, x)
	proposal := *bent
	proposal.Points = []*geo.Point{b, a}
	for _, edge := range []*layoutgraph.Edge{straight, &proposal} {
		for _, n := range []*layoutgraph.Node{edge.From, edge.To} {
			tip, next := fanEndpoint(edge, n)
			if _, ok := hierarchyMarkerBox(edge, n, tip, next); !ok {
				return
			}
		}
	}
	lanes := []labeledLane{{edge: straight, slot: slot}, {edge: &proposal, slot: slot}}
	// Try to keep the straight edge's ports, followed by roomier symmetric
	// assignments if a label on that edge also lacks clearance.
	third := math.Round((hi - lo) / 3)
	for _, pair := range [][2]float64{{slot, lo}, {slot, hi}, {lo + third, hi - third}, {hi - third, lo + third}, {lo, hi}, {hi, lo}} {
		if min(pair[0], pair[1]) < lo || max(pair[0], pair[1]) > hi || math.Abs(pair[0]-pair[1]) < fanMarkerPitch {
			continue
		}
		if laneLabelsClear(lanes, pair[:], x, g) {
			installLanes(lanes, pair[:], x)
			bent.Points = proposal.Points
			return
		}
	}
}
