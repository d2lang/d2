//go:build js && wasm

package d2wasm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"syscall/js"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2layouts/d2elklayout"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2lsp"
	"github.com/d2lang/d2/d2oracle"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/d2renderers/d2animate"
	"github.com/d2lang/d2/d2renderers/d2ascii"
	"github.com/d2lang/d2/d2renderers/d2ascii/charset"
	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2renderers/d2svg/appendix"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/memfs"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/d2/lib/urlenc"
	"github.com/d2lang/d2/lib/version"
	"github.com/d2lang/util-go/go2"
)

const DEFAULT_INPUT_PATH = "index"

const (
	getObjOrderDeprecation = "d2.getObjOrder is deprecated and will be removed after one compatibility release; use d2oracle.GetObjOrder in Go integrations."
	getELKGraphDeprecation = `d2.getELKGraph is deprecated and will be removed after one compatibility release; use d2.compile with options.layout set to "elk".`
)

var (
	getObjOrderDeprecationOnce sync.Once
	getELKGraphDeprecationOnce sync.Once
)

func GetParentID(args []js.Value) (interface{}, error) {
	if len(args) < 1 {
		return nil, &WASMError{Message: "missing id argument", Code: 400}
	}

	id := args[0].String()
	mk, err := d2parser.ParseMapKey(id)
	if err != nil {
		return nil, &WASMError{Message: err.Error(), Code: 400}
	}

	if len(mk.Edges) > 0 {
		return "", nil
	}

	if mk.Key != nil {
		if len(mk.Key.Path) == 1 {
			return "root", nil
		}
		mk.Key.Path = mk.Key.Path[:len(mk.Key.Path)-1]
		return strings.Join(mk.Key.StringIDA(), "."), nil
	}

	return "", nil
}

// GetObjOrder returns the breadth-first object order for D2 source passed by a
// raw WASM caller.
//
// Deprecated: use d2oracle.GetObjOrder in Go integrations. The raw WASM export
// will be removed after one compatibility release.
func GetObjOrder(args []js.Value) (interface{}, error) {
	warnDeprecatedWASMCall(&getObjOrderDeprecationOnce, getObjOrderDeprecation)

	if len(args) < 1 {
		return nil, &WASMError{Message: "missing dsl argument", Code: 400}
	}

	dsl := args[0].String()
	g, _, err := d2compiler.Compile("", strings.NewReader(dsl), &d2compiler.CompileOptions{
		UTF16Pos: true,
	})
	if err != nil {
		return nil, &WASMError{Message: err.Error(), Code: 400}
	}

	objOrder, err := d2oracle.GetObjOrder(g, nil)
	if err != nil {
		return nil, &WASMError{Message: err.Error(), Code: 500}
	}

	return map[string]interface{}{
		"order": objOrder,
	}, nil
}

func GetRefRanges(args []js.Value) (interface{}, error) {
	if len(args) < 4 {
		return nil, &WASMError{Message: "missing required arguments", Code: 400}
	}

	var fs map[string]string
	if err := json.Unmarshal([]byte(args[0].String()), &fs); err != nil {
		return nil, &WASMError{Message: "invalid fs argument", Code: 400}
	}

	file := args[1].String()
	key := args[2].String()

	var boardPath []string
	if err := json.Unmarshal([]byte(args[3].String()), &boardPath); err != nil {
		return nil, &WASMError{Message: "invalid boardPath argument", Code: 400}
	}

	ranges, importRanges, err := d2lsp.GetRefRanges(file, fs, boardPath, key)
	if err != nil {
		return nil, &WASMError{Message: err.Error(), Code: 500}
	}

	return RefRangesResponse{
		Ranges:       ranges,
		ImportRanges: importRanges,
	}, nil
}

