package nodeshape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/shape"
)

// Snap points that do not depend on the box, in top, left, bottom, and right
// order. Every call returns the same value: callers only read it.

var squareSnapPoints = [][]*geo.RelativePoint{
	{geo.NewRelativePoint(0.25, 0), geo.NewRelativePoint(0.5, 0), geo.NewRelativePoint(0.75, 0)},
	{geo.NewRelativePoint(0, 0.25), geo.NewRelativePoint(0, 0.5), geo.NewRelativePoint(0, 0.75)},
	{geo.NewRelativePoint(0.25, 1), geo.NewRelativePoint(0.5, 1), geo.NewRelativePoint(0.75, 1)},
	{geo.NewRelativePoint(1, 0.25), geo.NewRelativePoint(1, 0.5), geo.NewRelativePoint(1, 0.75)},
}

var ovalSnapPoints = [][]*geo.RelativePoint{
	{geo.NewRelativePoint(0.25, 0.066), geo.NewRelativePoint(0.5, 0), geo.NewRelativePoint(0.75, 0.066)},
	{geo.NewRelativePoint(0.066, 0.25), geo.NewRelativePoint(0, 0.5), geo.NewRelativePoint(0.066, 0.75)},
	{geo.NewRelativePoint(0.25, 0.934), geo.NewRelativePoint(0.5, 1), geo.NewRelativePoint(0.75, 0.934)},
	{geo.NewRelativePoint(0.934, 0.25), geo.NewRelativePoint(1, 0.5), geo.NewRelativePoint(0.934, 0.75)},
}

var hexagonSnapPoints = [][]*geo.RelativePoint{
	{geo.NewRelativePoint(0.25, 0), geo.NewRelativePoint(0.5, 0), geo.NewRelativePoint(0.75, 0)},
	{geo.NewRelativePoint(0.125, 0.25), geo.NewRelativePoint(0, 0.5), geo.NewRelativePoint(0.125, 0.75)},
	{geo.NewRelativePoint(0.25, 1), geo.NewRelativePoint(0.5, 1), geo.NewRelativePoint(0.75, 1)},
	{geo.NewRelativePoint(0.875, 0.25), geo.NewRelativePoint(1, 0.5), geo.NewRelativePoint(0.875, 0.75)},
}

var cloudSnapPoints = [][]*geo.RelativePoint{
	{geo.NewRelativePoint(0.16, 0.368), geo.NewRelativePoint(0.378, 0.155), geo.NewRelativePoint(0.815, 0.328)},
	{geo.NewRelativePoint(0, 0.7), geo.NewRelativePoint(0.066, 0.935)},
	{geo.NewRelativePoint(0.25, 1), geo.NewRelativePoint(0.5, 1), geo.NewRelativePoint(0.75, 1)},
	{geo.NewRelativePoint(1, 0.7), geo.NewRelativePoint(0.95, 0.89)},
}

var diamondSnapPoints = [][]*geo.RelativePoint{
	{geo.NewRelativePoint(0.5, 0)},
	{geo.NewRelativePoint(0, 0.5)},
	{geo.NewRelativePoint(0.5, 1)},
	{geo.NewRelativePoint(1, 0.5)},
}

var personSnapPoints = [][]*geo.RelativePoint{
	{geo.NewRelativePoint(0.21, 0.122), geo.NewRelativePoint(0.5, 0), geo.NewRelativePoint(0.79, 0.122)},
	{geo.NewRelativePoint(0.135, 0.35), geo.NewRelativePoint(0.08, 0.75)},
	{geo.NewRelativePoint(0.25, 0.985), geo.NewRelativePoint(0.5, 1), geo.NewRelativePoint(0.75, 0.985)},
	{geo.NewRelativePoint(0.865, 0.35), geo.NewRelativePoint(0.92, 0.75)},
}

