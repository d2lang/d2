//go:build !d2nomcp

package d2cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/d2lang/d2/d2renderers/d2ascii"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

func TestMCPRenderPNG(t *testing.T) {
	s := newTestMCP(t, mcpConfig{})
	for _, tc := range []struct {
		name         string
		maxDimension any
		wantCap      int
	}{
		{"default", nil, 1568}, {"sized", 300, 300}, {"maximum", 99999, 4096}, {"minimum", 10, 256}, {"zero", 0, 256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{"source": "x -> y"}
			if tc.maxDimension != nil {
				args["max_dimension"] = tc.maxDimension
				args["source"] = "x.width: 9000\nx -> y"
			}
			r := callMCP(t, s, "render", args)
			require.False(t, r.IsError, "%v", r.Content)
			require.Len(t, r.Content, 2)
			img := r.Content[0].(*mcp.ImageContent)
			require.Equal(t, "image/png", img.MIMEType)
			require.True(t, bytes.HasPrefix(img.Data, []byte("\x89PNG\r\n\x1a\n")))
			cfg, format, err := image.DecodeConfig(bytes.NewReader(img.Data))
			require.NoError(t, err)
			require.Equal(t, "png", format)
			side := max(cfg.Width, cfg.Height)
			require.LessOrEqual(t, side, tc.wantCap)
			if tc.maxDimension != nil {
				require.GreaterOrEqual(t, side, tc.wantCap-1)
			}
			info := mcpFields(t, r)
			require.Equal(t, true, info["ok"])
			require.Equal(t, "root", info["board"])
			require.Equal(t, float64(2), info["shapes"])
			require.Equal(t, float64(1), info["connections"])
			require.Equal(t, float64(cfg.Width), info["image_width"])
			require.Equal(t, float64(cfg.Height), info["image_height"])
			require.Equal(t, []any{}, info["boards"])
			require.Equal(t, []any{}, info["diagnostics"])
			require.Contains(t, r.Content[1].(*mcp.TextContent).Text, fmt.Sprintf("Image %dx%d px.", cfg.Width, cfg.Height))
			require.NotContains(t, r.Meta, "com.d2lang/svg")
		})
	}
}

func TestMCPPNGThinStrokeAtIntrinsicSize(t *testing.T) {
	server := newTestMCP(t, mcpConfig{})
	args := map[string]any{"source": `box: "" {width: 320; height: 48; style: {stroke: black; stroke-width: 1; fill: white}}`}
	result := callMCP(t, server, "render", args)
	require.False(t, result.IsError, "%v", result.Content)
	config, err := png.DecodeConfig(bytes.NewReader(result.Content[0].(*mcp.ImageContent).Data))
	require.NoError(t, err)
	args["max_dimension"] = config.Width / 2
	result = callMCP(t, server, "render", args)
	require.False(t, result.IsError, "%v", result.Content)
	img, err := png.Decode(bytes.NewReader(result.Content[0].(*mcp.ImageContent).Data))
	require.NoError(t, err)
	require.Equal(t, config.Width/2, img.Bounds().Dx())
	var rows []uint32
	for y := 0; y < img.Bounds().Dy()/2; y++ {
		r, _, _, _ := img.At(img.Bounds().Dx()/2, y).RGBA()
		if r != 0xffff {
			rows = append(rows, r)
		}
	}
	require.Equal(t, []uint32{0}, rows)
}

