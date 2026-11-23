package d2lib

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2target"
	d2log "github.com/d2lang/d2/lib/log"
)

func TestCompileVariableTween(t *testing.T) {
	const input = `vars: {charge: 100%}
battery: "Battery · ${charge}" {
  width: 240
  height: 60
  style.fill: "linear-gradient(to right, green ${charge}, transparent ${charge})"
}
steps: {
  drain: {vars: {charge: 0% {tween: 160ms}}}
  done: {battery.style.stroke: green}
}`

	for _, animate := range []bool{false, true} {
		calls := 0
		opts := dagreCompileOptions(&calls)
		opts.AnimateTweens = animate
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
