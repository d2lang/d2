package d2compiler_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/util-go/assert"
)

func TestNestedBoardNames(t *testing.T) {
	const source = `pipeline: {
  Layers: Storage
  scenarios
  steps: {retry}
  layers -> scenarios
  steps.retry -> _.pipeline."layers"
  *: {style.fill: red}
}
pipeline."scenarios" -> pipeline.steps
pipeline.(LAYERS -> scenarios)[0].label: uses
layers.overview: {copy: @lib.pipeline.steps}
scenarios.failure: {pipeline.steps: Rollback}
steps.deploy: {pipeline.layers: Release}
`
	fs := fstest.MapFS{"lib.d2": {Data: []byte("pipeline.steps: Imported")}}
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader(source), &d2compiler.CompileOptions{FS: fs})
	assert.Success(t, err)
	assert.Equal(t, 5, len(g.Objects))
	for _, child := range []string{"layers", "scenarios", "steps"} {
		obj, ok := g.Root.HasChild([]string{"pipeline", child})
		assert.True(t, ok && obj != nil && obj.Parent.ID == "pipeline")
		assert.True(t, obj.Style.Fill != nil && obj.Style.Fill.Value == "red")
	}
	assert.Equal(t, 3, len(g.Edges))
	assert.String(t, "pipeline.Layers", g.Edges[0].Src.AbsID())
	assert.String(t, "pipeline.scenarios", g.Edges[0].Dst.AbsID())
	assert.String(t, "uses", g.Edges[0].Label.Value)
	assert.String(t, "pipeline.steps.retry", g.Edges[1].Src.AbsID())
	assert.True(t, g.Edges[1].Dst == g.Edges[0].Src)
	assert.String(t, "pipeline.steps", g.Edges[2].Dst.AbsID())
	for _, tc := range []struct {
		board []*d2graph.Graph
		id    []string
		label string
	}{
		{g.Layers, []string{"copy"}, "Imported"},
		{g.Scenarios, []string{"pipeline", "steps"}, "Rollback"},
		{g.Steps, []string{"pipeline", "layers"}, "Release"},
	} {
		assert.Equal(t, 1, len(tc.board))
		obj, ok := tc.board[0].Root.HasChild(tc.id)
		assert.True(t, ok)
		assert.String(t, tc.label, obj.Label.Value)
	}
}

func TestNearNestedBoardName(t *testing.T) {
	for _, tc := range []struct {
		name, source, target string
		childExists          bool
	}{
		{"existing child", "p.steps; a.near: p.steps", "p.steps", true},
		{"quoted child", `p."steps"; a.near: p."steps"`, "p.steps", true},
		{"legacy missing child", "p; a.near: p.steps", "p", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, _, err := d2compiler.Compile("near.d2", strings.NewReader(tc.source), nil)
			assert.Success(t, err)
			near, ok := g.Root.Children["a"].NearTarget()
			assert.True(t, ok)
			assert.String(t, tc.target, near.AbsID())
			_, ok = g.Root.HasChild([]string{"p", "steps"})
			assert.Equal(t, tc.childExists, ok)
		})
	}
}

func TestSpreadShapeWithBoardNames(t *testing.T) {
	fs := fstest.MapFS{"library.d2": {Data: []byte("workflow: {steps.release: {}; scenarios.replay: {}}")}}
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader("job: {seed; ...@library.workflow}"), &d2compiler.CompileOptions{FS: fs})
	assert.Success(t, err)
	assert.Equal(t, 6, len(g.Objects))
	for _, parts := range [][]string{{"job", "steps", "release"}, {"job", "scenarios", "replay"}} {
		child, ok := g.Root.HasChild(parts)
		assert.True(t, ok && len(child.Children) == 0)
	}
}
