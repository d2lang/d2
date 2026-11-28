package d2lib

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2exporter"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/util-go/go2"
)

type CompileOptions struct {
	UTF16Pos bool
	// AnimateDuration overrides the root animate-duration, in milliseconds.
	AnimateDuration *int64
	// AnimateTweens controls sampling of step transitions. Nil follows
	// the root animate-interval. False compiles only the authored endpoints.
	AnimateTweens *bool
	// MaxVariableExpansion bounds work added by variable substitutions and the
	// automatic copies they induce. Zero uses the secure compiler default.
	MaxVariableExpansion int64
	// MaxGlobExpansion bounds work performed by glob matching and
	// materialization. Zero uses the secure compiler default. Explicit source
	// fields are not counted as materialization work.
	MaxGlobExpansion int64
	// MaxEdgeExpansion bounds distinct edge-segment and endpoint combinations
	// considered by edge globs. Zero uses the secure compiler default. Explicit
	// edges do not consume this budget.
	MaxEdgeExpansion int64
	// MaxEdgeExpansionWork bounds all endpoint-pair examinations performed by
	// edge globs, including lazy replays. Zero uses the secure compiler default.
	MaxEdgeExpansionWork int64
	// FS is the file system used for resolving imports in the D2 text. Nil
	// disables imports. Callers that accept untrusted input should prefer a
	// filesystem constrained to the intended import root; lib/localfile provides
	// rooted and explicit unrestricted host-filesystem policies.
	FS             fs.FS
	MeasuredTexts  []*d2target.MText
	Ruler          *textmeasure.Ruler
	RouterResolver func(engine string) (d2graph.RouteEdges, error)
	LayoutResolver func(engine string) (d2graph.LayoutGraph, error)

	// LayoutReuse allows scenarios and steps whose layout inputs are unchanged
	// to reuse the geometry of their base board. Callers should enable this only
	// when LayoutResolver and RouterResolver return a stable built-in Dagre or
	// ELK configuration for the duration of Compile. It is disabled by default
	// so custom resolvers retain their existing per-board behavior.
	LayoutReuse bool

	// LayoutContinuations supplies optional refinements for related boards,
	// keyed by lowercase layout engine name. Independent layers start fresh.
	LayoutContinuations map[string]d2graph.LayoutContinuation

	Layout *string

	// FontFamily controls the font family used for all texts that are not the following:
	// - code
	// - latex
	// - pre-measured (web setting)
	// TODO maybe some will want to configure code font too, but that's much lower priority
	FontFamily *d2fonts.FontFamily

	// MonoFontFamily controls the font family used for code/mono texts
	MonoFontFamily *d2fonts.FontFamily

	InputPath string
}

func Parse(ctx context.Context, input string, compileOpts *CompileOptions) (*d2ast.Map, error) {
	if compileOpts == nil {
		compileOpts = &CompileOptions{}
	}

	ast, err := d2parser.ParseContext(ctx, compileOpts.InputPath, strings.NewReader(input), &d2parser.ParseOptions{
		UTF16Pos: compileOpts.UTF16Pos,
	})
	return ast, err
}

func Compile(ctx context.Context, input string, compileOpts *CompileOptions, renderOpts *d2svg.RenderOpts) (*d2target.Diagram, *d2graph.Graph, error) {
	return compileInput(ctx, input, compileOpts, renderOpts)
}

func compileInput(ctx context.Context, input string, compileOpts *CompileOptions, renderOpts *d2svg.RenderOpts) (*d2target.Diagram, *d2graph.Graph, error) {
	if compileOpts == nil {
		compileOpts = &CompileOptions{}
	}
	if renderOpts == nil {
		renderOpts = &d2svg.RenderOpts{}
	}

	g, config, err := d2compiler.Compile(compileOpts.InputPath, strings.NewReader(input), &d2compiler.CompileOptions{
		Context:              ctx,
		UTF16Pos:             compileOpts.UTF16Pos,
		AnimateTweens:        compileOpts.AnimateTweens,
		AnimateDuration:      compileOpts.AnimateDuration,
		MaxVariableExpansion: compileOpts.MaxVariableExpansion,
		MaxGlobExpansion:     compileOpts.MaxGlobExpansion,
		MaxEdgeExpansion:     compileOpts.MaxEdgeExpansion,
		MaxEdgeExpansionWork: compileOpts.MaxEdgeExpansionWork,
		FS:                   compileOpts.FS,
	})
	if err != nil {
		return nil, nil, err
	}

	applyConfigs(config, compileOpts, renderOpts)
	applyDefaults(compileOpts, renderOpts)
	if config != nil {
		applyGraphData(g, config.Data)
	}

	d, err := compile(ctx, g, compileOpts, renderOpts)
	animated := config != nil && config.AnimateInterval != nil && *config.AnimateInterval > 0
	if compileOpts.AnimateTweens != nil {
		animated = *compileOpts.AnimateTweens
	}
	if err == nil && animated && d != nil {
		duration := 0
		if config != nil && config.AnimateDuration != nil {
			duration = int(*config.AnimateDuration)
		}
		// Annotations change on every step without changing the diagram under
		// them. Plan the diagram's own transitions without them, then draw them.
		annotations := liftAnnotations(d)
		if duration > 0 {
			structural := compileOpts.Layout != nil && strings.EqualFold(*compileOpts.Layout, "tala")
			if err := sampleBoardTransitions(ctx, d, g, duration, d2target.MaxRenderTargetBoards, *renderOpts.Sketch, structural); err != nil {
				return nil, nil, err
			}
		}
		if err := sampleAnnotations(ctx, d, annotations, duration, d2target.MaxRenderTargetBoards); err != nil {
			return nil, nil, err
		}
		alignCaptions(d)
		shareCanvas(d)
	}
	if d != nil {
		if config == nil {
			config = &d2target.Config{}
		}
		// These are fields that affect a diagram's appearance, so feed them back
		// into diagram.Config to ensure the hash computed for CSS styling purposes
		// is unique to its appearance
		config.ThemeID = renderOpts.ThemeID
		config.DarkThemeID = renderOpts.DarkThemeID
		config.Sketch = renderOpts.Sketch
		d.Config = config
	}
	return d, g, err
}

