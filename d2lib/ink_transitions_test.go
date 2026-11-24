package d2lib

import (
	"context"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	d2log "github.com/d2lang/d2/lib/log"
)

func TestInkFollowsPlaybackAcrossBoards(t *testing.T) {
	const source = `vars: {d2-config: {animate-interval: 200ms; animate-duration: 160ms}}
a: Stable {style.fill: white}
b
a -> b: Active
layers: {one: {
  a: Stable {style: {underline: true; fill: green}}
  b
  a -> b: Active {style.underline: true}
}}
scenarios: {two: {a.style.fill: red}}
steps: {three: {a: Different {style.underline: true}}}
`
	for _, sketch := range []bool{false, true} {
		calls := 0
		d, _, err := Compile(d2log.WithDefault(context.Background()), source, dagreCompileOptions(&calls), &d2svg.RenderOpts{Sketch: &sketch})
		if err != nil {
			t.Fatal(err)
		}
		if err := d2target.ValidateRenderTarget(d); err != nil {
			t.Fatal(err)
		}
		for idx, board := range []*d2target.Diagram{d.Layers[0], d.Scenarios[0]} {
			if len(board.TweenFrames) != 4 {
				t.Fatalf("board %d has %d frames", idx, len(board.TweenFrames))
			}
			for j, frame := range board.TweenFrames {
				want := float64(j) / 4
				if idx == 1 {
					want = 1 - want
				}
				s := shapeByID(t, frame, "a")
				for _, txt := range []d2target.Text{s.Text, frame.Connections[0].Text} {
					if txt.UnderlineProgress == nil || *txt.UnderlineProgress != want {
						t.Fatalf("board %d sample %d: underline %v, want %v", idx, j, txt.UnderlineProgress, want)
					}
				}
				if (s.SketchFillTransition != nil) != sketch {
					t.Fatalf("sketch=%v, fill=%v", sketch, s.SketchFillTransition)
				}
				if sketch && s.SketchFillTransition.Progress != float64(j)/4 {
					t.Fatalf("sketch fill at %d: %v", j, s.SketchFillTransition)
				}
			}
			if shapeByID(t, board, "a").UnderlineProgress != nil || board.Connections[0].UnderlineProgress != nil {
				t.Fatal("transition mutated a final label")
			}
		}
		for _, frame := range d.Steps[0].TweenFrames {
			if shapeByID(t, frame, "a").UnderlineProgress != nil {
				t.Fatal("animated a replacement label")
			}
		}
	}
}

func TestInkComposesWithGridAndMarkdown(t *testing.T) {
	const source = `vars: {d2-config: {sketch: true; animate-interval: 300ms; animate-duration: 240ms}}
grid-columns: 2
a: |md webhook v1 |
b: Retry queue {style.fill: white}
steps: {cutover: {a: |md webhook ~~v1~~ |; b.style.fill: green; c: Dead letters}}
`
	calls := 0
	d, _, err := Compile(d2log.WithDefault(context.Background()), source, dagreCompileOptions(&calls), nil)
	if err != nil {
		t.Fatal(err)
	}
	board := d.Steps[0]
	if !board.GridMotion || len(board.TweenFrames) < 2 {
		t.Fatal("missing grid/ink motion")
	}
	var last float64
	for i, frame := range board.TweenFrames {
		strike := shapeByID(t, frame, "a").MarkdownStrike
		fill := shapeByID(t, frame, "b").SketchFillTransition
		if strike == nil || fill == nil || strike.Progress != fill.Progress || (i > 0 && strike.Progress <= last) {
			t.Fatalf("frame %d: strike=%v fill=%v", i, strike, fill)
		}
		last = strike.Progress
	}
	off := false
	opts := dagreCompileOptions(&calls)
	opts.AnimateTweens = &off
	static, _, err := Compile(d2log.WithDefault(context.Background()), source, opts, nil)
	if err != nil || len(static.Steps[0].TweenFrames) != 0 {
		t.Fatalf("static export: %v", err)
	}
}

