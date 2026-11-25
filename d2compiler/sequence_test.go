package d2compiler_test

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
)

func compileSequence(t *testing.T, source string) *d2graph.Graph {
	t.Helper()
	g, _, err := d2compiler.Compile("sequence.d2", strings.NewReader(source), nil)
	require.NoError(t, err)
	return g
}

func sequenceObject(t *testing.T, g *d2graph.Graph, path string) *d2graph.Object {
	t.Helper()
	obj, ok := g.Root.HasChild(strings.Split(path, "."))
	require.True(t, ok, "missing %s", path)
	return obj
}

func TestSequenceV2Groups(t *testing.T) {
	t.Parallel()
	g := compileSequence(t, `shape: sequence-diagram {
  mirrored: true; numbered: true
}
team: {shape: actor-group; a}
first: {
  shape: edge-group
  a.work -> b: begin
  nested: {
    shape: edge-group
    b -> a.work: finish {vertical-gap: 0}
    a: {note: hello {shape: page}}
    team.a -> b
  }
}
`)
	require.Len(t, g.Objects, 8)
	require.True(t, g.Root.Sequence.Mirrored)
	require.True(t, g.Root.Sequence.Numbered)
	a := sequenceObject(t, g, "a")
	b := sequenceObject(t, g, "b")
	span := sequenceObject(t, g, "a.work")
	group := sequenceObject(t, g, "first")
	nested := sequenceObject(t, g, "first.nested")
	note := sequenceObject(t, g, "a.note")
	require.True(t, a.IsSequenceDiagramActor())
	require.True(t, b.IsSequenceDiagramActor())
	require.True(t, span.IsSequenceDiagramSpan())
	require.Empty(t, span.Label.Value)
	require.True(t, note.IsSequenceDiagramNote())
	require.True(t, note.ContainedBy(nested))
	require.True(t, sequenceObject(t, g, "team.a").IsSequenceDiagramActor())
	require.Len(t, group.ChildrenArray, 1)
	require.Empty(t, nested.ChildrenArray)
	require.Len(t, g.Edges, 3)
	require.Same(t, span, g.Edges[0].Src)
	require.Same(t, b, g.Edges[0].Dst)
	require.True(t, g.Edges[0].ContainedBy(group))
	require.Equal(t, "1. begin", g.Edges[0].Label.Value)
	require.Equal(t, "2. finish", g.Edges[1].Label.Value)
	require.Equal(t, "0", g.Edges[1].VerticalGap.Value)
	require.Equal(t, "3", g.Edges[2].Label.Value)
	require.Same(t, sequenceObject(t, g, "team.a"), g.Edges[2].Src)
}

func TestSequenceV2ActorDescendants(t *testing.T) {
	t.Parallel()
	g := compileSequence(t, `shape: sequence-diagram
a: {
  a
  named: Work {label.near: top-left}
  event: Retry {shape: diamond}
  again: {shape: actor}
  note: hello {shape: page}
}
a.a -> a.named
`)
	require.True(t, sequenceObject(t, g, "a.a").IsSequenceDiagramSpan())
	require.Empty(t, sequenceObject(t, g, "a.a").Label.Value)
	require.Equal(t, "Work", sequenceObject(t, g, "a.named").Label.Value)
	require.True(t, sequenceObject(t, g, "a.event").IsSequenceDiagramEvent())
	require.True(t, sequenceObject(t, g, "a.again").IsSequenceDiagramActorRepeat())
	require.True(t, sequenceObject(t, g, "a.note").IsSequenceDiagramNote())
}

func TestSequenceV2ShapeOptions(t *testing.T) {
	t.Parallel()
	g := compileSequence(t, `plain: {
  shape: sequence-diagram
  vars: {numbered: chapter; mirror: true; mirrored: true}
  numbered -> mirrored: ${numbered}
}
configured: {
  vars: {enabled: true}
  shape.numbered: ${enabled}
  shape: sequence-diagram {mirrored: true}
  numbered -> mirror: chapter
}
scenarios: {unmirrored: {configured.shape.mirrored: false}}
`)
	plain := sequenceObject(t, g, "plain")
	configured := sequenceObject(t, g, "configured")
	require.Equal(t, &d2graph.SequenceOptions{}, plain.Sequence)
	require.Equal(t, &d2graph.SequenceOptions{Numbered: true, Mirrored: true}, configured.Sequence)
	require.Len(t, g.Edges, 2)
	require.Equal(t, "chapter", g.Edges[0].Label.Value)
	require.Equal(t, "1. chapter", g.Edges[1].Label.Value)
	require.True(t, sequenceObject(t, g, "plain.numbered").IsSequenceDiagramActor())
	require.True(t, sequenceObject(t, g, "plain.mirrored").IsSequenceDiagramActor())
	require.Len(t, g.Scenarios, 1)
	require.Equal(t, &d2graph.SequenceOptions{Numbered: true}, sequenceObject(t, g.Scenarios[0], "configured").Sequence)
}

