package d2animate

import (
	"fmt"
	"image"
	"math"

	"github.com/d2lang/d2/d2renderers/internal/svgframe"
	"github.com/d2lang/d2/d2target"
)

// Independent layers can be authored thousands of units apart. Fit each layer
// on the canvas while keeping its steps, scenarios and samples in registration.
func layerViewport(nav *LayerNavigation, frames []Frame, svgs [][]byte, pad int) (image.Rectangle, []image.Point, error) {
	groups := make([]*d2target.Diagram, len(frames))
	bounds := make(map[*d2target.Diagram]image.Rectangle)
	var layer *d2target.Diagram
	for i := len(frames) - 1; i >= 0; i-- {
		frame := frames[i]
		if !frame.TweenSample {
			layer = nav.boards[frame.Diagram].layer
		}
		groups[i] = layer
		info, ok := svgframe.Read(svgs[i])
		box := info.ViewBox
		if !ok {
			// Public callers can pass group fragments without D2's viewBox.
			tl, br := frame.Diagram.BoundingBox()
			box = image.Rect(tl.X-pad, tl.Y-pad, br.X+pad, br.Y+pad)
		}
		bounds[layer] = bounds[layer].Union(box)
	}
	var width, height int
	for _, box := range bounds {
		if box.Dx() < 0 || box.Dy() < 0 {
			return image.Rectangle{}, nil, fmt.Errorf("layer animation viewport exceeds the supported range")
		}
		width, height = max(width, box.Dx()), max(height, box.Dy())
	}
	offsets := make([]image.Point, len(frames))
	for i, group := range groups {
		box := bounds[group]
		centered := image.Pt((width-box.Dx())/2, (height-box.Dy())/2)
		if box.Min.X < centered.X-math.MaxInt || box.Min.Y < centered.Y-math.MaxInt {
			return image.Rectangle{}, nil, fmt.Errorf("layer animation offset exceeds the supported range")
		}
		offsets[i] = centered.Sub(box.Min)
	}
	return image.Rect(0, 0, width, height), offsets, nil
}

func placedZoom(zoom LayerZoom, offsets []image.Point, from, to int) LayerZoom {
	if len(offsets) == 0 {
		return zoom
	}
	parent := from
	if !zoom.Inward {
		parent = to
	}
	zoom.offset = offsets[parent]
	return zoom
}
