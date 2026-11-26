package e2etests

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"

	trequire "github.com/stretchr/testify/require"

	"github.com/d2lang/util-go/assert"
	"github.com/d2lang/util-go/diff"
	"github.com/d2lang/util-go/go2"
	"github.com/d2lang/util-go/xjson"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2layouts/d2elklayout"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2animate"
	"github.com/d2lang/d2/d2renderers/d2ascii"
	"github.com/d2lang/d2/d2renderers/d2ascii/charset"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/internal/d2layoutfeatures"
	"github.com/d2lang/d2/internal/testlog"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestE2E(t *testing.T) {
	t.Parallel()

	// Every case is an independent parallel subtest. Start the groups together
	// so the suite is bounded by its total work rather than by the slowest case
	// of each group in turn.
	for _, group := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"sanity", testSanity},
		{"stable", testStable},
		{"real_world", testRealWorld},
		{"regression", testRegression},
		{"patterns", testPatterns},
		{"todo", testTodo},
		{"measured", testMeasured},
		{"unicode", testUnicode},
		{"root", testRoot},
		{"themes", testThemes},
		{"txtar", testTxtar},
		{"asciitxtar", testASCIITxtar},
	} {
		t.Run(group.name, func(t *testing.T) {
			t.Parallel()
			group.run(t)
		})
	}
}

func testSanity(t *testing.T) {
	tcs := []testCase{
		{
			name:   "empty",
			script: ``,
		},
		{
			name: "basic",
			script: `a -> b
`,
		},
		{
			name: "1 to 2",
			script: `a -> b
a -> c
`,
		},
		{
			name: "child to child",
			script: `a.b -> c.d
`,
		},
		{
			name: "connection label",
			script: `a -> b: hello
`,
		},
	}
	runa(t, tcs)
}

func testTxtar(t *testing.T) {
	var tcs []testCase
	archive, err := txtar.ParseFile("./txtar.txt")
	assert.Success(t, err)
	for _, f := range archive.Files {
		tcs = append(tcs, testCase{
			name:   f.Name,
			script: string(f.Data),
		})
	}
	runa(t, tcs)
}

func testASCIITxtar(t *testing.T) {
	archive, err := txtar.ParseFile("./asciitxtar.txt")
	assert.Success(t, err)

	for _, f := range archive.Files {
		tc := testCase{
			name:   f.Name,
			script: string(f.Data),
		}

		t.Run(tc.name, func(t *testing.T) {
			if tc.skip {
				t.Skip()
			}
			t.Parallel()

			runASCIITxtarTest(t, tc)
		})
	}
}

