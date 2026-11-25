package d2exporter_test

import (
	"context"
	"html"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2isometric"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestSequenceV2IsometricRoles(t *testing.T) {
	const timeline = `shape: sequence-diagram {mirrored: true}
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
		source := `shape: sequence-diagram {mirrored: true}
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
		plain, _, err := d2lib.Compile(context.Background(), strings.Replace(source, "mirrored: true", "mirrored: false", 1), opts, &d2svg.RenderOpts{ThemeID: &theme})
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

func TestSequenceV2FrameCaptionFill(t *testing.T) {
	t.Parallel()
	const source = `shape: sequence-diagram
vars: {caption: "linear-gradient(red, blue)"}
classes.frame: {shape: edge-group; style.fill: green; label.style.fill: ${caption}}
one: First {class: frame; a -> b}
two: Second {class: frame; label.style.fill: transparent; b -> a}
three: Third {shape: edge-group; style.fill: orange; a -> b}
`
	d, err := compile(context.Background(), source)
	require.NoError(t, err)
	frames := map[string]d2target.Shape{}
	for _, s := range d.Shapes {
		if s.SequenceRole == "group" {
			frames[s.ID] = s
		}
	}
	require.Equal(t, "green", frames["one"].Fill)
	require.Equal(t, "linear-gradient(red, blue)", frames["one"].LabelFill)
	require.Equal(t, "green", frames["two"].Fill)
	require.Equal(t, "transparent", frames["two"].LabelFill)
	require.Equal(t, frames["three"].Fill, frames["three"].LabelFill)
	svg, err := d2svg.Render(d, nil)
	require.NoError(t, err)
	gradient, err := color.ParseGradient(frames["one"].LabelFill)
	require.NoError(t, err)
	require.Contains(t, string(svg), `id="`+gradient.ID+`"`)
	require.Contains(t, html.UnescapeString(string(svg)), `url('#`+gradient.ID+`')`)

	for _, src := range []string{
		"a.label.style.fill: red",
		"shape: sequence-diagram\na -> b: {label.style.fill: red}",
		"shape: sequence-diagram\na: {shape: edge-group; label.style.stroke: red}",
		"shape: sequence-diagram\na: {shape: edge-group; label.style.fill: nonsense}",
	} {
		_, err := compile(context.Background(), src)
		require.Error(t, err, src)
	}
}

func TestSequenceV2BranchedFrames(t *testing.T) {
	const config = `vars: {enable: true}
classes: {alternatives: {shape: edge-group; shape.branched: ${enable}}}
`
	const timeline = `shape: sequence-diagram {numbered: true}
outcome: Payment outcome {
  class: alternatives
  success: Approved {api -> client: Receipt {vertical-gap: 0}}
  pending: On hold {
    wait: Wait {shape: edge-group; api -> worker: Await}
  }
  decline: {}
}
after: {shape: edge-group; api -> client: Done}`
	for _, nested := range []bool{false, true} {
		source, prefix := timeline, ""
		if nested {
			source = "trace: {\n" + timeline + "\n}\nexternal -> trace"
			prefix = "trace."
		}
		d, err := compile(context.Background(), config+source)
		require.NoError(t, err)
		shapes := map[string]d2target.Shape{}
		for _, s := range d.Shapes {
			shapes[s.ID] = s
		}
		frame := shapes[prefix+"outcome"]
		require.Equal(t, "group", frame.SequenceRole)
		var lastY int
		branches := []string{"success", "pending", "decline"}
		for i, id := range branches {
			guard := shapes[prefix+"outcome."+id]
			require.Equal(t, "branch", guard.SequenceRole, id)
			require.Equal(t, d2target.ShapeText, guard.Type)
			require.Equal(t, []string{"[Approved]", "[On hold]", "[decline]"}[i], guard.Label)
			require.Greater(t, guard.Pos.Y, lastY)
			if i == 0 {
				require.Equal(t, frame.Pos.Y, guard.Pos.Y)
				require.GreaterOrEqual(t, guard.Pos.X, frame.Pos.X+frame.LabelWidth)
			}
			lastY = guard.Pos.Y
		}
		require.Equal(t, "group", shapes[prefix+"outcome.pending.wait"].SequenceRole)
		var dividers, lifelines int
		var receiptY float64
		for _, e := range d.Connections {
			switch e.SequenceRole {
			case "message":
				if e.Label == "1. Receipt" {
					receiptY = e.Route[0].Y
				}
				require.Equal(t, prefix+"api", e.Src)
				require.Contains(t, []string{prefix + "client", prefix + "worker"}, e.Dst)
			case "divider":
				guard := shapes[e.Dst]
				require.Equal(t, float64(guard.Pos.Y), e.Route[0].Y)
				require.Equal(t, e.Route[0].Y, e.Route[1].Y)
				require.Equal(t, float64(frame.Pos.X), e.Route[0].X)
				require.Equal(t, float64(frame.Pos.X+frame.Width), e.Route[1].X)
				require.Positive(t, e.StrokeDash)
				require.Positive(t, receiptY)
				require.Greater(t, e.Route[0].Y, receiptY)
				dividers++
			case "lifeline":
				lifelines++
			}
		}
		require.Equal(t, 2, dividers)
		require.Equal(t, 3, lifelines)
	}
}

func TestSequenceV2BranchOverlay(t *testing.T) {
	const base = `shape: sequence-diagram
decision: {shape: edge-group {branched: true}
  yes: {api -> client: Yes}
  no: {api -> client: No}
}
`
	_, err := compile(context.Background(), base+"decision.yes.(api -> client)[0].style.stroke: green")
	require.NoError(t, err, "restyling an earlier branch must preserve the message's position")
	_, err = compile(context.Background(), base+"decision.yes: {api -> client: Another}")
	require.ErrorContains(t, err, "events in sequence diagram branch decision.yes must be consecutive")
}
