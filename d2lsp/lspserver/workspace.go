package lspserver

import (
	"cmp"
	"context"
	"io/fs"
	"slices"
	"strings"

	"github.com/d2lang/d2/d2ast"
)

type importRecorder struct {
	overlay
	paths map[string]bool
}

func (r importRecorder) Open(name string) (fs.File, error) {
	r.paths[name] = true
	return r.overlay.Open(name)
}

// Watchers are optional. A closed dependency may have moved since a previous
// hover, so rebuild its source ranges before sending edits to the client.
func (s *Server) refreshDiskImports() {
	open := make(map[string]bool, len(s.docs))
	for _, doc := range s.docs {
		open[doc.path] = true
	}
	for uri, snap := range s.snapshots {
		for path := range snap.imports {
			if !open[path] {
				delete(s.snapshots, uri)
				break
			}
		}
	}
}

// Join only open roots connected by imports. Plain files need no parsing to
// discover dependencies, and simple open-to-open import chains need no IR
// until they are known to be relevant. The compiler observes closed imports
// through the same overlay as the request and records their transitive paths.
func (s *Server) workspaceSnapshot(ctx context.Context, doc document, snap *snapshot) *snapshot {
	if doc.path == "" || len(s.docs) == 1 || snap.ir == nil {
		return snap
	}
	// Requests and changes to the open-document set run on the same goroutine.
	// Match the overlay's choice when a client opens two spellings of one path.
	byPath := make(map[string]document)
	for _, other := range s.docs {
		if other.path != "" && (byPath[other.path].uri == "" || other.uri < byPath[other.path].uri) {
			byPath[other.path] = other
		}
	}
	dependencies := make(map[string][]string)
	importers := make(map[string][]string)
	for path, other := range byPath {
		var imports map[string]bool
		if path == doc.path {
			imports = snap.imports
		} else if len(other.text) <= maxDocumentBytes && strings.Contains(other.text, "@") {
			cache := s.analyze(ctx, other)
			if !cache.attemptedIR || cache.ir == nil {
				imports = make(map[string]bool)
				d2ast.Walk(cache.ast, func(n d2ast.Node) bool {
					if imp, ok := n.(*d2ast.Import); ok {
						if path := importPath(other.path, imp); path != "" {
							imports[path] = true
						}
					}
					return true
				})
				for name := range imports {
					if _, open := byPath[name]; !open {
						// This closed dependency could itself import an open file.
						s.ensureIR(ctx, other, cache)
						break
					}
				}
			}
			if cache.ir != nil {
				imports = cache.imports
			}
		}
		for name := range imports {
			dependencies[path] = append(dependencies[path], name)
			importers[name] = append(importers[name], path)
		}
	}
	connected := reachablePaths(doc.path, dependencies, importers)
	var others []document
	for path := range connected {
		if other, open := byPath[path]; open && path != doc.path && len(other.text) <= maxDocumentBytes {
			others = append(others, other)
		}
	}
	if len(others) == 0 {
		return snap
	}
	slices.SortFunc(others, func(a, b document) int { return cmp.Compare(a.uri, b.uri) })
	merged := *snap
	merged.index = slices.Clone(snap.index)
	merged.imports = connected
	for _, other := range others {
		cache := s.analyze(ctx, other)
		s.ensureIR(ctx, other, cache)
		if cache.ir == nil || cache.parseErr != nil {
			// A sibling importer cannot reference shapes local to this root.
			// Guard edits only in the incomplete file and its dependencies.
			if merged.unresolvedSources == nil {
				merged.unresolvedSources = make(map[string]string)
			}
			for path := range reachablePaths(other.path, dependencies) {
				merged.unresolvedSources[path] = other.uri
			}
		}
		merged.index = append(merged.index, cache.index...)
	}
	groupSymbols(merged.index)
	return &merged
}

func reachablePaths(start string, graphs ...map[string][]string) map[string]bool {
	seen := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		for _, graph := range graphs {
			for _, next := range graph[path] {
				if !seen[next] {
					seen[next] = true
					queue = append(queue, next)
				}
			}
		}
	}
	return seen
}
