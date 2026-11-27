package shape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

const (
	SQUARE_TYPE        = "Square"
	REAL_SQUARE_TYPE   = "RealSquare"
	PARALLELOGRAM_TYPE = "Parallelogram"
	DOCUMENT_TYPE      = "Document"
	CYLINDER_TYPE      = "Cylinder"
	QUEUE_TYPE         = "Queue"
	PAGE_TYPE          = "Page"
	PACKAGE_TYPE       = "Package"
	STEP_TYPE          = "Step"
	CALLOUT_TYPE       = "Callout"
	STORED_DATA_TYPE   = "StoredData"
	PERSON_TYPE        = "Person"
	C4_PERSON_TYPE     = "C4Person"
	DIAMOND_TYPE       = "Diamond"
	OVAL_TYPE          = "Oval"
	CIRCLE_TYPE        = "Circle"
	HEXAGON_TYPE       = "Hexagon"
	CLOUD_TYPE         = "Cloud"

	TABLE_TYPE = "Table"
	CLASS_TYPE = "Class"
	TEXT_TYPE  = "Text"
	CODE_TYPE  = "Code"
	IMAGE_TYPE = "Image"

	defaultPadding = 40.
)

type Shape interface {
	Is(shape string) bool
	GetType() string

	AspectRatio1() bool
	IsRectangular() bool

	GetBox() *geo.Box
	GetInnerBox() *geo.Box
	// cloud shape has different innerBoxes depending on content's aspect ratio
	GetInnerBoxForContent(width, height float64) *geo.Box
	SetInnerBoxAspectRatio(aspectRatio float64)

	// placing a rectangle of the given size and padding inside the shape, return the position relative to the shape's TopLeft
	GetInsidePlacement(width, height, paddingX, paddingY float64) geo.Point

	GetDimensionsToFit(width, height, paddingX, paddingY float64) (float64, float64)
	GetDefaultPadding() (paddingX, paddingY float64)

	// Perimeter returns a slice of geo.Intersectables that together constitute the shape border
	Perimeter() []geo.Intersectable

	GetSVGPathData() []string
}

// PathCommandProvider is implemented by shapes with custom path geometry.
// Implementations return a defensive copy of each path's absolute commands.
// It is separate from Shape so adding typed path access does not break external
// implementations of Shape.
type PathCommandProvider interface {
	GetPathCommands() [][]svg.PathCommand
}

// GetPathCommands returns a shape's absolute custom path commands, or nil when
// the shape is represented by another primitive such as a rectangle or oval.
func GetPathCommands(s Shape) [][]svg.PathCommand {
	provider, ok := s.(PathCommandProvider)
	if !ok {
		return nil
	}
	return provider.GetPathCommands()
}

func pathCommands(paths ...*svg.SvgPathContext) [][]svg.PathCommand {
	commands := make([][]svg.PathCommand, len(paths))
	for i, path := range paths {
		commands[i] = path.PathCommands()
	}
	return commands
}

// shapeKind is what sets one kind of shape apart from the others: its flags
// and default padding, and the functions that compute what differs from a
// rectangle. A nil function means the shape behaves as a rectangle there.
type shapeKind struct {
	typ          string
	aspectRatio1 bool
	rectangular  bool
	paddingX     float64
	paddingY     float64

	innerBox           func(s *shape) *geo.Box
	innerBoxForContent func(s *shape, width, height float64) *geo.Box
	insidePlacement    func(s *shape, width, height, paddingX, paddingY float64) geo.Point
	dimensionsToFit    func(width, height, paddingX, paddingY float64) (float64, float64)

	// path is the shape's outline, the first of its SVG paths, and extraPath
	// the second one, if any. A shape with a path is a PathCommandProvider.
	path      func(box *geo.Box) *svg.SvgPathContext
	extraPath func(box *geo.Box) *svg.SvgPathContext
	// trace draws path into a borderTracer.
	trace func(box *geo.Box, t *borderTracer)
	// perimeter is the Perimeter of the shapes for which it is not path's Path.
	perimeter func(s *shape) []geo.Intersectable
}