func TestSequenceV2ShapeOptionGlobs(t *testing.T) {
	t.Parallel()
	g := compileSequence(t, `vars: {show-footer: true}
*: {
  &shape: sequence-diagram
  shape.numbered: true
  shape.mirrored: ${show-footer}
}
checkout: {shape: sequence-diagram; buyer -> shop: Pay}
refund: {shape: sequence-diagram; shop -> buyer: Refund; shape.numbered: false}
legacy: {shape: sequence_diagram; a -> b: Legacy}
box.shape: rectangle
*: {&shape.numbered: true; style.stroke: red}
*: {&shape.numbered: false; style.stroke: blue}
`)
	checkout := sequenceObject(t, g, "checkout")
	refund := sequenceObject(t, g, "refund")
	require.Equal(t, &d2graph.SequenceOptions{Numbered: true, Mirrored: true}, checkout.Sequence)
	require.Equal(t, &d2graph.SequenceOptions{Mirrored: true}, refund.Sequence)
	require.Equal(t, "red", checkout.Style.Stroke.Value)
	require.Equal(t, "blue", refund.Style.Stroke.Value)
	require.Nil(t, sequenceObject(t, g, "legacy").Style.Stroke)
	require.Nil(t, sequenceObject(t, g, "box").Style.Stroke)
	require.Len(t, g.Edges, 3)
	require.Equal(t, "1. Pay", g.Edges[0].Label.Value)
	require.Equal(t, "Refund", g.Edges[1].Label.Value)
	require.Equal(t, "Legacy", g.Edges[2].Label.Value)
}

