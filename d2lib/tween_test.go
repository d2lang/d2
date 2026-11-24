package d2lib

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2target"
	d2log "github.com/d2lang/d2/lib/log"
)

func TestCompileVariableTween(t *testing.T) {
	const input = `vars: {charge: 100%; d2-config: {animate-duration: 160ms}}
battery: "Battery · ${charge}" {
  width: 240
  height: 60
  style.fill: "linear-gradient(to right, green ${charge}, transparent ${charge})"
}
steps: {
  drain: {vars: {charge: 0%}}
  done: {battery.style.stroke: green}
}`

	for _, animate := range []bool{false, true} {
		calls := 0
		opts := dagreCompileOptions(&calls)
		opts.AnimateTweens = &animate
		opts.LayoutReuse = true
		diagram, graph, err := Compile(d2log.WithDefault(context.Background()), input, opts, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(diagram.Steps) != 2 || len(graph.Steps) != 2 {
			t.Fatal("sample frames became navigable steps")
		}
		drain := diagram.Steps[0]
		checkValue := func(board *d2target.Diagram, value string) {
			t.Helper()
			shape := shapeByID(t, board, "battery")
			if shape.Label != "Battery · "+value || shape.Fill != "linear-gradient(to right, green "+value+", transparent "+value+")" {
				t.Fatalf("sample label and fill = %q, %q; want %s in each", shape.Label, shape.Fill, value)
			}
		}
		checkValue(diagram, "100%")
		checkValue(drain, "0%")
		checkValue(diagram.Steps[1], "0%")
		if len(diagram.Steps[1].TweenFrames) != 0 || diagram.Steps[1].TweenDurationMS != 0 {
			t.Fatal("inherited endpoint replayed the preceding tween")
		}
		if !animate {
			if len(drain.TweenFrames) != 0 || drain.TweenDurationMS != 0 {
				t.Fatal("static compile produced tween samples")
			}
			continue
		}
		if len(drain.TweenFrames) != 4 || drain.TweenDurationMS != 160 {
			t.Fatalf("got %d samples in %dms, want 4 in 160ms", len(drain.TweenFrames), drain.TweenDurationMS)
		}
		for i, value := range []string{"100%", "75%", "50%", "25%"} {
			checkValue(drain.TweenFrames[i], value)
		}
		if !strings.Contains(diagram.GetNestedCorpus(), "Battery · 75%") {
			t.Fatal("font corpus omitted intermediate labels")
		}

		serialized, err := json.Marshal(diagram)
		if err != nil {
			t.Fatal(err)
		}
		var restored d2target.Diagram
		if err := json.Unmarshal(serialized, &restored); err != nil {
			t.Fatal(err)
		}
		if err := d2target.ValidateRenderTarget(&restored); err != nil {
			t.Fatal(err)
		}
		checkValue(restored.Steps[0].TweenFrames[1], "75%")
	}
}

func TestCompileFractionalStrokeWidths(t *testing.T) {
	const input = `vars: {n: 0; d2-config: {animate-interval: 200ms; animate-duration: 160ms}}
style.stroke-width: 5
classes: {
  route: {style.stroke-width: "${n}"}
  quiet: {style.stroke-width: 1}
  loud: {style.stroke-width: 6}
}
a: "Count ${n}" {class: route}
b: {style.stroke-width: ${n}}
mode: {class: quiet}
a -> b: {style.stroke-width: 2}
a -> b: {style.stroke-width: 7}
steps: {
  grow: {
    vars: {n: 1}
    style.stroke-width: 4
    (a -> b)[0].style.stroke-width: 8
    fresh.style.stroke-width: 8
    mode.class: loud
  }
  restore: {vars: {n: 0}; style.stroke-width: 5; (a -> b)[0].style.stroke-width: 2}
}
`
	calls := 0
	opts := dagreCompileOptions(&calls)
	opts.LayoutReuse = true
	board, _, err := Compile(d2log.WithDefault(context.Background()), input, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d2target.ValidateRenderTarget(board); err != nil {
		t.Fatal(err)
	}
	for i, step := range board.Steps {
		if len(step.TweenFrames) != 4 {
			t.Fatalf("step %d has %d frames", i, len(step.TweenFrames))
		}
		for j, frame := range append(slices.Clone(step.TweenFrames), step) {
			progress := float64(j) / 4
			if i == 1 {
				progress = 1 - progress
			}
			a := shapeByID(t, frame, "a")
			for _, stroke := range []struct {
				name      string
				got, want float64
			}{
				{"class variable", a.RenderedStrokeWidth(), progress},
				{"shared variable", shapeByID(t, frame, "b").RenderedStrokeWidth(), progress},
				{"board", frame.Root.RenderedStrokeWidth(), 5 - progress},
				{"first edge", frame.Connections[0].RenderedStrokeWidth(), 2 + 6*progress},
				{"parallel edge", frame.Connections[1].RenderedStrokeWidth(), 7},
				{"new shape", shapeByID(t, frame, "fresh").RenderedStrokeWidth(), 8},
				{"class switch", shapeByID(t, frame, "mode").RenderedStrokeWidth(), 6},
			} {
				if stroke.got != stroke.want {
					t.Errorf("step %d frame %d: %s stroke width %v, want %v", i, j, stroke.name, stroke.got, stroke.want)
				}
			}
			wantLabel := "Count " + strconv.Itoa(int(math.RoundToEven(progress)))
			if a.Label != wantLabel {
				t.Errorf("step %d frame %d: shared variable label %q, want %q", i, j, a.Label, wantLabel)
			}
		}
	}
}
