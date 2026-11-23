package d2lib

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/d2lang/d2/d2target"
	d2log "github.com/d2lang/d2/lib/log"
)

func TestSequenceEditInheritance(t *testing.T) {
	const src = `vars: {d2-config.animate-duration: 160ms}
shape: sequence_diagram
a -> b: request
classes: {trace: {style.stroke: blue}}
layers: {independent: {shape: sequence_diagram; a -> b: request}}
scenarios: {
  reply: {b -> a: received}
  recolor: {(a -> b)[0].class: trace}
}
steps: {
  respond: {b -> a: received}
  move: {a.label: "A much wider participant"}
  rename: {(a -> b)[0]: retry}
  newStyle: {(b -> a)[0].style.stroke-dash: 4}
  clear: {(a -> b)[0]: null; (b -> a)[0]: null}
}`
	calls := 0
	diagram, _, err := Compile(d2log.WithDefault(context.Background()), src, dagreCompileOptions(&calls), nil)
	require.NoError(t, err)
	check := func(board *d2target.Diagram, continuation bool, unchanged ...bool) {
		t.Helper()
		require.Equal(t, continuation, board.SequenceContinuation, board.Name)
		require.Len(t, board.SequenceTimelines, 1, board.Name)
		got := make([]bool, 0, len(unchanged))
		for _, m := range board.SequenceTimelines[0].Messages {
			got = append(got, m.Unchanged)
		}
		require.Equal(t, unchanged, got, board.Name)
	}
	check(diagram, false, false)
	check(diagram.Layers[0], false, false)
	check(diagram.Scenarios[0], true, true, false)
	check(diagram.Scenarios[1], true, false)
	check(diagram.Steps[0], true, true, false)
	check(diagram.Steps[1], true, true, true)
	check(diagram.Steps[2], true, false, true)
	check(diagram.Steps[3], true, true, false)
	require.Empty(t, diagram.Steps[4].SequenceTimelines)
	require.True(t, diagram.Steps[4].SequenceContinuation)
}

func TestSequenceImplicitIntervalForValueTweens(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		animate   bool
	}{
		{"actor only", "shape: sequence_diagram\na", true},
		{"class on nested sequence", "classes: {seq: {shape: sequence_diagram}}\npool: {class: seq; x -> y}\na", true},
		{"unused class", "classes: {unused: {shape: sequence_diagram}}\na", false},
		{"shape data in variable", "vars: {mock: {shape: sequence_diagram}}\na", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			source := "vars: {d2-config.animate-duration: 160ms}\n" + tc.src + "\na.style.fill: blue\nsteps: {change: {a.style.fill: red}}"
			d, _, err := Compile(d2log.WithDefault(context.Background()), source, dagreCompileOptions(&calls), nil)
			require.NoError(t, err)
			require.Equal(t, tc.animate, len(d.Steps[0].TweenFrames) > 0)
			require.Equal(t, tc.animate, d.HasSequencePlayback())
		})
	}
}
