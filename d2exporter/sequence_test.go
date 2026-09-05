package d2exporter_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2isometric"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestSequenceV2IsometricRoles(t *testing.T) {
	const timeline = `shape: sequence-diagram
vars: {mirror: true}
system: {shape: actor-group; api}
"client.app"
request: {shape: edge-group; "client.app" -> system.api.work: Start}
system.api.note: Check {shape: page}
system.api.event: Ready {shape: diamond}
system.api.again: {shape: actor}
system.api.work -> "client.app": Done
`
	for _, nested := range []bool{false, true} {
		source, prefix := timeline, ""
		if nested {
			source = "modern: {\n" + timeline + "\n}\nlegacy: {shape: sequence_diagram; a -> b: Legacy}\nother -> modern: Outside"
			prefix = "modern."
		}
		d, err := compile(context.Background(), source)
		require.NoError(t, err)
		scene, err := d2isometric.BuildScene(d, nil)
		require.NoError(t, err)
		require.True(t, scene.HasSequence)
		roles := make(map[string]string)
		for _, n := range scene.Nodes {
			roles[n.ID] = n.SequenceRole
			if strings.HasPrefix(n.ID, prefix) && n.ID != "modern" {
				require.False(t, n.Container, "%s should retain its timeline geometry", n.ID)
			}
		}
		for id, role := range map[string]string{
			"system": "group", "system.api": "actor", `"client.app"`: "actor", "request": "group",
			"system.api.work": "span", "system.api.note": "note", "system.api.event": "event",
			"system.api.again": "repeat", "system.api.mirror": "repeat",
		} {
			require.Equal(t, role, roles[prefix+id], "%s%s", prefix, id)
		}
		if nested {
			require.Equal(t, "container", roles["modern"])
			require.Equal(t, "actor", roles["legacy.a"])
			require.Empty(t, roles["other"])
		}
		var segments int
		for _, edge := range scene.Edges {
			switch edge.Label {
			case "Start", "Done", "Legacy":
				require.Equal(t, "message", edge.SequenceRole, edge.ID)
			case "Outside":
				require.Empty(t, edge.SequenceRole)
			default:
				require.Equal(t, "lifeline", edge.SequenceRole, edge.ID)
				if edge.Source == prefix+"system.api" {
					segments++
				}
			}
		}
		require.Greater(t, segments, 1, "events split the API's lifeline")
	}
}

func TestSequenceV2RepeatedActorAppearance(t *testing.T) {
	t.Parallel()
	for _, theme := range []int64{0, 303} {
		ruler, err := textmeasure.NewRuler()
		require.NoError(t, err)
		source := `shape: sequence-diagram
vars: {mirror: true}
a: Person {shape: person}
group: {shape: actor-group; b: Service}
a -> group.b
 a.note: Note {shape: page}
a.again: {shape: actor}
group.b -> a
`
		opts := &d2lib.CompileOptions{Ruler: ruler, LayoutResolver: func(string) (d2graph.LayoutGraph, error) { return d2dagrelayout.DefaultLayout, nil }}
		d, g, err := d2lib.Compile(context.Background(), source, opts, &d2svg.RenderOpts{ThemeID: &theme})
		require.NoError(t, err)
		byID := make(map[string]d2target.Shape)
		for _, s := range d.Shapes {
			byID[s.ID] = s
		}
		count := 0
		for _, obj := range g.Objects {
			if !obj.IsSequenceDiagramActorRepeat() {
				continue
			}
			count++
			repeated, ok := byID[obj.AbsID()]
			require.True(t, ok)
			original := byID[obj.SequenceDiagramActor().AbsID()]
			repeated.ID = original.ID
			repeated.Pos = original.Pos
			repeated.Level = original.Level
			repeated.ZIndex = original.ZIndex
			repeated.SequenceRole = original.SequenceRole
			require.Equal(t, original, repeated, "theme %d repeat %s", theme, obj.AbsID())
		}
		require.Equal(t, 3, count)
		plain, _, err := d2lib.Compile(context.Background(), strings.Replace(source, "mirror: true", "mirror: false", 1), opts, &d2svg.RenderOpts{ThemeID: &theme})
		require.NoError(t, err)
		for _, original := range plain.Shapes {
			if original.ID != "group.b" {
				continue
			}
			mirrored := byID[original.ID]
			mirrored.Pos = original.Pos
			require.Equal(t, original, mirrored, "mirror must not change the original actor in theme %d", theme)
		}

	}
}
