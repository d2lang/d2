package shape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

const (
	defaultTipWidth  = 30.
	defaultTipHeight = 45.
)

var calloutKind = shapeKind{
	typ:             CALLOUT_TYPE,
	paddingX:        defaultPadding,
	paddingY:        defaultPadding / 2,
	innerBox:        calloutInnerBox,
	dimensionsToFit: calloutDimensionsToFit,
	path:            calloutPath,
	trace:           calloutTrace,
}

func NewCallout(box *geo.Box) Shape {
	return newShape(&calloutKind, box)
}

func getTipWidth(box *geo.Box) float64 {
	tipWidth := defaultTipWidth
	if box.Width < tipWidth*2 {
		tipWidth = box.Width / 2.0
	}
	return tipWidth
}

func getTipHeight(box *geo.Box) float64 {
	tipHeight := defaultTipHeight
	if box.Height < tipHeight*2 {
		tipHeight = box.Height / 2.0
	}
	return tipHeight
}

func calloutInnerBox(s *shape) *geo.Box {
	tipHeight := getTipHeight(s.Box)
	height := s.Box.Height - tipHeight
	return geo.NewBox(s.Box.TopLeft.Copy(), s.Box.Width, height)
}

func calloutPath(box *geo.Box) *svg.SvgPathContext {
	return calloutOutline(box, svg.NewSVGPathContext)
}

// calloutOutline draws the outline of calloutPath into a path made by newPath.
func calloutOutline[P pathBuilder](box *geo.Box, newPath func(*geo.Point, float64, float64) P) P {
	tipWidth := getTipWidth(box)
	tipHeight := getTipHeight(box)
	pc := newPath(box.TopLeft, 1, 1)
	pc.StartAt(pc.Absolute(0, 0))
	pc.V(true, box.Height-tipHeight)
	pc.H(true, box.Width/2.0)
	pc.V(true, tipHeight)
	pc.L(true, tipWidth, -tipHeight)
	pc.H(true, box.Width/2.0-tipWidth)
	pc.V(true, -(box.Height - tipHeight))
	pc.H(true, -box.Width)
	pc.Z()
	return pc
}

func calloutTrace(box *geo.Box, t *borderTracer) {
	calloutOutline(box, t.newPath)
}

func calloutDimensionsToFit(width, height, paddingX, paddingY float64) (float64, float64) {
	// return the minimum shape dimensions needed to fit content (width x height)
	// in the shape's innerBox with padding
	baseHeight := height + paddingY
	if baseHeight < defaultTipHeight {
		baseHeight *= 2
	} else {
		baseHeight += defaultTipHeight
	}
	return math.Ceil(width + paddingX), math.Ceil(baseHeight)
}