// GetELKGraph converts D2 source into the pre-layout ELK graph used by the
// legacy JavaScript ELK integration.
//
// Deprecated: use the raw WASM compile API with options.layout set to "elk".
// ELK layout now runs inside D2, and this export will be removed after one
// compatibility release.
func GetELKGraph(args []js.Value) (interface{}, error) {
	warnDeprecatedWASMCall(&getELKGraphDeprecationOnce, getELKGraphDeprecation)

	if len(args) < 1 {
		return nil, &WASMError{Message: "missing JSON argument", Code: 400}
	}
	var input CompileRequest
	if err := json.Unmarshal([]byte(args[0].String()), &input); err != nil {
		return nil, &WASMError{Message: "invalid JSON input", Code: 400}
	}

	if input.FS == nil {
		return nil, &WASMError{Message: "missing 'fs' field in input JSON", Code: 400}
	}

	inputPath := DEFAULT_INPUT_PATH

	if input.InputPath != nil {
		inputPath = *input.InputPath
	}

	if _, ok := input.FS[inputPath]; !ok {
		return nil, &WASMError{Message: fmt.Sprintf("missing '%s' file in input fs", inputPath), Code: 400}
	}

	fs, err := memfs.New(input.FS)
	if err != nil {
		return nil, &WASMError{Message: fmt.Sprintf("invalid fs input: %s", err.Error()), Code: 400}
	}

	g, config, err := d2compiler.Compile(inputPath, strings.NewReader(input.FS[inputPath]), &d2compiler.CompileOptions{
		UTF16Pos: true,
		FS:       fs,
	})
	if err != nil {
		return nil, &WASMError{Message: err.Error(), Code: 400}
	}

	if config != nil {
		g.Data = config.Data
	}

	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, &WASMError{Message: fmt.Sprintf("text ruler cannot be initialized: %s", err.Error()), Code: 500}
	}
	err = g.SetDimensions(nil, ruler, nil, nil)
	if err != nil {
		return nil, err
	}

	elk, err := d2elklayout.ConvertGraph(context.Background(), g, nil)
	if err != nil {
		return nil, &WASMError{Message: err.Error(), Code: 400}
	}
	return elk, nil
}

func Format(args []js.Value) (interface{}, error) {
	if len(args) == 0 || args[0].Type() != js.TypeString {
		return nil, &WASMError{Message: "format expects a D2 source string", Code: 400}
	}
	m, err := d2parser.Parse(DEFAULT_INPUT_PATH, strings.NewReader(args[0].String()), &d2parser.ParseOptions{UTF16Pos: true})
	if err != nil {
		return nil, &WASMError{Message: err.Error(), Code: 400}
	}
	return d2format.Format(m), nil
}

func Compile(args []js.Value) (interface{}, error) {
	input, err := parseCompileRequest(args)
	if err != nil {
		return nil, err
	}
	// Two-phase callers can request an animated target later in render().
	result, err := input.compile(go2.Pointer(input.Opts.ASCII == nil || !*input.Opts.ASCII))
	if err != nil {
		return nil, err
	}
	result.FS[result.InputPath] = d2format.Format(result.Graph.AST)
	if fonts := input.Opts.customFonts(); fonts != nil {
		return struct {
			*CompileResponse
			Diagram compiledDiagram `json:"diagram"`
		}{result, compiledDiagram{result.Diagram, fonts}}, nil
	}
	return *result, nil
}

func RenderSVG(args []js.Value) (interface{}, error) {
	input, err := parseCompileRequest(args)
	if err != nil {
		return nil, err
	}
	if input.Opts.ASCII != nil && *input.Opts.ASCII {
		return nil, &WASMError{Message: "renderSVG does not support ASCII output", Code: 400}
	}
	var animateTweens *bool
	if input.Opts.Target == nil || (*input.Opts.Target != "*" && !strings.HasSuffix(*input.Opts.Target, ".*")) {
		animateTweens = go2.Pointer(false)
	} else if input.Opts.AnimateInterval != nil {
		animateTweens = go2.Pointer(*input.Opts.AnimateInterval > 0)
	}
	result, err := input.compile(animateTweens)
	if err != nil {
		return nil, err
	}
	out, err := render(RenderRequest{Diagram: &result.Diagram, Opts: &result.RenderOptions})
	if err != nil {
		return nil, err
	}
	return string(out), nil
}

func parseCompileRequest(args []js.Value) (*CompileRequest, error) {
	if len(args) < 1 {
		return nil, &WASMError{Message: "missing JSON argument", Code: 400}
	}
	var input CompileRequest
	if err := json.Unmarshal([]byte(args[0].String()), &input); err != nil {
		return nil, &WASMError{Message: "invalid JSON input", Code: 400}
	}
	if input.Opts == nil {
		input.Opts = &CompileOptions{}
	}
	if err := validateAnimateInterval(input.Opts.AnimateInterval); err != nil {
		return nil, err
	}
	if duration := input.Opts.AnimateDuration; duration != nil && (*duration < 0 || *duration > d2target.MaxTweenDurationMS) {
		return nil, &WASMError{Message: fmt.Sprintf("animateDuration must be between 0 and %d milliseconds", d2target.MaxTweenDurationMS), Code: 400}
	}
	return &input, nil
}

