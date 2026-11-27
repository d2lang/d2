package shape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
)

var circleKind = shapeKind{
	typ:             CIRCLE_TYPE,
	aspectRatio1:    true,
	paddingX:        defaultPadding / math.Sqrt2,
	paddingY:        defaultPadding / math.Sqrt2,
	innerBox:        ellipseInnerBox,
	insidePlacement: circleInsidePlacement,
	dimensionsToFit: circleDimensionsToFit,
	perimeter:       ellipsePerimeter,
}

func NewCircle(box *geo.Box) Shape {
	return newShape(&circleKind, box)
}

func circleDimensionsToFit(width, height, paddingX, paddingY float64) (float64, float64) {
	length := math.Max(width+paddingX, height+paddingY)
	diameter := math.Ceil(math.Sqrt2 * length)
	return diameter, diameter
}

func circleInsidePlacement(s *shape, width, height, paddingX, paddingY float64) geo.Point {
	r := s.GetBox().Width / 2
	halfLength := r * math.Sqrt2 / 2.
	p := geo.Point{
		X: s.GetBox().TopLeft.X + math.Ceil(r-halfLength+paddingX/2.),
		Y: s.GetBox().TopLeft.Y + math.Ceil(r-halfLength+paddingY/2.),
	}
	return p
}
