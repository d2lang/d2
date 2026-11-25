package d2compiler_test

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
)

func TestGridSpreadKeepsEarliestChildPosition(t *testing.T) {
	const source = `vars: {first: {a; b}; second: {b; c}}
g: {
  grid-rows: 1
  start
  ...${first}
  middle
  ...${second}
  end
}
out
g.c -> out
g.b -> out
g.a -> out
`
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, child := range g.Root.Children["g"].ChildrenArray {
		got = append(got, child.ID)
	}
	if want := []string{"start", "a", "b", "middle", "c", "end"}; !slices.Equal(got, want) {
		t.Fatalf("grid children = %v, want %v", got, want)
	}
}

func TestNestedGridSpreadOrder(t *testing.T) {
	const vars = `vars: {
  first: {g: {grid-rows: 2; a; "grid-rows"; b}}
  second: {g: {b; c}}
}
`
	const links = `g.c -> out
g.b -> out
g.a -> out`
	for _, tc := range []struct {
		name, source string
		cells        []string
		rowsFirst    bool
		step         bool
	}{
		{
			"grid supplied by spread",
			"...${first}\ng.grid-columns: 2\n" + links,
			[]string{"a", "grid-rows", "b", "c"}, true, false,
		},
		{
			"overlapping templates and explicit cells",
			"g.grid-columns: 2\ng.before\n...${first}\ng.middle\n...${second}\ng.after\n" + links,
			[]string{"before", "a", "grid-rows", "b", "middle", "c", "after"}, false, false,
		},
		{
			"nested spread after outer spread",
			"g.before\n...${first}\ng: {...${second.g}}\ng.grid-columns: 2\n" + links,
			[]string{"before", "a", "grid-rows", "b", "c"}, true, false,
		},
		{
			"cached import after spread",
			"unused: @cells\ng.before\n...${first}\n...@cells\ng.after\n" + links,
			[]string{"before", "a", "grid-rows", "b", "x", "c", "after"}, true, false,
		},
		{
			"inherited step",
			"g.before\nsteps: {update: {\n...${first}\ng.after\ng.grid-columns: 2\n" + links + "\n}}",
			[]string{"before", "a", "grid-rows", "b", "after", "c"}, true, true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := &d2compiler.CompileOptions{FS: fstest.MapFS{"cells.d2": {Data: []byte("g: {b; x; c; a; grid-columns: 2}")}}}
			g, _, err := d2compiler.Compile("main.d2", strings.NewReader(vars+tc.source), opts)
			if err != nil {
				t.Fatal(err)
			}
			if tc.step {
				g = g.Steps[0]
			}
			grid := g.Root.Children["g"]
			var ids []string
			for _, child := range grid.ChildrenArray {
				ids = append(ids, child.ID)
			}
			if !slices.Equal(ids, tc.cells) {
				t.Fatalf("cells %v, want %v", ids, tc.cells)
			}
			if grid.GridRowsFirst == nil || *grid.GridRowsFirst != tc.rowsFirst {
				t.Fatalf("lost grid axis order, want rows first %t", tc.rowsFirst)
			}
		})
	}
}

func TestGridImportSpreadsCallerVariable(t *testing.T) {
	const source = `vars: {cells: {g: {grid-rows: 1; a; b}}}
copy: @library
...@library`
	const library = `g.early
...${cells}
g.after
g.b -> out`
	opts := &d2compiler.CompileOptions{FS: fstest.MapFS{"library.d2": {Data: []byte(library)}}}
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader(source), opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, grid := range []*d2graph.Object{g.Root.Children["g"], g.Root.Children["copy"].Children["g"]} {
		var got []string
		for _, child := range grid.ChildrenArray {
			got = append(got, child.ID)
		}
		if want := []string{"early", "a", "b", "after"}; !slices.Equal(got, want) {
			t.Fatalf("%s: got %v, want %v", grid.AbsID(), got, want)
		}
	}
}

func TestNearGridContainerAndCells(t *testing.T) {
	const base = "grid: {grid-rows: 1; inner: {grid-rows: 1; a}}\nnote.near: "
	for _, target := range []string{"grid", "grid.inner", "grid.inner.a"} {
		_, _, err := d2compiler.Compile("main.d2", strings.NewReader(base+target), nil)
		if target == "grid" {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "like grid cells") {
			t.Fatalf("near %s: expected grid cell diagnostic, got %v", target, err)
		}
	}
}