func TestMCPRenderFormatsAndLayouts(t *testing.T) {
	s := newTestMCP(t, mcpConfig{})
	for _, tc := range []struct{ format, layout, sourceLayout, want string }{
		{"txt", "", "", "elk"}, {"svg", "", "", "dagre"}, {"svg", "dagre", "", "dagre"},
		{"svg", "elk", "", "elk"}, {"svg", "tala", "", "tala"}, {"svg", "", "tala", "tala"},
		{"txt", "", "tala", "tala"}, {"txt", "", "dagre", "elk"}, {"txt", "dagre", "tala", "elk"},
	} {
		t.Run(strings.Join([]string{tc.format, tc.layout, tc.sourceLayout}, "_"), func(t *testing.T) {
			source := "x -> y"
			if tc.sourceLayout != "" {
				source = "vars.d2-config.layout-engine: " + tc.sourceLayout + "\n" + source
			}
			args := map[string]any{"format": tc.format, "source": source}
			if tc.layout != "" {
				args["layout"] = tc.layout
			}
			r := callMCP(t, s, "render", args)
			require.False(t, r.IsError, "%v", r.Content)
			require.Len(t, r.Content, 2)
			info := mcpFields(t, r)
			require.Equal(t, tc.want, info["layout"])
			require.Contains(t, info["playground_url"], "https://play.d2lang.com/?script=")
			art := r.Content[0].(*mcp.TextContent).Text
			if tc.format == "txt" {
				require.Contains(t, art, "x")
				require.Contains(t, art, "y")
			} else {
				require.Contains(t, art, "<svg")
			}
		})
	}
}

func TestMCPRenderBoardsAndErrors(t *testing.T) {
	s := newTestMCP(t, mcpConfig{})
	source := "x -> y\nlayers: { api: { internal } }"
	for _, target := range []string{"", "layers.api", "layers.nope"} {
		r := callMCP(t, s, "render", map[string]any{"source": source, "target": target, "format": "svg"})
		text := r.Content[len(r.Content)-1].(*mcp.TextContent).Text
		info := mcpFields(t, r)
		switch target {
		case "":
			require.False(t, r.IsError)
			require.Equal(t, []any{"layers.api"}, info["boards"])
			require.Contains(t, text, "Other boards: layers.api")
			require.NotContains(t, r.Content[0].(*mcp.TextContent).Text, ">internal<")
		case "layers.api":
			require.False(t, r.IsError)
			require.Equal(t, []any{}, info["boards"])
			require.Equal(t, target, info["board"])
			require.Equal(t, float64(1), info["shapes"])
			require.Equal(t, float64(0), info["connections"])
		default:
			require.True(t, r.IsError)
			require.Contains(t, text, "not found. Boards: layers.api")
		}
	}
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"source": "x", "theme": 9999}, "unknown theme 9999"},
		{map[string]any{"source": "x", "target": "a."}, "invalid target"},
		{map[string]any{"source": "layers: { api: { x } }"}, "has no shapes of its own"},
		{map[string]any{"source": "a: @other"}, "local-file access is disabled"},
	} {
		r := callMCP(t, s, "render", tc.args)
		require.True(t, r.IsError)
		require.Contains(t, r.Content[0].(*mcp.TextContent).Text, tc.want)
	}
	r := callMCP(t, s, "render", map[string]any{"source": "direction: LR\nx.shape: database"})
	require.True(t, r.IsError)
	require.Len(t, mcpFields(t, r)["diagnostics"], 2)
	require.Contains(t, r.Content[0].(*mcp.TextContent).Text, "Fix the source and call render again. Call guide")
}

func TestMCPRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "diagrams")
	require.NoError(t, os.Mkdir(root, 0700))
	for name, source := range map[string]string{"outside.d2": "secret", "diagrams/other.d2": "x -> y", "diagrams/main.d2": "a: @other", "diagrams/large.d2": strings.Repeat(" ", mcpMaxSourceBytes+1)} {
		require.NoError(t, os.WriteFile(filepath.Join(parent, name), []byte(source), 0600))
	}
	s := newTestMCP(t, mcpConfig{root: root})
	for _, path := range []string{"main.d2", filepath.Join(root, "main.d2")} {
		r := callMCP(t, s, "render", map[string]any{"path": path, "format": "svg"})
		require.False(t, r.IsError, "%v", r.Content)
		require.NotContains(t, mcpFields(t, r), "playground_url")
	}
	r := callMCP(t, s, "check", map[string]any{"source": "a: @other"})
	require.False(t, r.IsError)
	r = callMCP(t, s, "render", map[string]any{"path": "large.d2"})
	require.True(t, r.IsError)
	require.Equal(t, "file is larger than 1 MiB", r.Content[0].(*mcp.TextContent).Text)
	escapes := []string{"../outside.d2", filepath.Join(parent, "outside.d2")}
	if runtime.GOOS != "windows" {
		require.NoError(t, os.Symlink(filepath.Join(parent, "outside.d2"), filepath.Join(root, "link.d2")))
		escapes = append(escapes, "link.d2")
	}
	for _, path := range escapes {
		r := callMCP(t, s, "render", map[string]any{"path": path})
		require.True(t, r.IsError, "%s", path)
	}
}

