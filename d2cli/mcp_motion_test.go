//go:build !d2nomcp

package d2cli

import (
	"bytes"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func requireMCPMotionSuccess(t *testing.T, r *mcp.CallToolResult) {
	t.Helper()
	if r.IsError {
		t.Fatalf("motion tool failed: %s", r.Content[0].(*mcp.TextContent).Text)
	}
}

func TestMCPStoryboard(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "service.d2"), []byte("primary"), 0600))
	const source = "vars.d2-config.animate-interval: 400ms\nserver: @service\nsteps.failover: { replica }"
	session := newTestMCP(t, mcpConfig{root: dir})
	for _, tc := range []struct {
		name     string
		override int
		interval int
	}{
		{"source timing", -1, 400}, {"explicit timing", 800, 800}, {"zero uses playback default", 0, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{"source": source, "animate_duration": 0}
			if tc.override >= 0 {
				args["animate_interval"] = tc.override
			}
			r := callMCP(t, session, "storyboard", args)
			requireMCPMotionSuccess(t, r)
			require.Len(t, r.Content, 2)
			sheet := r.Content[0].(*mcp.ImageContent)
			require.Equal(t, "image/png", sheet.MIMEType)
			_, err := png.Decode(bytes.NewReader(sheet.Data))
			require.NoError(t, err)
			fields := mcpFields(t, r)
			require.Equal(t, true, fields["ok"])
			require.Empty(t, fields["warnings"])
			require.Equal(t, []any{
				map[string]any{"board": "root", "time_ms": float64(tc.interval / 2)},
				map[string]any{"board": "steps.failover", "time_ms": float64(tc.interval + tc.interval/2)},
			}, fields["frames"])
		})
	}
}

func TestMCPStoryboardUsesSourceAppendix(t *testing.T) {
	session := newTestMCP(t, mcpConfig{})
	render := func(source string) []byte {
		t.Helper()
		r := callMCP(t, session, "storyboard", map[string]any{"source": source})
		requireMCPMotionSuccess(t, r)
		return r.Content[0].(*mcp.ImageContent).Data
	}
	const board = "service: {tooltip: A note below the frame}"
	plain := render(board)
	require.Equal(t, plain, render("vars.d2-config.force-appendix: false\n"+board))
	require.False(t, bytes.Equal(plain, render("vars.d2-config.force-appendix: true\n"+board)))
}

func TestMCPStoryboardTransportSize(t *testing.T) {
	// The raw PNG would fit on one line, but base64 plus the protocol
	// metadata pushes it beyond the client's default stdio read limit.
	data := make([]byte, mcp.DefaultMaxLineLength*3/4)
	r, _, err := mcpStoryboardResult(data, nil, nil)
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Len(t, r.Content, 1)
	require.Contains(t, r.Content[0].(*mcp.TextContent).Text, "run d2 storyboard locally")
}

func TestMCPStoryboardAssetPolicy(t *testing.T) {
	dir := t.TempDir()
	icon := filepath.Join(dir, "icon.png")
	require.NoError(t, os.WriteFile(icon, testSolidPNG(t, color.NRGBA{A: 255}), 0600))
	for _, test := range []struct {
		name        string
		root        string
		icon        string
		wantError   bool
		wantWarning bool
	}{
		{name: "rooted", root: dir, icon: icon},
		{name: "disabled", icon: icon, wantError: true},
		{name: "missing", root: dir, icon: "missing.png", wantWarning: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := newTestMCP(t, mcpConfig{root: test.root})
			args := map[string]any{"source": "server\nsteps.show: { server.icon: " + filepath.ToSlash(test.icon) + " }"}
			r := callMCP(t, session, "storyboard", args)
			if test.wantError {
				require.True(t, r.IsError)
				require.Contains(t, r.Content[0].(*mcp.TextContent).Text, "local-file access is disabled")
				return
			}
			requireMCPMotionSuccess(t, r)
			if test.wantWarning {
				require.NotEmpty(t, mcpFields(t, r)["warnings"])
				require.Contains(t, r.Content[1].(*mcp.TextContent).Text, test.icon)
			} else {
				require.Empty(t, mcpFields(t, r)["warnings"])
			}
		})
	}
}

func TestMCPMotionDiagnostics(t *testing.T) {
	session := newTestMCP(t, mcpConfig{})
	for _, name := range []string{"lint_motion", "storyboard"} {
		r := callMCP(t, session, name, map[string]any{"source": "direction: sideways"})
		require.True(t, r.IsError)
		fields := mcpFields(t, r)
		require.Len(t, fields["diagnostics"], 1)
		require.Contains(t, r.Content[0].(*mcp.TextContent).Text, "call "+name+" again")
		r = callMCP(t, session, name, map[string]any{"path": "diagram.d2"})
		require.True(t, r.IsError)
		require.Contains(t, r.Content[0].(*mcp.TextContent).Text, "file access is disabled")
	}
	r := callMCP(t, session, "lint_motion", map[string]any{"source": "server"})
	requireMCPMotionSuccess(t, r)
	require.Empty(t, mcpFields(t, r)["diagnostics"])
	require.Equal(t, "No motion warnings.", r.Content[0].(*mcp.TextContent).Text)
	const source = "server\nsteps: {\n flash: {\n  latency: 2 ms\n }\n clear: { latency: null }\n}"
	r = callMCP(t, session, "lint_motion", map[string]any{"source": source, "animate_interval": 100})
	requireMCPMotionSuccess(t, r)
	fields := mcpFields(t, r)
	require.Equal(t, true, fields["ok"])
	diagnostics := fields["diagnostics"].([]any)
	require.NotEmpty(t, diagnostics)
	d := diagnostics[0].(map[string]any)
	require.Equal(t, "steps.flash", d["board"])
	require.Equal(t, "inline.d2", d["file"])
	require.Equal(t, "warning", d["severity"])
	require.Equal(t, "short-label", d["code"])
	require.Equal(t, "latency", d["object"])
	require.Equal(t, float64(4), d["line"])
	require.Contains(t, r.Content[0].(*mcp.TextContent).Text, d["code"])
}
