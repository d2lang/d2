// Package scenepaint translates authored D2 paints into renderer-independent scenes.
package scenepaint

import (
	"fmt"
	"image/color"

	"github.com/mazznoer/csscolorparser"

	"github.com/d2lang/d2/d2renderers/d2scene"
	libcolor "github.com/d2lang/d2/lib/color"
)

// Gradient translates the object-bounding-box gradient geometry emitted
// by lib/color so the rasterizer does not need to parse CSS strings.
func Gradient(raw string) (d2scene.Paint, error) {
	gradient, err := libcolor.ParseGradient(raw)
	if err != nil {
		return nil, err
	}
	if len(gradient.ColorStops) == 0 {
		return nil, fmt.Errorf("gradient has no valid color stops")
	}
	stops := make([]d2scene.GradientStop, len(gradient.ColorStops))
	previous := 0.0
	for i, stop := range gradient.ColorStops {
		parsed, err := csscolorparser.Parse(stop.Color)
		if err != nil {
			return nil, fmt.Errorf("stop %d color %q: %w", i, stop.Color, err)
		}
		offset, err := stop.Offset(i, len(gradient.ColorStops), previous)
		if err != nil {
			return nil, fmt.Errorf("stop %d position %q: %w", i, stop.Position, err)
		}
		previous = offset
		r, g, blue, a := parsed.RGBA255()
		stops[i] = d2scene.GradientStop{Offset: offset, Color: color.NRGBA{R: r, G: g, B: blue, A: a}}
	}

	switch gradient.Type {
	case "linear":
		x1, y1, x2, y2, err := libcolor.LinearGradientVector(gradient.Direction)
		if err != nil {
			return nil, err
		}
		return d2scene.LinearGradient{
			Start: d2scene.Point{X: x1, Y: y1}, End: d2scene.Point{X: x2, Y: y2},
			Stops: stops, Units: d2scene.ObjectBoundingBox, Transform: d2scene.Identity(), Spread: d2scene.SpreadPad,
		}, nil
	case "radial":
		return d2scene.RadialGradient{
			Center: d2scene.Point{X: .5, Y: .5}, Radius: .5,
			Focal: d2scene.Point{X: .5, Y: .5}, Stops: stops,
			Units: d2scene.ObjectBoundingBox, Transform: d2scene.Identity(), Spread: d2scene.SpreadPad,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported gradient type %q", gradient.Type)
	}
}
