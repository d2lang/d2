//go:build !d2nomcp

package d2cli

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestMCPViewer(t *testing.T) {
	s := newTestMCP(t, mcpConfig{ui: true})
	list, err := s.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"resourceUri": "ui://d2/viewer"}, list.Tools[3].Meta["ui"])
	resources, err := s.ListResources(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, resources.Resources, 1)
	resource := resources.Resources[0]
	require.Equal(t, "d2_viewer", resource.Name)
	require.Equal(t, "ui://d2/viewer", resource.URI)
	result, err := s.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: resource.URI})
	require.NoError(t, err)
	require.Len(t, result.Contents, 1)
	page := result.Contents[0]
	require.Equal(t, "text/html;profile=mcp-app", page.MIMEType)
	require.Equal(t, map[string]any{"prefersBorder": true}, page.Meta["ui"])
	require.Contains(t, page.Text, "ui/initialize")
	require.Contains(t, page.Text, "ui/notifications/tool-result")
	for _, external := range []string{"http://", "https://", "<script src", "<link"} {
		require.NotContains(t, strings.ReplaceAll(page.Text, "https://play.d2lang.com", ""), external)
	}
	r := callMCP(t, s, "render", map[string]any{"source": "x -> y"})
	require.False(t, r.IsError)
	require.Contains(t, r.Meta["com.d2lang/svg"], "<svg")
}
