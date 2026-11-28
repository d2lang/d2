package d2exporter_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	tassert "github.com/stretchr/testify/assert"

	"github.com/d2lang/util-go/assert"
	"github.com/d2lang/util-go/diff"
	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2exporter"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2layouts/d2sequence"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/internal/testlog"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

type testCase struct {
	name string
	dsl  string

	assertions func(t *testing.T, d *d2target.Diagram)
}

func TestExport(t *testing.T) {
	t.Parallel()

	t.Run("shape", testShape)
	t.Run("connection", testConnection)
	t.Run("label", testLabel)
	t.Run("theme", testTheme)
	t.Run("legend", testLegend)
}

func testShape(t *testing.T) {
	tcs := []testCase{
		{
			name: "basic",
			dsl: `x
`,
		},
		{
			name: "synonyms",
			dsl: `x: {shape: circle}
y: {shape: square}
`,
		},
		{
			name: "text_color",
			dsl: `x: |md yo | { style.font-color: red }
`,
		},
		{
			name: "border-radius",
			dsl: `Square: "" { style.border-radius: 5 }
`,
		},
		{
			name: "image_dimensions",

			dsl: `hey: "" {
  icon: https://icons.terrastruct.com/essentials/004-picture.svg
  shape: image
	width: 200
	height: 230
}
`,
			assertions: func(t *testing.T, d *d2target.Diagram) {
				if d.Shapes[0].Width != 200 {
					t.Fatalf("expected width 200, got %v", d.Shapes[0].Width)
				}
				if d.Shapes[0].Height != 230 {
					t.Fatalf("expected height 230, got %v", d.Shapes[0].Height)
				}
			},
		},
		{
			name: "sequence_group_position",

			dsl: `hey {
  shape: sequence_diagram
	a
	b
  group: {
    a -> b
  }
}
`,
			assertions: func(t *testing.T, d *d2target.Diagram) {
				tassert.Equal(t, "hey.group", d.Shapes[3].ID)
				tassert.Equal(t, "INSIDE_TOP_LEFT", d.Shapes[3].LabelPosition)
			},
		},
	}

	runa(t, tcs)
}

func testConnection(t *testing.T) {
	tcs := []testCase{
		{
			name: "basic",
			dsl: `x -> y
`,
		},
		{
			name: "stroke-dash",
			dsl: `x -> y: { style.stroke-dash: 4 }
`,
		},
		{
			name: "arrowhead",
			dsl: `x -> y: {
  source-arrowhead: If you've done six impossible things before breakfast, why not round it
  target-arrowhead: {
    label: A man with one watch knows what time it is.
    shape: diamond
    style.filled: true
  }
}
`,
		},
		{
			// This is a regression test where a connection with stroke-dash of 0 on terrastruct flagship theme would have a diff color
			// than a connection without stroke dash
			name: "theme_stroke-dash",
			dsl: `x -> y: { style.stroke-dash: 0 }
x -> y
`,
		},
	}

	runa(t, tcs)
}

func testLabel(t *testing.T) {
	tcs := []testCase{
		{
			name: "basic_shape",
			dsl: `x: yo
`,
		},
		{
			name: "shape_font_color",
			dsl: `x: yo { style.font-color: red }
`,
		},
		{
			name: "connection_font_color",
			dsl: `x -> y: yo { style.font-color: red }
`,
		},
	}

	runa(t, tcs)
}

func testTheme(t *testing.T) {
	tcs := []testCase{
		{
			name: "shape_without_bold",
			dsl: `x: {
	style.bold: false
}
`,
		},
		{
			name: "shape_with_italic",
			dsl: `x: {
	style.italic: true
}
`,
		},
		{
			name: "connection_without_italic",
			dsl: `x -> y: asdf { style.italic: false }
`,
		},
		{
			name: "connection_with_italic",
			dsl: `x -> y: asdf {
  style.italic: true
}
`,
		},
		{
			name: "connection_with_bold",
			dsl: `x -> y: asdf {
  style.bold: true
}
`,
		},
	}

	runa(t, tcs)
}

func testLegend(t *testing.T) {
	tcs := []testCase{
		{
			name: "basic_legend",
			dsl: `vars: {
  d2-legend: {
    legend: {
      l1: Rectangles {shape: rectangle}
      l2: Ovals {shape: oval}
      l1 -> l2: Connection
		}
	}
}
x: {shape: rectangle}
y: {shape: oval}
x -> y: connects

`,
		},
	}

	runa(t, tcs)
}

func runa(t *testing.T, tcs []testCase) {
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			run(t, tc)
		})
	}
}

