package d2compiler_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
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