func TestSequenceV2Invalid(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ source, want string }{
		{`shape: sequence-diagram {branched: true}`, "unknown sequence diagram shape option"},
		{`shape: sequence-diagram; result: {shape: edge-group {branched: yes}}`, "shape.branched must be true or false"},
		{`shape: sequence-diagram; result: {shape: edge-group {branched: true}; a -> b}`, "place messages inside a named branch"},
		{`shape: sequence-diagram; result: {shape: edge-group {branched: true}; ok: {shape: square}}`, "a branch cannot have a shape"},
		{`shape: sequence-diagram {mirrored: maybe}`, "shape.mirrored must be true or false"},
		{`shape: sequence-diagram; shape.numbered: {yes}`, "shape.numbered must be true or false"},
		{`shape: sequence-diagram; shape.mirror: true`, "unknown sequence diagram shape option \"mirror\""},
		{`shape: sequence-diagram {numbering: true}`, "unknown sequence diagram shape option \"numbering\""},
		{`shape: sequence-diagram {a -> b}`, "cannot contain connections"},
		{`shape: rectangle {mirrored: true}`, "shape does not accept composite"},
		{`shape: sequence_diagram; shape.numbered: true`, "shape does not accept composite"},
		{`a -> b: {shape: sequence-diagram {numbered: true}}`, "shape options can only be used on sequence-diagram objects"},
		{`a -> b: {target-arrowhead.shape: diamond {mirrored: true}}`, "shape options can only be used on sequence-diagram objects"},
		{`shape: sequence-diagram; a -> b: hi {label.near: border-left-top}`, "sequence message label.near"},
		{`shape: sequence-diagram; a.note.shape: page; a.note -> b`, "can only connect actors or spans"},
		{`shape: sequence-diagram; a.event.shape: diamond; a.event -> b`, "can only connect actors or spans"},
		{`shape: sequence-diagram; a.again.shape: actor; a.again -> b`, "can only connect actors or spans"},
		{`shape: sequence-diagram; group.shape: edge-group; group -> b`, "can only connect actors or spans"},
		{`shape: sequence-diagram; group.shape: actor-group; group -> b`, "can only connect actors or spans"},
		{`shape: sequence-diagram; a.note: {shape: page; child}`, "cannot have children"},
		{`a.shape: edge-group`, "edge-group must be inside"},
		{`shape: actor-group`, "actor-group must be inside"},
		{`a.shape: actor`, "shape actor must be a descendant"},
		{`seq: {shape: sequence-diagram; a}; seq.a -> outside`, "same diagram"},
	} {
		t.Run(tt.source, func(t *testing.T) {
			_, _, err := d2compiler.Compile("sequence.d2", strings.NewReader(tt.source), nil)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestSequenceV2ScopesAndBoards(t *testing.T) {
	t.Parallel()
	g, _, err := d2compiler.Compile("root.d2", strings.NewReader(`...@actors
ordinary: {a -> b}
seq: {
  shape: sequence-diagram
  group: {shape: edge-group; a: {work -> _.b}; a -> b}
}
layers: {example: {shape: sequence-diagram; group: {shape: edge-group; x -> y}}}
`), &d2compiler.CompileOptions{FS: fstest.MapFS{
		"actors.d2": &fstest.MapFile{Data: []byte(`imported: {shape: sequence-diagram; group: {shape: edge-group; x -> y}}`)},
	}})
	require.NoError(t, err)
	require.NotNil(t, sequenceObject(t, g, "ordinary.a"))
	require.NotNil(t, sequenceObject(t, g, "ordinary.b"))
	require.NotNil(t, sequenceObject(t, g, "seq.a.work"))
	require.NotNil(t, sequenceObject(t, g, "seq.b"))
	require.NotNil(t, sequenceObject(t, g, "imported.x"))
	require.Len(t, g.Layers, 1)
	require.NotNil(t, sequenceObject(t, g.Layers[0], "x"))
	require.Empty(t, sequenceObject(t, g.Layers[0], "group").ChildrenArray)
}

func TestSequenceLegacyCompatibility(t *testing.T) {
	t.Parallel()
	g := compileSequence(t, `shape: sequence_diagram
a; b
group: {a -> b}
a.note: hello
`)
	require.Nil(t, g.Root.Sequence)
	require.False(t, g.Root.IsSequenceDiagramV2())
	require.Same(t, sequenceObject(t, g, "a"), g.Edges[0].Src)
	require.Same(t, sequenceObject(t, g, "b"), g.Edges[0].Dst)
	require.True(t, sequenceObject(t, g, "group").IsSequenceDiagramGroup())
	require.True(t, sequenceObject(t, g, "a.note").IsSequenceDiagramNote())
}

func TestSequenceV2HoistedAttributeOrder(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`first: {shape: edge-group; a: Early {style.fill: red}; a -> b}
 a: Later {style.fill: blue}`,
		`a: Early {style.fill: red}
 first: {shape: edge-group; a: Later {style.fill: blue}; a -> b}`,
		`first: {shape: edge-group; a: Early {style.fill: red}; a -> b}
 a: Middle {style.fill: green}
 second: {shape: edge-group; a: Later {style.fill: blue}; a -> b}`,
	} {
		g := compileSequence(t, "shape: sequence-diagram\n"+source)
		a := sequenceObject(t, g, "a")
		require.Equal(t, "Later", a.Label.Value)
		require.Equal(t, "blue", a.Style.Fill.Value)
	}
}

func TestSequenceV2ClassShapes(t *testing.T) {
	t.Parallel()
	g := compileSequence(t, `classes: {
 diagram: {shape: sequence-diagram {mirrored: true}}
 numbered: {shape.numbered: true}
 messages: {shape: edge-group}
 participants: {shape: actor-group}
}
class: [diagram; numbered]
shape.mirrored: false
team: {class: participants; a}
handshake: {class: messages; team.a -> b}
`)
	require.True(t, g.Root.IsSequenceDiagramV2())
	require.Equal(t, &d2graph.SequenceOptions{Numbered: true}, g.Root.Sequence)
	require.True(t, sequenceObject(t, g, "handshake").IsSequenceDiagramGroup())
	require.True(t, sequenceObject(t, g, "team.a").IsSequenceDiagramActor())
	require.Same(t, sequenceObject(t, g, "team.a"), g.Edges[0].Src)
	require.Same(t, sequenceObject(t, g, "b"), g.Edges[0].Dst)
}

func TestSequenceV2HoistedActorMessages(t *testing.T) {
	t.Parallel()
	g := compileSequence(t, `shape: sequence-diagram
first: {shape: edge-group; a: {work -> _.b: first}}
second: {shape: edge-group; a: {work -> _.b: second}}
`)
	require.Len(t, g.Edges, 2)
	require.Equal(t, "first", g.Edges[0].Label.Value)
	require.Equal(t, "second", g.Edges[1].Label.Value)
	require.Equal(t, 0, g.Edges[0].Index)
	require.Equal(t, 1, g.Edges[1].Index)
	require.True(t, g.Edges[0].ContainedBy(sequenceObject(t, g, "first")))
	require.True(t, g.Edges[1].ContainedBy(sequenceObject(t, g, "second")))
}

func TestSequenceV2ImportOrder(t *testing.T) {
	t.Parallel()
	g, _, err := d2compiler.Compile("root.d2", strings.NewReader(`shape: sequence-diagram
shape.numbered: true
a -> b: Before
...@messages
c -> a: After
`), &d2compiler.CompileOptions{FS: fstest.MapFS{
		"messages.d2": &fstest.MapFile{Data: []byte(`b -> c: Imported
a.note: At import {shape: page}`)},
	}})
	require.NoError(t, err)
	labels := make(map[string]*d2graph.Edge)
	for _, edge := range g.Edges {
		labels[edge.Label.Value] = edge
	}
	require.Contains(t, labels, "1. Before")
	require.Contains(t, labels, "2. Imported")
	require.Contains(t, labels, "3. After")
	require.Negative(t, slices.Compare(labels["2. Imported"].SequenceOrder, sequenceObject(t, g, "a.note").SequenceOrder))
	require.Negative(t, slices.Compare(sequenceObject(t, g, "a.note").SequenceOrder, labels["3. After"].SequenceOrder))
}

func TestSequenceV2ImportedAttributeOrder(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"shape: sequence-diagram\n...@early\na: Later {style.fill: blue}",
		"shape: sequence-diagram\na: Early {style.fill: red}\n...@late",
		"shape: sequence-diagram\n...@early\na: Middle {style.fill: green}\n...@late",
	} {
		g, _, err := d2compiler.Compile("root.d2", strings.NewReader(source), &d2compiler.CompileOptions{FS: fstest.MapFS{
			"early.d2": &fstest.MapFile{Data: []byte(`g: {shape: edge-group; a: Early {style.fill: red}; a -> b}`)},
			"late.d2":  &fstest.MapFile{Data: []byte(`h: {shape: edge-group; a: Later {style.fill: blue}; a -> b}`)},
		}})
		require.NoError(t, err)
		require.Equal(t, "Later", sequenceObject(t, g, "a").Label.Value)
		require.Equal(t, "blue", sequenceObject(t, g, "a").Style.Fill.Value)
	}
}

func TestSequenceV2SubstitutedAttributeOrder(t *testing.T) {
	t.Parallel()
	g := compileSequence(t, `shape: sequence-diagram
vars: {later: Later}
a: Early
first: {shape: edge-group; a: ${later}; a -> b}
a: {style.fill: blue}
`)
	require.Equal(t, "Later", sequenceObject(t, g, "a").Label.Value)
	require.Equal(t, "blue", sequenceObject(t, g, "a").Style.Fill.Value)
}

func TestSequenceV2GroupNameEndpoint(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`shape: sequence-diagram; g: {shape: edge-group; g -> b}`,
		`shape: sequence-diagram; g: {shape: edge-group; _.g -> b}`,
		`shape: sequence-diagram; g: {shape: edge-group; b -> g}`,
		`shape: sequence-diagram; g: {shape: edge-group; nested: {shape: edge-group; g -> b}}`,
	} {
		t.Run(source, func(t *testing.T) {
			_, _, err := d2compiler.Compile("sequence.d2", strings.NewReader(source), nil)
			require.ErrorContains(t, err, "can only connect actors or spans")
		})
	}
	_, _, err := d2compiler.Compile("sequence.d2", strings.NewReader(`shape: sequence-diagram
 g: {shape: edge-group; g: {shape: circle}; g -> b}`), nil)
	require.ErrorContains(t, err, "conflicts with an edge-group")
	g := compileSequence(t, `shape: sequence-diagram
messages: {shape: edge-group; a: {work -> _.a}}
`)
	require.Len(t, g.Edges, 1)
	require.Same(t, sequenceObject(t, g, "a.work"), g.Edges[0].Src)
	require.Same(t, sequenceObject(t, g, "a"), g.Edges[0].Dst)
}