var c4PersonSnapPoints = [][]*geo.RelativePoint{
	{geo.NewRelativePoint(0.5, 0)}, // center of the head
	// Left and right: ports on the body only.
	{
		geo.NewRelativePoint(0, 0.45), // Just below where body starts
		geo.NewRelativePoint(0, 0.65), // Middle of body
		geo.NewRelativePoint(0, 0.85), // Lower part of body
	},
	{geo.NewRelativePoint(0.25, 1), geo.NewRelativePoint(0.5, 1), geo.NewRelativePoint(0.75, 1)},
	{
		geo.NewRelativePoint(1, 0.45), // Just below where body starts
		geo.NewRelativePoint(1, 0.65), // Middle of body
		geo.NewRelativePoint(1, 0.85), // Lower part of body
	},
}

func parallelogramSnapPoints(width float64) [][]*geo.RelativePoint {
	wedgeWidth := 26.0
	if width < wedgeWidth {
		wedgeWidth = width / 2.0
	}

	return [][]*geo.RelativePoint{
		// Top
		{
			geo.NewRelativePoint(wedgeWidth/width+((width-wedgeWidth)/width)*0.25, 0),
			geo.NewRelativePoint(wedgeWidth/width+((width-wedgeWidth)/width)*0.5, 0),
			geo.NewRelativePoint(wedgeWidth/width+((width-wedgeWidth)/width)*0.75, 0),
		},
		// Left
		{
			geo.NewRelativePoint((wedgeWidth/2.0+wedgeWidth)/2.0/width, 0.25),
			geo.NewRelativePoint(wedgeWidth/2.0/width, 0.5),
			geo.NewRelativePoint(wedgeWidth/2.0/2.0/width, 0.75),
		},
		// Bottom
		{
			geo.NewRelativePoint(((width-wedgeWidth)/width)*0.25, 1),
			geo.NewRelativePoint(((width-wedgeWidth)/width)*0.5, 1),
			geo.NewRelativePoint(((width-wedgeWidth)/width)*0.75, 1),
		},
		// Right
		{
			geo.NewRelativePoint((width-wedgeWidth)/width+(wedgeWidth/2.0+wedgeWidth)/2.0/width, 0.25),
			geo.NewRelativePoint((width-wedgeWidth)/width+wedgeWidth/2.0/width, 0.5),
			geo.NewRelativePoint((width-wedgeWidth)/width+wedgeWidth/2.0/2.0/width, 0.75),
		},
	}
}

func documentSnapPoints(width, height float64) [][]*geo.RelativePoint {
	bottomLeftMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(0, height*(16.3/18.925)),
		geo.NewPoint(width/6, height*(19.8/18.925)),
		geo.NewPoint(width/3, height*(19.8/18.925)),
		geo.NewPoint(width/2, height*(16.3/18.925)),
	}).At(0.5)
	bottomRightCurve := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(width/2, height*(16.3/18.925)),
		geo.NewPoint(width*2/3, height*(12.8/18.925)),
		geo.NewPoint(width*5/6, height*(12.8/18.925)),
		geo.NewPoint(width, height*(16.3/18.925)),
	})
	return [][]*geo.RelativePoint{
		// Top
		{
			geo.NewRelativePoint(0.25, 0),
			geo.NewRelativePoint(0.5, 0),
			geo.NewRelativePoint(0.75, 0),
		},
		// Left
		{
			geo.NewRelativePoint(0, 0.25),
			geo.NewRelativePoint(0, 0.5),
			geo.NewRelativePoint(0, 0.75),
		},
		// Bottom
		{
			geo.NewRelativePoint(bottomLeftMidpoint.X/width, bottomLeftMidpoint.Y/height),
			geo.NewRelativePoint(bottomRightCurve.At(0).X/width, bottomRightCurve.At(0).Y/height),
			geo.NewRelativePoint(bottomRightCurve.At(0.5).X/width, bottomRightCurve.At(0.5).Y/height),
		},
		// Right
		{
			geo.NewRelativePoint(1, 0.25),
			geo.NewRelativePoint(1, 0.5),
			geo.NewRelativePoint(1, 0.75),
		},
	}
}

