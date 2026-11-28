package d2target

import (
	"math"
	"slices"

	"github.com/d2lang/d2/lib/geo"
)

// SequenceProgressAt returns linear message travel on the standalone board clock.
func SequenceProgressAt(step, elapsedMS int) float64 {
	if step == 0 {
		return 1
	}
	return clampUnit(float64(elapsedMS-(step-1)*SequenceMessageIntervalMS) / SequenceMessageRevealMS)
}

// SequencePlaybackFrame samples the geometry without changing the authored
// diagram. The full routes remain available on the original for labels, icons,
// gradient paint, and stable viewport and annotation placement.
func (d *Diagram) SequencePlaybackFrame(elapsedMS int) *Diagram {
	frame := *d
	frame.Shapes = slices.Clone(d.Shapes)
	for i := range frame.Shapes {
		s := &frame.Shapes[i]
		if s.SequenceStep == 0 {
			continue
		}
		height := s.SequenceHeightAt(elapsedMS)
		if height <= 0 {
			s.Opacity = 0
		} else {
			s.Height = int(math.Round(height))
		}
	}
	frame.Connections = slices.Clone(d.Connections)
	for i := range frame.Connections {
		c := &frame.Connections[i]
		progress := SequenceProgressAt(c.SequenceStep, elapsedMS)
		if progress >= 1 {
			continue
		}
		if progress <= 0 {
			c.Opacity = 0
			continue
		}
		// Sample the already-rounded centerline, including the return bend of
		// self messages. A source-only arrow moves in the opposite direction.
		points := flattenRoute(c)
		from, to := 0., progress
		c.MotionTip = TraceDestination
		if c.SrcArrow != NoArrowhead && c.DstArrow == NoArrowhead {
			from, to = 1-progress, 1.
			c.MotionTip = TraceSource
			if c.StrokeDash > 0 && !c.Animated {
				c.MotionDashOffset += strokeLength(points) * from
			}
		}
		part := cutStrokes([][]geo.Point{points}, &Annotation{Drawn: &[2]float64{from, to}})
		if len(part) == 0 {
			c.Opacity = 0
			continue
		}
		c.Route = make([]*geo.Point, len(part[0]))
		for j := range part[0] {
			c.Route[j] = &part[0][j]
		}
		c.IsCurve = false
		c.BorderRadius = 0
	}
	return &frame
}