func validateAnimateInterval(interval *int64) error {
	if interval != nil && (*interval < 0 || *interval > math.MaxInt) {
		return &WASMError{Message: "animateInterval must be non-negative and fit in the supported range", Code: 400}
	}
	return nil
}

func (input *CompileRequest) compile(animateTweens *bool) (*CompileResponse, error) {
	if input.FS == nil {
		return nil, &WASMError{Message: "missing 'fs' field in input JSON", Code: 400}
	}

	compileOpts := &d2lib.CompileOptions{
		UTF16Pos:        true,
		LayoutReuse:     true,
		AnimateTweens:   animateTweens,
		AnimateDuration: input.Opts.AnimateDuration,

		LayoutContinuations: map[string]d2graph.LayoutContinuation{"tala": d2talalayout.ContinueBoard},
	}

	inputPath := DEFAULT_INPUT_PATH

	if input.InputPath != nil {
		inputPath = *input.InputPath
	}

	if _, ok := input.FS[inputPath]; !ok {
		return nil, &WASMError{Message: fmt.Sprintf("missing '%s' file in input fs", inputPath), Code: 400}
	}

	compileOpts.InputPath = inputPath

	compileOpts.LayoutResolver = func(engine string) (d2graph.LayoutGraph, error) {
		switch engine {
		case "dagre":
			return d2dagrelayout.DefaultLayout, nil
		case "elk":
			return d2elklayout.DefaultLayout, nil
		case "tala":
			return d2talalayout.DefaultLayout, nil
		default:
			return nil, &WASMError{Message: fmt.Sprintf("layout option '%s' not recognized", engine), Code: 400}
		}
	}
	compileOpts.RouterResolver = func(engine string) (d2graph.RouteEdges, error) {
		if engine == "tala" {
			return d2talalayout.RouteEdges, nil
		}
		return nil, nil
	}

	var err error
	compileOpts.FS, err = memfs.New(input.FS)
	if err != nil {
		return nil, &WASMError{Message: fmt.Sprintf("invalid fs input: %s", err.Error()), Code: 400}
	}

	compileOpts.Ruler, compileOpts.FontFamily, err = input.Opts.customFonts().newRuler()
	if err != nil {
		return nil, err
	}

	if input.Opts != nil && input.Opts.Layout != nil {
		compileOpts.Layout = input.Opts.Layout
	}

	if input.Opts.ASCII != nil && *input.Opts.ASCII {
		if compileOpts.Layout == nil || *compileOpts.Layout == "dagre" {
			compileOpts.Layout = go2.Pointer("elk")
		}
	}

	renderOpts := &d2svg.RenderOpts{}
	if input.Opts != nil && input.Opts.Sketch != nil {
		renderOpts.Sketch = input.Opts.Sketch
	}
	if input.Opts != nil && input.Opts.Pad != nil {
		renderOpts.Pad = input.Opts.Pad
	}
	if input.Opts != nil && input.Opts.Center != nil {
		renderOpts.Center = input.Opts.Center
	}
	if input.Opts != nil && input.Opts.ThemeID != nil {
		renderOpts.ThemeID = input.Opts.ThemeID
	}
	if input.Opts != nil && input.Opts.DarkThemeID != nil {
		renderOpts.DarkThemeID = input.Opts.DarkThemeID
	}
	if input.Opts != nil && input.Opts.ThemeOverrides != nil {
		renderOpts.ThemeOverrides = input.Opts.ThemeOverrides
	}
	if input.Opts != nil && input.Opts.DarkThemeOverrides != nil {
		renderOpts.DarkThemeOverrides = input.Opts.DarkThemeOverrides
	}
	if input.Opts != nil && input.Opts.Scale != nil {
		renderOpts.Scale = input.Opts.Scale
	}

	ctx := log.WithDefault(context.Background())
	diagram, g, err := d2lib.Compile(ctx, input.FS[inputPath], compileOpts, renderOpts)
	if err != nil {
		if pe, ok := err.(*d2parser.ParseError); ok {
			errs, _ := json.Marshal(pe.Errors)
			return nil, &WASMError{Message: string(errs), Code: 400}
		}
		return nil, &WASMError{Message: err.Error(), Code: 500}
	}

	return &CompileResponse{
		FS:        input.FS,
		InputPath: inputPath,
		Diagram:   *diagram,
		Graph:     *g,
		RenderOptions: RenderOptions{
			ThemeID:            renderOpts.ThemeID,
			DarkThemeID:        renderOpts.DarkThemeID,
			ThemeOverrides:     renderOpts.ThemeOverrides,
			DarkThemeOverrides: renderOpts.DarkThemeOverrides,
			Sketch:             renderOpts.Sketch,
			Pad:                renderOpts.Pad,
			Center:             renderOpts.Center,
			Scale:              renderOpts.Scale,
			ForceAppendix:      diagram.Config.ResolveForceAppendix(input.Opts.ForceAppendix),
			Target:             input.Opts.Target,
			AnimateInterval:    diagram.Config.ResolveAnimateInterval(input.Opts.AnimateInterval),
			Salt:               input.Opts.Salt,
			NoXMLTag:           input.Opts.NoXMLTag,
			ASCII:              input.Opts.ASCII,
			ASCIIMode:          input.Opts.ASCIIMode,
		},
	}, nil
}