func TestInvisibleInkEdits(t *testing.T) {
	for _, pair := range [][2]string{{"white", "#fff"}, {"#fff", "#ffffff"}, {"Transparent", "transparent"}} {
		old := d2target.Shape{Fill: pair[0]}
		next := d2target.Shape{Fill: pair[1]}
		if changedSketchFill(&old, &next) {
			t.Errorf("planned pencil strokes for %q -> %q", pair[0], pair[1])
		}
	}
	for _, kind := range []string{d2target.ShapeClass, d2target.ShapeSQLTable} {
		shape := d2target.Shape{ID: "a", Type: kind, Text: d2target.Text{Label: "header"}}
		prev := &d2target.Diagram{Shapes: []d2target.Shape{shape}}
		shape.Underline = true
		next := &d2target.Diagram{Shapes: []d2target.Shape{shape}}
		if planBoardInk(prev, next, true, false) != nil {
			t.Errorf("planned ink for %s header", kind)
		}
	}
}

func TestPrepareSketchTransitionsPreservesExistingGrid(t *testing.T) {
	const source = `vars: {d2-config: {animate-interval: 2000ms; animate-duration: 1600ms}}
grid-columns: 3
a: Block {style.fill: white}
b: Retry
steps: {ready: {a.style.fill: green; b.style.underline: true; c: Other}}
`
	calls := 0
	d, _, err := Compile(d2log.WithDefault(context.Background()), source, dagreCompileOptions(&calls), nil)
	if err != nil {
		t.Fatal(err)
	}
	board := d.Steps[0]
	original := append([]*d2target.Diagram(nil), board.TweenFrames...)
	ends := append([]int(nil), board.TweenFrameEndsMS...)
	if len(original) == 0 || !board.GridMotion {
		t.Fatal("missing grid motion")
	}
	if err := PrepareSketchTransitions(context.Background(), d, 1600); err != nil {
		t.Fatal(err)
	}
	if err := d2target.ValidateRenderTarget(d); err != nil {
		t.Fatal(err)
	}
	if !board.GridMotion || len(board.TweenFrames) > d2target.MaxTweenFrames {
		t.Fatal("lost grid timing")
	}
	start := 0
	for i, frame := range board.TweenFrames {
		j := 0
		for j < len(ends) && ends[j] <= start {
			j++
		}
		want := board
		if j < len(original) {
			want = original[j]
		}
		gotB, wantB := shapeByID(t, frame, "b"), shapeByID(t, want, "b")
		if gotB.Pos != wantB.Pos || gotB.UnderlineProgress == nil || wantB.UnderlineProgress == nil || *gotB.UnderlineProgress != *wantB.UnderlineProgress {
			t.Fatalf("changed existing frame's position or underline at %dms", start)
		}
		fill := shapeByID(t, frame, "a").SketchFillTransition
		if fill == nil || fill.From != "white" || fill.To != "green" || fill.Progress != float64(start)/1600 {
			t.Fatalf("pencil at %dms: %+v", start, fill)
		}
		start = board.TweenFrameEndsMS[i]
	}
	if start != 1600 || shapeByID(t, board, "a").SketchFillTransition != nil {
		t.Fatal("invalid endpoint")
	}
}

func TestPencilScheduleWithExistingFrames(t *testing.T) {
	const duration = 6000
	early := make([]int, d2target.MaxTweenFrames)
	for i := range early {
		early[i] = i + 1
	}
	for _, ends := range [][]int{{50, duration}, early} {
		d := &d2target.Diagram{TweenFrameEndsMS: ends}
		schedule := inkBoundaries(d, duration, d2target.TweenFrameCount(duration))
		if len(schedule) != d2target.MaxTweenFrames+1 || schedule[0] != 0 || schedule[len(schedule)-1] != duration {
			t.Fatalf("invalid merged schedule: %v", schedule)
		}
		d.TweenFrameEndsMS = schedule[1:]
		if again := inkBoundaries(d, duration, d2target.TweenFrameCount(duration)); !slices.Equal(again, schedule) {
			t.Fatalf("preparing the schedule again changed it: %v -> %v", schedule, again)
		}
	}
}