func run(t *testing.T, tc testCase) {
	ctx := context.Background()
	ctx = log.With(ctx, testlog.New(t))
	ctx = log.Leveled(ctx, slog.LevelDebug)

	g, config, err := d2compiler.Compile("", strings.NewReader(tc.dsl), &d2compiler.CompileOptions{
		UTF16Pos: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	ruler, err := textmeasure.NewRuler()
	assert.JSON(t, nil, err)

	err = g.SetDimensions(nil, ruler, nil, nil)
	assert.JSON(t, nil, err)

	graphInfo := d2layouts.NestedGraphInfo(g.Root)
	err = d2layouts.LayoutNested(ctx, g, graphInfo, d2dagrelayout.DefaultLayout, d2layouts.DefaultRouter)
	if err != nil {
		t.Fatal(err)
	}

	got, err := d2exporter.Export(ctx, g, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		got.Config = config
	}

	if tc.assertions != nil {
		t.Run("assertions", func(t *testing.T) {
			tc.assertions(t, got)
		})
	}

	// This test is agnostic of layout changes
	for i := range got.Shapes {
		got.Shapes[i].Pos = d2target.Point{}
		got.Shapes[i].Width = 0
		got.Shapes[i].Height = 0
		got.Shapes[i].LabelWidth = 0
		got.Shapes[i].LabelHeight = 0
		got.Shapes[i].LabelPosition = ""
	}
	for i := range got.Connections {
		got.Connections[i].Route = []*geo.Point{}
		got.Connections[i].LabelWidth = 0
		got.Connections[i].LabelHeight = 0
		got.Connections[i].LabelPosition = ""
	}

	err = diff.TestdataJSON(filepath.Join("..", "testdata", "d2exporter", t.Name()), got)
	assert.Success(t, err)
}

// TestHashID tests that 2 diagrams with different theme configs do not equal each other
func TestHashID(t *testing.T) {
	ctx := context.Background()
	ctx = log.With(ctx, testlog.New(t))
	ctx = log.Leveled(ctx, slog.LevelDebug)

	aString := `
vars: {
  d2-config: {
    theme-id: 3
  }
}
a -> b
`

	bString := `
vars: {
  d2-config: {
    theme-id: 4
  }
}
a -> b
`

	da, err := compile(ctx, aString)
	assert.JSON(t, nil, err)

	db, err := compile(ctx, bString)
	assert.JSON(t, nil, err)

	hashA, err := da.HashID(nil)
	assert.JSON(t, nil, err)

	hashB, err := db.HashID(nil)
	assert.JSON(t, nil, err)

	assert.NotEqual(t, hashA, hashB)
}

func layoutResolver(engine string) (d2graph.LayoutGraph, error) {
	return d2dagrelayout.DefaultLayout, nil
}

func compile(ctx context.Context, d2 string) (*d2target.Diagram, error) {
	ruler, _ := textmeasure.NewRuler()
	opts := &d2lib.CompileOptions{
		Ruler:          ruler,
		LayoutResolver: layoutResolver,
		Layout:         go2.Pointer("dagre"),
	}
	d, _, e := d2lib.Compile(ctx, d2, opts, nil)
	return d, e
}

func TestSequencePlayback(t *testing.T) {
	for _, tc := range []struct {
		name     string
		source   string
		ranks    map[string]int
		duration int
	}{
		{
			name: "root",
			source: `shape: sequence_diagram
style.animated: true
a; b
a -> b: request
group: {
  b -> a: hidden {style.opacity: 0}
  a."checking the request"
  a <- b: answer
}
b -> a
a -> a: finish`,
			ranks: map[string]int{"request": 1, "hidden": 0, "answer": 3, "": 4, "finish": 5}, duration: 6000,
		},
		{
			name: "nested",
			source: `outer.buy: {
  shape: sequence_diagram
  style.animated: true
  a -> b: first
  b -> a: second
}
outer.ship: {
  shape: sequence_diagram
  style.animated: true
  c -> d: parallel
}
still: {
  shape: sequence_diagram
  style.animated: false
  a -> b: static
}
external.style.animated: true
outer.buy.a -> external: outside`,
			ranks: map[string]int{"first": 1, "second": 2, "parallel": 1, "static": 0, "outside": 0}, duration: 3000,
		},
		{
			name:     "no visible messages",
			source:   "shape: sequence_diagram\nstyle.animated: true\na -> b: hidden {style.opacity: 0}",
			ranks:    map[string]int{"hidden": 0},
			duration: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := compile(log.With(t.Context(), testlog.New(t)), tc.source)
			if err != nil {
				t.Fatal(err)
			}
			ranks := make(map[string]int)
			for _, c := range d.Connections {
				if c.ZIndex == d2sequence.LIFELINE_Z_INDEX {
					tassert.Zero(t, c.SequenceStep, c.ID)
					continue
				}
				ranks[c.Label] = c.SequenceStep
			}
			tassert.Equal(t, tc.ranks, ranks)
			tassert.Equal(t, tc.duration, d.SequencePlaybackDurationMS())
			for _, s := range d.Shapes {
				tassert.Equal(t, s.ID == "external", s.Animated, s.ID)
			}
			tassert.NoError(t, d2target.ValidateRenderTarget(d))
		})
	}
}

func TestSequenceStructurePlayback(t *testing.T) {
	d, err := compile(log.With(t.Context(), testlog.New(t)), `nested: {
  shape: sequence_diagram
  animated: true
  customer; api; db
  customer -> api: Connect
  work: {
    stage: {
      customer -> api.job: Submit
      api.job -> db.txn: Save
      api.job."Stored"
      api.job <- db.txn: Receipt
    }
    api.job -> api.job.log: Record
  }
  after: {customer."Done"; customer -> api: Close; api."Disconnected"}
  closing: {inside: {customer."Goodbye"}}
}`)
	if err != nil {
		t.Fatal(err)
	}
	tassert.NoError(t, d2target.ValidateRenderTarget(d))
	wantStep := map[string]int{
		"work": 2, "work.stage": 2, "api.job": 2, "db.txn": 3, `api.job.Stored`: 4, "api.job.log": 6,
		"after": 7, "customer.Done": 7, "api.Disconnected": 9, "closing": 10, "closing.inside": 10, "customer.Goodbye": 10,
	}
	for i := range d.Shapes {
		s := &d.Shapes[i]
		id := strings.TrimPrefix(s.ID, "nested.")
		tassert.Equal(t, wantStep[id], s.SequenceStep, s.ID)
		delete(wantStep, id)
		switch id {
		case "api.job", "api.job.log", "db.txn":
			tassert.True(t, s.SequenceAtArrival, s.ID)
		default:
			tassert.False(t, s.SequenceAtArrival, s.ID)
		}
		if id == "work" || id == "work.stage" || id == "api.job" || id == "db.txn" {
			if len(s.SequenceGrowth) < 2 {
				t.Errorf("missing growing extent for %s: %+v", s.ID, s.SequenceGrowth)
				continue
			}
			tassert.Less(t, s.SequenceGrowth[0].Height, s.Height)
			tassert.Equal(t, s.Height, s.SequenceGrowth[len(s.SequenceGrowth)-1].Height)
		}
	}
	tassert.Empty(t, wantStep)
	shapes := make(map[string]*d2target.Shape)
	for i := range d.Shapes {
		shapes[d.Shapes[i].ID] = &d.Shapes[i]
	}
	for _, pair := range [][2]string{{"work", "work.stage"}, {"closing", "closing.inside"}, {"api.job", "api.job.log"}} {
		parent, child := shapes["nested."+pair[0]], shapes["nested."+pair[1]]
		for ms := 0; ms < d.SequencePlaybackDurationMS(); ms += 50 {
			childHeight := child.SequenceHeightAt(ms)
			if childHeight > 0 && float64(parent.Pos.Y)+parent.SequenceHeightAt(ms) < float64(child.Pos.Y)+childHeight {
				t.Fatalf("%s extends beyond %s at %dms: %.1f > %.1f", child.ID, parent.ID, ms, float64(child.Pos.Y)+childHeight, float64(parent.Pos.Y)+parent.SequenceHeightAt(ms))
			}
		}
	}
	for _, c := range d.Connections {
		tassert.Equal(t, map[string]int{"Connect": 1, "Submit": 2, "Save": 3, "Receipt": 5, "Record": 6, "Close": 8}[c.Label], c.SequenceStep, c.ID)
	}
	tassert.Equal(t, 11000, d.SequencePlaybackDurationMS())
}

func TestNoteOnlySequenceWithHiddenStructure(t *testing.T) {
	d, err := compile(log.With(t.Context(), testlog.New(t)), `shape: sequence_diagram
animated: true
a; b
hidden: {a.job -> b: Secret {style.opacity: 0}}
notes: {a."First"; b."Second"}`)
	if err != nil {
		t.Fatal(err)
	}
	tassert.NoError(t, d2target.ValidateRenderTarget(d))
	tassert.Equal(t, 3000, d.SequencePlaybackDurationMS())
	for i := range d.Shapes {
		s := &d.Shapes[i]
		switch s.ID {
		case "notes", "a.First":
			tassert.Equal(t, 1, s.SequenceStep, s.ID)
			tassert.Equal(t, 0., s.SequenceHeightAt(0))
			tassert.Greater(t, s.SequenceHeightAt(1), 0.)
		case "b.Second":
			tassert.Equal(t, 2, s.SequenceStep, s.ID)
		case "a.job", "hidden":
			tassert.Equal(t, 2, s.SequenceStep, s.ID)
			tassert.Equal(t, 1650, s.SequenceStartMS(), s.ID)
		}
	}
}