func applyGraphData(g *d2graph.Graph, data map[string]interface{}) {
	if g == nil {
		return
	}
	g.Data = cloneGraphData(data)
	for _, boards := range [][]*d2graph.Graph{g.Layers, g.Scenarios, g.Steps, g.TweenFrames} {
		for _, board := range boards {
			applyGraphData(board, data)
		}
	}
}

func cloneGraphData(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return nil
	}
	clone := make(map[string]interface{}, len(data))
	for key, value := range data {
		clone[key] = cloneGraphDataValue(value)
	}
	return clone
}

func cloneGraphDataValue(value interface{}) interface{} {
	switch value := value.(type) {
	case map[string]interface{}:
		return cloneGraphData(value)
	case []interface{}:
		clone := make([]interface{}, len(value))
		for i, entry := range value {
			clone[i] = cloneGraphDataValue(entry)
		}
		return clone
	default:
		return value
	}
}

func compile(ctx context.Context, g *d2graph.Graph, compileOpts *CompileOptions, renderOpts *d2svg.RenderOpts) (*d2target.Diagram, error) {
	d, _, err := compileBoard(ctx, g, compileOpts, renderOpts, nil, nil, false)
	return d, err
}

func compileBoard(ctx context.Context, g *d2graph.Graph, compileOpts *CompileOptions, renderOpts *d2svg.RenderOpts, previous *d2graph.Graph, reuse *layoutGeometry, captureForCaller bool) (*d2target.Diagram, *layoutGeometry, error) {
	var frames []*d2target.Diagram
	transitionContinuous := true
	for _, sample := range g.TweenFrames {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		frame, geometry, err := compileBoard(ctx, sample, compileOpts, renderOpts, previous, reuse, true)
		if err != nil {
			return nil, nil, err
		}
		frames = append(frames, frame)
		transitionContinuous = transitionContinuous && frame.LayoutContinuity
		previous, reuse = sample, geometry
	}

	err := g.ApplyTheme(*renderOpts.ThemeID)
	if err != nil {
		return nil, nil, err
	}

	var geometry *layoutGeometry
	var continuity bool
	if len(g.Objects) > 0 {
		err := g.SetDimensions(compileOpts.MeasuredTexts, compileOpts.Ruler, compileOpts.FontFamily, compileOpts.MonoFontFamily)
		if err != nil {
			return nil, nil, err
		}

		needsGeometry := compileOpts.LayoutReuse && (reuse != nil || captureForCaller || len(g.Scenarios) > 0 || len(g.Steps) > 0)
		var signature layoutInputSignature
		reusable := false
		if needsGeometry {
			engine := ""
			if compileOpts.Layout != nil {
				engine = *compileOpts.Layout
			}
			signature, reusable, err = newLayoutInputSignature(g, engine)
			if err != nil {
				return nil, nil, err
			}
		}
		// A built-in layout may surface context cancellation at the start of
		// every board. Do not let geometry replay bypass that error path.
		reused := ctx.Err() == nil && reusable && compileOpts.LayoutReuse && reuse.apply(g, signature)
		if !reused {
			coreLayout, err := getLayout(compileOpts)
			if err != nil {
				return nil, nil, err
			}
			edgeRouter, err := getEdgeRouter(compileOpts)
			if err != nil {
				return nil, nil, err
			}
			graphInfo := d2layouts.NestedGraphInfo(g.Root)
			err = d2layouts.LayoutNested(ctx, g, graphInfo, coreLayout, edgeRouter)
			if err != nil {
				return nil, nil, err
			}
		}
		if previous != nil && compileOpts.Layout != nil && len(compileOpts.LayoutContinuations) != 0 {
			if adjust := compileOpts.LayoutContinuations[strings.ToLower(*compileOpts.Layout)]; adjust != nil {
				continuity, err = adjust(ctx, previous, g)
				if err != nil {
					return nil, nil, err
				}
			}
		}
		if reusable && (captureForCaller || len(g.Scenarios) > 0 || len(g.Steps) > 0) {
			geometry = captureLayoutGeometry(g, signature)
		}
	}

	d, err := d2exporter.Export(ctx, g, compileOpts.FontFamily, compileOpts.MonoFontFamily)
	if err != nil {
		return nil, nil, err
	}

	// The endpoint is related to its authored predecessor only if every
	// sampled hop, including the final hop to the endpoint, kept that frame.
	d.LayoutContinuity = continuity && transitionContinuous
	d.TweenFrames = frames
	d.TweenDurationMS = g.TweenDurationMS

	for _, l := range g.Layers {
		// Layers are independent boards rather than overlays of their parent, so
		// retain their existing always-layout behavior.
		ld, _, err := compileBoard(ctx, l, compileOpts, renderOpts, nil, nil, false)
		if err != nil {
			return nil, nil, err
		}
		d.Layers = append(d.Layers, ld)
	}
	for _, l := range g.Scenarios {
		ld, _, err := compileBoard(ctx, l, compileOpts, renderOpts, g, geometry, false)
		if err != nil {
			return nil, nil, err
		}
		d.Scenarios = append(d.Scenarios, ld)
	}
	previous, reuse = g, geometry
	for i, l := range g.Steps {
		ld, stepGeometry, err := compileBoard(ctx, l, compileOpts, renderOpts, previous, reuse, i+1 < len(g.Steps))
		if err != nil {
			return nil, nil, err
		}
		matchStepIconStreams(previous, l, ld)
		d.Steps = append(d.Steps, ld)
		previous, reuse = l, stepGeometry
	}
	return d, geometry, nil
}