func cylinderSnapPoints(width, height float64) [][]*geo.RelativePoint {
	arcDepth := 24.0
	controlPointsMultiplier := 0.45

	if height < arcDepth*2 {
		arcDepth = height / 2.0
	}

	topLeftMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(0, arcDepth),
		geo.NewPoint(0, 0),
		geo.NewPoint(width*controlPointsMultiplier, 0),
		geo.NewPoint(width/2.0, 0),
	}).At(0.5)
	topRightMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(width/2.0, 0),
		geo.NewPoint(width-(width*controlPointsMultiplier), 0),
		geo.NewPoint(width, 0),
		geo.NewPoint(width, arcDepth),
	}).At(0.5)
	bottomRightMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(width, height-arcDepth),
		geo.NewPoint(width, height),
		geo.NewPoint(width-width*controlPointsMultiplier, height),
		geo.NewPoint(width/2.0, height),
	}).At(0.5)
	bottomLeftMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(width/2.0, height),
		geo.NewPoint(width*controlPointsMultiplier, height),
		geo.NewPoint(0, height),
		geo.NewPoint(0, height-arcDepth),
	}).At(0.5)

	return [][]*geo.RelativePoint{
		// Top
		{
			geo.NewRelativePoint(topLeftMidpoint.X/width, topLeftMidpoint.Y/height),
			geo.NewRelativePoint(0.5, 0),
			geo.NewRelativePoint(topRightMidpoint.X/width, topRightMidpoint.Y/height),
		},
		// Left
		{
			geo.NewRelativePoint(0, arcDepth/height+((height-arcDepth*2)/height)*0.25),
			geo.NewRelativePoint(0, arcDepth/height+((height-arcDepth*2)/height)*0.5),
			geo.NewRelativePoint(0, arcDepth/height+((height-arcDepth*2)/height)*0.75),
		},
		// Bottom
		{
			geo.NewRelativePoint(bottomLeftMidpoint.X/width, bottomLeftMidpoint.Y/height),
			geo.NewRelativePoint(0.5, 1),
			geo.NewRelativePoint(bottomRightMidpoint.X/width, bottomRightMidpoint.Y/height),
		},
		// Right
		{
			geo.NewRelativePoint(1, arcDepth/height+((height-arcDepth*2)/height)*0.25),
			geo.NewRelativePoint(1, arcDepth/height+((height-arcDepth*2)/height)*0.5),
			geo.NewRelativePoint(1, arcDepth/height+((height-arcDepth*2)/height)*0.75),
		},
	}
}

func queueSnapPoints(width, height float64) [][]*geo.RelativePoint {
	arcDepth := 24.0
	controlPointsMultiplier := 0.45

	if width < arcDepth*2 {
		arcDepth = width / 2.0
	}

	topLeftMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(arcDepth, 0),
		geo.NewPoint(0, 0),
		geo.NewPoint(0, height*controlPointsMultiplier),
		geo.NewPoint(0, height/2.0),
	}).At(0.5)
	topRightMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(width-arcDepth, 0),
		geo.NewPoint(width, 0),
		geo.NewPoint(width, height*controlPointsMultiplier),
		geo.NewPoint(width, height/2.0),
	}).At(0.5)
	bottomRightMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(width, height/2.0),
		geo.NewPoint(width, height-height*controlPointsMultiplier),
		geo.NewPoint(width, height),
		geo.NewPoint(width-arcDepth, height),
	}).At(0.5)
	bottomLeftMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(0, height/2.0),
		geo.NewPoint(0, height-height*controlPointsMultiplier),
		geo.NewPoint(0, height),
		geo.NewPoint(arcDepth, height),
	}).At(0.5)

	return [][]*geo.RelativePoint{
		// Top
		{
			geo.NewRelativePoint(arcDepth/width+((width-arcDepth*2)/width)*0.25, 0),
			geo.NewRelativePoint(arcDepth/width+((width-arcDepth*2)/width)*0.5, 0),
			geo.NewRelativePoint(arcDepth/width+((width-arcDepth*2)/width)*0.75, 0),
		},
		// Left
		{
			geo.NewRelativePoint(topLeftMidpoint.X/width, topLeftMidpoint.Y/height),
			geo.NewRelativePoint(0, 0.5),
			geo.NewRelativePoint(bottomLeftMidpoint.X/width, bottomLeftMidpoint.Y/height),
		},
		// Bottom
		{
			geo.NewRelativePoint(arcDepth/width+((width-arcDepth*2)/width)*0.25, 1),
			geo.NewRelativePoint(arcDepth/width+((width-arcDepth*2)/width)*0.5, 1),
			geo.NewRelativePoint(arcDepth/width+((width-arcDepth*2)/width)*0.75, 1),
		},
		// Right
		{
			geo.NewRelativePoint(topRightMidpoint.X/width, topRightMidpoint.Y/height),
			geo.NewRelativePoint(1, 0.5),
			geo.NewRelativePoint(bottomRightMidpoint.X/width, bottomRightMidpoint.Y/height),
		},
	}
}

