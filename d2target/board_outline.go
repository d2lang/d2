package d2target

import (
	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// CanDrawOutline reports whether a plain rectangular body has one visible
// solid contour. Rounded, double and decorated contours keep their own paint.
func (s *Shape) CanDrawOutline() bool {
	if c, ok := color.ParseRGBA(s.Stroke); ok && c.A == 0 {
		return false
	}
	return (s.Type == ShapeRectangle || s.Type == "") && s.Width > 0 && s.Height > 0 &&
		s.Opacity > 0 && s.RenderedStrokeWidth() > 0 && s.Stroke != "" && !color.IsGradient(s.Stroke) &&
		s.BorderRadius == 0 && s.StrokeDash == 0 && s.Link == "" && s.Icon == nil && s.Tooltip == "" && !s.Blend && !s.ThreeDee && !s.Multiple && !s.DoubleBorder && !s.Shadow && !s.Animated &&
		(s.FillPattern == "" || s.FillPattern == "none") && (s.Label == "" || !label.FromString(s.LabelPosition).IsBorder())
}

// DrawingOutline selects partial geometry for intermediate frames. A complete
// frame goes through the ordinary closed-stroke renderer, including its joins.
func (s *Shape) DrawingOutline() bool {
	return s.OutlineProgress != nil && *s.OutlineProgress < 1 && s.CanDrawOutline()
}

// DrawnOutline follows two equal-length strokes from the top center down the
// sides. Joining them through the common origin yields one open polyline in
// both SVG and raster, and keeps the stroke width constant as the frame grows.
func (s *Shape) DrawnOutline() []geo.Point {
	if !s.DrawingOutline() || *s.OutlineProgress <= 0 {
		return nil
	}
	x, y, w, h := float64(s.Pos.X), float64(s.Pos.Y), float64(s.Width), float64(s.Height)
	top := geo.Point{X: x + w/2, Y: y}
	travel := *s.OutlineProgress * (w + h)
	span := min(travel, w/2)
	drop := min(max(0, travel-w/2), h)
	closeDistance := max(0, travel-w/2-h)
	left, right := top.X-span, top.X+span
	out := make([]geo.Point, 0, 7)
	if closeDistance > 0 {
		out = append(out, geo.Point{X: left + closeDistance, Y: y + h})
	}
	if drop > 0 {
		out = append(out, geo.Point{X: left, Y: y + drop})
	}
	out = append(out, geo.Point{X: left, Y: y}, top, geo.Point{X: right, Y: y})
	if drop > 0 {
		out = append(out, geo.Point{X: right, Y: y + drop})
	}
	if closeDistance > 0 {
		out = append(out, geo.Point{X: right - closeDistance, Y: y + h})
	}
	return out
}