func getLayout(opts *CompileOptions) (d2graph.LayoutGraph, error) {
	if opts.Layout == nil {
		return nil, errors.New("no available layout")
	}
	if opts.LayoutResolver == nil {
		return nil, fmt.Errorf("no layout resolver configured for layout engine %q", *opts.Layout)
	}
	return opts.LayoutResolver(*opts.Layout)
}

func getEdgeRouter(opts *CompileOptions) (d2graph.RouteEdges, error) {
	if opts.Layout != nil && opts.RouterResolver != nil {
		router, err := opts.RouterResolver(*opts.Layout)
		if err != nil {
			return nil, err
		}
		if router != nil {
			return router, nil
		}
	}
	return d2layouts.DefaultRouter, nil
}

// applyConfigs applies the configs read from D2 and applies it to passed in opts
// It will only write to opt fields that are nil, as passed-in opts have precedence
func applyConfigs(config *d2target.Config, compileOpts *CompileOptions, renderOpts *d2svg.RenderOpts) {
	if config == nil {
		return
	}

	if compileOpts.Layout == nil {
		compileOpts.Layout = config.LayoutEngine
	}

	if renderOpts.ThemeID == nil {
		renderOpts.ThemeID = config.ThemeID
	}
	if renderOpts.DarkThemeID == nil {
		renderOpts.DarkThemeID = config.DarkThemeID
	}
	if renderOpts.Sketch == nil {
		renderOpts.Sketch = config.Sketch
	}
	if renderOpts.Pad == nil {
		renderOpts.Pad = config.Pad
	}
	if renderOpts.Scale == nil {
		renderOpts.Scale = config.Scale
	}
	if renderOpts.Center == nil {
		renderOpts.Center = config.Center
	}
	if renderOpts.ThemeOverrides == nil {
		renderOpts.ThemeOverrides = config.ThemeOverrides
	}
	if renderOpts.DarkThemeOverrides == nil {
		renderOpts.DarkThemeOverrides = config.DarkThemeOverrides
	}
}

func applyDefaults(compileOpts *CompileOptions, renderOpts *d2svg.RenderOpts) {
	if compileOpts.Layout == nil {
		compileOpts.Layout = go2.Pointer("dagre")
	}

	if renderOpts.ThemeID == nil {
		renderOpts.ThemeID = &d2themescatalog.NeutralDefault.ID
	}
	if renderOpts.Sketch == nil {
		renderOpts.Sketch = go2.Pointer(false)
	}
	if *renderOpts.Sketch && compileOpts.FontFamily == nil {
		compileOpts.FontFamily = go2.Pointer(d2fonts.HandDrawn)
	}
	if renderOpts.Pad == nil {
		renderOpts.Pad = go2.Pointer(int64(d2svg.DEFAULT_PADDING))
	}
	if renderOpts.Center == nil {
		renderOpts.Center = go2.Pointer(false)
	}
}
