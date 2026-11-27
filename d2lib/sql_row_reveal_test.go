package d2lib

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

func TestSQLTrailingRowsKeepExistingFieldPortsStill(t *testing.T) {
	const src = `vars: {d2-config: {animate-interval: 700ms; animate-duration: 420ms}}
direction: right
albums: Albums {shape: sql_table; id: int {constraint: primary_key}; title: text}
tracks: Tracks {shape: sql_table; id: int; album_id: int {constraint: foreign_key}; title: text}
albums.id -> tracks.album_id
steps: {
  add: {tracks.seconds: int {constraint: foreign_key}; tracks.plays: int}
  remove: {tracks.seconds: null; tracks.plays: null}
}`
	root, _ := compileContinuity(t, src, d2talalayout.ContinueBoard)
	from := root
	for _, to := range root.Steps {
		t.Run(to.Name, func(t *testing.T) {
			before, after := shapeByID(t, from, "tracks"), shapeByID(t, to, "tracks")
			retained, full, fullBoard := before, after, to
			removing := len(before.Columns) > len(after.Columns)
			if removing {
				retained, full, fullBoard = after, before, from
			}
			if len(to.TweenFrames) < 5 || before.Pos != after.Pos {
				t.Fatal("expected intermediate samples with a stationary header")
			}
			oldRow := visibleArrivalLettering(t, from, ">album_id<")
			rows := []string{">seconds<", ">plays<"}
			lastHeight := before.Height
			var partial [2]bool
			for i, frame := range to.TweenFrames {
				table := shapeByID(t, frame, "tracks")
				if table.Height < retained.Height || table.Height > full.Height || table.Pos != before.Pos ||
					(removing && table.Height > lastHeight) || (!removing && table.Height < lastHeight) ||
					!reflect.DeepEqual(frame.Connections, from.Connections) {
					t.Fatalf("sample %d moved a port, rerouted a wire, or changed the direction of travel", i)
				}
				lastHeight = table.Height
				if got := visibleArrivalLettering(t, frame, ">album_id<"); len(got) != 1 || len(oldRow) != 1 || got[0] != oldRow[0] {
					t.Fatalf("sample %d moved an existing foreign-key field", i)
				}
				if i == 0 && !reflect.DeepEqual(table, before) {
					t.Fatal("first frame differs from the previous table")
				}
				for j, text := range rows {
					slot := full.SQLRowHeight()
					top := float64(table.Pos.Y) + float64(len(retained.Columns)+j+1)*slot
					height := max(0, min(slot, float64(table.Pos.Y+table.Height)-top))
					got := visibleArrivalLettering(t, frame, text)
					if height == 0 {
						if len(got) != 0 {
							t.Fatalf("sample %d painted %s in a closed slot", i, text)
						}
						continue
					}
					reference := visibleArrivalLettering(t, fullBoard, text)
					if len(got) != 1 || len(reference) != 1 || got[0].element != reference[0].element || got[0].opacity != reference[0].opacity {
						t.Fatalf("sample %d changed %s lettering", i, text)
					}
					if height == slot {
						if got[0] != reference[0] {
							t.Fatalf("sample %d displaced the fully visible row %s", i, text)
						}
						continue
					}
					partial[j] = true
					viewport := fmt.Sprintf("x=%d y=%s width=%d height=%s viewBox=%d %s %d %s overflow=hidden",
						table.Pos.X, svg.FormatFloat(top), table.Width, svg.FormatFloat(height),
						table.Pos.X, svg.FormatFloat(top+(slot-height)), table.Width, svg.FormatFloat(height))
					if !strings.Contains(got[0].transform, viewport) {
						t.Fatalf("sample %d clipped %s with %q, want %q", i, text, got[0].transform, viewport)
					}
				}
			}
			if !partial[0] || !partial[1] || after.RowReveal != nil || before.RowReveal != nil {
				t.Fatalf("missing sliding row or mutated board: %v", partial)
			}
		})
		from = to
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

func TestSQLRowChangesInOppositeDirections(t *testing.T) {
	const src = `vars: {d2-config: {animate-interval: 500ms; animate-duration: 300ms}}
a: A {shape: sql_table; top: 0; left: 0; account_id: int; old: int}
b: B {shape: sql_table; top: 0; left: 360; account_id: int}
steps: {
  swap: {a.old: null; b.new: int}
  restore: {a.old: int; b.new: null}
}`
	d, _ := compileContinuity(t, src, d2talalayout.ContinueBoard)
	previous := d
	for _, step := range d.Steps {
		if len(step.TweenFrames) < 5 {
			t.Fatalf("expected simultaneous row insertion and removal in %s", step.Name)
		}
		begin := step.TweenFrames[0]
		mid := step.TweenFrames[len(step.TweenFrames)/2]
		for _, id := range []string{"a", "b"} {
			from, to := shapeByID(t, previous, id), shapeByID(t, step, id)
			first, halfway := shapeByID(t, begin, id), shapeByID(t, mid, id)
			if !reflect.DeepEqual(first, from) || halfway.Pos != to.Pos ||
				halfway.Height <= min(from.Height, to.Height) || halfway.Height >= max(from.Height, to.Height) {
				t.Fatalf("table %s did not pass between its own endpoints in step %s", id, step.Name)
			}
		}
		previous = step
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
		{"changed field links", func(d *d2target.Diagram) { d.Connections = nil }, false},
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
			if got := planSQLRowReveal(&next, prev) != nil; got != tc.want {
				t.Fatalf("reverse planned = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStructuredShapeDividersStayInsideRoundedOutline(t *testing.T) {
	const config = "vars: {d2-config: {animate-interval: 700ms; animate-duration: 420ms}}\n"
	for _, test := range []struct{ name, source string }{
		{"SQL row drop", `catalog: {shape: sql_table; style.border-radius: 250; catalog_id: int; title: text}
steps: {pages: {catalog.pages: int}}`},
		{"class rounding", `Message: {shape: class; id: int; style.border-radius: 0}
steps: {round: {Message.style.border-radius: 250}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, _ := compileContinuity(t, config+test.source, d2talalayout.ContinueBoard)
			step := root.Steps[0]
			if len(step.TweenFrames) < 3 {
				t.Fatal("expected intermediate frames")
			}
			for _, frame := range append(slices.Clone(step.TweenFrames), step) {
				shape := frame.Shapes[0]
				radius := min(float64(shape.BorderRadius), float64(min(shape.Height, shape.Width))/2)
				svgBytes, err := d2svg.Render(frame, nil)
				if err != nil {
					t.Fatal(err)
				}
				doc := string(svgBytes)
				x, y := float64(shape.Pos.X), float64(shape.Pos.Y)
				bottom := svg.FormatFloat(y + float64(shape.Height))
				divider := fmt.Sprintf(`<line x1="%s" x2="%s" y1="%s" y2="%s"`, svg.FormatFloat(x+radius), svg.FormatFloat(x+float64(shape.Width)-radius), bottom, bottom)
				clipStart := fmt.Sprintf(`<path d="M %s %s L`, svg.FormatFloat(x), svg.FormatFloat(y+radius))
				if !strings.Contains(doc, divider) || radius > 0 && (!strings.Contains(doc, clipStart) || !strings.Contains(doc, `Z" fill="none"/></clipPath>`)) {
					t.Fatalf("header clip or bottom divider misses the outline radius %.1f", radius)
				}
			}
		})
	}
}
