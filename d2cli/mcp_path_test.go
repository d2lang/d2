//go:build !d2nomcp

package d2cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/unicode"
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

func TestMCPUTF16Diagram(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "boards")
	require.NoError(t, os.Mkdir(subdir, 0700))
	writeUTF16 := func(name, source string) {
		t.Helper()
		data, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().Bytes([]byte(source))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(name, data, 0600))
	}

	child := filepath.Join(subdir, "child.d2")
	writeUTF16(child, "éé.shape: database")
	entry := filepath.Join(subdir, "entry.d2")
	writeUTF16(entry, "...@child\n")
	session := newTestMCP(t, mcpConfig{root: root})
	for _, tool := range []string{"check", "render"} {
		for _, path := range []string{child, entry} {
			res := callMCP(t, session, tool, map[string]any{"path": path})
			require.True(t, res.IsError)
			diagnostics := mcpFields(t, res)["diagnostics"].([]any)
			require.Len(t, diagnostics, 1)
			require.Equal(t, map[string]any{
				"file": child, "line": float64(1), "end_line": float64(1),
				"column": float64(1 + len("éé.shape: ")), "end_column": float64(1 + len("éé.shape: database")),
				"message": `unknown shape "database"`, "severity": "error",
			}, diagnostics[0])
			require.Contains(t, res.Content[0].(*mcp.TextContent).Text, "child.d2:1:13:")
		}
	}

	// A semantic error still permits formatting the syntactically valid source.
	res := callMCP(t, session, "check", map[string]any{"path": child, "fmt": true})
	require.True(t, res.IsError)
	require.Equal(t, "éé.shape: database\n", mcpFields(t, res)["formatted"])
	require.Equal(t, true, mcpFields(t, res)["changed"])

	writeUTF16(child, "é->界")
	res = callMCP(t, session, "check", map[string]any{"path": child, "fmt": true})
	require.False(t, res.IsError)
	require.Equal(t, "é -> 界\n", mcpFields(t, res)["formatted"])
	require.Equal(t, true, mcpFields(t, res)["changed"])
	writeUTF16(child, "é -> 界\n")
	res = callMCP(t, session, "check", map[string]any{"path": child, "fmt": true})
	require.Equal(t, false, mcpFields(t, res)["changed"])

	// This file is over the on-disk limit even though its decoded text fits.
	writeUTF16(child, strings.Repeat(" ", mcpMaxSourceBytes/2))
	res = callMCP(t, session, "check", map[string]any{"path": child})
	require.True(t, res.IsError)
	require.Equal(t, "file is larger than 1 MiB", res.Content[0].(*mcp.TextContent).Text)
}