func TestSequenceV2StructuredActorTimeline(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ shape, members string }{
		{"sql_table", "id: int"},
		{"class", "+name: string; +method()"},
	} {
		t.Run(tt.shape, func(t *testing.T) {
			g := compileSequence(t, `shape: sequence-diagram
 a: Actor {shape: `+tt.shape+`; `+tt.members+`}
 a -> b
 a.repeat: {shape: actor}
 a.note: Hello {shape: page}
 a.event: Retry {shape: diamond}
`)
			a := sequenceObject(t, g, "a")
			require.Len(t, a.ChildrenArray, 3)
			require.True(t, sequenceObject(t, g, "a.repeat").IsSequenceDiagramActorRepeat())
			require.True(t, sequenceObject(t, g, "a.note").IsSequenceDiagramNote())
			require.True(t, sequenceObject(t, g, "a.event").IsSequenceDiagramEvent())
			if tt.shape == "sql_table" {
				require.Len(t, a.SQLTable.Columns, 1)
				require.Equal(t, "id", a.SQLTable.Columns[0].Name.Label)
			} else {
				require.Len(t, a.Class.Fields, 1)
				require.Len(t, a.Class.Methods, 1)
			}
		})
	}
}

func TestSequenceV2BranchedGlob(t *testing.T) {
	g := compileSequence(t, `shape: sequence-diagram
*: {&shape: edge-group; &shape.branched: false; shape.branched: true}
decision: {shape: edge-group; yes: {a -> b: Done}}
branched: ordinary actor`)
	require.True(t, sequenceObject(t, g, "decision.yes").IsSequenceDiagramBranch())
	require.True(t, sequenceObject(t, g, "branched").IsSequenceDiagramActor())
	require.Equal(t, "a", g.Edges[0].Src.AbsID())
	require.Equal(t, "b", g.Edges[0].Dst.AbsID())
}