func TestMCPTextSizeWarning(t *testing.T) {
	var nodes []string
	for i := range 40 {
		nodes = append(nodes, fmt.Sprintf("node%d", i))
	}
	s := newTestMCP(t, mcpConfig{})
	r := callMCP(t, s, "render", map[string]any{"source": "direction: right\n" + strings.Join(nodes, " -> "), "max_dimension": 256})
	require.False(t, r.IsError, "%v", r.Content)
	warnings := mcpFields(t, r)["warnings"].([]any)
	require.Len(t, warnings, 1)
	require.True(t, strings.HasPrefix(warnings[0].(string), "smallest text is about"))
}

func TestMCPConcurrentRender(t *testing.T) {
	s := newTestMCP(t, mcpConfig{ui: true})
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			r, err := s.CallTool(t.Context(), &mcp.CallToolParams{Name: "render", Arguments: map[string]any{"source": fmt.Sprintf("a%d -> y", i)}})
			if err != nil {
				t.Errorf("render %d: %v", i, err)
				return
			}
			if r.IsError {
				t.Errorf("render %d: %v", i, r.Content)
			}
		})
	}
	wg.Wait()
}

func TestMCPTimeout(t *testing.T) {
	s := newTestMCP(t, mcpConfig{timeout: time.Nanosecond})
	r := callMCP(t, s, "render", map[string]any{"source": "x -> y"})
	require.True(t, r.IsError)
	require.Equal(t, "timed out after 1ns", r.Content[0].(*mcp.TextContent).Text)
}

func TestMCPImageAccess(t *testing.T) {
	root := t.TempDir()
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 4, 4))))
	require.NoError(t, os.WriteFile(filepath.Join(root, "icon.png"), data.Bytes(), 0600))
	inline := "x.icon: icon.png"
	rooted := newTestMCP(t, mcpConfig{root: root})
	r := callMCP(t, rooted, "render", map[string]any{"source": inline, "format": "svg"})
	require.False(t, r.IsError)
	require.Empty(t, mcpFields(t, r)["warnings"])
	require.Contains(t, r.Content[0].(*mcp.TextContent).Text, base64.StdEncoding.EncodeToString(data.Bytes()))
	denied := newTestMCP(t, mcpConfig{})
	r = callMCP(t, denied, "render", map[string]any{"source": "x.icon: " + filepath.ToSlash(filepath.Join(root, "icon.png")), "format": "svg"})
	require.NotEmpty(t, mcpFields(t, r)["warnings"])
	require.NotContains(t, r.Content[0].(*mcp.TextContent).Text, base64.StdEncoding.EncodeToString(data.Bytes()))

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data.Bytes())
	}))
	t.Cleanup(server.Close)
	r = callMCP(t, rooted, "render", map[string]any{"source": "x.icon: " + server.URL + "/icon.png", "format": "svg"})
	require.NotEmpty(t, mcpFields(t, r)["warnings"])
	require.Zero(t, requests.Load())
}

func TestMCPRenderTextCanvasLimit(t *testing.T) {
	s := newTestMCP(t, mcpConfig{})
	r := callMCP(t, s, "render", map[string]any{
		"source": "a: {width: 20000; height: 20000}", "format": "txt",
	})
	require.True(t, r.IsError)
	require.Contains(t, r.Content[0].(*mcp.TextContent).Text, "text canvas exceeds limit")
}

