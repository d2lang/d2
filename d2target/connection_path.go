package d2target

import (
	"iter"
	"math"

	"github.com/d2lang/d2/lib/geo"
)

// ConnectionPathCommand is an absolute M, L, C, or S command on the rendered
// centerline. Cubics include both control points, including the implicit first
// control of an S command.
type ConnectionPathCommand struct {
	Kind                    byte
	Control1, Control2, End geo.Point
}

// ArrowheadAdjustments inset the centerline's endpoints for the strokes and markers.
func (c Connection) ArrowheadAdjustments(shapes map[string]Shape) (src, dst *geo.Point) {
	return c.ArrowheadAdjustmentsForWidths(shapes[c.Src].RenderedStrokeWidth(), shapes[c.Dst].RenderedStrokeWidth())
}

// ArrowheadAdjustmentsForWidths uses the current stroke widths of the two endpoint shapes.
func (c Connection) ArrowheadAdjustmentsForWidths(srcWidth, dstWidth float64) (src, dst *geo.Point) {
	return c.arrowheadAdjustments(c.EndpointInsets(srcWidth, dstWidth))
}

// EndpointInsets gives the two distances that trim the painted centerline.
func (c *Connection) EndpointInsets(srcWidth, dstWidth float64) [2]float64 {
	width := c.RenderedStrokeWidth()
	src, dst := (width+srcWidth)/2, (width+dstWidth)/2
	if c.SrcArrow != NoArrowhead {
		src += width
	}
	if c.DstArrow != NoArrowhead {
		dst += width
	}
	return [2]float64{src, dst}
}

func (c *Connection) arrowheadAdjustments(insets [2]float64) (src, dst *geo.Point) {
	inset := func(start, end *geo.Point, distance float64, tip TraceEndpoint) *geo.Point {
		v := start.VectorTo(end)
		if v.Length() == 0 {
			return geo.NewPoint(0, 0)
		}
		// A short approach to the first or last corner can be shorter than
		// the stroke inset. Leave some shaft on its authored side of the bend.
		if c.MotionTip == tip || !c.IsCurve && len(c.Route) > 2 && distance > v.Length() {
			distance = min(distance, v.Length()/2)
		}
		return v.Unit().Multiply(-distance).ToPoint()
	}
	last := len(c.Route) - 1
	return inset(c.Route[1], c.Route[0], insets[0], TraceSource),
		inset(c.Route[last-1], c.Route[last], insets[1], TraceDestination)
}

// PathCommands walks the final routed path, including arrowhead insets and
// rounded polyline corners. Consumers must first validate the render target.
func (c Connection) PathCommands(srcAdj, dstAdj *geo.Point) iter.Seq[ConnectionPathCommand] {
	return func(yield func(ConnectionPathCommand) bool) {
		route := c.Route
		last := len(route) - 1
		end := geo.Point{X: route[last].X + dstAdj.X, Y: route[last].Y + dstAdj.Y}
		start := geo.Point{X: route[0].X + srcAdj.X, Y: route[0].Y + srcAdj.Y}
		if !yield(ConnectionPathCommand{Kind: 'M', End: start}) {
			return
		}
		if c.IsCurve {
			for i := 1; i < len(route); i += 3 {
				p := *route[i+2]
				if i+2 == last {
					p = end
				}
				if !yield(ConnectionPathCommand{Kind: 'C', Control1: *route[i], Control2: *route[i+1], End: p}) {
					return
				}
			}
			return
		}
		// A rounded corner must stay between the two drawn endpoints, even
		// when a resizing shape leaves a first or last run shorter than its
		// corner radius. A moving trace tip also needs a forward tangent.
		radiusAtTip := func(units float64, terminal bool, endpoint TraceEndpoint, corner, tip *geo.Point) float64 {
			if terminal {
				length := geo.EuclideanDistance(corner.X, corner.Y, tip.X, tip.Y)
				if c.MotionTip == endpoint {
					length /= 2
				}
				return min(units, length)
			}
			return units
		}
		for i := 1; i < last; i++ {
			prev, corner, next := route[i-1], route[i], route[i+1]
			if *prev == *corner || *corner == *next {
				if !yield(ConnectionPathCommand{Kind: 'L', End: *corner}) {
					return
				}
				continue
			}
			units := math.Min(c.BorderRadius, geo.EuclideanDistance(corner.X, corner.Y, next.X, next.Y)/2)
			approach := prev.VectorTo(corner).Unit().Multiply(radiusAtTip(units, i == 1, TraceSource, corner, &start)).ToPoint()
			leave := corner.VectorTo(next).Unit().Multiply(radiusAtTip(units, i == last-1, TraceDestination, corner, &end)).ToPoint()
			lineEnd := geo.Point{X: corner.X - approach.X, Y: corner.Y - approach.Y}
			if !yield(ConnectionPathCommand{Kind: 'L', End: lineEnd}) {
				return
			}
			if units < c.BorderRadius && i < last-1 {
				// A short intermediate segment joins two bends in one cubic.
				if *next == *route[i+2] {
					continue
				}
				i++
				leave = next.VectorTo(route[i+1]).Unit().Multiply(radiusAtTip(units, i == last-1, TraceDestination, next, &end)).ToPoint()
				if !yield(ConnectionPathCommand{Kind: 'C',
					Control1: geo.Point{X: corner.X + approach.X, Y: corner.Y + approach.Y},
					Control2: geo.Point{X: next.X - leave.X, Y: next.Y - leave.Y},
					End:      geo.Point{X: next.X + leave.X, Y: next.Y + leave.Y},
				}) {
					return
				}
			} else if !yield(ConnectionPathCommand{Kind: 'S', Control1: lineEnd, Control2: *corner,
				End: geo.Point{X: corner.X + leave.X, Y: corner.Y + leave.Y},
			}) {
				return
			}
		}
		yield(ConnectionPathCommand{Kind: 'L', End: end})
	}
}

// GradientBounds anchors a connection's stroke paint to its full route, before
// the shaft is shortened for arrowheads. Even a straight horizontal or vertical
// connection needs a paintable cross section for its stroke and markers.
func (c Connection) GradientBounds() *geo.Box {
	tl, br := geo.Route(c.Route).GetBoundingBox()
	stroke := c.RenderedStrokeWidth()
	_, srcHeight := c.SrcArrow.Dimensions(stroke)
	_, dstHeight := c.DstArrow.Dimensions(stroke)
	minSize := max(1, stroke, srcHeight, dstHeight)
	w, h := br.X-tl.X, br.Y-tl.Y
	width, height := max(w, minSize), max(h, minSize)
	return geo.NewBox(geo.NewPoint(tl.X-(width-w)/2, tl.Y-(height-h)/2), width, height)
}
