package shape

// The outline functions as they were before they became generic over the
// path builder (copied from the previous version, renamed), and
// TraceToShapeBorder's search over Perimeter(), for outline_test.go.

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

func legacyCalloutPath(box *geo.Box) *svg.SvgPathContext {
	tipWidth := getTipWidth(box)
	tipHeight := getTipHeight(box)
	pc := svg.NewSVGPathContext(box.TopLeft, 1, 1)
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

func legacyCloudPath(box *geo.Box) *svg.SvgPathContext {
	pc := svg.NewSVGPathContext(box.TopLeft, box.Width/834, box.Height/523)
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

func legacyCylinderOuterPath(box *geo.Box) *svg.SvgPathContext {
	arcHeight := getArcHeight(box)
	multiplier := 0.45
	pc := svg.NewSVGPathContext(box.TopLeft, 1, 1)
	pc.StartAt(pc.Absolute(0, arcHeight))
	pc.C(false, 0, 0, box.Width*multiplier, 0, box.Width/2, 0)
	pc.C(false, box.Width-box.Width*multiplier, 0, box.Width, 0, box.Width, arcHeight)
	pc.V(true, box.Height-arcHeight*2)
	pc.C(false, box.Width, box.Height, box.Width-box.Width*multiplier, box.Height, box.Width/2, box.Height)
	pc.C(false, box.Width*multiplier, box.Height, 0, box.Height, 0, box.Height-arcHeight)
	pc.V(true, -(box.Height - arcHeight*2))
	pc.Z()
	return pc
}

func legacyDiamondPath(box *geo.Box) *svg.SvgPathContext {
	pc := svg.NewSVGPathContext(box.TopLeft, box.Width/77, box.Height/76.9)
	pc.StartAt(pc.Absolute(38.5, 76.9))
	pc.C(true, -0.3, 0, -0.5, -0.1, -0.7, -0.3)
	pc.L(false, 0.3, 39.2)
	pc.C(true, -0.4, -0.4, -0.4, -1, 0, -1.4)
	pc.L(false, 37.8, 0.3)
	pc.C(true, 0.4, -0.4, 1, -0.4, 1.4, 0)
	pc.L(true, 37.5, 37.5)
	pc.C(true, 0.4, 0.4, 0.4, 1, 0, 1.4)
	pc.L(false, 39.2, 76.6)
	pc.C(false, 39, 76.8, 38.8, 76.9, 38.5, 76.9)
	pc.Z()
	return pc
}

func legacyDocumentPath(box *geo.Box) *svg.SvgPathContext {
	pc := svg.NewSVGPathContext(box.TopLeft, box.Width, box.Height)
	pc.StartAt(pc.Absolute(0, docPathBottom/docPathHeight))
	pc.L(false, 0, 0)
	pc.L(false, 1, 0)
	pc.L(false, 1, docPathBottom/docPathHeight)
	pc.C(false, 5/6.0, 12.8/docPathHeight, 2/3.0, 12.8/docPathHeight, 1/2.0, docPathBottom/docPathHeight)
	pc.C(false, 1/3.0, 19.8/docPathHeight, 1/6.0, 19.8/docPathHeight, 0, docPathBottom/docPathHeight)
	pc.Z()
	return pc
}

func legacyHexagonPath(box *geo.Box) *svg.SvgPathContext {
	halfYFactor := 43.6 / 87.3
	pc := svg.NewSVGPathContext(box.TopLeft, box.Width, box.Height)
	pc.StartAt(pc.Absolute(0.25, 0))
	pc.L(false, 0, halfYFactor)
	pc.L(false, 0.25, 1)
	pc.L(false, 0.75, 1)
	pc.L(false, 1, halfYFactor)
	pc.L(false, 0.75, 0)
	pc.Z()
	return pc
}

func legacyPackagePath(box *geo.Box) *svg.SvgPathContext {
	topWidth, topHeight := getTopDimensions(box)

	pc := svg.NewSVGPathContext(box.TopLeft, 1, 1)
	pc.StartAt(pc.Absolute(0, 0))
	pc.L(false, topWidth, 0)
	pc.L(false, topWidth, topHeight)
	pc.L(false, box.Width, topHeight)
	pc.L(false, box.Width, box.Height)
	pc.L(false, 0, box.Height)
	pc.Z()
	return pc
}

func legacyPageOuterPath(box *geo.Box) *svg.SvgPathContext {
	// TODO: cleanup
	pc := svg.NewSVGPathContext(box.TopLeft, 1., 1.)
	pc.StartAt(pc.Absolute(0.5, 0))
	pc.H(false, box.Width-20.8164)
	pc.C(false, box.Width-19.6456, 0.0, box.Width-18.521, 0.456297, box.Width-17.6811, 1.27202)
	pc.L(false, box.Width-1.3647, 17.12)
	pc.C(false, box.Width-0.4923, 17.9674, box.Width, 19.1318, box.Width, 20.348)
	pc.V(false, box.Height-0.5)
	pc.C(false, box.Width, box.Height-0.2239, box.Width-0.2239, box.Height, box.Width-0.5, box.Height)

	pc.H(false, 0.499999)
	pc.C(false, 0.223857, box.Height, 0, box.Height-0.2239, 0, box.Height-0.5)
	pc.V(false, 0.499999)
	pc.C(false, 0, 0.223857, 0.223857, 0, 0.5, 0)
	pc.Z()
	return pc
}

func legacyParallelogramPath(box *geo.Box) *svg.SvgPathContext {
	wedgeWidth := parallelWedgeWidth
	// Note: box width should always be larger than parallelWedgeWidth
	// this just handles after collapsing into a line
	if box.Width <= wedgeWidth {
		wedgeWidth = box.Width / 2.0
	}
	pc := svg.NewSVGPathContext(box.TopLeft, 1, 1)
	pc.StartAt(pc.Absolute(wedgeWidth, 0))
	pc.L(false, box.Width, 0)
	pc.L(false, box.Width-wedgeWidth, box.Height)
	pc.L(false, 0, box.Height)
	pc.L(false, 0, box.Height)
	pc.Z()
	return pc
}

func legacyPersonPath(box *geo.Box) *svg.SvgPathContext {
	pc := svg.NewSVGPathContext(box.TopLeft, box.Width/68.3, box.Height/77.4)

	// Bottom side
	pc.StartAt(pc.Absolute(68.3, 77.4))
	pc.H(false, 0)
	pc.V(true, -1.1)
	pc.C(true, 0, -13.2, 7.5, -25.1, 19.3, -30.8)
	pc.C(false, 12.8, 40.9, 8.9, 33.4, 8.9, 25.2)
	pc.C(false, 8.9, 11.3, 20.2, 0, 34.1, 0)

	// TODO: implement s command with mirroring last control point
	// s 			25.2,11.3, 	25.2,25.2
	// mirroring last control point (20.2,0) -> (34.1,0) = relative(13.9,0)
	pc.C(true, 13.9, 0, 25.2, 11.3, 25.2, 25.2)

	pc.C(true, 0, 8.2, -3.8, 15.6, -10.4, 20.4)
	pc.C(true, 11.8, 5.7, 19.3, 17.6, 19.3, 30.8)
	pc.V(true, 1)
	pc.H(false, 68.3)
	pc.Z()
	return pc
}

func legacyQueueOuterPath(box *geo.Box) *svg.SvgPathContext {
	arcWidth := getArcWidth(box)
	multiplier := 0.45
	pc := svg.NewSVGPathContext(box.TopLeft, 1, 1)
	pc.StartAt(pc.Absolute(arcWidth, 0))
	pc.H(true, box.Width-2*arcWidth)
	pc.C(false, box.Width, 0, box.Width, box.Height*multiplier, box.Width, box.Height/2.0)
	pc.C(false, box.Width, box.Height-box.Height*multiplier, box.Width, box.Height, box.Width-arcWidth, box.Height)
	pc.H(true, -1*(box.Width-2*arcWidth))
	pc.C(false, 0, box.Height, 0, box.Height-box.Height*multiplier, 0, box.Height/2.0)
	pc.C(false, 0, box.Height*multiplier, 0, 0, arcWidth, 0)
	pc.Z()
	return pc
}

func legacyStepPath(box *geo.Box) *svg.SvgPathContext {
	wedgeWidth := STEP_WEDGE_WIDTH
	if box.Width <= wedgeWidth {
		wedgeWidth = box.Width / 2.0
	}
	pc := svg.NewSVGPathContext(box.TopLeft, 1, 1)
	pc.StartAt(pc.Absolute(0, 0))
	pc.L(false, box.Width-wedgeWidth, 0)
	pc.L(false, box.Width, box.Height/2)
	pc.L(false, box.Width-wedgeWidth, box.Height)
	pc.L(false, 0, box.Height)
	pc.L(false, wedgeWidth, box.Height/2)
	pc.Z()
	return pc
}

func legacyStoredDataPath(box *geo.Box) *svg.SvgPathContext {
	wedgeWidth := storedDataWedgeWidth
	multiplier := 0.27
	if box.Width < wedgeWidth*2 {
		wedgeWidth = box.Width / 2.0
	}
	pc := svg.NewSVGPathContext(box.TopLeft, 1, 1)
	pc.StartAt(pc.Absolute(wedgeWidth, 0))
	pc.H(true, box.Width-wedgeWidth)
	pc.C(false, box.Width-wedgeWidth*multiplier, 0, box.Width-wedgeWidth, box.Height*multiplier, box.Width-wedgeWidth, box.Height/2.0)
	pc.C(false, box.Width-wedgeWidth, box.Height-box.Height*multiplier, box.Width-wedgeWidth*multiplier, box.Height, box.Width, box.Height)
	pc.H(true, -(box.Width - wedgeWidth))
	pc.C(false, wedgeWidth-wedgeWidth*multiplier, box.Height, 0, box.Height-box.Height*multiplier, 0, box.Height/2.0)
	pc.C(false, 0, box.Height*multiplier, wedgeWidth-wedgeWidth*multiplier, 0, wedgeWidth, 0)
	pc.Z()
	return pc
}

func legacyTraceToShapeBorder(shape Shape, perimeter []geo.Intersectable, rectBorderPoint, prevPoint *geo.Point) *geo.Point {
	if shape.Is("") || shape.IsRectangular() {
		return rectBorderPoint
	}

	// We want to extend the line all the way through to the other end of the shape to get the intersections
	scaleSize := shape.GetBox().Width
	if prevPoint.X == rectBorderPoint.X {
		scaleSize = shape.GetBox().Height
	}
	vector := prevPoint.VectorTo(rectBorderPoint)
	vector = vector.AddLength(scaleSize)
	extendedSegment := geo.Segment{Start: prevPoint, End: prevPoint.AddVector(vector)}

	closestD := math.Inf(1)
	closestPoint := rectBorderPoint

	for _, perimeterSegment := range perimeter {
		for _, intersectingPoint := range perimeterSegment.Intersections(extendedSegment) {
			d := geo.EuclideanDistance(rectBorderPoint.X, rectBorderPoint.Y, intersectingPoint.X, intersectingPoint.Y)
			if d < closestD {
				closestD = d
				closestPoint = intersectingPoint
			}
		}
	}

	closestPoint.TruncateFloat32()
	return geo.NewPoint(math.Round(closestPoint.X), math.Round(closestPoint.Y))
}
