//go:build !d2nomcp

package d2cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/d2lang/d2/d2parser"
)

const mcpMaxSourceBytes = 1 << 20

func (s *mcpServer) resolveInput(source, path *string) (text, inputPath, msg string) {
	switch {
	case source == nil && path == nil:
		return "", "", `pass the D2 text in "source" or a file in "path"`
	case source != nil && path != nil:
		return "", "", `pass only one of "source" and "path"`
	case source != nil:
		if len(*source) > mcpMaxSourceBytes {
			return "", "", "source is larger than 1 MiB"
		}
		return *source, filepath.Join(s.cfg.root, "inline.d2"), ""
	}
	if s.cfg.root == "" {
		return "", "", `file access is disabled. Pass the D2 text in "source", or start the server with --root DIR`
	}
	p := filepath.Clean(*path)
	if !filepath.IsAbs(p) {
		p = filepath.Join(s.cfg.root, p)
	}
	f, err := s.files.Open(p)
	if err != nil {
		return "", "", fmt.Sprintf("cannot read %s: %v", *path, err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, mcpMaxSourceBytes+1))
	if err != nil {
		return "", "", fmt.Sprintf("cannot read %s: %v", *path, err)
	}
	if len(b) > mcpMaxSourceBytes {
		return "", "", "file is larger than 1 MiB"
	}
	return string(b), p, ""
}

func plural(n int, word string) string {
	if n != 1 {
		word += "s"
	}
	return fmt.Sprintf("%d %s", n, word)
}

func mcpCompileFailure(err error, tool string) (*mcp.CallToolResult, any, error) {
	result, status := mcpCompileError(err, tool)
	return result, status, nil
}

func mcpCompileError(err error, tool string) (*mcp.CallToolResult, mcpStatus) {
	status := mcpStatus{Diagnostics: []mcpDiagnostic{}, Warnings: []string{}}
	text := err.Error()
	var pe *d2parser.ParseError
	if errors.As(err, &pe) && len(pe.Errors) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "D2 failed to compile (%s):\n", plural(len(pe.Errors), "error"))
		for _, e := range pe.Errors {
			d := mcpDiagnostic{
				File: e.Range.Path, Line: e.Range.Start.Line + 1, Column: e.Range.Start.Column + 1,
				EndLine: e.Range.End.Line + 1, EndColumn: e.Range.End.Column + 1,
				Message: strings.TrimPrefix(e.Message, e.Range.String()+": "), Severity: "error",
			}
			status.Diagnostics = append(status.Diagnostics, d)
			fmt.Fprintf(&b, "%s:%d:%d: %s\n", filepath.Base(d.File), d.Line, d.Column, d.Message)
		}
		fmt.Fprintf(&b, "Fix the source and call %s again.", tool)
		if tool == "render" {
			b.WriteString(" Call guide if you are unsure about the syntax.")
		}
		text = b.String()
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{mcpText(text)}}, status
}
