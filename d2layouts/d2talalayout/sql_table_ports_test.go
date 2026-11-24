package d2talalayout

import (
	"context"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestSQLTableRowPortsWithPlainEndpoint(t *testing.T) {
	operations := []struct {
		name string
		run  func(context.Context, *d2graph.Graph, *d2graph.Edge) error
	}{
		{
			name: "Layout",
			run: func(ctx context.Context, graph *d2graph.Graph, _ *d2graph.Edge) error {
				return Layout(ctx, graph, &Options{Seeds: []int64{1}, MaxConcurrency: 1})
			},
		},
		{
			name: "RouteEdges",
			run: func(ctx context.Context, graph *d2graph.Graph, edge *d2graph.Edge) error {
				return RouteEdges(ctx, graph, []*d2graph.Edge{edge})
			},
		},
	}
	connections := []struct {
		name          string
		tableIsSource bool
		reverseArrow  bool
	}{
		{name: "table.row -> plain", tableIsSource: true},
		{name: "plain <- table.row", reverseArrow: true},
		{name: "plain -> table.row"},
		{name: "table.row <- plain", tableIsSource: true, reverseArrow: true},
	}

	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			for _, connection := range connections {
				t.Run(connection.name, func(t *testing.T) {
					graph, edge := newD2TransactionGraph(false)
					table := graph.Objects[0]
					table.Shape.Value = d2target.ShapeSQLTable
					table.SQLTable = &d2target.SQLTable{Columns: make([]d2target.SQLColumn, 3)}
					table.Height = 160
					for _, object := range graph.Objects {
						object.Top = &d2graph.Scalar{Value: strconv.Itoa(int(object.TopLeft.Y))}
						object.Left = &d2graph.Scalar{Value: strconv.Itoa(int(object.TopLeft.X))}
					}
					if connection.tableIsSource {
						edge.SrcTableColumnIndex = new(2)
					} else {
						edge.Src, edge.Dst = edge.Dst, edge.Src
						slices.Reverse(edge.Route)
						edge.DstTableColumnIndex = new(2)
					}
					edge.SrcArrow = connection.reverseArrow
					edge.DstArrow = !connection.reverseArrow

					if err := operation.run(t.Context(), graph, edge); err != nil {
						t.Fatal(err)
					}
					if len(edge.Route) < 2 {
						t.Fatalf("edge has %d route points", len(edge.Route))
					}
					endpoint := edge.Route[0]
					if !connection.tableIsSource {
						endpoint = edge.Route[len(edge.Route)-1]
					}
					// The table has a header and three equally sized rows. The
					// requested third row is centered 3.5 row heights below its top.
					want := geo.Point{
						X: table.TopLeft.X + table.Width,
						Y: table.TopLeft.Y + math.Round(table.Height/4*3.5),
					}
					if *endpoint != want {
						t.Errorf("table endpoint = %v, want third-row port %v", *endpoint, want)
					}
				})
			}
		})
	}
}

func TestSQLRowBindingsSurviveRouting(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	const table = "table: {shape: sql_table; id: int; ref: int; status: text}\n"
	for _, tc := range []struct {
		name, links string
		// Row indices in authored endpoint order. -1 denotes a table-level or plain end.
		want [][2]int
	}{
		{"reciprocal", "table.status -> client: send\nclient -> table.ref: read", [][2]int{{2, -1}, {-1, 1}}},
		{"same table", "table.ref -> table.id", [][2]int{{1, 0}}},
		{"same row", "table.ref -> table.ref", [][2]int{{1, 1}}},
		{"table to row", "table -> table.status", [][2]int{{-1, 2}}},
		{"row to table", "table.ref <- table", [][2]int{{1, -1}}},
		{"multiple loops", "table.ref -> table.id\ntable.id -> table.ref", [][2]int{{1, 0}, {0, 1}}},
		{"whole table", "table -> table", [][2]int{{-1, -1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, _, err := d2compiler.Compile("table.d2", strings.NewReader(table+tc.links), nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
				t.Fatal(err)
			}
			if err := DefaultLayout(t.Context(), g); err != nil {
				t.Fatal(err)
			}
			for _, operation := range []string{"layout", "reroute"} {
				if operation == "reroute" {
					if tc.name == "whole table" {
						continue
					}
					if err := RouteEdges(t.Context(), g, g.Edges); err != nil {
						t.Fatal(err)
					}
				}
				for i, edge := range g.Edges {
					if len(edge.Route) < 2 {
						t.Fatalf("%s: empty route", operation)
					}
					start, end := edge.Route[0], edge.Route[len(edge.Route)-1]
					if *start == *end {
						t.Fatalf("%s: coincident ends of %s", operation, edge.AbsID())
					}
					for j, row := range tc.want[i] {
						idx, obj, pt := edge.SrcTableColumnIndex, edge.Src, start
						if j == 1 {
							idx, obj, pt = edge.DstTableColumnIndex, edge.Dst, end
						}
						if row < 0 {
							if idx != nil {
								t.Fatalf("unexpected binding %d", *idx)
							}
							continue
						}
						wantY := obj.TopLeft.Y + math.Round(obj.Height/float64(1+len(obj.SQLTable.Columns))*(float64(row)+1.5))
						if idx == nil || *idx != row || pt.Y != wantY || (pt.X != obj.TopLeft.X && pt.X != obj.TopLeft.X+obj.Width) {
							t.Errorf("%s: edge %d endpoint %d at %v, want side of row %d at y=%v", operation, i, j, pt, row, wantY)
						}
					}
				}
			}
		})
	}
}

func TestPartialSQLLoopReroutingRespectsFrozenLanes(t *testing.T) {
	const source = "table: {shape: sql_table; id: int; ref: int; status: text}\ntable.ref -> table.id: long label\ntable.status -> table.id"
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	g, _, err := d2compiler.Compile("table.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := DefaultLayout(t.Context(), g); err != nil {
		t.Fatal(err)
	}
	selected, frozen := g.Edges[0], g.Edges[1]
	if len(selected.Route) != 4 || len(frozen.Route) != 4 {
		t.Fatalf("unexpected row loop routes %v, %v", selected.Route, frozen.Route)
	}
	original := selected.Route[1].X
	reroute := func() float64 {
		t.Helper()
		if err := RouteEdges(t.Context(), g, []*d2graph.Edge{selected}); err != nil {
			t.Fatal(err)
		}
		if len(selected.Route) != 4 || selected.Route[1].X != selected.Route[2].X {
			t.Fatalf("invalid row loop: %v", selected.Route)
		}
		return selected.Route[1].X
	}
	if got := reroute(); got != original {
		t.Fatalf("partial reroute changed native lane from %g to %g", original, got)
	}

	// A caller can retain a lane laid out by another engine. Put it exactly
	// where the selected loop was and keep its port and route storage fixed.
	frozen.Route[1].X, frozen.Route[2].X = original, original
	retained := slices.Clone(frozen.Route)
	got := reroute()
	if math.Abs(got-original) < 30 {
		t.Fatalf("selected lane %g overlaps retained lane %g", got, original)
	}
	if again := reroute(); again != got {
		t.Fatalf("repeated reroute moved lane from %g to %g", got, again)
	}
	if !slices.Equal(retained, frozen.Route) || frozen.Route[1].X != original || frozen.Route[2].X != original {
		t.Fatalf("unselected path changed: %v", frozen.Route)
	}
}