func packageSnapPoints(width, height float64) [][]*geo.RelativePoint {
	verticalScalar := 0.2
	horizontalScalar := 0.5
	minTopHeight := 34.0
	maxTopHeight := 55.0

	minTopWidth := 50.0
	maxTopWidth := 150.0

	topWidth := math.Min(maxTopWidth, math.Max(minTopWidth, width*horizontalScalar))

	if width < 2*minTopWidth {
		topWidth = width * horizontalScalar
	}

	topHeight := math.Min(maxTopHeight, math.Max(minTopHeight, height*verticalScalar))

	if height < 2*minTopHeight {
		topHeight = height * verticalScalar
	}

	topWidthRatio := topWidth / width
	topHeightRatio := topHeight / height

	// Space occupied by main shape not overlapping
	negativeTopWidthRatio := 1 - topWidthRatio
	negativeTopHeightRatio := 1 - topHeightRatio

	return [][]*geo.RelativePoint{
		// Top
		{
			geo.NewRelativePoint(topWidthRatio*0.33, 0),
			geo.NewRelativePoint(topWidthRatio*0.66, 0),
			geo.NewRelativePoint(topWidthRatio+negativeTopWidthRatio*0.5, topHeightRatio),
		},
		// Left
		{
			geo.NewRelativePoint(0, topHeightRatio+negativeTopHeightRatio*0.25),
			geo.NewRelativePoint(0, topHeightRatio+negativeTopHeightRatio*0.5),
			geo.NewRelativePoint(0, topHeightRatio+negativeTopHeightRatio*0.75),
		},
		// Bottom
		{
			geo.NewRelativePoint(0.25, 1),
			geo.NewRelativePoint(0.5, 1),
			geo.NewRelativePoint(0.75, 1),
		},
		// Right
		{
			geo.NewRelativePoint(1, topHeightRatio+negativeTopHeightRatio*0.25),
			geo.NewRelativePoint(1, topHeightRatio+negativeTopHeightRatio*0.5),
			geo.NewRelativePoint(1, topHeightRatio+negativeTopHeightRatio*0.75),
		},
	}
}

func stepSnapPoints(width float64) [][]*geo.RelativePoint {
	wedgeWidth := shape.StepWedgeWidth(width)

	return [][]*geo.RelativePoint{
		// Top
		{
			geo.NewRelativePoint(((width-wedgeWidth)/width)*0.25, 0),
			geo.NewRelativePoint(((width-wedgeWidth)/width)*0.5, 0),
			geo.NewRelativePoint(((width-wedgeWidth)/width)*0.75, 0),
		},
		// Left
		{
			geo.NewRelativePoint(wedgeWidth/width/2, 0.25),
			geo.NewRelativePoint(wedgeWidth/width, 0.5),
			geo.NewRelativePoint(wedgeWidth/width/2, 0.75),
		},
		// Bottom
		{
			geo.NewRelativePoint(((width-wedgeWidth)/width)*0.25, 1),
			geo.NewRelativePoint(((width-wedgeWidth)/width)*0.5, 1),
			geo.NewRelativePoint(((width-wedgeWidth)/width)*0.75, 1),
		},
		// Right
		{
			geo.NewRelativePoint(1, 0.5),
		},
	}
}

