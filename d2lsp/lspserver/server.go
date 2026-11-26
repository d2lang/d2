// Package lspserver implements the stdio language server for D2.
package lspserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"runtime/debug"
	"slices"
	"sync"

	"github.com/d2lang/d2/lib/localfile"
)

type Options struct {
	In      io.Reader
	Out     io.Writer
	Log     io.Writer
	Version string
}

type Server struct {
	conn    *conn
	log     *log.Logger
	version string

	mu      sync.Mutex
	workers sync.WaitGroup
	docs    map[string]document
	jobs    map[string]*diagnosticJob
	epoch   uint64
	overlay overlay

	snapshots     map[string]*snapshot
	initialized   bool
	shutdown      bool
	watchDynamic  bool
	hoverMarkdown bool
}

func New(opts Options) *Server {
	if opts.Log == nil {
		opts.Log = io.Discard
	}
	s := &Server{
		conn:          &conn{r: bufio.NewReader(opts.In), w: opts.Out},
		log:           log.New(opts.Log, "d2 lsp: ", 0),
		docs:          make(map[string]document),
		jobs:          make(map[string]*diagnosticJob),
		version:       opts.Version,
		hoverMarkdown: true,
		snapshots:     make(map[string]*snapshot),
	}
	s.overlay = overlay{server: s, Policy: localfile.Unrestricted()}
	return s
}

// Run serves until exit, EOF, or a transport error. A graceful exit requires shutdown first.
func (s *Server) Run(ctx context.Context) int {
	defer s.stopDiagnostics()
	for {
		body, err := s.conn.read()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.log.Print(err)
			}
			return 1
		}
		var msg incoming
		if decodeErr := json.Unmarshal(body, &msg); decodeErr != nil {
			code := -32700
			message := "parse error"
			if json.Valid(body) {
				code, message = -32600, "invalid request"
			}
			err = s.conn.write(errResponse{JSONRPC: "2.0", Error: rpcError{code, message}})
		} else if msg.Method == "" {
			continue // a response to a server request
		} else if msg.Method == "exit" && msg.ID == nil {
			if s.shutdown {
				return 0
			}
			return 1
		} else if msg.ID == nil {
			err = s.handleNotification(ctx, msg)
		} else {
			result, rerr := s.handleRequest(ctx, msg)
			if rerr != nil {
				err = s.conn.write(errResponse{JSONRPC: "2.0", ID: msg.ID, Error: *rerr})
			} else {
				err = s.conn.write(okResponse{JSONRPC: "2.0", ID: msg.ID, Result: result})
			}
		}
		if err != nil {
			s.log.Print(err)
			return 1
		}
	}
}

func (s *Server) handleRequest(ctx context.Context, msg incoming) (result any, rerr *rpcError) {
	defer func() {
		if v := recover(); v != nil {
			s.log.Printf("%s: %v\n%s", msg.Method, v, debug.Stack())
			result, rerr = nil, &rpcError{-32603, "internal error"}
		}
	}()
	switch {
	case s.shutdown:
		return nil, &rpcError{-32600, "server has shut down"}
	case msg.Method == "initialize":
		if s.initialized {
			return nil, &rpcError{-32600, "server already initialized"}
		}
		return s.initialize(msg.Params)
	case !s.initialized:
		return nil, &rpcError{-32002, "server not initialized"}
	case msg.Method == "shutdown":
		s.shutdown = true
		s.stopDiagnostics()
		return nil, nil
	default:
		return s.documentRequest(ctx, msg)
	}
}

