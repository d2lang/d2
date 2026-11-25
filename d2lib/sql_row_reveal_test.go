package d2lib

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

func TestSQLAppendDropsRowsAndKeepsExistingFieldPortsStill(t *testing.T) {
	const src = `vars: {d2-config: {animate-interval: 700ms; animate-duration: 420ms}}
direction: right
albums: Albums {shape: sql_table; id: int {constraint: primary_key}; title: text}
tracks: Tracks {shape: sql_table; id: int; album_id: int {constraint: foreign_key}; title: text}
albums.id -> tracks.album_id
steps: {timing: {tracks.seconds: int {constraint: foreign_key}; tracks.plays: int}}`
	before, _ := compileContinuity(t, src, d2talalayout.ContinueBoard)
	board := before.Steps[0]
	if len(board.TweenFrames) < 5 {
		t.Fatal("expected intermediate SQL row frames")
	}
	beforeTable, afterTable := gridEdgeShapeMap(before.Shapes)["tracks"], gridEdgeShapeMap(board.Shapes)["tracks"]
	oldRow := visibleArrivalLettering(t, before, ">album_id<")
	newRows := []string{">seconds<", ">plays<"}
	start := len(beforeTable.Columns)
	lastHeight := beforeTable.Height
	var partial [2]bool
	for i, frame := range board.TweenFrames {
		table := gridEdgeShapeMap(frame.Shapes)["tracks"]
		if table.Height < lastHeight || table.Height > afterTable.Height || table.Pos != beforeTable.Pos ||
			!reflect.DeepEqual(frame.Connections, before.Connections) {
			t.Fatalf("frame %d moved a port, rerouted an existing wire, or overshot the growing table", i)
		}
		lastHeight = table.Height
		if got := visibleArrivalLettering(t, frame, ">album_id<"); len(got) != 1 || len(oldRow) != 1 || got[0] != oldRow[0] {
			t.Fatalf("frame %d moved an existing field", i)
		}
		if i == 0 {
			if !reflect.DeepEqual(*table, *beforeTable) {
				t.Fatal("first frame differs from the previous table")
			}
			continue
		}
		for j, label := range newRows {
			rowHeight := afterTable.SQLRowHeight()
			top := float64(table.Pos.Y) + float64(start+j+1)*rowHeight
			height := max(0, min(rowHeight, float64(table.Pos.Y+table.Height)-top))
			got := visibleArrivalLettering(t, frame, label)
			if height == 0 {
				if len(got) > 0 {
					t.Fatalf("frame %d painted %s before its slot opened", i, label)
				}
				continue
			}
			final := visibleArrivalLettering(t, board, label)
			if len(got) != 1 || len(final) != 1 || got[0].element != final[0].element || got[0].opacity != final[0].opacity {
				t.Fatalf("frame %d changed %s lettering", i, label)
			}
			if height == rowHeight {
				if got[0] != final[0] {
					t.Fatalf("frame %d displaced the settled row %s", i, label)
				}
				continue
			}
			partial[j] = true
			// The clip starts at the previous divider and maps glyphs one to one.
			// As the bottom edge descends, the source window slides up the row.
			viewport := fmt.Sprintf("x=%d y=%s width=%d height=%s viewBox=%d %s %d %s overflow=hidden",
				table.Pos.X, svg.FormatFloat(top), table.Width, svg.FormatFloat(height),
				table.Pos.X, svg.FormatFloat(top+(rowHeight-height)), table.Width, svg.FormatFloat(height))
			if !strings.Contains(got[0].transform, viewport) {
				t.Fatalf("frame %d clipped %s with %q, want %q", i, label, got[0].transform, viewport)
			}
		}
	}
	if !partial[0] || !partial[1] || afterTable.RowReveal != nil || beforeTable.RowReveal != nil {
		t.Fatalf("missing sliding row or mutated board: %v", partial)
	}
}

func TestStandaloneSQLRowsGrowFromTheSameHeader(t *testing.T) {
	const src = `vars: {d2-config: {animate-interval: 300ms; animate-duration: 200ms}}
catalog: {shape: sql_table; catalog_id: int; title: text}
steps: {
  pages: {catalog.pages: int}
  copies: {catalog.copies: int}
}`
	d, _ := compileContinuity(t, src, d2talalayout.ContinueBoard)
	origin := d.Shapes[0].Pos
	for _, board := range d.Steps {
		if len(board.TweenFrames) < 2 || board.Shapes[0].Pos != origin {
			t.Fatalf("table moved or skipped its row drop on %q", board.Name)
		}
		for _, frame := range board.TweenFrames {
			if frame.Shapes[0].Pos != origin {
				t.Fatalf("header drifted on %q", board.Name)
			}
		}
	}
}

func TestSQLRowRevealRequiresStableLayout(t *testing.T) {
	prev := d2target.NewDiagram()
	table := d2target.BaseShape()
	table.ID, table.Type, table.Width, table.Height = "tracks", d2target.ShapeSQLTable, 200, 60
	table.Columns = []d2target.SQLColumn{{Name: d2target.Text{Label: "album_id", LabelWidth: 40}}}
	prev.Shapes = []d2target.Shape{*table}
	prev.Connections = []d2target.Connection{{ID: "field link", Route: []*geo.Point{geo.NewPoint(1, 2), geo.NewPoint(3, 2)}}}
	for _, tc := range []struct {
		name   string
		change func(*d2target.Diagram)
		want   bool
	}{
		{"append", func(*d2target.Diagram) {}, true},
		{"changed row height", func(d *d2target.Diagram) { d.Shapes[0].Height = table.Height }, false},
		{"wider field", func(d *d2target.Diagram) { d.Shapes[0].Columns[1].Name.LabelWidth = 60 }, false},
		{"reordered rows", func(d *d2target.Diagram) { c := d.Shapes[0].Columns; c[0], c[1] = c[1], c[0] }, false},
		{"shifted table", func(d *d2target.Diagram) { d.Shapes[0].Pos.Y++ }, false},
		{"moved field port", func(d *d2target.Diagram) {
			d.Connections[0].Route = []*geo.Point{geo.NewPoint(1, 2), geo.NewPoint(4, 2)}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := *prev
			next.Shapes = slices.Clone(prev.Shapes)
			next.Connections = slices.Clone(prev.Connections)
			next.Shapes[0].Columns = append(slices.Clone(table.Columns), d2target.SQLColumn{Name: d2target.Text{Label: "sec", LabelWidth: 20}})
			next.Shapes[0].Height = 90
			tc.change(&next)
			if got := planSQLRowReveal(prev, &next) != nil; got != tc.want {
				t.Fatalf("planned = %v, want %v", got, tc.want)
			}
		})
	}
}
