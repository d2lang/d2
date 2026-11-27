package shape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
)

const OVAL_AR_LIMIT = 3.

var ovalKind = shapeKind{
	typ:             OVAL_TYPE,
	paddingX:        defaultPadding,
	paddingY:        defaultPadding,
	innerBox:        ellipseInnerBox,
	insidePlacement: ovalInsidePlacement,
	dimensionsToFit: ovalDimensionsToFit,
	perimeter:       ellipsePerimeter,
}

func NewOval(box *geo.Box) Shape {
	return newShape(&ovalKind, box)
}

func ovalDimensionsToFit(width, height, paddingX, paddingY float64) (float64, float64) {
	theta := float64(float32(math.Atan2(height, width)))
	// add padding in direction of diagonal so there is padding distance between top left and border
	paddedWidth := width + paddingX*math.Cos(theta)
	paddedHeight := height + paddingY*math.Sin(theta)
	// see https://stackoverflow.com/questions/433371/ellipse-bounding-a-rectangle
	totalWidth, totalHeight := math.Ceil(math.Sqrt2*paddedWidth), math.Ceil(math.Sqrt2*paddedHeight)

	// prevent oval from becoming too flat
	totalWidth, totalHeight = LimitAR(totalWidth, totalHeight, OVAL_AR_LIMIT)
	return totalWidth, totalHeight
}

func ovalInsidePlacement(s *shape, width, height, paddingX, paddingY float64) geo.Point {
	// showing the top left arc of the ellipse (drawn with '*')
	// ┌──────────────────* ┬
	// │         *        │ │ry
	// │     *┌───────────┤ │ ┬
	// │  *   │           │ │ │sin*r
	// │*     │           │ │ │
	// *──────┴───────────┼ ┴ ┴
	// ├────────rx────────┤
	//        ├───cos*r───┤
	rx := s.Box.Width / 2
	ry := s.Box.Height / 2
	theta := float64(float32(math.Atan2(ry, rx)))
	sin := math.Sin(theta)
	cos := math.Cos(theta)
	// r is the ellipse radius on the line between node.TopLeft and the ellipse center
	// see https://math.stackexchange.com/questions/432902/how-to-get-the-radius-of-an-ellipse-at-a-specific-angle-by-knowing-its-semi-majo
	r := rx * ry / math.Sqrt(math.Pow(rx*sin, 2)+math.Pow(ry*cos, 2))
	// we want to offset r-padding/2 away from the center
	return *geo.NewPoint(s.Box.TopLeft.X+math.Ceil(rx-cos*(r-paddingX/2)), s.Box.TopLeft.Y+math.Ceil(ry-sin*(r-paddingY/2)))
}
