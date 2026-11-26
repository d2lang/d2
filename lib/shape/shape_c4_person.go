package shape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

// Constants to match frontend implementation
const (
	C4_PERSON_AR_LIMIT   = 1.5
	HEAD_RADIUS_FACTOR   = 0.22
	BODY_TOP_FACTOR      = 0.8
	CORNER_RADIUS_FACTOR = 0.175
	c4VerticalPadding    = 0.03
	c4MinimumHeightRatio = 0.95
)

var c4PersonKind = shapeKind{
	typ:             C4_PERSON_TYPE,
	paddingX:        10,
	paddingY:        defaultPadding,
	innerBox:        c4PersonInnerBox,
	dimensionsToFit: c4PersonDimensionsToFit,
	path:            bodyPath,
	extraPath:       headPath,
	perimeter:       c4PersonPerimeter,
}

func NewC4Person(box *geo.Box) Shape {
	return newShape(&c4PersonKind, box)
}

func c4PersonInnerBox(s *shape) *geo.Box {
	width := s.Box.Width
	height := s.Box.Height

	headRadius := width * HEAD_RADIUS_FACTOR
	headCenterY := headRadius
	bodyTop := headCenterY + headRadius*BODY_TOP_FACTOR

	// Horizontal padding = 5% of width
	horizontalPadding := width * 0.05
	// Vertical padding = 3% of height
	verticalPadding := height * c4VerticalPadding

	tl := s.Box.TopLeft.Copy()
	tl.X += horizontalPadding
	tl.Y += bodyTop + verticalPadding

	innerWidth := width - (horizontalPadding * 2)
	innerHeight := height - bodyTop - (verticalPadding * 2)

	return geo.NewBox(tl, innerWidth, innerHeight)
}

func bodyPath(box *geo.Box) *svg.SvgPathContext {
	width := box.Width
	height := box.Height

	pc := svg.NewSVGPathContext(box.TopLeft, 1, 1)

	headRadius := width * HEAD_RADIUS_FACTOR
	headCenterY := headRadius
	bodyTop := headCenterY + headRadius*BODY_TOP_FACTOR
	bodyWidth := width
	bodyHeight := height - bodyTop
	bodyLeft := 0

	// Use the same corner radius calculation as frontend
	cornerRadius := math.Min(width*CORNER_RADIUS_FACTOR, bodyHeight*0.25)

	pc.StartAt(pc.Absolute(float64(bodyLeft), bodyTop+cornerRadius))

	pc.C(true, 0, -4*(math.Sqrt(2)-1)/3*cornerRadius, 4*(math.Sqrt(2)-1)/3*cornerRadius, -cornerRadius, cornerRadius, -cornerRadius)
	pc.H(true, bodyWidth-2*cornerRadius)
	pc.C(true, 4*(math.Sqrt(2)-1)/3*cornerRadius, 0, cornerRadius, 4*(math.Sqrt(2)-1)/3*cornerRadius, cornerRadius, cornerRadius)
	pc.V(true, bodyHeight-2*cornerRadius)
	pc.C(true, 0, 4*(math.Sqrt(2)-1)/3*cornerRadius, -4*(math.Sqrt(2)-1)/3*cornerRadius, cornerRadius, -cornerRadius, cornerRadius)
	pc.H(true, -(bodyWidth - 2*cornerRadius))
	pc.C(true, -4*(math.Sqrt(2)-1)/3*cornerRadius, 0, -cornerRadius, -4*(math.Sqrt(2)-1)/3*cornerRadius, -cornerRadius, -cornerRadius)
	pc.Z()

	return pc
}

func headPath(box *geo.Box) *svg.SvgPathContext {
	width := box.Width

	pc := svg.NewSVGPathContext(box.TopLeft, 1, 1)

	headRadius := width * HEAD_RADIUS_FACTOR
	headCenterX := width / 2
	headCenterY := headRadius

	pc.StartAt(pc.Absolute(headCenterX, headCenterY-headRadius))

	pc.C(false,
		headCenterX+headRadius*4*(math.Sqrt(2)-1)/3, headCenterY-headRadius,
		headCenterX+headRadius, headCenterY-headRadius*4*(math.Sqrt(2)-1)/3,
		headCenterX+headRadius, headCenterY)

	pc.C(false,
		headCenterX+headRadius, headCenterY+headRadius*4*(math.Sqrt(2)-1)/3,
		headCenterX+headRadius*4*(math.Sqrt(2)-1)/3, headCenterY+headRadius,
		headCenterX, headCenterY+headRadius)

	pc.C(false,
		headCenterX-headRadius*4*(math.Sqrt(2)-1)/3, headCenterY+headRadius,
		headCenterX-headRadius, headCenterY+headRadius*4*(math.Sqrt(2)-1)/3,
		headCenterX-headRadius, headCenterY)

	pc.C(false,
		headCenterX-headRadius, headCenterY-headRadius*4*(math.Sqrt(2)-1)/3,
		headCenterX-headRadius*4*(math.Sqrt(2)-1)/3, headCenterY-headRadius,
		headCenterX, headCenterY-headRadius)

	return pc
}

func c4PersonPerimeter(s *shape) []geo.Intersectable {
	width := s.Box.Width

	bodyPerimeter := bodyPath(s.Box).Path

	headRadius := width * HEAD_RADIUS_FACTOR
	headCenterX := s.Box.TopLeft.X + width/2
	headCenterY := s.Box.TopLeft.Y + headRadius
	headCenter := geo.NewPoint(headCenterX, headCenterY)

	headEllipse := geo.NewEllipse(headCenter, headRadius, headRadius)

	return append(bodyPerimeter, headEllipse)
}

func c4PersonDimensionsToFit(width, height, paddingX, paddingY float64) (float64, float64) {
	contentWidth := width + paddingX
	contentHeight := height + paddingY

	// Account for 10% total horizontal padding (5% on each side)
	totalWidth := contentWidth / 0.9
	headRadius := totalWidth * HEAD_RADIUS_FACTOR

	// Use positioning matching frontend
	headCenterY := headRadius
	bodyTop := headCenterY + headRadius*BODY_TOP_FACTOR

	// Include vertical padding
	verticalPadding := totalWidth * 0.06 // 3% top + 3% bottom
	totalHeight := contentHeight + bodyTop + verticalPadding

	// Calculate minimum height
	minHeight := totalWidth * c4MinimumHeightRatio
	if totalHeight < minHeight {
		totalHeight = minHeight
	}

	totalWidth, totalHeight = LimitAR(totalWidth, totalHeight, C4_PERSON_AR_LIMIT)
	return math.Ceil(totalWidth), math.Ceil(totalHeight)
}

// FitC4PersonBody enlarges a grid container to keep its content below the
// head after applying authored minimum dimensions. The available body height
// is (1 - 2*padding)*height - headFraction*width.
func FitC4PersonBody(width, height, contentHeight float64) (float64, float64) {
	const usableHeight = 1 - 2*c4VerticalPadding
	const headFraction = HEAD_RADIUS_FACTOR * (1 + BODY_TOP_FACTOR)
	// Widen tall containers enough to enclose the body at the aspect limit.
	width = math.Ceil(max(width, height/C4_PERSON_AR_LIMIT, contentHeight/(usableHeight*C4_PERSON_AR_LIMIT-headFraction)))
	height = math.Ceil(max(height, width*c4MinimumHeightRatio, (contentHeight+headFraction*width)/usableHeight))
	return width, height
}
