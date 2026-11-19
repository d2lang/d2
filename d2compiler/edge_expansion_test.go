package d2compiler_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d2lang/d2/d2compiler"
)

func TestCompileEdgeExpansionLimit(t *testing.T) {
	t.Parallel()

	_, _, err := d2compiler.Compile(
		"edge-expansion.d2",
		strings.NewReader("a\nb\nc\n* -> *\n"),
		&d2compiler.CompileOptions{MaxEdgeExpansion: 8},
	)
	if err == nil || !strings.Contains(err.Error(), "edge glob expansion exceeds limit of 8 endpoint pairs") {
		t.Fatalf("Compile error = %v, want edge expansion limit", err)
	}
}

func TestCompileEdgeSelectorExpansionLimit(t *testing.T) {
	t.Parallel()

	_, _, err := d2compiler.Compile(
		"edge-selector-expansion.d2",
		strings.NewReader("a\nb\nc\n(* -> *)[*].style.opacity: 0\n"),
		&d2compiler.CompileOptions{MaxEdgeExpansion: 8},
	)
	if err == nil || !strings.Contains(err.Error(), "edge glob expansion exceeds limit of 8 endpoint pairs") {
		t.Fatalf("Compile error = %v, want edge expansion limit", err)
	}
}

func TestCompileEdgeExpansionWorkLimit(t *testing.T) {
	t.Parallel()

	_, _, err := d2compiler.Compile(
		"edge-expansion-work.d2",
		strings.NewReader("(* -> *)[*].style.opacity: 0\na\nb\nc\n"),
		&d2compiler.CompileOptions{
			MaxEdgeExpansion:     9,
			MaxEdgeExpansionWork: 9,
		},
	)
	if err == nil || !strings.Contains(err.Error(), "work limit of 9 endpoint-pair examinations") {
		t.Fatalf("Compile error = %v, want edge expansion work limit", err)
	}
}

func TestCompileEdgeExpansionDoesNotChargeExplicitEdges(t *testing.T) {
	t.Parallel()

	_, _, err := d2compiler.Compile(
		"explicit-edges.d2",
		strings.NewReader("a -> b\na -> b\na -> b\n"),
		&d2compiler.CompileOptions{MaxEdgeExpansion: 1},
	)
	if err != nil {
		t.Fatalf("Compile explicit edges: %v", err)
	}
}

func TestRecursiveEdgeStylesOnSQLTables(t *testing.T) {
	t.Parallel()

	const styles = `
(*** -- ***)[*].style.bold: false
(*** -> ***)[*].style.italic: false
(*** <- ***)[*].style.bold: false
(*** <-> ***)[*].style.italic: false
`
	const tables = `
users: {shape: sql_table; id; name; email}
orders: {shape: sql_table; id; user_id; total}
`
	const relationship = "orders.user_id -> users.id\n"
	for _, tc := range []struct {
		name  string
		graph string
		edges int
	}{
		{"no edges", styles + tables, 0},
		{"later edge", styles + tables + relationship, 1},
		{"existing edge", tables + relationship + styles, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, _, err := d2compiler.Compile("tables.d2", strings.NewReader(tc.graph),
				&d2compiler.CompileOptions{MaxEdgeExpansion: 4})
			if err != nil {
				t.Fatal(err)
			}
			if len(g.Objects) != 2 || len(g.Edges) != tc.edges {
				t.Fatalf("got %d objects and %d edges, want 2 and %d", len(g.Objects), len(g.Edges), tc.edges)
			}
			for _, table := range g.Objects {
				if table.SQLTable == nil || len(table.SQLTable.Columns) != 3 {
					t.Fatalf("missing columns on table %q", table.ID)
				}
			}
			for _, edge := range g.Edges {
				if edge.SrcTableColumnIndex == nil || *edge.SrcTableColumnIndex != 1 ||
					edge.DstTableColumnIndex == nil || *edge.DstTableColumnIndex != 0 {
					t.Fatal("relationship lost its column endpoints")
				}
				if edge.Style.Italic == nil || edge.Style.Italic.Value != "false" ||
					edge.Style.Bold == nil || edge.Style.Bold.Value != "false" {
					t.Fatal("forward and reversed selectors must both style the relationship")
				}
			}
		})
	}
}

func TestRecursiveEdgeStylesOnImportedConnections(t *testing.T) {
	t.Parallel()

	const source = `
first_group: @library.group
removed: @library.group
second_group: { ...@library }
removed: null
(*** -> ***)[*].style.bold: true
`
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader(source), &d2compiler.CompileOptions{
		FS: fstest.MapFS{"library.d2": {Data: []byte("group: { a -> b }")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Edges) != 2 {
		t.Fatalf("got %d edges, want 2", len(g.Edges))
	}
	for _, edge := range g.Edges {
		if edge.Style.Bold == nil || edge.Style.Bold.Value != "true" {
			t.Fatalf("missing style on imported edge %s", edge.AbsID())
		}
	}
}

func TestRecursiveEdgeSelectorParallelWork(t *testing.T) {
	t.Parallel()

	const undirected = "(*** -- ***)[*].style.bold: true\n"
	const directed = "(*** -> ***)[*].style.bold: true\n"
	for _, tc := range []struct {
		name   string
		source string
		work   int64
		edges  int
		bold   bool
	}{
		{"leading selector with default limit", undirected + strings.Repeat("a -> b\n", 365), 0, 365, false},
		{"trailing selector with four endpoint pairs", strings.Repeat("a -> b\n", 8) + undirected, 4, 8, false},
		{"styled endpoints", "a.style: {fill: red; stroke: black}\nb.style: {fill: blue; stroke: black}\n" + strings.Repeat("a -> b\n", 8) + undirected, 4, 8, false},
		{"matching parallel edges in a container", "box: {\n" + strings.Repeat("a -> b\n", 16) + "}\n" + directed, 32, 16, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, _, err := d2compiler.Compile("parallel.d2", strings.NewReader(tc.source), &d2compiler.CompileOptions{MaxEdgeExpansionWork: tc.work})
			if err != nil {
				t.Fatal(err)
			}
			if len(g.Edges) != tc.edges {
				t.Fatalf("got %d edges, want %d", len(g.Edges), tc.edges)
			}
			for _, edge := range g.Edges {
				bold := edge.Style.Bold != nil && edge.Style.Bold.Value == "true"
				if bold != tc.bold {
					t.Fatalf("edge %s bold = %v, want %v", edge.AbsID(), bold, tc.bold)
				}
			}
		})
	}
}
