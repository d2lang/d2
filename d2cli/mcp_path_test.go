//go:build !d2nomcp

package d2cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestMCPDiagramPaths(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".env", "notes.txt", "diagram.d2.bak", "map.D2"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte("x -> y"), 0600))
	}
	session := newTestMCP(t, mcpConfig{root: root})
	for _, tool := range []string{"check", "render"} {
		t.Run(tool, func(t *testing.T) {
			for _, name := range []string{".env", "notes.txt", "diagram.d2.bak", filepath.Join(root, ".env")} {
				res := callMCP(t, session, tool, map[string]any{"path": name})
				require.True(t, res.IsError, name)
				require.Contains(t, res.Content[0].(*mcp.TextContent).Text, "expected a .d2 file")
			}
			require.False(t, callMCP(t, session, tool, map[string]any{"path": "map.D2"}).IsError)
		})
	}
	if runtime.GOOS == "windows" {
		return // Creating symbolic links may require administrator privileges.
	}
	require.NoError(t, os.Symlink("notes.txt", filepath.Join(root, "notes.d2")))
	require.NoError(t, os.Symlink("map.D2", filepath.Join(root, "alias.d2")))
	for _, tool := range []string{"check", "render"} {
		for _, args := range []map[string]any{{"path": "notes.d2"}, {"source": "a: @notes"}} {
			res := callMCP(t, session, tool, args)
			require.True(t, res.IsError)
			require.Contains(t, res.Content[0].(*mcp.TextContent).Text, "expected a .d2 file")
		}
		for _, args := range []map[string]any{{"path": "alias.d2"}, {"source": "a: @alias"}} {
			require.False(t, callMCP(t, session, tool, args).IsError)
		}
	}
}