func (opts *CompileOptions) customFonts() *customFonts {
	if opts.FontRegular == nil && opts.FontItalic == nil && opts.FontBold == nil && opts.FontSemibold == nil {
		return nil
	}
	face := func(data *[]byte) []byte {
		if data == nil {
			return nil
		}
		return *data
	}
	return &customFonts{face(opts.FontRegular), face(opts.FontItalic), face(opts.FontBold), face(opts.FontSemibold)}
}

// One worker runs each WASM call to completion. Keep a single custom family
// installed and restore it from the diagram before a later render. Identical
// requests need no font conversion, and disposing of a diagram releases its
// bytes independently of other compiled diagrams.
var currentCustomFonts *customFonts

func (fonts *customFonts) register() (*d2fonts.FontFamily, error) {
	if fonts == nil {
		return nil, nil
	}
	current := currentCustomFonts
	same := func(a, b []byte) bool { return (a == nil) == (b == nil) && bytes.Equal(a, b) }
	if current != nil && same(fonts.Regular, current.Regular) && same(fonts.Italic, current.Italic) && same(fonts.Bold, current.Bold) && same(fonts.Semibold, current.Semibold) {
		family := d2fonts.FontFamily("custom")
		return &family, nil
	}
	family, err := d2fonts.AddFontFamily("custom", fonts.Regular, fonts.Italic, fonts.Bold, fonts.Semibold)
	if err != nil {
		return nil, &WASMError{Message: fmt.Sprintf("custom fonts could not be initialized: %s", err), Code: 400}
	}
	currentCustomFonts = fonts
	return family, nil
}

// The font encoder and the text measurer read different parts of a face. Only
// retain a replacement that both can use, so the next request can still load
// its ruler if this diagram's font lacks usable metrics.
func (fonts *customFonts) newRuler() (*textmeasure.Ruler, *d2fonts.FontFamily, error) {
	previous := currentCustomFonts
	family, err := fonts.register()
	if err != nil {
		return nil, nil, err
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		if currentCustomFonts != previous {
			if previous == nil {
				previous = &customFonts{}
			}
			// The previous set was already measured. An empty set restores
			// the bundled faces if this was the first custom request.
			if _, restoreErr := previous.register(); restoreErr != nil {
				return nil, nil, restoreErr
			}
		}
		return nil, nil, &WASMError{Message: fmt.Sprintf("text ruler cannot be initialized: %s", err), Code: 500}
	}
	return ruler, family, nil
}

func Render(args []js.Value) (interface{}, error) {
	if len(args) < 1 {
		return nil, &WASMError{Message: "missing JSON argument", Code: 400}
	}
	var input struct {
		Diagram *compiledDiagram `json:"diagram"`
		Opts    *RenderOptions   `json:"options"`
	}
	if err := json.Unmarshal([]byte(args[0].String()), &input); err != nil {
		return nil, &WASMError{Message: "invalid JSON input", Code: 400}
	}

	if input.Diagram == nil {
		return nil, &WASMError{Message: "missing 'diagram' field in input JSON", Code: 400}
	}
	return render(RenderRequest{Diagram: &input.Diagram.Diagram, Opts: input.Opts, fonts: input.Diagram.CustomFonts})
}

