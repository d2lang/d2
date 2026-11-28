//go:build !d2nomcp

package d2cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2lint"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
	"github.com/d2lang/d2/internal/d2layoutfeatures"
	"github.com/d2lang/d2/lib/textmeasure"
)

type mcpMotion struct {
	diagram  *d2target.Diagram
	graph    *d2graph.Graph
	path     string
	opts     d2svg.RenderOpts
	interval int
}

func (s *mcpServer) compileMotion(ctx context.Context, in *motionInput) (*mcpMotion, error) {
	if in.Theme != nil && d2themescatalog.Find(*in.Theme).Name == "" {
		return nil, fmt.Errorf(`unknown theme %d; call guide with topic "reference" for the list`, *in.Theme)
	}
	if duration := in.AnimateDuration; duration != nil && (*duration < 0 || *duration > d2target.MaxTweenDurationMS) {
		return nil, fmt.Errorf("animate_duration must be between 0 and %d milliseconds", d2target.MaxTweenDurationMS)
	}
	source, path, msg := s.resolveInput(in.Source, in.Path)
	if msg != "" {
		return nil, errors.New(msg)
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, err
	}
	opts := d2svg.RenderOpts{ThemeID: in.Theme, Sketch: in.Sketch}
	diagram, graph, layout, err := s.compileForRender(ctx, path, source, in.Layout, "png", &opts, ruler, in)
	if err != nil {
		return nil, err
	}
	if err := d2layoutfeatures.Check(layout, graph); err != nil {
		return nil, err
	}
	interval, err := motionReviewInterval(diagram.Config, in.AnimateInterval)
	if err != nil {
		return nil, err
	}
	return &mcpMotion{diagram: diagram, graph: graph, path: path, opts: opts, interval: interval}, nil
}

func (s *mcpServer) storyboard(ctx context.Context, _ *mcp.CallToolRequest, in *motionInput) (*mcp.CallToolResult, any, error) {
	film, err := s.compileMotion(ctx, in)
	if err != nil {
		return mcpCompileFailure(err, "storyboard")
	}
	plan, err := planStoryboard(ctx, film.diagram, film.interval)
	if err != nil {
		return mcpFailure(err.Error())
	}
	assets, err := gifSceneAssetOptions(ctx, film.path, false, len(plan))
	if err != nil {
		return mcpFailure(err.Error())
	}
	defer assets.Resolver.CloseIdleConnections()
	// Like render, resolve glyphs only from fonts that ship with D2.
	fonts, err := bundledFontFallbackOptions(len(plan), nil)
	if err != nil {
		return mcpFailure(err.Error())
	}
	var missingImage error
	assets.OnUnavailableImage = func(err error) {
		if missingImage == nil {
			missingImage = err
		}
	}
	data, frames, err := renderStoryboard(ctx, plan, film.opts, assets, fonts)
	if err != nil {
		return mcpFailure(err.Error())
	}
	return mcpStoryboardResult(data, frames, missingImage)
}

func mcpStoryboardResult(data []byte, frames []storyboardFrameInfo, missingImage error) (*mcp.CallToolResult, any, error) {
	info := struct {
		mcpStatus
		Frames []storyboardFrameInfo `json:"frames"`
	}{
		mcpStatus: mcpStatus{OK: true, Diagnostics: []mcpDiagnostic{}, Warnings: []string{}},
		Frames:    frames,
	}
	summary := fmt.Sprintf("Storyboard with %s in playback order.", plural(len(frames), "frame"))
	if missingImage != nil {
		warning := fmt.Sprintf("could not bundle an image: %v", missingImage)
		info.Warnings = append(info.Warnings, warning)
		summary += "\nWarning: " + warning
	}
	sheet := &mcp.ImageContent{MIMEType: "image/png"}
	res := &mcp.CallToolResult{Content: []mcp.Content{sheet, mcpText(summary)}, StructuredContent: info}
	// Account for escaped board names and warnings before attaching the image.
	// Leave room for the JSON-RPC envelope, request ID and protocol fields.
	header, err := json.Marshal(res)
	if err != nil {
		return mcpFailure(err.Error())
	}
	const envelopeBytes = 1024
	if base64.StdEncoding.EncodedLen(len(data))+len(header)+envelopeBytes > mcp.DefaultMaxLineLength {
		return mcpFailure("storyboard exceeds the MCP stdio response size limit. Split the animation into smaller files, or run d2 storyboard locally to save the full PNG.")
	}
	sheet.Data = data
	return res, nil, nil
}

func (s *mcpServer) lintMotion(ctx context.Context, _ *mcp.CallToolRequest, in *motionInput) (*mcp.CallToolResult, any, error) {
	film, err := s.compileMotion(ctx, in)
	if err != nil {
		return mcpCompileFailure(err, "lint_motion")
	}
	findings, err := d2lint.Motion(ctx, film.diagram, film.graph, film.interval)
	if err != nil {
		return mcpFailure(err.Error())
	}
	status := mcpStatus{OK: true, Diagnostics: []mcpDiagnostic{}, Warnings: []string{}}
	var summary strings.Builder
	if len(findings) == 0 {
		summary.WriteString("No motion warnings.")
	} else {
		fmt.Fprintf(&summary, "%s:", plural(len(findings), "motion warning"))
	}
	for _, f := range findings {
		status.Diagnostics = append(status.Diagnostics, mcpDiagnostic{
			Code: f.Code, Board: f.Board, Object: f.Object, File: f.Path,
			Line: f.Line, Column: f.Column, EndLine: f.Line, EndColumn: f.Column,
			Message: f.Message, Severity: "warning",
		})
		fmt.Fprintf(&summary, "\n%s:%d:%d  %s  [%s] %s", filepath.Base(f.Path), f.Line, f.Column, f.Board, f.Code, f.Message)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{mcpText(summary.String())}}, status, nil
}
