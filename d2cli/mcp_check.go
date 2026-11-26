//go:build !d2nomcp

package d2cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
)

type checkInfo struct {
	mcpStatus
	Shapes      *int    `json:"shapes,omitempty"`
	Connections *int    `json:"connections,omitempty"`
	Formatted   *string `json:"formatted,omitempty"`
	Changed     *bool   `json:"changed,omitempty"`
}

func (s *mcpServer) check(ctx context.Context, _ *mcp.CallToolRequest, in checkInput) (*mcp.CallToolResult, any, error) {
	source, path, msg := s.resolveInput(in.Source, in.Path)
	if msg != "" {
		return mcpFailure(msg)
	}

	info := checkInfo{}
	if in.Fmt {
		// Formatting uses the original AST even if semantic compilation fails.
		if ast, err := d2parser.ParseContext(ctx, path, strings.NewReader(source), nil); err == nil {
			formatted := d2format.Format(ast)
			info.Formatted, info.Changed = &formatted, new(formatted != source)
		}
	}

	g, _, err := d2compiler.Compile(path, strings.NewReader(source), &d2compiler.CompileOptions{Context: ctx, FS: s.sources})
	if err != nil {
		result, status := mcpCompileError(err, "check")
		info.mcpStatus = status
		return result, info, nil
	}
	info.mcpStatus = mcpStatus{OK: true, Diagnostics: []mcpDiagnostic{}, Warnings: []string{}}
	info.Shapes, info.Connections = new(len(g.Objects)), new(len(g.Edges))
	summary := fmt.Sprintf("No errors. %s, %s.", plural(*info.Shapes, "shape"), plural(*info.Connections, "connection"))
	content := []mcp.Content{mcpText(summary)}
	if info.Changed != nil && *info.Changed {
		content = append(content, mcpText(*info.Formatted))
	}
	return &mcp.CallToolResult{Content: content}, info, nil
}
