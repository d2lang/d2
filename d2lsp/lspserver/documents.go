package lspserver

import (
	"context"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/d2lang/d2/lib/localfile"
)

const maxDocumentBytes = 2 << 20

type document struct {
	uri     string
	path    string
	version int32
	text    string
}

func (s *Server) document(uri string) (document, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, ok := s.docs[uri]
	return doc, ok
}

func (s *Server) textForPath(path string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, doc := range s.docs {
		if doc.path == path {
			return doc.text, true
		}
	}
	return "", false
}

func (s *Server) fileSystem(doc document) fs.FS {
	if doc.path == "" {
		return nil
	}
	return s.overlay
}

// Open documents shadow disk files, including files which have not been saved yet.
type overlay struct {
	server *Server
	localfile.Policy
}

func (o overlay) Open(name string) (fs.File, error) {
	if text, ok := o.server.textForPath(filepath.Clean(name)); ok {
		return &memFile{
			Reader: strings.NewReader(text),
			info:   memInfo{name: filepath.Base(name), size: int64(len(text))},
		}, nil
	}
	return o.Policy.Open(name)
}

type memFile struct {
	*strings.Reader
	info memInfo
}

func (f *memFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *memFile) Close() error               { return nil }

type memInfo struct {
	name string
	size int64
}

func (i memInfo) Name() string       { return i.name }
func (i memInfo) Size() int64        { return i.size }
func (i memInfo) Mode() fs.FileMode  { return 0o444 }
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return false }
func (i memInfo) Sys() any           { return nil }

const editDelay = 150 * time.Millisecond

// Notify and reschedule in one critical section so an old diagnostics job
// cannot publish between a document change and an importer's invalidation.
func (s *Server) updateDocuments(ctx context.Context, msg incoming) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var immediateURI string
	switch msg.Method {
	case "textDocument/didOpen":
		var p didOpenParams
		if json.Unmarshal(msg.Params, &p) != nil || p.TextDocument.URI == "" {
			return nil
		}
		d := p.TextDocument
		path, _ := uriToPath(d.URI)
		s.docs[d.URI] = document{uri: d.URI, path: path, version: d.Version, text: d.Text}
		immediateURI = d.URI
	case "textDocument/didChange":
		var p didChangeParams
		if json.Unmarshal(msg.Params, &p) != nil || len(p.ContentChanges) == 0 {
			return nil
		}
		doc, ok := s.docs[p.TextDocument.URI]
		if !ok {
			return nil
		}
		doc.text = p.ContentChanges[len(p.ContentChanges)-1].Text
		doc.version = p.TextDocument.Version
		s.docs[doc.uri] = doc
	case "textDocument/didSave", "textDocument/didClose":
		var p documentParams
		if json.Unmarshal(msg.Params, &p) != nil {
			return nil
		}
		uri := p.TextDocument.URI
		if _, ok := s.docs[uri]; !ok {
			return nil
		}
		if msg.Method == "textDocument/didSave" {
			immediateURI = uri
		} else {
			s.cancelDiagnostics(uri)
			delete(s.docs, uri)
			delete(s.snapshots, uri)
			params := map[string]any{"uri": uri, "diagnostics": []diagnostic{}}
			if err := s.conn.write(notification{"2.0", "textDocument/publishDiagnostics", params}); err != nil {
				return err
			}
		}
	case "workspace/didChangeWatchedFiles":
		// Reload imported files when the jobs below compile their documents.
	default:
		return nil
	}

	s.epoch++
	for uri, doc := range s.docs {
		delay := editDelay
		if uri == immediateURI {
			delay = 0
		}
		s.scheduleDiagnostics(ctx, doc, delay)
	}
	return nil
}

// Each open document has at most one current job. Its identity distinguishes
// generations even when a client closes and reopens a URI at the same version.
type diagnosticJob struct {
	timer  *time.Timer
	cancel context.CancelFunc
}

// scheduleDiagnostics and cancelDiagnostics run with s.mu held.
func (s *Server) scheduleDiagnostics(parent context.Context, doc document, delay time.Duration) {
	s.cancelDiagnostics(doc.uri)
	ctx, cancel := context.WithCancel(parent)
	job := &diagnosticJob{cancel: cancel}
	s.jobs[doc.uri] = job
	s.workers.Add(1)
	job.timer = time.AfterFunc(delay, func() {
		defer s.workers.Done()
		defer cancel()
		if ctx.Err() != nil {
			return
		}
		diags := s.computeDiagnostics(ctx, doc)

		s.mu.Lock()
		defer s.mu.Unlock()
		if s.jobs[doc.uri] != job || ctx.Err() != nil {
			return
		}
		delete(s.jobs, doc.uri)
		params := publishDiagnosticsParams{URI: doc.uri, Version: doc.version, Diagnostics: diags}
		if err := s.conn.write(notification{"2.0", "textDocument/publishDiagnostics", params}); err != nil {
			s.log.Printf("publish diagnostics: %v", err)
		}
	})
}

func (s *Server) cancelDiagnostics(uri string) {
	if job := s.jobs[uri]; job != nil {
		delete(s.jobs, uri)
		job.cancel()
		if job.timer.Stop() {
			s.workers.Done()
		}
	}
}

func (s *Server) stopDiagnostics() {
	s.mu.Lock()
	for uri := range s.jobs {
		s.cancelDiagnostics(uri)
	}
	s.mu.Unlock()
	s.workers.Wait()
}