func (s *Server) documentRequest(ctx context.Context, msg incoming) (any, *rpcError) {
	var p textDocumentPositionParams
	switch msg.Method {
	case "textDocument/formatting", "textDocument/documentFormatting", "textDocument/completion", "textDocument/hover",
		"textDocument/documentSymbol", "textDocument/foldingRange", "textDocument/documentLink",
		"textDocument/definition", "textDocument/references", "textDocument/prepareRename", "textDocument/rename",
		"textDocument/documentColor", "textDocument/colorPresentation":
	default:
		return nil, &rpcError{-32601, "method not found"}
	}
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return nil, &rpcError{-32602, err.Error()}
	}
	doc, ok := s.document(p.TextDocument.URI)
	if !ok {
		return nil, &rpcError{-32602, "document is not open"}
	}
	if len(doc.text) > maxDocumentBytes {
		return nil, nil
	}
	if p.Position.Line < 0 || p.Position.Character < 0 {
		return nil, &rpcError{-32602, "position must be nonnegative"}
	}
	switch msg.Method {
	case "textDocument/formatting", "textDocument/documentFormatting":
		return formatDocument(doc), nil
	case "textDocument/completion":
		return completions(doc, p.Position), nil
	case "textDocument/colorPresentation":
		var params colorPresentationParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, &rpcError{-32602, err.Error()}
		}
		return presentColor(params), nil
	}
	snap := s.analyze(ctx, doc)
	switch msg.Method {
	case "textDocument/documentSymbol":
		return symbols(snap.ast), nil
	case "textDocument/foldingRange":
		return folds(snap.ast), nil
	case "textDocument/documentLink":
		return s.links(doc, snap.ast), nil
	case "textDocument/documentColor":
		return colors(snap.ast), nil
	case "textDocument/hover":
		if h := s.keywordHover(snap.ast, p.Position); h != nil {
			return h, nil
		}
	}
	s.ensureIR(ctx, doc, snap)
	switch msg.Method {
	case "textDocument/definition":
		return s.definition(doc, snap, p.Position), nil
	case "textDocument/references":
		var ref referenceParams
		if err := json.Unmarshal(msg.Params, &ref); err != nil {
			return nil, &rpcError{-32602, err.Error()}
		}
		return s.references(doc, snap, p.Position, ref.Context.IncludeDeclaration), nil
	case "textDocument/prepareRename":
		return prepareRename(doc, snap, p.Position), nil
	case "textDocument/rename":
		var params renameParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, &rpcError{-32602, err.Error()}
		}
		if snap.ir == nil {
			return nil, nil
		}
		return s.rename(doc, snap, p.Position, params.NewName)
	case "textDocument/hover":
		return s.symbolHover(doc, snap, p.Position), nil
	default:
		return nil, &rpcError{-32601, "method not found"}
	}
}

func (s *Server) initialize(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Capabilities struct {
			Workspace struct {
				DidChangeWatchedFiles struct {
					DynamicRegistration bool
				}
			}
			TextDocument struct {
				Hover struct {
					ContentFormat []string
				}
			}
		}
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{-32602, err.Error()}
		}
	}
	s.initialized = true
	s.watchDynamic = p.Capabilities.Workspace.DidChangeWatchedFiles.DynamicRegistration
	formats := p.Capabilities.TextDocument.Hover.ContentFormat
	s.hoverMarkdown = formats == nil || slices.Contains(formats, "markdown")
	return map[string]any{
		"capabilities": map[string]any{
			"positionEncoding":           "utf-16",
			"documentFormattingProvider": true,
			"documentSymbolProvider":     true,
			"definitionProvider":         true,
			"renameProvider":             map[string]bool{"prepareProvider": true},
			"colorProvider":              true,
			"referencesProvider":         true,
			"hoverProvider":              true,
			"foldingRangeProvider":       true,
			"documentLinkProvider":       map[string]bool{"resolveProvider": false},
			"completionProvider":         map[string]any{"triggerCharacters": []string{".", ":"}},
			"textDocumentSync": map[string]any{
				"openClose": true, "change": 1, "save": map[string]bool{"includeText": false},
			},
		},
		"serverInfo": map[string]string{"name": "d2", "version": s.version},
	}, nil
}

func (s *Server) handleNotification(ctx context.Context, msg incoming) error {
	defer func() {
		if v := recover(); v != nil {
			s.log.Printf("%s: %v\n%s", msg.Method, v, debug.Stack())
		}
	}()
	if !s.initialized || s.shutdown {
		return nil
	}
	if msg.Method == "initialized" && s.watchDynamic {
		s.watchDynamic = false // register only once
		return s.conn.write(map[string]any{
			"jsonrpc": "2.0", "id": "d2-watch-1", "method": "client/registerCapability",
			"params": map[string]any{
				"registrations": []map[string]any{{
					"id": "d2-watch-d2-files", "method": "workspace/didChangeWatchedFiles",
					"registerOptions": map[string]any{
						"watchers": []map[string]string{{"globPattern": "**/*.d2"}},
					},
				}},
			},
		})
	}
	return s.updateDocuments(ctx, msg)
}
