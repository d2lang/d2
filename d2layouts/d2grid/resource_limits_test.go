package d2grid

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

func dimensionedGridRoot(rows, columns string, objectCount int) *d2graph.Object {
	rowKey := &d2ast.Key{Range: d2ast.Range{Start: d2ast.Position{Line: 1}}}
	columnKey := &d2ast.Key{Range: d2ast.Range{Start: d2ast.Position{Line: 2}}}
	root := &d2graph.Object{
		GridRows:    &d2graph.Scalar{Value: rows, MapKey: rowKey},
		GridColumns: &d2graph.Scalar{Value: columns, MapKey: columnKey},
	}
	root.ChildrenArray = make([]*d2graph.Object, objectCount)
	for i := range root.ChildrenArray {
		root.ChildrenArray[i] = &d2graph.Object{Box: &geo.Box{Width: 100, Height: 50}}
	}
	return root
}

func TestNewGridDiagramRejectsUnsafeCapacity(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		rows    string
		columns string
	}{
		{
			name:    "max_int_panic_regression",
			rows:    strconv.Itoa(math.MaxInt),
			columns: strconv.Itoa(math.MaxInt),
		},
		{
			name:    "twenty_million_allocator_regression",
			rows:    "20000000",
			columns: "20000000",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gd, err := newGridDiagram(dimensionedGridRoot(tc.rows, tc.columns, 1))
			if err == nil || gd != nil {
				t.Fatalf("newGridDiagram(%sx%s) = %#v, %v; want bounded capacity error", tc.rows, tc.columns, gd, err)
			}
			if !strings.Contains(err.Error(), "grid-rows "+tc.rows+" exceeds the maximum of 10000") {
				t.Fatalf("newGridDiagram error = %q, want dimension limit", err)
			}
		})
	}
}

func TestGridCapacityBoundaryAndOccupiedStorage(t *testing.T) {
	t.Parallel()

	gd, err := newGridDiagram(dimensionedGridRoot("1000", "1000", 1))
	if err != nil {
		t.Fatal(err)
	}
	if gd.rows != 1_000 || gd.columns != 1_000 {
		t.Fatalf("grid dimensions = %dx%d, want retained 1000x1000", gd.rows, gd.columns)
	}
	if rows, columns := gd.occupiedDimensions(); rows != 1 || columns != 1 {
		t.Fatalf("occupied dimensions = %dx%d, want 1x1", rows, columns)
	}

	gd.layoutEvenly(nil, nil)
	if want := 100.0 + 999*DEFAULT_GAP; gd.width != want {
		t.Fatalf("grid width = %v, want %v", gd.width, want)
	}
	if want := 50.0 + 999*DEFAULT_GAP; gd.height != want {
		t.Fatalf("grid height = %v, want %v", gd.height, want)
	}

	if capacity, err := d2graph.GridCapacity(1_000, 1_001); err == nil || capacity != 0 {
		t.Fatalf("over-limit capacity = %d, %v; want 0 and an error", capacity, err)
	}
}
