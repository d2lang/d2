//go:build !d2nomcp

package d2cli

import (
	"bytes"
	"image/png"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestMCPPNGUnavailableIconWarnings(t *testing.T) {
	for _, tc := range []struct {
		name string
		ui   bool
	}{{"no-ui", false}, {"viewer", true}} {
		t.Run(tc.name, func(t *testing.T) {
			session := newTestMCP(t, mcpConfig{root: t.TempDir(), ui: tc.ui})
			result := callMCP(t, session, "render", map[string]any{
				"source": "a.icon: missing.png\nb.icon: missing.png",
			})
			require.False(t, result.IsError)
			require.Len(t, result.Content, 2)
			_, err := png.DecodeConfig(bytes.NewReader(result.Content[0].(*mcp.ImageContent).Data))
			require.NoError(t, err)

			warnings := mcpFields(t, result)["warnings"].([]any)
			require.Len(t, warnings, 1)
			warning := warnings[0].(string)
			require.Contains(t, warning, "could not bundle 1 image")
			require.Contains(t, warning, "missing.png")
			summary := result.Content[1].(*mcp.TextContent).Text
			require.Contains(t, summary, "Warning: "+warning)
			require.Equal(t, 1, strings.Count(summary, "Warning: "))
			if tc.ui {
				require.Contains(t, result.Meta[mcpSVGMetaKey], string(unavailableImageURI))
			} else {
				require.NotContains(t, result.Meta, mcpSVGMetaKey)
			}
		})
	}
}