// shape is every Shape of this package: a box and the kind of shape drawn in it.
type shape struct {
	kind *shapeKind
	typ  string
	Box  *geo.Box
	// innerBoxAspectRatio is set by SetInnerBoxAspectRatio; only the cloud uses it.
	innerBoxAspectRatio float64
}

// pathShape is a shape whose kind has a path: only it has GetPathCommands.
type pathShape struct {
	shape
}

func newShape(kind *shapeKind, box *geo.Box) Shape {
	s := shape{kind: kind, typ: kind.typ, Box: box}
	if kind.path != nil {
		return &pathShape{s}
	}
	return &s
}

func (s *shape) Is(shapeType string) bool {
	return s.typ == shapeType
}

func (s *shape) GetType() string {
	return s.typ
}

func (s *shape) AspectRatio1() bool {
	return s.kind.aspectRatio1
}

func (s *shape) IsRectangular() bool {
	return s.kind.rectangular
}

func (s *shape) GetBox() *geo.Box {
	return s.Box
}

func (s *shape) GetInnerBox() *geo.Box {
	if s.kind.innerBox != nil {
		return s.kind.innerBox(s)
	}
	return s.Box
}

// only cloud shape needs this right now
func (s *shape) GetInnerBoxForContent(width, height float64) *geo.Box {
	if s.kind.innerBoxForContent != nil {
		return s.kind.innerBoxForContent(s, width, height)
	}
	return nil
}

func (s *shape) SetInnerBoxAspectRatio(aspectRatio float64) {
	s.innerBoxAspectRatio = aspectRatio
}

func (s *shape) GetInsidePlacement(width, height, paddingX, paddingY float64) geo.Point {
	if s.kind.insidePlacement != nil {
		return s.kind.insidePlacement(s, width, height, paddingX, paddingY)
	}
	innerTL := s.GetInnerBox().TopLeft
	return *geo.NewPoint(innerTL.X+paddingX/2, innerTL.Y+paddingY/2)
}

// return the minimum shape dimensions needed to fit content (width x height)
// in the shape's innerBox with padding
func (s *shape) GetDimensionsToFit(width, height, paddingX, paddingY float64) (float64, float64) {
	if s.kind.dimensionsToFit != nil {
		return s.kind.dimensionsToFit(width, height, paddingX, paddingY)
	}
	return math.Ceil(width + paddingX), math.Ceil(height + paddingY)
}

func (s *shape) GetDefaultPadding() (paddingX, paddingY float64) {
	return s.kind.paddingX, s.kind.paddingY
}

func (s *shape) Perimeter() []geo.Intersectable {
	switch {
	case s.kind.perimeter != nil:
		return s.kind.perimeter(s)
	case s.kind.path != nil:
		return s.kind.path(s.Box).Path
	}
	return nil
}

// traceBorder draws the outline into t; it reports false if the shape has
// no outline to trace.
func (s *shape) traceBorder(t *borderTracer) bool {
	if s.kind.trace == nil {
		return false
	}
	s.kind.trace(s.Box, t)
	return true
}

// paths returns the shape's SVG paths.
func (s *shape) paths() []*svg.SvgPathContext {
	if s.kind.path == nil {
		return nil
	}
	if s.kind.extraPath == nil {
		return []*svg.SvgPathContext{s.kind.path(s.Box)}
	}
	return []*svg.SvgPathContext{s.kind.path(s.Box), s.kind.extraPath(s.Box)}
}

func (s *shape) GetSVGPathData() []string {
	paths := s.paths()
	if paths == nil {
		return nil
	}
	data := make([]string, len(paths))
	for i, path := range paths {
		data[i] = path.PathData()
	}
	return data
}

func (s *pathShape) GetPathCommands() [][]svg.PathCommand {
	return pathCommands(s.paths()...)
}

