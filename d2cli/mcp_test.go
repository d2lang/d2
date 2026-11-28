//go:build !d2nomcp

package d2cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/d2lang/util-go/cmdlog"
	"github.com/d2lang/util-go/xmain"
	"github.com/d2lang/util-go/xos"

	"github.com/d2lang/d2/lib/log"
)

type mcpWriteCloser struct{ io.Writer }

func (mcpWriteCloser) Close() error { return nil }

func newTestMCP(t *testing.T, cfg mcpConfig) *mcp.ClientSession {
	t.Helper()
	env := xos.NewEnv(nil)
	ms := &xmain.State{Name: "d2", Env: env, Opts: xmain.NewOpts(env, nil), Log: cmdlog.NewTB(env, t), Stderr: mcpWriteCloser{io.Discard}}
	ctx, cancel := context.WithCancel(log.WithDefault(context.Background()))
	if cfg.timeout == 0 {
		cfg.timeout = 30 * time.Second
	}
	t.Cleanup(cancel)
	server, cleanup, err := newMCPServer(ctx, ms, cfg)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	st, ct := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, st) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			require.NoError(t, err)
		}
	})
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, session.Close()) })
	return session
}

func callMCP(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	return res
}

func TestMCPListTools(t *testing.T) {
	s := newTestMCP(t, mcpConfig{})
	list, err := s.ListTools(t.Context(), nil)
	require.NoError(t, err)
	var names []string
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
		require.NotEmpty(t, tool.Description)
		require.NotEmpty(t, tool.Title)
		require.NotNil(t, tool.InputSchema)
		require.Nil(t, tool.Meta)
		require.True(t, tool.Annotations.ReadOnlyHint)
		require.True(t, tool.Annotations.IdempotentHint)
		require.Equal(t, tool.Name == "render" || tool.Name == "storyboard", *tool.Annotations.OpenWorldHint)
	}
	require.Equal(t, []string{"guide", "check", "lint_motion", "render", "storyboard"}, names)
	resources, err := s.ListResources(t.Context(), nil)
	require.NoError(t, err)
	require.Empty(t, resources.Resources)
}

func mcpFields(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	data, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	require.NotNil(t, fields)
	return fields
}

func TestMCPCheck(t *testing.T) {
	session := newTestMCP(t, mcpConfig{})
	check := func(args map[string]any) (*mcp.CallToolResult, map[string]any) {
		r := callMCP(t, session, "check", args)
		return r, mcpFields(t, r)
	}
	r, fields := check(map[string]any{"source": "x -> y"})
	require.False(t, r.IsError)
	require.Equal(t, map[string]any{"ok": true, "diagnostics": []any{}, "warnings": []any{}, "shapes": float64(2), "connections": float64(1)}, fields)
	require.Equal(t, "No errors. 2 shapes, 1 connection.", r.Content[0].(*mcp.TextContent).Text)

	r, fields = check(map[string]any{"source": "x->y", "fmt": true})
	require.Len(t, r.Content, 2)
	require.Equal(t, "x -> y\n", r.Content[1].(*mcp.TextContent).Text)
	require.Equal(t, "x -> y\n", fields["formatted"])
	require.Equal(t, true, fields["changed"])

	r, fields = check(map[string]any{"source": "a: {", "fmt": true})
	require.True(t, r.IsError)
	require.NotContains(t, fields, "formatted")
	diagnostics := fields["diagnostics"].([]any)
	require.Len(t, diagnostics, 1)
	require.Equal(t, float64(1), diagnostics[0].(map[string]any)["line"])
	require.True(t, strings.HasPrefix(r.Content[0].(*mcp.TextContent).Text, "D2 failed to compile (1 error):\n"))
	r, fields = check(map[string]any{"source": "direction: LR\nx.shape: database"})
	require.True(t, r.IsError)
	diagnostics = fields["diagnostics"].([]any)
	require.Len(t, diagnostics, 2)
	for i, raw := range diagnostics {
		d := raw.(map[string]any)
		require.Equal(t, float64(i+1), d["line"])
		require.Equal(t, "inline.d2", d["file"])
		require.Equal(t, "error", d["severity"])
		require.NotContains(t, d["message"], "inline.d2:")
	}
	require.True(t, strings.HasSuffix(r.Content[0].(*mcp.TextContent).Text, "Fix the source and call check again."))

	for _, tt := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"source": "x", "path": "diagram.d2"}, `pass only one of "source" and "path"`},
		{map[string]any{}, `pass the D2 text in "source" or a file in "path"`},
		{map[string]any{"path": "diagram.d2"}, "file access is disabled"},
		{map[string]any{"source": strings.Repeat("x", 1<<20+1)}, "source is larger than 1 MiB"},
		{map[string]any{"source": "x", "fmt": "yes"}, "validating"},
	} {
		r, fields = check(tt.args)
		require.True(t, r.IsError)
		require.Contains(t, r.Content[0].(*mcp.TextContent).Text, tt.want)
		require.Equal(t, map[string]any{"ok": false, "warnings": []any{}, "diagnostics": []any{}}, fields)
	}
	r, fields = check(map[string]any{"source": ""})
	require.False(t, r.IsError)
	require.Equal(t, float64(0), fields["shapes"])
	require.Equal(t, float64(0), fields["connections"])
}
