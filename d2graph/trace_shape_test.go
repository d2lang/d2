package d2graph

import (
	"fmt"
	"math"
	"math/rand"
	"net/url"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

// TestTracedShapeTypeMatchesShape checks tracedShapeType against what
// shape.TraceToShapeBorder does with each shape type: return the rectangle
// point unchanged for type "" and rectangular shapes, trace otherwise.
func TestTracedShapeTypeMatchesShape(t *testing.T) {
	types := []string{
		"", "unknown", "Square", "square", "SQUARE", "Oval", "oval",
		shape.SQUARE_TYPE, shape.REAL_SQUARE_TYPE, shape.PARALLELOGRAM_TYPE, shape.DOCUMENT_TYPE,
		shape.CYLINDER_TYPE, shape.QUEUE_TYPE, shape.PAGE_TYPE, shape.PACKAGE_TYPE, shape.STEP_TYPE,
		shape.CALLOUT_TYPE, shape.STORED_DATA_TYPE, shape.PERSON_TYPE, shape.C4_PERSON_TYPE,
		shape.DIAMOND_TYPE, shape.OVAL_TYPE, shape.CIRCLE_TYPE, shape.HEXAGON_TYPE, shape.CLOUD_TYPE,
		shape.TABLE_TYPE, shape.CLASS_TYPE, shape.TEXT_TYPE, shape.CODE_TYPE, shape.IMAGE_TYPE,
	}
	for dsl, typ := range d2target.DSL_SHAPE_TO_SHAPE_TYPE {
		types = append(types, typ, d2target.DSL_SHAPE_TO_SHAPE_TYPE[strings.ToLower(strings.ToUpper(dsl))])
	}
	for _, typ := range types {
		s := shape.NewShape(typ, geo.NewBox(geo.NewPoint(1, 2), 30, 40))
		want := !(s.Is("") || s.IsRectangular())
		if got := tracedShapeType(typ); got != want {
			t.Errorf("tracedShapeType(%q) = %v, want %v", typ, got, want)
		}
	}
}

// legacyTraceToShape is Edge.TraceToShape as it was before the endpoint
// shapes were made lazily.
func legacyTraceToShape(edge *Edge, points []*geo.Point, startIndex, endIndex int) (newStart, newEnd int) {
	srcShape := edge.Src.ToShape()
	dstShape := edge.Dst.ToShape()

	startingSegment := geo.Segment{Start: points[startIndex+1], End: points[startIndex]}
	var overlapsOutsideLabel, overlapsOutsideIcon bool
	if edge.Src.HasLabel() {
		labelPosition := label.FromString(*edge.Src.LabelPosition)
		if labelPosition.IsOutside() {
			labelWidth := float64(edge.Src.LabelDimensions.Width)
			labelHeight := float64(edge.Src.LabelDimensions.Height)
			labelTL := labelPosition.GetPointOnBox(edge.Src.Box, label.PADDING, labelWidth, labelHeight)

			labelBox := geo.NewBox(labelTL, labelWidth, labelHeight)
			labelBox.TopLeft.X -= label.PADDING
			labelBox.Width += 2 * label.PADDING

			for labelBox.Contains(startingSegment.End) && startIndex+1 > endIndex {
				startingSegment.Start = startingSegment.End
				startingSegment.End = points[startIndex+2]
				startIndex++
			}
			if intersections := labelBox.Intersections(startingSegment); len(intersections) > 0 {
				overlapsOutsideLabel = true
				p := intersections[0]
				if len(intersections) > 1 {
					p = findOuterIntersection(labelPosition, intersections)
				}
				points[startIndex] = p
				startingSegment.End = p
				if startIndex+1 < endIndex && startingSegment.Length() < MIN_SEGMENT_LEN {
					points[startIndex+1] = points[startIndex]
					startIndex++
				}
			}
		}
	}
	if !overlapsOutsideLabel && edge.Src.HasIcon() {
		iconPosition := label.FromString(*edge.Src.IconPosition)
		if iconPosition.IsOutside() {
			iconWidth := float64(d2target.MAX_ICON_SIZE)
			iconHeight := float64(d2target.MAX_ICON_SIZE)
			iconTL := iconPosition.GetPointOnBox(edge.Src.Box, label.PADDING, iconWidth, iconHeight)

			iconBox := geo.NewBox(iconTL, iconWidth, iconHeight)
			for iconBox.Contains(startingSegment.End) && startIndex+1 > endIndex {
				startingSegment.Start = startingSegment.End
				startingSegment.End = points[startIndex+2]
				startIndex++
			}
			if intersections := iconBox.Intersections(startingSegment); len(intersections) > 0 {
				overlapsOutsideIcon = true
				p := intersections[0]
				if len(intersections) > 1 {
					p = findOuterIntersection(iconPosition, intersections)
				}
				points[startIndex] = p
				startingSegment.End = p
				if startIndex+1 < endIndex && startingSegment.Length() < MIN_SEGMENT_LEN {
					points[startIndex+1] = points[startIndex]
					startIndex++
				}
			}
		}
	}
	if !overlapsOutsideLabel && !overlapsOutsideIcon {
		if intersections := edge.Src.Intersections(startingSegment); len(intersections) > 0 {
			points[startIndex] = intersections[0]
			startingSegment.End = intersections[0]
			if startIndex+1 < endIndex && startingSegment.Length() < MIN_SEGMENT_LEN {
				points[startIndex+1] = points[startIndex]
				startIndex++
			}
		}
		points[startIndex] = shape.TraceToShapeBorder(srcShape, points[startIndex], points[startIndex+1])
	}
	endingSegment := geo.Segment{Start: points[endIndex-1], End: points[endIndex]}
	overlapsOutsideLabel = false
	overlapsOutsideIcon = false
	if edge.Dst.HasLabel() {
		labelPosition := label.FromString(*edge.Dst.LabelPosition)
		if labelPosition.IsOutside() {
			labelWidth := float64(edge.Dst.LabelDimensions.Width)
			labelHeight := float64(edge.Dst.LabelDimensions.Height)
			labelTL := labelPosition.GetPointOnBox(edge.Dst.Box, label.PADDING, labelWidth, labelHeight)

			labelBox := geo.NewBox(labelTL, labelWidth, labelHeight)
			labelBox.TopLeft.X -= label.PADDING
			labelBox.Width += 2 * label.PADDING
			for labelBox.Contains(endingSegment.Start) && endIndex-1 > startIndex {
				endingSegment.End = endingSegment.Start
				endingSegment.Start = points[endIndex-2]
				endIndex--
			}
			if intersections := labelBox.Intersections(endingSegment); len(intersections) > 0 {
				overlapsOutsideLabel = true
				p := intersections[0]
				if len(intersections) > 1 {
					p = findOuterIntersection(labelPosition, intersections)
				}
				points[endIndex] = p
				endingSegment.End = p
				if endIndex-1 > startIndex && endingSegment.Length() < MIN_SEGMENT_LEN {
					points[endIndex-1] = points[endIndex]
					endIndex--
				}
			}
		}
	}
	if !overlapsOutsideLabel && edge.Dst.HasIcon() {
		iconPosition := label.FromString(*edge.Dst.IconPosition)
		if iconPosition.IsOutside() {
			iconSize := d2target.GetIconSize(edge.Dst.Box, iconPosition.String())
			iconWidth := float64(iconSize)
			iconHeight := float64(iconSize)
			labelTL := iconPosition.GetPointOnBox(edge.Dst.Box, label.PADDING, iconWidth, iconHeight)

			iconBox := geo.NewBox(labelTL, iconWidth, iconHeight)
			for iconBox.Contains(endingSegment.Start) && endIndex-1 > startIndex {
				endingSegment.End = endingSegment.Start
				endingSegment.Start = points[endIndex-2]
				endIndex--
			}
			if intersections := iconBox.Intersections(endingSegment); len(intersections) > 0 {
				overlapsOutsideIcon = true
				p := intersections[0]
				if len(intersections) > 1 {
					p = findOuterIntersection(iconPosition, intersections)
				}
				points[endIndex] = p
				endingSegment.End = p
				if endIndex-1 > startIndex && endingSegment.Length() < MIN_SEGMENT_LEN {
					points[endIndex-1] = points[endIndex]
					endIndex--
				}
			}
		}
	}
	if !overlapsOutsideLabel && !overlapsOutsideIcon {
		if intersections := edge.Dst.Intersections(endingSegment); len(intersections) > 0 {
			points[endIndex] = intersections[0]
			endingSegment.End = intersections[0]
			if endIndex-1 > startIndex && endingSegment.Length() < MIN_SEGMENT_LEN {
				points[endIndex-1] = points[endIndex]
				endIndex--
			}
		}
		points[endIndex] = shape.TraceToShapeBorder(dstShape, points[endIndex], points[endIndex-1])
	}
	return startIndex, endIndex
}

// TestTraceToShapeMatchesLegacy runs Edge.TraceToShape and its previous
// version on the same random edges (every DSL shape, outside and inside
// labels and icons, routes entering the shapes at any angle) and requires the
// same indices, the same points (float bits) and the same in-place changes.
func TestTraceToShapeMatchesLegacy(t *testing.T) {
	t.Parallel()
	shapes := []string{"", "rectangle", "Rectangle", "square", "page", "parallelogram", "document", "cylinder",
		"queue", "package", "step", "callout", "stored_data", "person", "c4-person", "diamond", "oval", "circle",
		"hexagon", "cloud", "text", "code", "class", "sql_table", "image", "CLOUD", "Oval", "nope"}
	positions := []string{"", "OUTSIDE_TOP_LEFT", "OUTSIDE_TOP_CENTER", "OUTSIDE_TOP_RIGHT", "OUTSIDE_LEFT_TOP",
		"OUTSIDE_LEFT_MIDDLE", "OUTSIDE_LEFT_BOTTOM", "OUTSIDE_RIGHT_TOP", "OUTSIDE_RIGHT_MIDDLE",
		"OUTSIDE_RIGHT_BOTTOM", "OUTSIDE_BOTTOM_LEFT", "OUTSIDE_BOTTOM_CENTER", "OUTSIDE_BOTTOM_RIGHT",
		"INSIDE_MIDDLE_CENTER", "INSIDE_TOP_LEFT", "BORDER_TOP_CENTER"}
	icon, _ := url.Parse("https://example.com/icon.svg")
	r := rand.New(rand.NewSource(11))
	coord := func(scale float64) float64 {
		if r.Intn(2) == 0 {
			return math.Round(r.Float64() * scale)
		}
		return r.Float64() * scale
	}
	newObject := func() *Object {
		obj := &Object{}
		obj.Box = geo.NewBox(geo.NewPoint(coord(800), coord(800)), 1+coord(200), 1+coord(150))
		obj.Shape.Value = shapes[r.Intn(len(shapes))]
		if r.Intn(2) == 0 {
			obj.Label.Value = "label"
			pos := positions[r.Intn(len(positions))]
			obj.LabelPosition = &pos
			obj.LabelDimensions = d2target.TextDimensions{Width: r.Intn(120), Height: r.Intn(40)}
		}
		if r.Intn(3) == 0 {
			obj.Icon = icon
			pos := positions[r.Intn(len(positions))]
			obj.IconPosition = &pos
		}
		if r.Intn(4) == 0 {
			ratio := 0.2 + r.Float64()*3
			obj.ContentAspectRatio = &ratio
		}
		return obj
	}
	cloneObject := func(o *Object) *Object {
		c := *o
		c.Box = geo.NewBox(geo.NewPoint(o.TopLeft.X, o.TopLeft.Y), o.Width, o.Height)
		return &c
	}
	moved := 0
	for i := 0; i < 20000; i++ {
		src, dst := newObject(), newObject()
		if r.Intn(10) == 0 {
			dst = src
		}
		n := 2 + r.Intn(6)
		points := make([]*geo.Point, n)
		sc, dc := src.Center(), dst.Center()
		for k := range points {
			f := float64(k) / float64(n-1)
			points[k] = geo.NewPoint(sc.X*(1-f)+dc.X*f+(coord(200)-100)*float64(r.Intn(2)), sc.Y*(1-f)+dc.Y*f+(coord(200)-100)*float64(r.Intn(2)))
		}
		points[0], points[n-1] = sc, dc
		if r.Intn(3) == 0 {
			points[0] = geo.NewPoint(src.TopLeft.X+coord(src.Width), src.TopLeft.Y+coord(src.Height))
		}
		lsrc, ldst := cloneObject(src), cloneObject(dst)
		if dst == src {
			ldst = lsrc
		}
		lpoints := make([]*geo.Point, n)
		for k, p := range points {
			lpoints[k] = p.Copy()
		}
		// Shared points stay shared in both copies.
		for k := range points {
			for j := 0; j < k; j++ {
				if points[j] == points[k] {
					lpoints[k] = lpoints[j]
				}
			}
		}
		e, le := &Edge{Src: src, Dst: dst}, &Edge{Src: lsrc, Dst: ldst}
		gs, ge := e.TraceToShape(points, 0, n-1)
		ws, we := legacyTraceToShape(le, lpoints, 0, n-1)
		what := fmt.Sprintf("case %d: %q -> %q", i, src.Shape.Value, dst.Shape.Value)
		if gs != ws || ge != we {
			t.Fatalf("%s: indices %d,%d, want %d,%d", what, gs, ge, ws, we)
		}
		for k := range points {
			if math.Float64bits(points[k].X) != math.Float64bits(lpoints[k].X) || math.Float64bits(points[k].Y) != math.Float64bits(lpoints[k].Y) {
				t.Fatalf("%s: point %d %v, want %v", what, k, *points[k], *lpoints[k])
			}
			for j := 0; j < k; j++ {
				if (points[j] == points[k]) != (lpoints[j] == lpoints[k]) {
					t.Fatalf("%s: aliasing of points %d and %d differs", what, j, k)
				}
			}
		}
		if *points[gs] != *sc || *points[ge] != *dc {
			moved++
		}
	}
	if moved < 5000 {
		t.Fatalf("only %d edges had an endpoint moved", moved)
	}
}
