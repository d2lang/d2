package d2isometricimg

import (
	"context"
	"testing"
)

func svgNewVisibleFragmentSet(points []svgPoint, budget *svgVisibilityBudget, maxFragments, maxVertices int) (*svgVisibleFragmentSet, error) {
	set := &svgVisibleFragmentSet{}
	box := svgPolygonBox(points)
	set.reset(box, budget, &svgPointPool{}, maxFragments, maxVertices)
	return set, set.add(svgVisibleFragment{points: points, box: box})
}

func TestSVGFragmentIndexBoundsLiveReferences(t *testing.T) {
	limits := svgDefaultVisibilityLimits
	limits.gridReferences = 2
	budget := &svgVisibilityBudget{ctx: context.Background(), limits: limits}
	s, err := svgNewVisibleFragmentSet(svgVisibilityRectangle(0, 0, 100, 100, 0, 0, 0), budget, 100, 400)
	if err != nil {
		t.Fatal(err)
	}
	small := svgVisibleFragment{points: svgVisibilityRectangle(1, 1, 2, 2, 0, 0, 0)}
	small.box = svgPolygonBox(small.points)
	if err := s.add(small); err != nil {
		t.Fatal(err)
	}
	if err := s.add(small); err == nil {
		t.Fatal("fragment index exceeded its reference budget")
	}
	if s.references != 2 || s.live != 2 || len(s.fragments) != 2 || s.vertices != 8 {
		t.Fatal("rejected fragment changed the active index")
	}
	if err := s.remove(int32(len(s.fragments))); err != nil {
		t.Fatal(err)
	}
	if err := s.add(small); err != nil || s.references != 2 || s.live != 2 {
		t.Fatalf("removed fragments did not release their references: %v", err)
	}
}
