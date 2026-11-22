package shape

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

// outlinePaths pairs each outline (the current path function) with its
// previous version (legacy_outline_test.go).
var outlinePaths = []struct {
	shapeType   string
	path        func(*geo.Box) *svg.SvgPathContext
	legacyPath  func(*geo.Box) *svg.SvgPathContext
	isPerimeter bool
}{
	{CALLOUT_TYPE, calloutPath, legacyCalloutPath, true},
	{CLOUD_TYPE, cloudPath, legacyCloudPath, true},
	{CYLINDER_TYPE, cylinderOuterPath, legacyCylinderOuterPath, true},
	{DIAMOND_TYPE, diamondPath, legacyDiamondPath, true},
	{DOCUMENT_TYPE, documentPath, legacyDocumentPath, true},
	{HEXAGON_TYPE, hexagonPath, legacyHexagonPath, true},
	{PACKAGE_TYPE, packagePath, legacyPackagePath, true},
	{PAGE_TYPE, pageOuterPath, legacyPageOuterPath, true},
	{PARALLELOGRAM_TYPE, parallelogramPath, legacyParallelogramPath, true},
	{PERSON_TYPE, personPath, legacyPersonPath, true},
	{QUEUE_TYPE, queueOuterPath, legacyQueueOuterPath, true},
	{STEP_TYPE, stepPath, legacyStepPath, true},
	{STORED_DATA_TYPE, storedDataPath, legacyStoredDataPath, true},
}

// legacyPerimeter is Perimeter() as it was: the legacy path for the outline
// shapes, the shape's own Perimeter for the others (which did not change).
func legacyPerimeter(s Shape) []geo.Intersectable {
	for _, o := range outlinePaths {
		if s.GetType() == o.shapeType && o.isPerimeter {
			return o.legacyPath(s.GetBox()).Path
		}
	}
	return s.Perimeter()
}

// randomOutlineBox returns a box with fractional coordinates; sizes include
// the small ones for which the outlines change shape (wedges, arcs, tips).
func randomOutlineBox(r *rand.Rand) *geo.Box {
	size := func() float64 {
		switch r.Intn(4) {
		case 0:
			return float64(1 + r.Intn(60))
		case 1:
			return r.Float64() * 400
		default:
			return float64(20 + r.Intn(300))
		}
	}
	tl := geo.NewPoint(math.Round(r.Float64()*2000-500)/[]float64{1, 2, 4, 3}[r.Intn(4)], math.Round(r.Float64()*2000-500)/[]float64{1, 2, 4, 3}[r.Intn(4)])
	return geo.NewBox(tl, size(), size())
}

func sameFloat(a, b float64) bool { return math.Float64bits(a) == math.Float64bits(b) }

func samePoint(a, b geo.Point) bool { return sameFloat(a.X, b.X) && sameFloat(a.Y, b.Y) }

// TestOutlinesMatchLegacy checks that the generic outlines draw exactly what
// the previous path functions drew: the same SVG path data, the same typed
// commands (float bits) and the same perimeter pieces.
func TestOutlinesMatchLegacy(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
		box := randomOutlineBox(r)
		for _, o := range outlinePaths {
			got, want := o.path(box), o.legacyPath(box)
			what := fmt.Sprintf("%s box %v %vx%v", o.shapeType, *box.TopLeft, box.Width, box.Height)
			if got.PathData() != want.PathData() {
				t.Fatalf("%s: path data %q, want %q", what, got.PathData(), want.PathData())
			}
			gc, wc := got.PathCommands(), want.PathCommands()
			if len(gc) != len(wc) {
				t.Fatalf("%s: %d commands, want %d", what, len(gc), len(wc))
			}
			for j := range wc {
				if gc[j].Kind != wc[j].Kind || !samePoint(gc[j].Control1, wc[j].Control1) ||
					!samePoint(gc[j].Control2, wc[j].Control2) || !samePoint(gc[j].End, wc[j].End) {
					t.Fatalf("%s: command %d %+v, want %+v", what, j, gc[j], wc[j])
				}
			}
			if len(got.Path) != len(want.Path) {
				t.Fatalf("%s: %d perimeter pieces, want %d", what, len(got.Path), len(want.Path))
			}
			for j := range want.Path {
				if fmt.Sprintf("%T", got.Path[j]) != fmt.Sprintf("%T", want.Path[j]) {
					t.Fatalf("%s: piece %d is %T, want %T", what, j, got.Path[j], want.Path[j])
				}
				if ws, ok := want.Path[j].(*geo.Segment); ok {
					gs := got.Path[j].(*geo.Segment)
					if !samePoint(*gs.Start, *ws.Start) || !samePoint(*gs.End, *ws.End) {
						t.Fatalf("%s: piece %d %v -> %v, want %v -> %v", what, j, *gs.Start, *gs.End, *ws.Start, *ws.End)
					}
				}
			}
		}
	}
}