func TestSequenceV2SpreadActorPrecedence(t *testing.T) {
	t.Parallel()
	for _, root := range []struct{ declarations, label, fill string }{
		{"a: Local {style.fill: red}\n...${update}", "Grouped", "blue"},
		{"...${update}\na: Local {style.fill: red}", "Local", "red"},
	} {
		g := compileSequence(t, "shape: sequence-diagram\n"+`vars: {update: {g: {shape: edge-group; a: Grouped {style.fill: blue}; a -> b}}}
`+root.declarations)
		a := sequenceObject(t, g, "a")
		require.Equal(t, root.label, a.Label.Value)
		require.Equal(t, root.fill, a.Style.Fill.Value)
		require.Equal(t, root.label, a.Label.MapKey.Primary.Unbox().ScalarString())
	}
}

func TestSequenceV2MaterializedFrameMembership(t *testing.T) {
	t.Parallel()
	for _, frame := range []string{"@frame", "${frame}"} {
		t.Run(frame, func(t *testing.T) {
			const body = `shape: edge-group {branched: true}
ready: {a.note: Need input {shape: page}; a: {work -> _.b: Ask}}`
			src := "shape: sequence-diagram\nvars: {frame: {" + body + "}}\na\nop: " + frame
			g, _, err := d2compiler.Compile("root.d2", strings.NewReader(src), &d2compiler.CompileOptions{FS: fstest.MapFS{
				"frame.d2": &fstest.MapFile{Data: []byte(body)},
			}})
			require.NoError(t, err)
			branch := sequenceObject(t, g, "op.ready")
			require.True(t, sequenceObject(t, g, "a.note").ContainedBy(branch))
			require.Len(t, g.Edges, 1)
			require.True(t, g.Edges[0].ContainedBy(branch))
		})
	}
}

func TestNestedSequenceActorError(t *testing.T) {
	t.Parallel()
	for _, shape := range []string{"sequence-diagram", "sequence_diagram"} {
		src := "shape: sequence-diagram\nremote -> outer\ninner: {shape: " + shape + "; group: {a -> b}}"
		_, _, err := d2compiler.Compile("sequence.d2", strings.NewReader(src), nil)
		require.ErrorContains(t, err, "sequence.d2:3:1: actors in sequence diagrams cannot themselves be sequence diagrams: inner")
	}
}

