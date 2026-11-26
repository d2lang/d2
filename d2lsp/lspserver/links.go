package lspserver

import (
	"os"
	"strings"

	"github.com/d2lang/d2/d2ast"
)

func (s *Server) links(doc document, root *d2ast.Map) []documentLink {
	links := []documentLink{}
	d2ast.Walk(root, func(n d2ast.Node) bool {
		switch n := n.(type) {
		case *d2ast.Import:
			if target := s.importLocation(doc, n); target != nil {
				links = append(links, documentLink{Range: toLSPRange(n.Range), Target: target.URI})
			}
		case *d2ast.Key:
			path := attributePath(n)
			if path == nil || len(path.Path) == 0 || path.Last() == nil {
				break
			}
			switch d2ast.Keyword(path.Last().Unbox()) {
			case "link", "icon":
				if value := literalStringValue(n); value != nil {
					target := value.ScalarString()
					if strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://") {
						links = append(links, documentLink{Range: toLSPRange(value.GetRange()), Target: target})
					}
				}
			}
		}
		return true
	})
	return links
}

func attributePath(key *d2ast.Key) *d2ast.KeyPath {
	if len(key.Edges) > 0 {
		return key.EdgeKey
	}
	return key.Key
}

// ScalarString on an interpolated AST value returns only its first fragment.
// An edit or hyperlink must refer to the entire literal.
func literalStringValue(key *d2ast.Key) d2ast.String {
	value, ok := key.Primary.Unbox().(d2ast.String)
	if !ok {
		value = key.Value.StringBox().Unbox()
	}
	if value == nil || len(value.Children()) != 0 {
		return nil
	}
	return value
}

func (s *Server) importLocation(doc document, imp *d2ast.Import) *location {
	path := importPath(doc.path, imp)
	if path == "" {
		return nil
	}
	uri := s.uriForPath(doc, path)
	if _, open := s.document(uri); !open {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
	}
	return &location{URI: uri}
}

func (s *Server) uriForPath(doc document, path string) string {
	if path == doc.path {
		return doc.uri
	}
	if open, ok := s.documentForPath(path); ok {
		return open.uri
	}
	return pathToURI(path)
}
