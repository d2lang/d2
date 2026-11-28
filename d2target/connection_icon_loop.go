package d2target

import (
	"fmt"
	"iter"
	"math"

	"github.com/d2lang/d2/lib/color"
)

func (d *Diagram) shapeStrokeWidths() map[string]float64 {
	widths := make(map[string]float64, len(d.Shapes))
	for i := range d.Shapes {
		s := &d.Shapes[i]
		widths[s.ID] = s.RenderedStrokeWidth()
	}
	return widths
}

// ConnectionIconRoutes yields motion paths in connection order, using the
// stroke widths of this board's endpoints. Routes must already be laid out.
func (d *Diagram) ConnectionIconRoutes() iter.Seq2[int, IconMotionPath] {
	return func(yield func(int, IconMotionPath) bool) {
		var widths map[string]float64
		for i := range d.Connections {
			c := &d.Connections[i]
			if c.IconAnimation == nil {
				continue
			}
			if widths == nil {
				widths = d.shapeStrokeWidths()
			}
			if !yield(i, c.IconMotionPath(widths[c.Src], widths[c.Dst])) {
				return
			}
		}
	}
}

// HasVisibleIconStream reports whether the connection's clock can paint an icon.
// An image remains visible on a transparent wire, as can part of a gradient.
func (c *Connection) HasVisibleIconStream() bool {
	return c.IconAnimation != nil && c.Opacity > 0 && c.IconPositions == nil &&
		(c.Icon != nil || c.Stroke == "" || color.IsGradient(c.Stroke) || color.PaintVisibleThroughout(c.Stroke))
}

// ConnectionIconPeriods yields the effective departure period in seconds for
// each visible stream on this laid-out board. For a single repeating icon,
// the period is the travel time along its painted route.
func (d *Diagram) ConnectionIconPeriods() iter.Seq2[*Connection, float64] {
	return func(yield func(*Connection, float64) bool) {
		for i, path := range d.ConnectionIconRoutes() {
			c := &d.Connections[i]
			if !c.HasVisibleIconStream() {
				continue
			}
			period := path.LaunchPeriod(c.IconAnimation)
			if !math.IsInf(period, 1) && !yield(c, period) {
				return
			}
		}
	}
}

// ValidateIconLoopDuration checks authored departure periods against the entire
// repeating film. Individual board holds can be shorter than a departure period.
func ValidateIconLoopDuration(board *Diagram, loopMS int) error {
	loop := float64(loopMS) / 1000
	for c, period := range board.ConnectionIconPeriods() {
		if c.IconAnimation.HasExplicitPeriod() && period > math.Nextafter(loop, math.Inf(1)) {
			value := fmt.Sprintf("%dms", c.IconAnimation.DurationMS)
			if c.IconAnimation.Rate != nil {
				value = fmt.Sprintf("%g/s", *c.IconAnimation.Rate)
				if c.IconAnimation.DurationMS > 0 {
					value += fmt.Sprintf(" in %dms", c.IconAnimation.DurationMS)
				}
			}
			return fmt.Errorf("connection %q with icon.style.animated %s needs an animation loop of at least %gs (got %gs)", c.ID, value, period, loop)
		}
	}
	return nil
}
