//go:build !d2nomcp

package d2cli

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/d2renderers/d2ascii"
	"github.com/d2lang/d2/d2renderers/d2ascii/charset"
	"github.com/d2lang/d2/d2renderers/d2raster"
	"github.com/d2lang/d2/d2renderers/d2scenebuild"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
	"github.com/d2lang/d2/internal/d2layoutfeatures"
	"github.com/d2lang/d2/lib/imgbundler"
	"github.com/d2lang/d2/lib/simplelog"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/d2/lib/urlenc"
)

const mcpSVGMetaKey = "com.d2lang/svg"

type renderInfo struct {
	mcpStatus
	Format        string   `json:"format"`
	Board         string   `json:"board"`
	Layout        string   `json:"layout"`
	Shapes        int      `json:"shapes"`
	Connections   int      `json:"connections"`
	Width         int      `json:"width,omitempty"`
	Height        int      `json:"height,omitempty"`
	ImageWidth    int      `json:"image_width,omitempty"`
	ImageHeight   int      `json:"image_height,omitempty"`
	Boards        []string `json:"boards"`
	PlaygroundURL string   `json:"playground_url,omitempty"`
}

func (s *mcpServer) render(ctx context.Context, _ *mcp.CallToolRequest, in renderInput) (*mcp.CallToolResult, any, error) {
	format := in.Format
	if format == "" {
		format = "png"
	}
	if in.Theme != nil && d2themescatalog.Find(*in.Theme).Name == "" {
		return mcpFailure(fmt.Sprintf(`unknown theme %d; call guide with topic "reference" for the list`, *in.Theme))
	}
	var target []string
	if in.Target != "" {
		key, err := d2parser.ParseKey(in.Target)
		if err != nil {
			return mcpFailure(fmt.Sprintf("invalid target %q", in.Target))
		}
		target = key.StringIDA()
	}
	source, path, msg := s.resolveInput(in.Source, in.Path)
	if msg != "" {
		return mcpFailure(msg)
	}
	opts := d2svg.RenderOpts{ThemeID: in.Theme, Sketch: in.Sketch}
	root, layout, err := s.compileForRender(ctx, path, source, in.Layout, format, &opts)
	if err != nil {
		return mcpCompileFailure(err, "render")
	}
	selected := root.GetBoard(target)
	boardName := in.Target
	if len(target) == 0 {
		boardName = "root"
	}
	if selected == nil || selected.IsFolderOnly {
		names := strings.Join(mcpOtherBoards(root, nil), ", ")
		if names == "" {
			names = "(none)"
		}
		if selected == nil {
			return mcpFailure(fmt.Sprintf("board %q not found. Boards: %s", boardName, names))
		}
		return mcpFailure(fmt.Sprintf("board %q has no shapes of its own. Boards: %s", boardName, names))
	}
	board := *selected
	board.Layers, board.Scenarios, board.Steps, board.TweenFrames = nil, nil, nil, nil
	info := renderInfo{
		mcpStatus: mcpStatus{OK: true, Diagnostics: []mcpDiagnostic{}, Warnings: []string{}},
		Format:    format, Layout: layout, Board: boardName, Boards: mcpOtherBoards(root, selected),
		Shapes: len(board.Shapes), Connections: len(board.Connections),
	}
	res := &mcp.CallToolResult{}
	var pngAssets *d2scenebuild.AssetOptions
	var drawing mcp.Content
	switch format {
	case "txt":
		// Each cell requires a string in the renderer's dense grid. Bound
		// both output and temporary memory for model-supplied dimensions.
		art, err := d2ascii.NewASCIIartist().Render(ctx, &board, &d2ascii.RenderOpts{Scale: opts.Scale, Charset: charset.Unicode, MaxCells: 1 << 20})
		if err != nil {
			return mcpFailure(err.Error())
		}
		drawing = mcpText(string(art))
	case "png":
		maxDimension := 1568
		if in.MaxDimension != nil {
			maxDimension = min(4096, max(256, *in.MaxDimension))
		}
		pngAssets, err = sceneAssetOptions(ctx, path, false)
		if err != nil {
			return mcpFailure(err.Error())
		}
		defer pngAssets.Resolver.CloseIdleConnections()
		if s.cfg.ui {
			pngAssets.UnavailableImages = make(map[string]struct{})
		}
		drawing, err = mcpPNG(ctx, pngAssets, &board, opts, maxDimension, &info)
		if err != nil {
			return mcpFailure(err.Error())
		}
	}
	if s.cfg.ui || format == "svg" {
		svg, warning, err := s.renderSVG(ctx, path, &board, opts, pngAssets)
		if err != nil {
			return mcpFailure(err.Error())
		}
		if warning != "" {
			info.Warnings = append(info.Warnings, warning)
		}
		if format == "svg" {
			drawing = mcpText(svg)
		}
		if s.cfg.ui && len(svg) <= 2<<20 {
			res.Meta = mcp.Meta{mcpSVGMetaKey: svg}
		}
	}
	if in.Source != nil {
		if encoded, err := urlenc.Encode(source); err == nil {
			if url := "https://play.d2lang.com/?script=" + encoded; len(url) <= 8192 {
				info.PlaygroundURL = url
			}
		}
	}
	res.Content = []mcp.Content{drawing, mcpText(info.summary())}
	return res, info, nil
}

