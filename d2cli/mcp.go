//go:build !d2nomcp

package d2cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/pflag"

	"github.com/d2lang/util-go/xmain"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/localfile"
	"github.com/d2lang/d2/lib/netpolicy"
	"github.com/d2lang/d2/lib/version"
)

const mcpInstructions = `D2 turns text into diagrams: software architecture, sequence, entity-relationship and class diagrams. It is not for charts, Gantt charts or mind maps.
Workflow: call guide once per conversation, write D2, call render, look at the image, fix and render again.
Use plain identifiers for names and quoted strings for labels, for example api: "API gateway".
Pass layout "tala" for architecture diagrams with nested containers.`

type mcpConfig struct {
	root    string // absolute directory, or "" for no file access
	ui      bool
	timeout time.Duration
}

type mcpServer struct {
	ms      *xmain.State
	cfg     mcpConfig
	files   localfile.Policy
	layouts func(string) (d2graph.LayoutGraph, error)
	routers func(string) (d2graph.RouteEdges, error)
	sem     chan struct{}
}

func mcpCmd(ctx context.Context, ms *xmain.State, args []string) error {
	opts := xmain.NewOpts(ms.Env, args)
	rootFlag := opts.String("D2_MCP_ROOT", "root", "", "", "directory that diagrams may read; unset disables local file access")
	noUIFlag, err := opts.Bool("D2_MCP_NO_UI", "no-ui", "", false, "do not advertise the inline diagram viewer")
	if err != nil {
		return err
	}
	timeoutFlag, err := opts.Int64("D2_MCP_TIMEOUT", "timeout", "", 60, "seconds allowed for one tool call")
	if err != nil {
		return err
	}
	err = opts.Flags.Parse(opts.Args)
	if errors.Is(err, pflag.ErrHelp) {
		fmt.Fprintf(ms.Stdout, "Usage:\n  %s mcp [--root dir] [--no-ui] [--timeout seconds]\n\nFlags:\n%s\n", filepath.Base(ms.Name), opts.Defaults())
		return nil
	}
	if err != nil {
		return xmain.UsageErrorf("failed to parse flags: %v", err)
	}
	if len(opts.Flags.Args()) > 0 {
		return xmain.UsageErrorf("mcp takes no arguments")
	}
	if *timeoutFlag < 1 || *timeoutFlag > 600 {
		return xmain.UsageErrorf("--timeout must be between 1 and 600 seconds")
	}
	cfg := mcpConfig{ui: !*noUIFlag, timeout: time.Duration(*timeoutFlag) * time.Second}
	if *rootFlag != "" {
		abs, err := filepath.Abs(ms.AbsPath(*rootFlag))
		if err != nil {
			return xmain.UsageErrorf("--root: %s is not a directory", *rootFlag)
		}
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return xmain.UsageErrorf("--root: %s is not a directory", *rootFlag)
		}
		cfg.root = abs
	}
	// Layout engines read their options from ms.Opts. Give them the
	// subcommand's options so they fall back to their defaults.
	ms.Opts = opts

	server, cleanup, err := newMCPServer(ctx, ms, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	err = server.Run(ctx, mcpTransport(ms))
	if err == nil || errors.Is(err, io.EOF) || ctx.Err() != nil {
		return nil
	}
	return err
}

func newMCPServer(ctx context.Context, ms *xmain.State, cfg mcpConfig) (*mcp.Server, func(), error) {
	var stderr io.Writer = io.Discard
	if ms.Stderr != nil {
		stderr = ms.Stderr
	}
	s := &mcpServer{
		ms:      ms,
		cfg:     cfg,
		layouts: LayoutResolver(ctx, ms),
		routers: RouterResolver(ctx, ms),
		sem:     make(chan struct{}, 2),
	}
	cleanup := func() {}
	if cfg.root != "" {
		policy, err := localfile.Rooted(cfg.root)
		if err != nil {
			return nil, nil, err
		}
		s.files = policy
		cleanup = func() { _ = policy.Close() }
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:       "d2",
		Title:      "D2",
		Version:    version.Version,
		WebsiteURL: "https://d2lang.com",
	}, &mcp.ServerOptions{
		Instructions: mcpInstructions,
		Logger:       slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})

	server.AddReceivingMiddleware(mcpToolOrder, mcpErrorContent)
	mcp.AddTool(server, guideTool(), mcpHandler(s, "guide", s.guide))
	mcp.AddTool(server, checkTool(), mcpHandler(s, "check", s.check))
	mcp.AddTool(server, renderTool(cfg.ui), mcpHandler(s, "render", s.render))
	if cfg.ui {
		addViewerResource(server)
	}
	return server, cleanup, nil
}

type mcpDiagnostic struct {
	File      string `json:"file,omitempty"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	EndLine   int    `json:"end_line"`
	EndColumn int    `json:"end_column"`
	Message   string `json:"message"`
	Severity  string `json:"severity"`
}

type mcpStatus struct {
	OK          bool            `json:"ok"`
	Diagnostics []mcpDiagnostic `json:"diagnostics"`
	Warnings    []string        `json:"warnings"`
}

func mcpFailure(msg string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{mcpText(msg)}}, mcpStatus{Diagnostics: []mcpDiagnostic{}, Warnings: []string{}}, nil
}

func mcpText(s string) mcp.Content { return &mcp.TextContent{Text: s} }

// Typed tool validation happens in the SDK before our handlers. Give those
// failures the same structured status as errors raised by a handler.
func mcpErrorContent(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		res, err := next(ctx, method, req)
		if r, ok := res.(*mcp.CallToolResult); ok && r.IsError && r.StructuredContent == nil {
			r.StructuredContent = mcpStatus{Diagnostics: []mcpDiagnostic{}, Warnings: []string{}}
		}
		return res, err
	}
}

func mcpHandler[I any](s *mcpServer, name string, h mcp.ToolHandlerFor[I, any]) mcp.ToolHandlerFor[I, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in I) (res *mcp.CallToolResult, out any, err error) {
		defer func() {
			if p := recover(); p != nil {
				if s.ms.Stderr != nil {
					fmt.Fprintf(s.ms.Stderr, "mcp: panic in %s: %v\n%s", name, p, debug.Stack())
				}
				res, out, err = mcpFailure(fmt.Sprintf("internal error: %v", p))
			}
		}()
		ctx, cancel := context.WithTimeout(ctx, s.cfg.timeout)
		defer cancel()
		ctx = withLocalFiles(ctx, s.files)
		ctx = netpolicy.WithPolicy(ctx, netpolicy.Policy{})
		if name != "guide" {
			select {
			case s.sem <- struct{}{}:
				defer func() { <-s.sem }()
			case <-ctx.Done():
			}
		}
		if ctx.Err() == nil {
			res, out, err = h(ctx, req, in)
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return mcpFailure(fmt.Sprintf("timed out after %s", s.cfg.timeout))
		}
		if err := ctx.Err(); err != nil {
			return mcpFailure(err.Error())
		}
		return
	}
}