func render(input RenderRequest) ([]byte, error) {
	if input.Diagram == nil {
		return nil, &WASMError{Message: "missing 'diagram' field in input JSON", Code: 400}
	}
	// Validation checks that the target refers to registered font families.
	ruler, _, err := input.fonts.newRuler()
	if err != nil {
		return nil, err
	}
	if err := d2target.ValidateRenderTarget(input.Diagram); err != nil {
		return nil, &WASMError{Message: fmt.Sprintf("invalid diagram: %s", err.Error()), Code: 400}
	}

	if input.Opts == nil {
		input.Opts = &RenderOptions{}
	}
	animateInterval := 0
	if interval := input.Diagram.Config.ResolveAnimateInterval(input.Opts.AnimateInterval); interval != nil {
		if err := validateAnimateInterval(interval); err != nil {
			return nil, err
		}
		animateInterval = int(*interval)
	}
	appendixOption := input.Diagram.Config.ResolveForceAppendix(input.Opts.ForceAppendix)
	forceAppendix := appendixOption != nil && *appendixOption
	scale := input.Opts.Scale
	if scale == nil && input.Diagram.Config != nil {
		scale = input.Diagram.Config.Scale
	}
	if scale != nil && *scale == -1 {
		scale = nil
	}

	var boardPath []string
	noChildren := true

	if input.Opts.Target != nil {
		switch *input.Opts.Target {
		case "*":
			noChildren = false
		case "":
		default:
			target := *input.Opts.Target
			if strings.HasSuffix(target, ".*") {
				target = target[:len(target)-2]
				noChildren = false
			}
			key, err := d2parser.ParseKey(target)
			if err != nil {
				return nil, &WASMError{Message: fmt.Sprintf("target '%s' not recognized", target), Code: 400}
			}
			boardPath = key.StringIDA()
		}
	}

	diagram := input.Diagram.GetBoard(boardPath)
	if diagram == nil {
		return nil, &WASMError{Message: fmt.Sprintf("render target '%s' not found", strings.Join(boardPath, ".")), Code: 400}
	}
	if !noChildren && animateInterval <= 0 && !diagram.HasStandaloneSequencePlayback() {
		return nil, &WASMError{Message: fmt.Sprintf("target '%s' only supported for animated SVGs", *input.Opts.Target), Code: 500}
	}
	if noChildren {
		diagram.Layers = nil
		diagram.Scenarios = nil
		diagram.Steps = nil
		diagram.TweenFrames = nil
		diagram.TweenFrameEndsMS = nil
		diagram.GridMotion = false
		diagram.LayoutContinuity = false
		diagram.TweenDurationMS = 0
	}

	renderOpts := &d2svg.RenderOpts{
		Scale:              scale,
		Salt:               input.Opts.Salt,
		Sketch:             input.Opts.Sketch,
		Pad:                input.Opts.Pad,
		Center:             input.Opts.Center,
		ThemeID:            input.Opts.ThemeID,
		DarkThemeID:        input.Opts.DarkThemeID,
		ThemeOverrides:     input.Opts.ThemeOverrides,
		DarkThemeOverrides: input.Opts.DarkThemeOverrides,
		NoXMLTag:           input.Opts.NoXMLTag,
	}

	if input.Opts.ASCII != nil && *input.Opts.ASCII {
		if !noChildren && animateInterval > 0 {
			return nil, &WASMError{Message: "ASCII rendering does not support multi-board animation", Code: 400}
		}
		if !noChildren {
			return nil, &WASMError{Message: "ASCII rendering does not support multi-board targets", Code: 400}
		}

		ctx := log.WithDefault(context.Background())
		artist := d2ascii.NewASCIIartist()
		asciiOpts := &d2ascii.RenderOpts{Scale: scale}
		// Set charset based on ASCII mode (default to "extended"/Unicode)
		var charsetType charset.Type
		asciiMode := "extended" // Default
		if input.Opts.ASCIIMode != nil {
			asciiMode = *input.Opts.ASCIIMode
		}
		switch asciiMode {
		case "standard":
			charsetType = charset.ASCII
		default: // "extended" or any other value defaults to Unicode
			charsetType = charset.Unicode
		}
		asciiOpts.Charset = charsetType

		out, err := artist.Render(ctx, diagram, asciiOpts)
		if err != nil {
			return nil, &WASMError{Message: fmt.Sprintf("ASCII render failed: %s", err.Error()), Code: 500}
		}
		return out, nil
	}

	if animateInterval == 0 || diagram.HasStandaloneSequencePlayback() {
		return renderSingleBoard(renderOpts, forceAppendix, ruler, diagram)
	}
	// An animated subtree includes its first board's incoming transition.
	// Late sketch needs the original playback order to find its predecessor.
	config := input.Diagram.Config
	if !noChildren && renderOpts.Sketch != nil && *renderOpts.Sketch && config != nil &&
		(config.Sketch == nil || !*config.Sketch) && config.AnimateDuration != nil && *config.AnimateDuration > 0 {
		if err := d2lib.PrepareSketchTransitions(context.Background(), input.Diagram, *config.AnimateDuration); err != nil {
			return nil, &WASMError{Message: fmt.Sprintf("cannot prepare sketch animation: %s", err), Code: 400}
		}
	}
	masterID, err := diagram.HashID(renderOpts.Salt)
	if err != nil {
		return nil, &WASMError{Message: fmt.Sprintf("cannot process animate interval: %s", err.Error()), Code: 500}
	}
	renderOpts.MasterID = masterID
	frames, err := d2animate.Timeline(diagram, animateInterval)
	if err != nil {
		return nil, &WASMError{Message: fmt.Sprintf("invalid timeline: %s", err), Code: 400}
	}
	if len(frames) == 0 {
		if !noChildren {
			return []byte(nil), nil
		}
		// An explicitly selected empty board still has its own background.
		frames = []d2animate.Frame{{Diagram: diagram, DurationMS: animateInterval}}
	}
	boards := make([][]byte, len(frames))
	durations := make([]int, len(frames))
	for i, frame := range frames {
		boards[i], err = renderSingleBoard(renderOpts, forceAppendix, ruler, frame.Diagram)
		if err != nil {
			return nil, err
		}
		durations[i] = frame.DurationMS
	}
	out, err := d2animate.WrapTimed(diagram, boards, *renderOpts, durations)
	if err != nil {
		return nil, &WASMError{Message: fmt.Sprintf("animation failed: %s", err), Code: 500}
	}
	return out, nil
}