func runASCIITxtarTest(t *testing.T, tc testCase) {
	ctx := context.Background()
	ctx = log.With(ctx, testlog.New(t))
	ctx = log.Leveled(ctx, slog.LevelDebug)

	ruler, err := textmeasure.NewRuler()
	trequire.Nil(t, err)

	serde(t, tc, ruler)

	layoutResolver := func(engine string) (d2graph.LayoutGraph, error) {
		return d2elklayout.DefaultLayout, nil
	}

	compileOpts := &d2lib.CompileOptions{
		Ruler:          ruler,
		Layout:         go2.Pointer("elk"),
		LayoutResolver: layoutResolver,
		LayoutReuse:    true,
	}
	renderOpts := &d2svg.RenderOpts{
		Pad:     go2.Pointer(int64(0)),
		ThemeID: tc.themeID,
	}

	formatCompileOpts, formatRenderOpts := *compileOpts, *renderOpts
	diagram, g, err := d2lib.Compile(ctx, tc.script, compileOpts, renderOpts)
	if tc.expErr != "" {
		assert.Error(t, err)
		assert.ErrorString(t, err, tc.expErr)
		return
	}
	assert.Success(t, err)

	checkFormattedDiagram(t, ctx, tc.script, diagram, formatCompileOpts, formatRenderOpts)
	err = d2layoutfeatures.Check("elk", g)
	if tc.elkFeatureError != "" {
		assert.Error(t, err)
		assert.ErrorString(t, err, tc.elkFeatureError)
		return
	}
	assert.Success(t, err)

	if tc.assertions != nil {
		t.Run("assertions", func(t *testing.T) {
			tc.assertions(t, diagram)
		})
	}

	// Generate master ID for multiboard rendering
	if len(diagram.Layers) > 0 || len(diagram.Scenarios) > 0 || len(diagram.Steps) > 0 {
		masterID, err := diagram.HashID(nil)
		assert.Success(t, err)
		renderOpts.MasterID = masterID
	}

	// Render SVG
	boards, err := d2svg.RenderMultiboard(diagram, renderOpts)
	assert.Success(t, err)

	var svgBytes []byte
	if len(boards) == 1 {
		svgBytes = boards[0]
	} else {
		svgBytes, err = d2animate.Wrap(diagram, boards, *renderOpts, 1000)
		assert.Success(t, err)
	}

	// Check that it's valid SVG
	var xmlParsed interface{}
	err = xml.Unmarshal(svgBytes, &xmlParsed)
	assert.Success(t, err)
	assert.False(t, strings.Contains(string(svgBytes), "<foreignObject"))

	// Output files to asciitxtar subdirectory for each test
	testName := strings.TrimPrefix(t.Name(), "TestE2E/asciitxtar/")
	outputDir := filepath.Join("testdata", "asciitxtar", testName)

	err = os.MkdirAll(outputDir, 0755)
	assert.Success(t, err)

	// Write SVG file
	var err2, err3 error
	if os.Getenv("SKIP_SVG_CHECK") == "" {
		err2 = testdata(filepath.Join(outputDir, "sketch"), ".svg", svgBytes)
	}
	extendedAsciiArtist := d2ascii.NewASCIIartist()

	// Extended (Unicode) ASCII
	extendedRenderOpts := &d2ascii.RenderOpts{
		Scale:   renderOpts.Scale,
		Charset: charset.Unicode,
	}
	extendedBytes, err := extendedAsciiArtist.Render(ctx, diagram, extendedRenderOpts)
	assert.Success(t, err)
	err3 = testdata(filepath.Join(outputDir, "extended"), ".txt", extendedBytes)

	// Standard ASCII
	var err4 error
	standardAsciiArtist := d2ascii.NewASCIIartist()
	standardRenderOpts := &d2ascii.RenderOpts{
		Scale:   renderOpts.Scale,
		Charset: charset.ASCII,
	}
	standardBytes, err := standardAsciiArtist.Render(ctx, diagram, standardRenderOpts)
	assert.Success(t, err)
	err4 = testdata(filepath.Join(outputDir, "standard"), ".txt", standardBytes)

	assert.Success(t, err2)
	assert.Success(t, err3)
	assert.Success(t, err4)
}

// testdata checks got against the golden path.exp<ext>.
//
// diff.Testdata writes path.got<ext> and runs `git diff` for every comparison,
// which costs a process spawn per golden. Nearly every golden matches, so
// compare the bytes first and only fall back to diff.Testdata, which reports
// the diff and implements $TESTDATA_ACCEPT, when they differ.
func testdata(path, ext string, got []byte) error {
	exp, err := os.ReadFile(path + ".exp" + ext)
	if err != nil || !bytes.Equal(exp, got) {
		return diff.Testdata(path, ext, got)
	}
	// diff.Testdata removes the got file once it matches the golden.
	if err := os.Remove(path + ".got" + ext); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// testdataJSON is diff.TestdataJSON on top of testdata.
func testdataJSON(path string, got interface{}) error {
	return testdata(path, ".json", append(xjson.Marshal(got), '\n'))
}

type testCase struct {
	name string
	// If the test is just exercising rendering or styling, the default layout is sufficient.
	justDagre         bool
	testSerialization bool
	script            string
	mtexts            []*d2target.MText
	assertions        func(t *testing.T, diagram *d2target.Diagram)
	skip              bool
	dagreFeatureError string
	elkFeatureError   string
	talaLayoutError   string
	expErr            string
	themeID           *int64
}

func runa(t *testing.T, tcs []testCase) {
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.skip {
				t.Skip()
			}
			t.Parallel()

			run(t, tc)
		})
	}
}