func TestMCPRenderSelectedBoardLayoutFeatures(t *testing.T) {
	s := newTestMCP(t, mcpConfig{})
	for _, target := range []string{"layers.detail", "detail"} {
		r := callMCP(t, s, "render", map[string]any{
			"source": "root_shape\nlayers: {detail: {box: {left: 20}}}", "target": target, "format": "svg",
		})
		require.True(t, r.IsError)
		require.Contains(t, r.Content[0].(*mcp.TextContent).Text, "does not support locked positions")
	}
	// An unrelated board's unsupported geometry should not prevent selecting the layer.
	r := callMCP(t, s, "render", map[string]any{
		"source": "box: {left: 20}\nlayers: {detail: {root_shape}}", "target": "layers.detail", "format": "svg",
	})
	require.False(t, r.IsError, "%v", r.Content)
}

func TestMCPRenderPrimarySurvivesPreviewFailure(t *testing.T) {
	s := newTestMCP(t, mcpConfig{ui: true})
	// Text output has no page padding. The same padding produces an invalid SVG viewport.
	source := "vars.d2-config.pad: -500\nx -> y"
	r := callMCP(t, s, "render", map[string]any{"source": source, "format": "txt"})
	require.False(t, r.IsError)
	require.Contains(t, r.Content[0].(*mcp.TextContent).Text, "x")
	require.NotContains(t, r.Meta, mcpSVGMetaKey)
	warnings := mcpFields(t, r)["warnings"].([]any)
	require.Len(t, warnings, 1)
	require.True(t, strings.HasPrefix(warnings[0].(string), "could not create the inline SVG preview"))

	r = callMCP(t, s, "render", map[string]any{"source": source, "format": "svg"})
	require.True(t, r.IsError)
}

func TestMCPViewerPreviewBundleLimit(t *testing.T) {
	dir := t.TempDir()
	asset := testSolidPNG(t, color.NRGBA{A: 255})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "icon.png"), asset, 0o600))
	assets, err := sceneAssetOptions(t.Context(), filepath.Join(dir, "input.d2"), false)
	require.NoError(t, err)
	t.Cleanup(assets.Resolver.CloseIdleConnections)
	raw := []byte(`<svg><image href="icon.png"/><image href="icon.png"/></svg>`)
	want := strings.ReplaceAll(string(raw), "icon.png", "data:image/png;base64,"+base64.StdEncoding.EncodeToString(asset))
	exact, err := bundleRasterPreviewWithLimit(t.Context(), assets, raw, int64(len(want)))
	require.NoError(t, err)
	require.Equal(t, want, string(exact))
	small, err := bundleRasterPreviewWithLimit(t.Context(), assets, raw, int64(len(want)-1))
	var tooLarge *rasterPreviewSizeError
	require.ErrorAs(t, err, &tooLarge)
	require.Nil(t, small)
}

func TestMCPDrawingStopsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	board := d2target.NewDiagram()
	_, err := d2ascii.NewASCIIartist().Render(ctx, board, nil)
	require.ErrorIs(t, err, context.Canceled)
	_, err = d2svg.RenderContext(ctx, board, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func TestMCPSourceSVGAppendix(t *testing.T) {
	s := newTestMCP(t, mcpConfig{ui: true})
	for _, enabled := range []bool{false, true} {
		source := fmt.Sprintf("vars.d2-config: {force-appendix: %t; scale: .5}\nlayers.detail: {box: {tooltip: saved note}}", enabled)
		result := callMCP(t, s, "render", map[string]any{"source": source, "format": "svg", "target": "layers.detail"})
		require.False(t, result.IsError, "%v", result.Content)
		svg := result.Content[0].(*mcp.TextContent).Text
		require.Equal(t, enabled, strings.Contains(svg, `class="appendix"`))
		require.Equal(t, svg, result.Meta[mcpSVGMetaKey])
		native, err := imageCLI(t, t.TempDir(), source, "--target=layers.detail", "-")
		require.NoError(t, err)
		require.Equal(t, strings.TrimSpace(string(native)), strings.TrimSpace(svg))

		result = callMCP(t, s, "render", map[string]any{"source": source, "format": "png", "target": "layers.detail"})
		require.False(t, result.IsError, "%v", result.Content)
		require.Contains(t, result.Meta[mcpSVGMetaKey], `class="appendix"`)
	}
}