func renderSingleBoard(opts *d2svg.RenderOpts, forceAppendix bool, ruler *textmeasure.Ruler, diagram *d2target.Diagram) ([]byte, error) {
	out, err := d2svg.Render(diagram, opts)
	if err != nil {
		return nil, &WASMError{Message: fmt.Sprintf("render failed: %s", err.Error()), Code: 500}
	}
	if forceAppendix {
		return appendix.AppendRendered(diagram, opts, ruler, out)
	}
	return out, nil
}

func GetBoardAtPosition(args []js.Value) (interface{}, error) {
	if len(args) < 3 {
		return nil, &WASMError{Message: "missing required arguments", Code: 400}
	}

	dsl := args[0].String()
	line := args[1].Int()
	column := args[2].Int()

	boardPath, err := d2lsp.GetBoardAtPosition(dsl, d2ast.Position{
		Line:   line,
		Column: column,
	})
	if err != nil {
		return BoardPositionResponse{BoardPath: boardPath, Err: err.Error()}, nil
	}

	return BoardPositionResponse{BoardPath: boardPath}, nil
}

func Encode(args []js.Value) (interface{}, error) {
	if len(args) < 1 {
		return nil, &WASMError{Message: "missing script argument", Code: 400}
	}

	script := args[0].String()
	encoded, err := urlenc.Encode(script)
	// should never happen
	if err != nil {
		return nil, &WASMError{Message: err.Error(), Code: 500}
	}

	return map[string]string{"result": encoded}, nil
}

func Decode(args []js.Value) (interface{}, error) {
	if len(args) < 1 {
		return nil, &WASMError{Message: "missing script argument", Code: 400}
	}

	script := args[0].String()
	script, err := urlenc.Decode(script)
	if err != nil {
		return nil, &WASMError{Message: err.Error(), Code: 500}
	}
	return map[string]string{"result": script}, nil
}

func GetVersion(args []js.Value) (interface{}, error) {
	return version.Version, nil
}

// Injected at build time
// See d2js/js/ci/build.sh
var jsVersion = "HEAD"

func GetJSVersion(args []js.Value) (interface{}, error) {
	return jsVersion, nil
}

func GetCompletions(args []js.Value) (interface{}, error) {
	if len(args) < 3 {
		return nil, &WASMError{Message: "missing required arguments", Code: 400}
	}

	text := args[0].String()
	line := args[1].Int()
	column := args[2].Int()

	completions, err := d2lsp.GetCompletionItemsUTF16(text, line, column)
	if err != nil {
		return nil, &WASMError{Message: err.Error(), Code: 500}
	}

	// Convert to map for JSON serialization
	items := make([]map[string]interface{}, len(completions))
	for i, completion := range completions {
		items[i] = map[string]interface{}{
			"label":      completion.Label,
			"kind":       int(completion.Kind),
			"detail":     completion.Detail,
			"insertText": completion.InsertText,
		}
	}

	return CompletionResponse{
		Items: items,
	}, nil
}
