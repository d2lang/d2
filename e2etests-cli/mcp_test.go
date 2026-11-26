//go:build !d2nomcp

package e2etests_cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/d2lang/util-go/xos"
)

const legacyMCPHandshake = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"manual","version":"0"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
`

const mcpListTools = `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`

func mcpTranscript(t *testing.T, dir string, env []string, requests string) map[int]json.RawMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	state := testMain(dir, xos.NewEnv(env), "mcp")
	stdin, stdout := state.StdinPipe(), state.StdoutPipe()
	state.Start(t, ctx)
	defer state.Cleanup(t)

	var lines []string
	readErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		readErr <- scanner.Err()
	}()
	_, err := io.WriteString(stdin, requests+"\n")
	require.NoError(t, err)
	require.NoError(t, stdin.Close()) // The client may close stdin before reading stdout.
	require.NoError(t, state.Wait(ctx))
	require.NoError(t, <-readErr)

	replies := make(map[int]json.RawMessage)
	for _, line := range lines {
		var reply struct {
			Version string `json:"jsonrpc"`
			ID      int
			Result  json.RawMessage
		}
		require.NoError(t, json.Unmarshal([]byte(line), &reply))
		require.Equal(t, "2.0", reply.Version)
		require.NotEmpty(t, reply.Result, line)
		require.NotContains(t, replies, reply.ID)
		replies[reply.ID] = reply.Result
	}
	return replies
}

func TestMCPTranscript(t *testing.T) {
	replies := mcpTranscript(t, t.TempDir(), nil, legacyMCPHandshake+mcpListTools)
	require.Len(t, replies, 2)
	var init mcp.InitializeResult
	require.NoError(t, json.Unmarshal(replies[1], &init))
	require.Equal(t, "d2", init.ServerInfo.Name)
	require.Equal(t, "2025-06-18", init.ProtocolVersion)
	var tools mcp.ListToolsResult
	require.NoError(t, json.Unmarshal(replies[2], &tools))
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	require.Equal(t, []string{"guide", "check", "render"}, names)
}

func TestMCPDiscover(t *testing.T) {
	const discover = `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"manual","version":"0"},"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`
	replies := mcpTranscript(t, t.TempDir(), nil, discover)
	require.Len(t, replies, 1)
	var result struct {
		ResultType        string
		Capabilities      mcp.ServerCapabilities
		SupportedVersions []string
		Meta              struct {
			ServerInfo mcp.Implementation `json:"io.modelcontextprotocol/serverInfo"`
		} `json:"_meta"`
	}
	require.NoError(t, json.Unmarshal(replies[1], &result))
	require.Equal(t, "complete", result.ResultType)
	require.Equal(t, mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}, Resources: &mcp.ResourceCapabilities{}}, result.Capabilities)
	require.Equal(t, "d2", result.Meta.ServerInfo.Name)
	require.Contains(t, result.SupportedVersions, "2026-07-28")
	require.Contains(t, result.SupportedVersions, "2025-06-18")
}

func TestMCPEnvironment(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "example.d2"), []byte("a -> b"), 0600))
	env := []string{"D2_MCP_ROOT=" + dir, "D2_MCP_NO_UI=true", "D2_MCP_TIMEOUT=600"}
	const check = `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"check","arguments":{"path":"example.d2"}}}`
	replies := mcpTranscript(t, dir, env, legacyMCPHandshake+mcpListTools+"\n"+check)
	var init mcp.InitializeResult
	require.NoError(t, json.Unmarshal(replies[1], &init))
	require.Equal(t, &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}}, init.Capabilities)
	var tools mcp.ListToolsResult
	require.NoError(t, json.Unmarshal(replies[2], &tools))
	require.Len(t, tools.Tools, 3)
	require.Equal(t, "render", tools.Tools[2].Name)
	require.Nil(t, tools.Tools[2].Meta)
	var result struct {
		StructuredContent struct {
			OK     bool
			Shapes int
		}
	}
	require.NoError(t, json.Unmarshal(replies[3], &result))
	require.True(t, result.StructuredContent.OK)
	require.Equal(t, 2, result.StructuredContent.Shapes)
}

func TestMCPFlags(t *testing.T) {
	dir := t.TempDir()
	for _, test := range []struct {
		name      string
		args      []string
		env       []string
		wantError string
		wantHelp  bool
	}{
		{name: "positional", args: []string{"extra"}, wantError: "mcp takes no arguments"},
		{name: "root", args: []string{"--root", filepath.Join(dir, "missing")}, wantError: "is not a directory"},
		{name: "short timeout", args: []string{"--timeout", "0"}, wantError: "--timeout must be between 1 and 600 seconds"},
		{name: "long timeout", args: []string{"--timeout", "601"}, wantError: "--timeout must be between 1 and 600 seconds"},
		{name: "environment timeout", env: []string{"D2_MCP_TIMEOUT=601"}, wantError: "--timeout must be between 1 and 600 seconds"},
		{name: "flag overrides environment", args: []string{"--timeout", "1"}, env: []string{"D2_MCP_TIMEOUT=0"}},
		{name: "help", args: []string{"--help"}, wantHelp: true},
		{name: "short help", args: []string{"-h"}, wantHelp: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			state := testMain(dir, xos.NewEnv(test.env), append([]string{"mcp"}, test.args...)...)
			state.Stdout = &stdout
			state.Start(t, t.Context())
			defer state.Cleanup(t)
			err := state.Wait(t.Context())
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
			} else {
				require.NoError(t, err)
			}
			if test.wantHelp {
				for _, flag := range []string{"--root", "--no-ui", "--timeout"} {
					require.Contains(t, stdout.String(), flag)
				}
			} else {
				require.Empty(t, strings.TrimSpace(stdout.String()))
			}
		})
	}
}
