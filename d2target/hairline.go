package d2target

import (
	"math"
	"slices"

	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// HairlineBorderInset gives a static one-unit rectangle an inside border at
// nominal pixel size. Its fill, stroke and corner radii share the inset box.
// Layout coordinates, text, and connection anchors keep their original bounds.
func (s Shape) HairlineBorderInset() float64 {
	if s.Type != ShapeRectangle && s.Type != ShapeSquare && s.Type != "" {
		return 0
	}
	if s.RenderedStrokeWidth() != 1 || s.Width <= 1 || s.Height <= 1 || s.StrokeDash != 0 ||
		s.Animated || s.DoubleBorder || s.ThreeDee || s.Multiple || s.Shadow || s.Blend ||
		s.MotionClip != nil || s.MotionScale != 0 || s.OutlineProgress != nil ||
		s.Label != "" && label.FromString(s.LabelPosition).IsBorder() ||
		s.FillPattern != "" && s.FillPattern != "none" || color.IsGradient(s.Fill) ||
		color.IsGradient(s.Stroke) || !color.PaintVisibleThroughout(s.Stroke) {
		return 0
	}
	return .5
}

// alignedHairlineRoute returns replacement paint geometry only when a run needs hinting.
// Adjust only the coordinate perpendicular to each axis-aligned run. That keeps
// ports on their original faces and connected bends continuous. In particular,
// a half-integral centerline is already sharp and must not be moved.
func (c Connection) alignedHairlineRoute() []*geo.Point {
	if c.RenderedStrokeWidth() != 1 || c.StrokeDash != 0 || c.Animated || c.IconAnimation != nil ||
		c.MotionClip != nil || c.MotionLabel != nil || c.MotionTip != "" ||
		c.Annotation != nil || c.LeavingAnnotation != nil ||
		color.IsGradient(c.Stroke) || len(c.Route) < 2 {
		return nil
	}
	var route []*geo.Point
	alignRuns := func(axis func(*geo.Point) *float64) {
		for first := 0; first < len(c.Route); {
			at := *axis(c.Route[first])
			last := first
			for last+1 < len(c.Route) && *axis(c.Route[last+1]) == at {
				last++
			}
			start, end := first, last
			first = last + 1
			if at != math.Trunc(at) || start == end {
				continue
			}
			// Keep a subpixel jog from collapsing or reversing at a bend.
			if start > 0 && math.Abs(*axis(c.Route[start-1])-at) < 1 ||
				end+1 < len(c.Route) && math.Abs(*axis(c.Route[end+1])-at) < 1 {
				continue
			}
			if c.IsCurve {
				// Only hint complete collinear cubics. A control polygon can
				// contain a horizontal or vertical leg even when its curve does not.
				start = (start + 2) / 3 * 3
				end = end / 3 * 3
			}
			if start >= end {
				continue
			}
			p := c.Route[start]
			if !slices.ContainsFunc(c.Route[start+1:end+1], func(q *geo.Point) bool {
				return math.Abs(q.X-p.X) >= 1 || math.Abs(q.Y-p.Y) >= 1
			}) {
				continue
			}
			if route == nil {
				route = make([]*geo.Point, len(c.Route))
				points := make([]geo.Point, len(c.Route))
				for i, p := range c.Route {
					points[i] = *p
					route[i] = &points[i]
				}
			}
			for _, p := range route[start : end+1] {
				*axis(p) = at + .5
			}
		}
	}
	alignRuns(func(p *geo.Point) *float64 { return &p.X })
	alignRuns(func(p *geo.Point) *float64 { return &p.Y })
	return route
}

// WithAlignedHairlineRoutes prepares a static board for nominal-size painting.
// Build crossings and masks from the same routes that draw the shafts and markers.
func (d *Diagram) WithAlignedHairlineRoutes() *Diagram {
	if len(d.Connections) == 0 {
		return d
	}
	// An inside-painted border ends at the layout box. Even wider or diagonal
	// connections need less endpoint trimming when attached to such a border.
	var borders map[string]float64
	for i := range d.Shapes {
		s := &d.Shapes[i]
		if inset := s.HairlineBorderInset(); inset != 0 {
			if borders == nil {
				borders = make(map[string]float64)
			}
			borders[s.ID] = inset
		}
	}

	result := d
	for i := range d.Connections {
		c := &d.Connections[i]
		route := c.alignedHairlineRoute()
		insets := [2]float64{borders[c.Src], borders[c.Dst]}
		if route == nil && c.paintBorderInsets == insets {
			continue
		}
		if result == d {
			board := *d
			board.Connections = slices.Clone(d.Connections)
			result = &board
		}
		if route != nil {
			result.Connections[i].Route = route
		}
		result.Connections[i].paintBorderInsets = insets
	}
	if result == d {
		return d
	}
	return result.WithIconLabelInsets()
}