func calloutSnapPoints(width, height float64) [][]*geo.RelativePoint {
	tipWidth := 30.0
	tipHeight := 45.0

	if width < tipWidth*2 {
		tipWidth = width / 2.0
	}

	if height < tipHeight*2 {
		tipHeight = height / 2.0
	}

	return [][]*geo.RelativePoint{
		// Top
		{
			geo.NewRelativePoint(0.25, 0),
			geo.NewRelativePoint(0.5, 0),
			geo.NewRelativePoint(0.75, 0),
		},
		// Left
		{
			geo.NewRelativePoint(0, ((height-tipHeight)/height)*0.25),
			geo.NewRelativePoint(0, ((height-tipHeight)/height)*0.5),
			geo.NewRelativePoint(0, ((height-tipHeight)/height)*0.75),
		},
		// Bottom
		{
			geo.NewRelativePoint(0.5*0.33, (height-tipHeight)/height),
			geo.NewRelativePoint(0.5*0.66, (height-tipHeight)/height),
			geo.NewRelativePoint(0.5, 1),
			geo.NewRelativePoint(
				1-((width/2.0-tipWidth)/width)*0.5,
				(height-tipHeight)/height,
			),
		},
		// Right
		{
			geo.NewRelativePoint(1, ((height-tipHeight)/height)*0.25),
			geo.NewRelativePoint(1, ((height-tipHeight)/height)*0.5),
			geo.NewRelativePoint(1, ((height-tipHeight)/height)*0.75),
		},
	}
}

func storedDataSnapPoints(width, height float64) [][]*geo.RelativePoint {
	wedgeWidth := 15.0
	if width < wedgeWidth {
		wedgeWidth = width / 2
	}
	controlPointsMultiplier := 0.27

	topLeftMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(wedgeWidth, 0.0),
		geo.NewPoint(wedgeWidth-(wedgeWidth*controlPointsMultiplier), 0.0),
		geo.NewPoint(0.0, height*controlPointsMultiplier),
		geo.NewPoint(0.0, height/2.0),
	}).At(0.5)

	bottomLeftMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(wedgeWidth, height),
		geo.NewPoint(wedgeWidth-(wedgeWidth*controlPointsMultiplier), height),
		geo.NewPoint(0.0, height-height*controlPointsMultiplier),
		geo.NewPoint(0.0, height/2.0),
	}).At(0.5)

	topRightMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(width, 0),
		geo.NewPoint(width-(wedgeWidth*controlPointsMultiplier), 0),
		geo.NewPoint(width-wedgeWidth, height*controlPointsMultiplier),
		geo.NewPoint(width-wedgeWidth, height/2.0),
	}).At(0.5)

	bottomRightMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(width-wedgeWidth, height/2.0),
		geo.NewPoint(width-wedgeWidth, height-height*controlPointsMultiplier),
		geo.NewPoint(width-wedgeWidth*controlPointsMultiplier, height),
		geo.NewPoint(width, height),
	}).At(0.5)

	sideWidthPercentage := (width - wedgeWidth) / width
	sideWidthStartPercentage := 1 - sideWidthPercentage

	return [][]*geo.RelativePoint{
		// Top
		{
			geo.NewRelativePoint(sideWidthStartPercentage+(0.25*sideWidthPercentage), 0),
			geo.NewRelativePoint(sideWidthStartPercentage+(0.5*sideWidthPercentage), 0),
			geo.NewRelativePoint(sideWidthStartPercentage+(0.75*sideWidthPercentage), 0),
		},
		// Left
		{
			geo.NewRelativePoint(topLeftMidpoint.X/width, topLeftMidpoint.Y/height),
			geo.NewRelativePoint(0.0, 0.5),
			geo.NewRelativePoint(bottomLeftMidpoint.X/width, bottomLeftMidpoint.Y/height),
		},
		// Bottom
		{
			geo.NewRelativePoint(sideWidthStartPercentage+(0.25*sideWidthPercentage), 1),
			geo.NewRelativePoint(sideWidthStartPercentage+(0.5*sideWidthPercentage), 1),
			geo.NewRelativePoint(sideWidthStartPercentage+(0.75*sideWidthPercentage), 1),
		},
		// Right
		{
			geo.NewRelativePoint(topRightMidpoint.X/width, topRightMidpoint.Y/height),
			geo.NewRelativePoint((width-wedgeWidth)/width, 0.5),
			geo.NewRelativePoint(bottomRightMidpoint.X/width, bottomRightMidpoint.Y/height),
		},
	}
}