// randomTracePoints returns a point on the border of box and a previous point
// of a route ending there: outside, inside, or aligned with it on one axis.
func randomTracePoints(r *rand.Rand, box *geo.Box) (rect, prev *geo.Point) {
	tl := box.TopLeft
	along := func(length float64) float64 {
		switch r.Intn(4) {
		case 0:
			return 0
		case 1:
			return length
		case 2:
			return math.Round(r.Float64() * length)
		default:
			return r.Float64() * length
		}
	}
	switch r.Intn(4) {
	case 0:
		rect = geo.NewPoint(tl.X+along(box.Width), tl.Y)
	case 1:
		rect = geo.NewPoint(tl.X+along(box.Width), tl.Y+box.Height)
	case 2:
		rect = geo.NewPoint(tl.X, tl.Y+along(box.Height))
	default:
		rect = geo.NewPoint(tl.X+box.Width, tl.Y+along(box.Height))
	}
	switch r.Intn(5) {
	case 0:
		prev = geo.NewPoint(rect.X, rect.Y+(r.Float64()-0.5)*600)
	case 1:
		prev = geo.NewPoint(rect.X+(r.Float64()-0.5)*600, rect.Y)
	case 2:
		prev = geo.NewPoint(tl.X+r.Float64()*box.Width, tl.Y+r.Float64()*box.Height)
	case 3:
		prev = box.Center()
	default:
		prev = geo.NewPoint(rect.X+math.Round((r.Float64()-0.5)*800), rect.Y+math.Round((r.Float64()-0.5)*800))
	}
	return rect, prev
}

// TestTraceToShapeBorderMatchesLegacy compares TraceToShapeBorder with its
// previous search over the previous perimeters, for every shape type: the
// returned point and the in-place truncation of the border point.
func TestTraceToShapeBorderMatchesLegacy(t *testing.T) {
	t.Parallel()
	types := []string{
		SQUARE_TYPE, REAL_SQUARE_TYPE, PARALLELOGRAM_TYPE, DOCUMENT_TYPE, CYLINDER_TYPE, QUEUE_TYPE,
		PAGE_TYPE, PACKAGE_TYPE, STEP_TYPE, CALLOUT_TYPE, STORED_DATA_TYPE, PERSON_TYPE, C4_PERSON_TYPE,
		DIAMOND_TYPE, OVAL_TYPE, CIRCLE_TYPE, HEXAGON_TYPE, CLOUD_TYPE, TABLE_TYPE, CLASS_TYPE, TEXT_TYPE,
		CODE_TYPE, IMAGE_TYPE, "",
	}
	r := rand.New(rand.NewSource(2))
	moved := 0
	for i := 0; i < 3000; i++ {
		box := randomOutlineBox(r)
		for _, shapeType := range types {
			s := NewShape(shapeType, box)
			rect, prev := randomTracePoints(r, box)
			rect2, prev2 := rect.Copy(), prev.Copy()
			want := legacyTraceToShapeBorder(s, legacyPerimeter(s), rect2, prev2)
			got := TraceToShapeBorder(s, rect, prev)
			what := fmt.Sprintf("%q box %v %vx%v rect %v prev %v", shapeType, *box.TopLeft, box.Width, box.Height, *rect2, *prev2)
			if !samePoint(*got, *want) || !samePoint(*rect, *rect2) || !samePoint(*prev, *prev2) {
				t.Fatalf("%s: got %v (rect %v), want %v (rect %v)", what, *got, *rect, *want, *rect2)
			}
			if (got == rect) != (want == rect2) {
				t.Fatalf("%s: result identity differs", what)
			}
			if *got != *rect {
				moved++
			}
		}
	}
	if moved < 10000 {
		t.Fatalf("only %d traces moved off the box border", moved)
	}
}