func TestSequenceV2InheritedBranchOptions(t *testing.T) {
	t.Parallel()
	g := compileSequence(t, `classes: {
  frame: {shape: edge-group}
  split: {shape.branched: true}
  colored: {style.fill: blue}
  ordinary: {shape.branched: false}
}
shape: sequence-diagram
branchedFrame: {class: [frame; colored; split]; yes: {a -> b}}
plainFrame: {class: [frame; split; ordinary]; c -> d}
`)
	require.True(t, sequenceObject(t, g, "branchedFrame.yes").IsSequenceDiagramBranch())
	require.False(t, sequenceObject(t, g, "plainFrame").Sequence.Branched)
	require.True(t, sequenceObject(t, g, "c").IsSequenceDiagramActor())
}

func TestCaptionFillNullInReusableStyles(t *testing.T) {
	ordinary := compileSequence(t, `vars: {label: {style.fill: null}}
app: {style.fill: red; ...${label}}`)
	require.Nil(t, sequenceObject(t, ordinary, "app").Style.Fill)

	// The same variable can clear an ordinary actor fill and an inherited
	// frame caption. Its name or its position in vars cannot choose which.
	g := compileSequence(t, `vars: {label: {style.fill: null}}
shape: sequence-diagram
classes: {tinted: {label.style.fill: green}}
a: {style.fill: red; ...${label}}
frame: {shape: edge-group; class: tinted; label: {...${label}}; a -> b}`)
	require.Nil(t, sequenceObject(t, g, "a").Style.Fill)
	require.Nil(t, sequenceObject(t, g, "frame").LabelFill)
}

func TestSequenceGroupedNulls(t *testing.T) {
	t.Parallel()
	const start = `shape: sequence-diagram {numbered: true}
a
frame: {shape: edge-group; a.n: Canceled {shape: page}; a.style.fill: red; a -> b: Old}
`
	for _, input := range []struct {
		edit string
		fs   fstest.MapFS
	}{
		{"a.n: null\na.style.fill: null", nil},
		{"clear: {shape: edge-group; a.n: null; a.style.fill: null}", nil},
		{"vars: {clean: {a.n: null; a.style.fill: null}}\n...${clean}", nil},
		{"...@clean", fstest.MapFS{"clean.d2": {Data: []byte("a.n: null\na.style.fill: null")}}},
	} {
		g, _, err := d2compiler.Compile("sequence.d2", strings.NewReader(start+input.edit), &d2compiler.CompileOptions{FS: input.fs})
		require.NoError(t, err)
		_, found := g.Root.HasChild([]string{"a", "n"})
		require.False(t, found)
		require.Nil(t, sequenceObject(t, g, "a").Style.Fill)
		require.Len(t, g.Edges, 1)
	}

	g := compileSequence(t, start+`steps: {replace: {
  a: null
  restored: {shape: edge-group; a -> b: New}
  a.n: Restored {shape: page}
}}`)
	require.Equal(t, "Canceled", sequenceObject(t, g, "a.n").Label.Value)
	require.Equal(t, "1. Old", g.Edges[0].Label.Value)
	step := g.Steps[0]
	require.Equal(t, "Restored", sequenceObject(t, step, "a.n").Label.Value)
	require.Nil(t, sequenceObject(t, step, "a").Style.Fill)
	require.Len(t, step.Edges, 1)
	require.Equal(t, "1. New", step.Edges[0].Label.Value)
}

func TestSequenceNestedFrameDeletionKeepsActor(t *testing.T) {
	t.Parallel()
	for _, decl := range []string{"shape: edge-group", "class: frame"} {
		src := "shape: sequence-diagram\nclasses: {frame: {shape: edge-group}}\n" +
			"a: Actor {shape: diamond}\ng: {shape: edge-group; a: {" + decl + "; x -> y}; a: null}\na -> b"
		g := compileSequence(t, src)
		require.Equal(t, "Actor", sequenceObject(t, g, "a").Label.Value)
		require.Equal(t, "diamond", sequenceObject(t, g, "a").Shape.Value)
		require.Len(t, g.Edges, 1)
	}
}
