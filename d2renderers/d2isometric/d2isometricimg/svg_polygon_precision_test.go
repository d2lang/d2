package d2isometricimg

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2renderers/d2svgimport"
)

func TestSVGPolygonPathOmitsSubprecisionCollinearSplits(t *testing.T) {
	// The same pattern-face clip on ARM64 and amd64 placed one redundant
	// intersection at different positions along an otherwise identical edge.
	// Both rounded positions are less than one serialized coordinate unit
	// from that edge. They must not change its SVG or its silhouette.
	polygon := []svgPoint{
		{316.141043, 347.158271, 0}, {318.065866, 344.688423, 0},
		{318.661261, 344.688423, 0}, {318.661261, 347.986151, 0},
		{318.032125, 348.793430, 0}, {317.690070, 348.793430, 0},
		{316.141043, 347.481102, 0},
	}
	want := svgPolygonPath([][]svgPoint{polygon})
	for _, split := range []svgPoint{
		{317.423128, 348.567277, 0},
		{317.423670, 348.567737, 0},
	} {
		points := append([]svgPoint(nil), polygon[:6]...)
		points = append(points, split, polygon[6])
		original := append([]svgPoint(nil), points...)
		for repeat := 0; repeat < 2; repeat++ {
			if got := svgPolygonPath([][]svgPoint{points}); got != want {
				t.Fatalf("subprecision clip split changed its serialized silhouette:\n%s\nwant %s", got, want)
			}
		}
		if !reflect.DeepEqual(points, original) {
			t.Fatal("SVG serialization mutated source visibility geometry")
		}
		// The redundant vertex can also straddle the cyclic loop boundary.
		for offset := range points {
			rotated := append(append([]svgPoint(nil), points[offset:]...), points[:offset]...)
			if got := svgPolygonPath([][]svgPoint{rotated}); got != want {
				t.Fatalf("rotating a clip split changed its serialized silhouette: %s", got)
			}
		}
	}
}

func TestSVGPolygonPathKeepsRealCornersAndTriangles(t *testing.T) {
	for _, polygon := range [][]svgPoint{
		{{0, 0, 0}, {2, .00001, 0}, {4, 0, 0}, {4, 4, 0}, {0, 4, 0}},
		{{0, 0, 0}, {2, .000001, 0}, {4, 0, 0}},
	} {
		path := svgPolygonPath([][]svgPoint{polygon})
		if got := strings.Count(path, "L") + strings.Count(path, "M"); got != len(polygon) {
			t.Fatalf("serialization erased a real corner or triangle: %s", path)
		}
	}
}

func TestSVGFillPathPreservesHolesOverlapsAndPointContacts(t *testing.T) {
	var cells [][]svgPoint
	square := func(x, y float64) []svgPoint {
		return []svgPoint{{x, y, 0}, {x + 1, y, 0}, {x + 1, y + 1, 0}, {x, y + 1, 0}}
	}
	for y := range 3 {
		for x := range 3 {
			if x != 1 || y != 1 {
				cells = append(cells, square(float64(x), float64(y)))
			}
		}
	}
	// A second layer in the first cell and a square touching one corner.
	cells = append(cells, square(0, 0), square(3, 3))
	path := svgFillPath(cells)
	if len(path) >= len(svgPolygonPath(cells)) || path != svgFillPath(cells) {
		t.Fatalf("expected a smaller deterministic path: %s", path)
	}
	cmds, err := d2svgimport.ParsePath(context.Background(), "fill", path, d2svgimport.PathLimits{MaxBytes: 4096, MaxCommands: 128})
	if err != nil {
		t.Fatal(err)
	}
	// Count oriented crossings of a ray to the right of each sample center.
	for _, sample := range []struct {
		x, y float64
		want int
	}{
		{.5, .5, 2}, {1.5, .5, 1}, {.5, 1.5, 1}, {1.5, 1.5, 0}, {2.5, 2.5, 1},
		{3.5, 3.5, 1}, {3.5, 2.5, 0}, {2.5, 3.5, 0}, {-.5, .5, 0},
	} {
		var start, prev d2scene.Point
		winding := 0
		for _, cmd := range cmds {
			end := cmd.P1
			switch cmd.Kind {
			case d2scene.MoveCommand:
				start, prev = end, end
				continue
			case d2scene.CloseCommand:
				end = start
			case d2scene.LineCommand:
			default:
				t.Fatalf("unexpected command %v", cmd.Kind)
			}
			if prev.X == end.X && end.X > sample.x {
				if prev.Y <= sample.y && end.Y > sample.y {
					winding++
				} else if end.Y <= sample.y && prev.Y > sample.y {
					winding--
				}
			}
			prev = end
		}
		if winding != sample.want {
			t.Errorf("winding at (%g,%g) = %d, want %d", sample.x, sample.y, winding, sample.want)
		}
	}
}

func TestSVGFillPathCancelsEdgesAcrossSignedZeros(t *testing.T) {
	cell := func(x0, x1, y float64) []svgPoint {
		return []svgPoint{{x: x0, y: y}, {x: x1, y: y}, {x: x1, y: y + 1}, {x: x0, y: y + 1}}
	}
	// Rounding -3e-7 spells the shared boundary -0 on the left and +0 on the
	// right. Both are the same vertex, so the cells still merge into one loop.
	var signed, plain [][]svgPoint
	for y := 0.; y < 4; y++ {
		signed = append(signed, cell(-1, -3e-7, y), cell(0, 1, y))
		plain = append(plain, cell(-1, 0, y), cell(0, 1, y))
	}
	if got, want := svgFillPath(signed), svgFillPath(plain); got != want || strings.Count(got, "M") != 1 {
		t.Fatalf("the two zeros were not one vertex: %s, want %s", got, want)
	}
}