func (s *mcpServer) compileForRender(ctx context.Context, path, source, layout, format string, render *d2svg.RenderOpts) (*d2target.Diagram, string, error) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, "", err
	}
	// The compiler resolves the layout from the argument, source (including
	// imports), or default, in that order. Substitute ELK for text only after
	// that decision so a source-selected TALA layout is honored.
	engine := func(name string) string {
		if format == "txt" && strings.EqualFold(name, "dagre") {
			return "elk"
		}
		return strings.ToLower(name)
	}
	opts := &d2lib.CompileOptions{
		InputPath: path, FS: s.sources, Ruler: ruler,
		LayoutResolver: func(name string) (d2graph.LayoutGraph, error) { return s.layouts(engine(name)) },
		RouterResolver: func(name string) (d2graph.RouteEdges, error) { return s.routers(engine(name)) },
		LayoutReuse:    true, AnimateTweens: new(false),
		LayoutContinuations: map[string]d2graph.LayoutContinuation{"tala": d2talalayout.ContinueBoard},
	}
	if layout != "" {
		opts.Layout = &layout
	}
	diagram, graph, err := d2lib.Compile(ctx, source, opts, render)
	if err != nil {
		return nil, "", err
	}
	layout = engine(*opts.Layout)
	if !isBuiltinLayout(layout) {
		return nil, "", layoutNotFound(layout)
	}
	if err := d2layoutfeatures.Check(layout, graph); err != nil {
		return nil, "", err
	}
	if render.Scale != nil && !(*render.Scale > 0) {
		render.Scale = nil
	}
	return diagram, layout, nil
}

func (s *mcpServer) renderSVG(ctx context.Context, path string, board *d2target.Diagram, opts d2svg.RenderOpts, pngAssets *d2scenebuild.AssetOptions) (svg, warning string, err error) {
	opts.Scale = nil
	data, err := d2svg.Render(board, &opts)
	if err != nil {
		return "", "", err
	}
	if pngAssets != nil {
		// Use the same resources and missing-image placeholders as the PNG.
		preview, err := bundleRasterPreview(ctx, pngAssets, data)
		if err != nil {
			return string(data), "could not bundle images: " + err.Error(), nil
		}
		return string(preview), "", nil
	}
	resolver, err := svgBundleResolver(ctx, path, false)
	if err == nil {
		defer resolver.CloseIdleConnections()
		data, err = imgbundler.BundleWithResolver(ctx, simplelog.FromCmdLog(s.ms.Log), data, imgbundler.BundleOptions{Resolver: resolver, Local: true, Remote: true})
	}
	if err != nil {
		// The bundler still returns the SVG, including successful replacements.
		return string(data), "could not bundle images: " + err.Error(), nil
	}
	return string(data), "", nil
}

