package routing

import (
	"github.com/d2lang/d2/lib/geo"
)

type OVGEdge struct {
	From *OVGNode
	To   *OVGNode

	Distance float64
	// index is the edge's position in its OVG's Edges, which only grows. Edges
	// built outside OVG.Connect keep 0; users verify Edges[index] == edge.
	index int32
}

func NewOVGEdge(from, to *OVGNode) *OVGEdge {
	return &OVGEdge{
		From: from,
		To:   to,
	}
}

func (e *OVGEdge) isVertical() bool {
	return e.From.X == e.To.X
}

func (e *OVGEdge) isHorizontal() bool {
	return e.From.Y == e.To.Y
}

func (e *OVGEdge) sharePoints(other OVGEdge) bool {
	return (nonNilEquals(e.From.Point, other.From.Point) ||
		nonNilEquals(e.From.Point, other.To.Point) ||
		nonNilEquals(e.To.Point, other.From.Point) ||
		nonNilEquals(e.To.Point, other.To.Point))
}

// geo.Point.Equals skipping nil check
// TODO move to d2 as Equals, rename existing to NilEquals
func nonNilEquals(p1, p2 *geo.Point) bool {
	return p1.X == p2.X && p1.Y == p2.Y
}
