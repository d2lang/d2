package d2svg

import (
	"fmt"
	"io"
	"math"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

// An SVG marker has its own coordinate system. Project the same route-space
// gradient into each painted piece, sharing the original gradient's stops.
type connectionGradient struct {
	writer  io.Writer
	defined map[string]struct{}
	base    color.Gradient
	box     d2scene.Matrix
}

func newConnectionGradient(writer io.Writer, c d2target.Connection, defined map[string]struct{}) (*connectionGradient, error) {
	base, err := color.ParseGradient(c.Stroke)
	if err != nil {
		return nil, err
	}
	defineGradient(writer, base, defined)
	b := c.GradientBounds()
	return &connectionGradient{
		writer: writer, defined: defined, base: base,
		box: d2scene.Translate(b.TopLeft.X, b.TopLeft.Y).Mul(d2scene.Scale(b.Width, b.Height)),
	}, nil
}

func (g *connectionGradient) paint(worldToLocal d2scene.Matrix) string {
	return projectedGradient(g.writer, g.defined, g.base, worldToLocal.Mul(g.box))
}

func projectedGradient(writer io.Writer, defined map[string]struct{}, base color.Gradient, m d2scene.Matrix) string {
	transform := fmt.Sprintf("matrix(%s %s %s %s %s %s)",
		svg.FormatFloat(m.A), svg.FormatFloat(m.B), svg.FormatFloat(m.C),
		svg.FormatFloat(m.D), svg.FormatFloat(m.E), svg.FormatFloat(m.F))
	id := color.UniqueGradientID(base.ID + transform)
	iri := svg.LocalIRI(svg.LiteralID(id))
	if _, ok := defined[id]; ok {
		return iri
	}
	defined[id] = struct{}{}
	attrs := []svg.Attribute{svg.Attr("id", id), svg.Attr("gradientUnits", "userSpaceOnUse"), svg.Attr("gradientTransform", transform)}
	attrs = append(attrs, svg.HrefAttributes("#"+base.ID)...)
	kind := "radialGradient"
	if base.Type == "linear" {
		kind = "linearGradient"
		x1, y1, x2, y2, _ := color.LinearGradientVector(base.Direction)
		attrs = append(attrs, svg.FloatAttr("x1", x1), svg.FloatAttr("y1", y1), svg.FloatAttr("x2", x2), svg.FloatAttr("y2", y2))
	} else {
		attrs = append(attrs, svg.FloatAttr("cx", .5), svg.FloatAttr("cy", .5), svg.FloatAttr("r", .5))
	}
	fmt.Fprint(writer, svg.OpenElement("defs"), svg.EmptyElement(kind, attrs...), svg.CloseElement("defs"))
	return iri
}

func worldToEndpoint(p geo.Point, radians float64) d2scene.Matrix {
	return d2scene.Rotate(-radians).Mul(d2scene.Translate(-p.X, -p.Y))
}

// Match orient="auto" on the emitted path, including degenerate leading or
// trailing cubic handles. The target tangent points into the marker.
func markerEndpoints(c d2target.Connection, srcAdj, dstAdj *geo.Point) (start, end d2scene.Matrix) {
	var first, prev geo.Point
	var haveStart bool
	var startAngle, endAngle float64
	for cmd := range c.PathCommands(srcAdj, dstAdj) {
		if cmd.Kind == 'M' {
			first, prev = cmd.End, cmd.End
			continue
		}
		points := []geo.Point{cmd.End}
		if cmd.Kind == 'C' || cmd.Kind == 'S' {
			points = []geo.Point{cmd.Control1, cmd.Control2, cmd.End}
		}
		for _, p := range points {
			if p != prev {
				endAngle = math.Atan2(p.Y-prev.Y, p.X-prev.X)
				if !haveStart {
					startAngle, haveStart = endAngle, true
				}
			}
			prev = p
		}
	}
	return worldToEndpoint(first, startAngle), worldToEndpoint(prev, endAngle)
}
