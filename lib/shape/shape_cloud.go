package shape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

// The percentage values of the cloud's wide inner box
const CLOUD_WIDE_INNER_X = 0.085
const CLOUD_WIDE_INNER_Y = 0.409
const CLOUD_WIDE_INNER_WIDTH = 0.819
const CLOUD_WIDE_INNER_HEIGHT = 0.548
const CLOUD_WIDE_ASPECT_BOUNDARY = (1 + CLOUD_WIDE_INNER_WIDTH/CLOUD_WIDE_INNER_HEIGHT) / 2

// The percentage values of the cloud's tall inner box
const CLOUD_TALL_INNER_X = 0.228
const CLOUD_TALL_INNER_Y = 0.179
const CLOUD_TALL_INNER_WIDTH = 0.549
const CLOUD_TALL_INNER_HEIGHT = 0.820
const CLOUD_TALL_ASPECT_BOUNDARY = (1 + CLOUD_TALL_INNER_WIDTH/CLOUD_TALL_INNER_HEIGHT) / 2

// The percentage values of the cloud's square inner box
const CLOUD_SQUARE_INNER_X = 0.167
const CLOUD_SQUARE_INNER_Y = 0.335
const CLOUD_SQUARE_INNER_WIDTH = 0.663
const CLOUD_SQUARE_INNER_HEIGHT = 0.663

var cloudKind = shapeKind{
	typ:                CLOUD_TYPE,
	paddingX:           defaultPadding,
	paddingY:           defaultPadding / 2,
	innerBox:           cloudInnerBox,
	innerBoxForContent: cloudInnerBoxForContent,
	insidePlacement:    cloudInsidePlacement,
	dimensionsToFit:    cloudDimensionsToFit,
	path:               cloudPath,
	trace:              cloudTrace,
}

func NewCloud(box *geo.Box) Shape {
	return newShape(&cloudKind, box)
}

func cloudInnerBox(s *shape) *geo.Box {
	if s.innerBoxAspectRatio != 0. {
		return cloudInnerBoxForContent(s, s.innerBoxAspectRatio, 1)
	}
	return cloudInnerBoxForContent(s, s.Box.Width, s.Box.Height)
}

// we need this since the content's aspect ratio determines which placement is used
func cloudInnerBoxForContent(s *shape, width, height float64) *geo.Box {
	insideTL := s.GetInsidePlacement(width, height, 0, 0)
	aspectRatio := width / height
	// aspect ratio and position are computed with given dimensions, but final box size is determined by shape size
	width, height = s.Box.Width, s.Box.Height
	if aspectRatio > CLOUD_WIDE_ASPECT_BOUNDARY {
		width *= CLOUD_WIDE_INNER_WIDTH
		height *= CLOUD_WIDE_INNER_HEIGHT
	} else if aspectRatio < CLOUD_TALL_ASPECT_BOUNDARY {
		width *= CLOUD_TALL_INNER_WIDTH
		height *= CLOUD_TALL_INNER_HEIGHT
	} else {
		width *= CLOUD_SQUARE_INNER_WIDTH
		height *= CLOUD_SQUARE_INNER_HEIGHT
	}
	return geo.NewBox(&insideTL, width, height)
}

func cloudDimensionsToFit(width, height, paddingX, paddingY float64) (float64, float64) {
	width += paddingX
	height += paddingY
	aspectRatio := width / height
	// use the inner box with the closest aspect ratio (wide, tall, or square box)
	if aspectRatio > CLOUD_WIDE_ASPECT_BOUNDARY {
		return math.Ceil(width / CLOUD_WIDE_INNER_WIDTH), math.Ceil(height / CLOUD_WIDE_INNER_HEIGHT)
	} else if aspectRatio < CLOUD_TALL_ASPECT_BOUNDARY {
		return math.Ceil(width / CLOUD_TALL_INNER_WIDTH), math.Ceil(height / CLOUD_TALL_INNER_HEIGHT)
	} else {
		return math.Ceil(width / CLOUD_SQUARE_INNER_WIDTH), math.Ceil(height / CLOUD_SQUARE_INNER_HEIGHT)
	}
}

func cloudInsidePlacement(s *shape, width, height, paddingX, paddingY float64) geo.Point {
	r := s.Box
	width += paddingX
	height += paddingY
	aspectRatio := width / height
	if aspectRatio > CLOUD_WIDE_ASPECT_BOUNDARY {
		return *geo.NewPoint(r.TopLeft.X+math.Ceil(r.Width*CLOUD_WIDE_INNER_X+paddingX/2), r.TopLeft.Y+math.Ceil(r.Height*CLOUD_WIDE_INNER_Y+paddingY/2))
	} else if aspectRatio < CLOUD_TALL_ASPECT_BOUNDARY {
		return *geo.NewPoint(r.TopLeft.X+math.Ceil(r.Width*CLOUD_TALL_INNER_X+paddingX/2), r.TopLeft.Y+math.Ceil(r.Height*CLOUD_TALL_INNER_Y+paddingY/2))
	} else {
		return *geo.NewPoint(r.TopLeft.X+math.Ceil(r.Width*CLOUD_SQUARE_INNER_X+paddingX/2), r.TopLeft.Y+math.Ceil(r.Height*CLOUD_SQUARE_INNER_Y+paddingY/2))
	}
}

func cloudPath(box *geo.Box) *svg.SvgPathContext {
	return cloudOutline(box, svg.NewSVGPathContext)
}

// cloudOutline draws the outline of cloudPath into a path made by newPath.
func cloudOutline[P pathBuilder](box *geo.Box, newPath func(*geo.Point, float64, float64) P) P {
	pc := newPath(box.TopLeft, box.Width/834, box.Height/523)
	// Note: original path TopLeft=(83, 238), absolute values updated so top left is at 0,0
	pc.StartAt(pc.Absolute(137.833, 182.833))
	pc.C(true, 0, 5.556, -5.556, 11.111, -11.111, 11.111)
	pc.C(true, -70.833, 6.944, -126.389, 77.778, -126.389, 163.889)
	pc.C(true, 0, 91.667, 62.5, 165.278, 141.667, 165.278)
	pc.H(true, 537.5)
	pc.C(true, 84.723, 0, 154.167, -79.167, 154.167, -175)
	pc.C(true, 0, -91.667, -63.89, -168.056, -144.444, -173.611)
	pc.C(true, -5.556, 0, -11.111, -4.167, -12.5, -11.111)
	pc.C(true, -18.056, -93.055, -101.39, -162.5, -198.611, -162.5)
	pc.C(true, -63.889, 0, -120.834, 29.167, -156.944, 75)
	pc.C(true, -4.167, 5.556, -11.111, 6.945, -15.278, 5.556)
	pc.C(true, -13.889, -5.556, -29.166, -8.333, -45.833, -8.333)
	pc.C(false, 196.167, 71.722, 143.389, 120.333, 137.833, 182.833)
	pc.Z()
	return pc
}

func cloudTrace(box *geo.Box, t *borderTracer) {
	cloudOutline(box, t.newPath)
}