func mcpPNG(ctx context.Context, assets *d2scenebuild.AssetOptions, board *d2target.Diagram, opts d2svg.RenderOpts, maxDim int, info *renderInfo) (*mcp.ImageContent, error) {
	var missing int
	var firstMissing error
	assets.OnUnavailableImage = func(err error) {
		missing++
		if firstMissing == nil {
			firstMissing = err
		}
	}
	// A rendered label may request missing glyphs. Restrict fallback to
	// bundled fonts so labels cannot trigger reads of system font directories.
	fonts, err := bundledFontFallbackOptions(1, nil)
	if err != nil {
		return nil, err
	}
	opts.Scale = new(1.0)
	document, err := buildSceneWithAssetsAndFonts(ctx, board, opts, assets, fonts)
	if err != nil {
		return nil, err
	}
	longest := math.Max(document.LogicalWidth, document.LogicalHeight)
	scale := math.Min(2, float64(maxDim)/longest)
	// d2raster rounds pixel dimensions up. Roundoff in division followed by
	// multiplication can otherwise add a pixel at the requested boundary.
	if scale*longest > float64(maxDim) {
		scale = math.Nextafter(scale, 0)
	}
	frameOpts := rasterFrameOptions(scale, 0)
	frameOpts.MaxWidth, frameOpts.MaxHeight = maxDim, maxDim
	frame, err := d2raster.Render(ctx, document, frameOpts)
	if err != nil {
		return nil, err
	}
	var encoder rasterPNGEncoder
	defer encoder.close()
	data, err := encoder.encode(ctx, frame)
	if err != nil {
		return nil, err
	}
	info.Width, info.Height = int(math.Ceil(document.LogicalWidth)), int(math.Ceil(document.LogicalHeight))
	info.ImageWidth, info.ImageHeight = frame.Bounds().Dx(), frame.Bounds().Dy()
	if missing > 0 {
		info.Warnings = append(info.Warnings, fmt.Sprintf("could not bundle %s: %v", plural(missing, "image"), firstMissing))
	}
	if warning := textSizeWarning(board, scale, info.Width, info.Height, maxDim); warning != "" {
		info.Warnings = append(info.Warnings, warning)
	}
	return &mcp.ImageContent{Data: data, MIMEType: "image/png"}, nil
}

func textSizeWarning(board *d2target.Diagram, scale float64, logicalW, logicalH, maxDim int) string {
	minFont := 0
	consider := func(text d2target.Text) {
		if text.Label != "" && text.FontSize > 0 && (minFont == 0 || text.FontSize < minFont) {
			minFont = text.FontSize
		}
	}
	for _, shape := range board.Shapes {
		consider(shape.Text)
	}
	for _, connection := range board.Connections {
		consider(connection.Text)
	}
	px := float64(minFont) * scale
	if minFont == 0 || px >= 9 {
		return ""
	}
	msg := fmt.Sprintf("smallest text is about %d px tall in this image and may be unreadable. Split the diagram into layers", int(math.Round(px)))
	suggest := int(min(4096, math.Ceil(float64(max(logicalW, logicalH))*10/float64(minFont))))
	if suggest > maxDim {
		msg += fmt.Sprintf(", or pass max_dimension %d", suggest)
	}
	return msg + "."
}

func (info renderInfo) summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Rendered board %q with layout %s: %s, %s.", info.Board, info.Layout, plural(info.Shapes, "shape"), plural(info.Connections, "connection"))
	if info.Format == "png" {
		fmt.Fprintf(&b, " Image %dx%d px.", info.ImageWidth, info.ImageHeight)
	}
	if len(info.Boards) > 0 {
		fmt.Fprintf(&b, "\nOther boards: %s", strings.Join(info.Boards, ", "))
	}
	for _, warning := range info.Warnings {
		fmt.Fprintf(&b, "\nWarning: %s", warning)
	}
	if info.PlaygroundURL != "" {
		fmt.Fprintf(&b, "\nPlayground: %s", info.PlaygroundURL)
	}
	return b.String()
}

// Collect authored boards in D2's navigation order, excluding the selected
// board by identity so alternative quoting of a target cannot list it twice.
func mcpOtherBoards(root, selected *d2target.Diagram) []string {
	out := []string{}
	var walk func(*d2target.Diagram, []string)
	walk = func(board *d2target.Diagram, path []string) {
		for _, group := range []struct {
			kind     string
			children []*d2target.Diagram
		}{{"layers", board.Layers}, {"scenarios", board.Scenarios}, {"steps", board.Steps}} {
			for _, child := range group.children {
				if len(out) == 50 {
					return
				}
				childPath := append(path, group.kind, child.Name)
				if child != selected {
					out = append(out, d2format.Format(d2ast.MakeKeyPath(childPath)))
				}
				walk(child, childPath)
			}
		}
	}
	walk(root, nil)
	return out
}