// serde exercises serializing and deserializing the graph
// We want to run all the steps leading up to serialization in the course of regular layout
func serde(t *testing.T, tc testCase, ruler *textmeasure.Ruler) {
	g, _, err := d2compiler.Compile("", strings.NewReader(tc.script), &d2compiler.CompileOptions{
		UTF16Pos: false,
	})
	trequire.Nil(t, err)
	if len(g.Objects) > 0 {
		err = g.SetDimensions(nil, ruler, nil, nil)
		trequire.Nil(t, err)
	}
	b, err := d2graph.SerializeGraph(g)
	trequire.Nil(t, err)
	var newG d2graph.Graph
	err = d2graph.DeserializeGraph(b, &newG)
	trequire.Nil(t, err)
	roundTripBytes, err := d2graph.SerializeGraph(&newG)
	trequire.Nil(t, err)
	trequire.JSONEq(t, string(b), string(roundTripBytes))
}

func run(t *testing.T, tc testCase) {
	ctx := context.Background()
	ctx = log.With(ctx, testlog.New(t))
	ctx = log.Leveled(ctx, slog.LevelDebug)

	var ruler *textmeasure.Ruler
	var err error
	if tc.mtexts == nil {
		ruler, err = textmeasure.NewRuler()
		trequire.Nil(t, err)

		serde(t, tc, ruler)
	}

	layoutsTested := []string{"dagre"}
	if !tc.justDagre {
		layoutsTested = append(layoutsTested, "elk", "tala")
	}
	talaOpts := d2talalayout.DefaultOptions()
	// One deterministic attempt keeps the full cross-engine E2E matrix tractable.
	// Multi-seed selection is covered by the TALA package and built-in engine tests.
	talaOpts.Seeds = []int64{1}
	talaOpts.MaxConcurrency = 1

	layoutResolver := func(engine string) (d2graph.LayoutGraph, error) {
		layout := d2dagrelayout.DefaultLayout
		if strings.EqualFold(engine, "elk") {
			layout = d2elklayout.DefaultLayout
		} else if strings.EqualFold(engine, "tala") {
			layout = func(ctx context.Context, g *d2graph.Graph) error {
				return d2talalayout.Layout(ctx, g, &talaOpts)
			}
		}
		if tc.testSerialization {
			return func(ctx context.Context, g *d2graph.Graph) error {
				bytes, err := d2graph.SerializeGraph(g)
				if err != nil {
					return err
				}
				err = d2graph.DeserializeGraph(bytes, g)
				if err != nil {
					return err
				}
				err = layout(ctx, g)
				if err != nil {
					return err
				}
				bytes, err = d2graph.SerializeGraph(g)
				if err != nil {
					return err
				}
				return d2graph.DeserializeGraph(bytes, g)
			}, nil
		}
		return layout, nil
	}
	routerResolver := func(engine string) (d2graph.RouteEdges, error) {
		if strings.EqualFold(engine, "tala") {
			return d2talalayout.RouteEdges, nil
		}
		return nil, nil
	}

	for _, layoutName := range layoutsTested {
		// If measured texts exist, we are specifically exercising text
		// measurements, so the default layout is sufficient.
		if tc.mtexts != nil && layoutName != "dagre" {
			continue
		}

		compileOpts := &d2lib.CompileOptions{
			Ruler:          ruler,
			MeasuredTexts:  tc.mtexts,
			Layout:         go2.Pointer(layoutName),
			LayoutResolver: layoutResolver,
			RouterResolver: routerResolver,
			LayoutReuse:    true,
		}
		renderOpts := &d2svg.RenderOpts{
			Pad:     go2.Pointer(int64(0)),
			ThemeID: tc.themeID,
			// To compare deltas at a fixed scale
			// Scale: go2.Pointer(1.),
		}

		formatCompileOpts, formatRenderOpts := *compileOpts, *renderOpts
		diagram, g, err := d2lib.Compile(ctx, tc.script, compileOpts, renderOpts)
		if layoutName == "tala" && tc.talaLayoutError != "" {
			assert.Error(t, err)
			assert.ErrorString(t, err, tc.talaLayoutError)
			return
		}
		if tc.expErr != "" {
			assert.Error(t, err)
			assert.ErrorString(t, err, tc.expErr)
			return
		} else {
			assert.Success(t, err)
		}

		if layoutName == "dagre" {
			checkFormattedDiagram(t, ctx, tc.script, diagram, formatCompileOpts, formatRenderOpts)
		}
		err = d2layoutfeatures.Check(layoutName, g)
		switch layoutName {
		case "dagre":
			if tc.dagreFeatureError != "" {
				assert.Error(t, err)
				assert.ErrorString(t, err, tc.dagreFeatureError)
				return
			}
		case "elk":
			if tc.elkFeatureError != "" {
				assert.Error(t, err)
				assert.ErrorString(t, err, tc.elkFeatureError)
				return
			}
		}
		assert.Success(t, err)

		if tc.assertions != nil {
			t.Run("assertions", func(t *testing.T) {
				tc.assertions(t, diagram)
			})
		}

		dataPath := filepath.Join("testdata", strings.TrimPrefix(t.Name(), "TestE2E/"), layoutName)
		pathGotSVG := filepath.Join(dataPath, "sketch.got.svg")

		if len(diagram.Layers) > 0 || len(diagram.Scenarios) > 0 || len(diagram.Steps) > 0 {
			masterID, err := diagram.HashID(nil)
			assert.Success(t, err)
			renderOpts.MasterID = masterID
		}
		boards, err := d2svg.RenderMultiboard(diagram, renderOpts)
		assert.Success(t, err)

		var svgBytes []byte
		if len(boards) == 1 {
			svgBytes = boards[0]
		} else {
			svgBytes, err = d2animate.Wrap(diagram, boards, *renderOpts, 1000)
			assert.Success(t, err)
		}

		err = os.MkdirAll(dataPath, 0755)
		assert.Success(t, err)
		err = os.WriteFile(pathGotSVG, svgBytes, 0600)
		assert.Success(t, err)

		// Check that it's valid SVG
		var xmlParsed interface{}
		err = xml.Unmarshal(svgBytes, &xmlParsed)
		assert.Success(t, err)
		assert.False(t, strings.Contains(string(svgBytes), "<foreignObject"))

		var err2 error
		err = testdataJSON(filepath.Join(dataPath, "board"), diagram)
		if os.Getenv("SKIP_SVG_CHECK") == "" {
			err2 = testdata(filepath.Join(dataPath, "sketch"), ".svg", svgBytes)
		}
		renderIsometric(t, ctx, diagram, dataPath, renderOpts)

		assert.Success(t, err)
		assert.Success(t, err2)
	}
}

func mdTestScript(md string) string {
	return fmt.Sprintf(`
md: |md
%s
|
a -> md -> b
`, md)
}

func loadFromFile(t *testing.T, name string) testCase {
	fn := filepath.Join("testdata", "files", fmt.Sprintf("%s.d2", name))
	d2Text, err := os.ReadFile(fn)
	if err != nil {
		t.Fatalf("failed to load test from file:%s. %s", name, err.Error())
	}

	return testCase{
		name:   name,
		script: string(d2Text),
	}
}

func loadFromFileWithOptions(t *testing.T, name string, options testCase) testCase {
	tc := options
	tc.name = name
	tc.script = loadFromFile(t, name).script
	return tc
}
