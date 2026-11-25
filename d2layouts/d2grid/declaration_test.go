package d2grid

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
)

func TestGridDeclarationOrder(t *testing.T) {
	for _, tc := range []struct {
		name, source, imported string
		rowsFirst              bool
	}{
		{"rows redefined", "grid-rows: 1\ngrid-columns: 2\ngrid-rows: 2", "", true},
		{"columns redefined", "grid-columns: 1\ngrid-rows: 2\ngrid-columns: 2", "", false},
		{"removed and reintroduced", "grid-rows: 2\ngrid-columns: 2\ngrid-rows: null\ngrid-rows: 2", "", false},
		{"import before local", "...@axis\ngrid-columns: 2", "# Changing this comment cannot move the declaration.\ngrid-rows: 2", true},
		{"local before import", "grid-columns: 2\n...@axis", "grid-rows: 2", false},
		{"spread before local", "vars: {axis: {grid-rows: 2}}\n...${axis}\ngrid-columns: 2\ngrid-rows: 2", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := &d2compiler.CompileOptions{FS: fstest.MapFS{"axis.d2": {Data: []byte(tc.imported)}}}
			g, _, err := d2compiler.Compile("main.d2", strings.NewReader(tc.source), opts)
			if err != nil {
				t.Fatal(err)
			}
			data, err := d2graph.SerializeGraph(g)
			if err != nil {
				t.Fatal(err)
			}
			var restored d2graph.Graph
			if err := d2graph.DeserializeGraph(data, &restored); err != nil {
				t.Fatal(err)
			}
			for _, root := range []*d2graph.Object{g.Root, restored.Root} {
				gd, err := newGridDiagram(root)
				if err != nil {
					t.Fatal(err)
				}
				if gd.rowDirected != tc.rowsFirst || gd.rows != 2 || gd.columns != 2 {
					t.Fatalf("grid = %d×%d, rows first %t; want 2×2, rows first %t", gd.rows, gd.columns, gd.rowDirected, tc.rowsFirst)
				}
			}
		})
	}
}

func TestGridDeclarationOrderInOlderGraphs(t *testing.T) {
	root := dimensionedGridRoot("2", "2", 4)
	root.GridRows.MapKey, root.GridColumns.MapKey = root.GridColumns.MapKey, root.GridRows.MapKey
	gd, err := newGridDiagram(root)
	if err != nil {
		t.Fatal(err)
	}
	if gd.rowDirected {
		t.Fatal("lost column-first order from the older graph's source keys")
	}
	// Earlier serialized graphs omit both source keys and the resolved order.
	root.GridRows.MapKey, root.GridColumns.MapKey = nil, nil
	gd, err = newGridDiagram(root)
	if err != nil {
		t.Fatal(err)
	}
	if !gd.rowDirected {
		t.Fatal("legacy graph without ordering metadata should fill by row")
	}
}