// ellipseInnerBox is the inner box of the circle and the oval: the box
// between the inside placement without padding and its mirror image.
func ellipseInnerBox(s *shape) *geo.Box {
	width := s.Box.Width
	height := s.Box.Height
	insideTL := s.GetInsidePlacement(width, height, 0, 0)
	tl := s.Box.TopLeft.Copy()
	width -= 2 * (insideTL.X - tl.X)
	height -= 2 * (insideTL.Y - tl.Y)
	return geo.NewBox(&insideTL, width, height)
}

func ellipsePerimeter(s *shape) []geo.Intersectable {
	return []geo.Intersectable{geo.NewEllipse(s.Box.Center(), s.Box.Width/2, s.Box.Height/2)}
}

// shapeKinds are the kinds NewShape makes by type.
var shapeKinds = []*shapeKind{
	&calloutKind, &circleKind, &classKind, &cloudKind, &codeKind, &cylinderKind,
	&diamondKind, &documentKind, &hexagonKind, &imageKind, &ovalKind, &packageKind,
	&pageKind, &parallelogramKind, &personKind, &c4PersonKind, &queueKind,
	&realSquareKind, &stepKind, &storedDataKind, &squareKind, &tableKind, &textKind,
}

// NewShape returns a shape of the given type; an unknown type gets a
// rectangle.
func NewShape(shapeType string, box *geo.Box) Shape {
	for _, kind := range shapeKinds {
		if kind.typ == shapeType {
			return newShape(kind, box)
		}
	}
	return &shape{kind: &squareKind, typ: shapeType, Box: box}
}

// TraceToShapeBorder takes the point on the rectangular border
// r here is the point on rectangular border
// p is the prev point (used to calculate slope)
// s is the point on the actual shape border that'll be returned
//
// .      p
// .      │
// .      │
// .      ▼
// . ┌────r─────────────────────────┐
// . │                              │
// . │    │                         │
// . │    │      xxxxxxxx           │
// . │    ▼  xxxxx       xxxx       │
// . │    sxxx               xx     │
// . │   x                    xx    │
// . │  xx                     xx   │
// . │  x                      xx   │
// . │  xx                   xxx    │
// . │   xxxx             xxxx      │
// . └──────xxxxxxxxxxxxxx──────────┘
func TraceToShapeBorder(shape Shape, rectBorderPoint, prevPoint *geo.Point) *geo.Point {
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

	closestD := math.Inf(1)
	closestPoint := rectBorderPoint

	// The same search, done while the outline is drawn instead of over a
	// Perimeter() built for it, on point values: the segment's ends, read
	// now, and rectBorderPoint do not change while it runs.
	t := borderTracer{seg0: *prevPoint, seg1: *prevPoint.AddVector(vector), ref: *rectBorderPoint, closestD: closestD}
	if outline, ok := shape.(borderOutline); ok && outline.traceBorder(&t) {
		if t.found {
			// A fresh point, as the intersections are: truncating it
			// changes none of the caller's points.
			closestPoint = &t.closest
		}
	} else {
		extendedSegment := geo.Segment{Start: prevPoint, End: prevPoint.AddVector(vector)}
		for _, perimeterSegment := range shape.Perimeter() {
			for _, intersectingPoint := range perimeterSegment.Intersections(extendedSegment) {
				d := geo.EuclideanDistance(rectBorderPoint.X, rectBorderPoint.Y, intersectingPoint.X, intersectingPoint.Y)
				if d < closestD {
					closestD = d
					closestPoint = intersectingPoint
				}
			}
		}
	}

	closestPoint.TruncateFloat32()
	return geo.NewPoint(math.Round(closestPoint.X), math.Round(closestPoint.Y))
}

func LimitAR(width, height, aspectRatio float64) (float64, float64) {
	if width > aspectRatio*height {
		height = math.Round(width / aspectRatio)
	} else if height > aspectRatio*width {
		width = math.Round(height / aspectRatio)
	}
	return width, height
}
