//go:build !d2nomcp

package d2cli

import (
	"context"
	_ "embed"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed mcpassets/viewer.html
var mcpViewerHTML string

const mcpViewerURI = "ui://d2/viewer"
const mcpViewerMIME = "text/html;profile=mcp-app"

func addViewerResource(server *mcp.Server) {
	server.AddResource(&mcp.Resource{
		URI:         mcpViewerURI,
		Name:        "d2_viewer",
		Title:       "D2 diagram viewer",
		Description: "Displays the diagram returned by the render tool.",
		MIMEType:    mcpViewerMIME,
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI:      mcpViewerURI,
			MIMEType: mcpViewerMIME,
			Text:     mcpViewerHTML,
			Meta:     mcp.Meta{"ui": map[string]any{"prefersBorder": true}},
		}}}, nil
	})
}
