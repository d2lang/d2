package lspserver

import (
	"fmt"
	"slices"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2ir"
)

func (s *Server) keywordHover(root *d2ast.Map, pos lspPosition) *hover {
	path := nodePathAt(root, pos)
	if len(path) < 2 {
		return nil
	}
	if _, ok := path[len(path)-2].(*d2ast.KeyPath); !ok {
		return nil
	}
	word, ok := path[len(path)-1].(d2ast.String)
	if !ok {
		return nil
	}
	keyword := d2ast.Keyword(word)
	doc, ok := keywordDocs[keyword]
	if !ok {
		return nil
	}
	return s.makeHover(keyword, doc.group, doc.text, word.GetRange())
}

func (s *Server) makeHover(name, group, text string, r d2ast.Range) *hover {
	kind := "plaintext"
	if s.hoverMarkdown {
		name = "**" + name + "**"
		kind = "markdown"
	}
	rng := toLSPRange(r)
	return &hover{
		Contents: markupContent{Kind: kind, Value: fmt.Sprintf("%s (%s)\n\n%s", name, group, text)},
		Range:    &rng,
	}
}

func (s *Server) symbolHover(doc document, snap *snapshot, pos lspPosition) *hover {
	sym := snap.symbolAt(doc.path, pos)
	if sym == nil || sym.hit.field == nil {
		return nil
	}
	field := sym.hit.field
	name := field.Name.ScalarString()
	var group string
	var lines []string
	switch sym.hit.kind {
	case kindObject:
		group = "shape"
		label := name
		if value := field.Primary(); value != nil {
			label = value.Value.ScalarString()
		}
		if m := field.Map(); m != nil {
			if attr := m.GetField(d2ast.FlatUnquotedString("label")); attr != nil && attr.Primary() != nil {
				label = attr.Primary().Value.ScalarString()
			}
		}
		name = shapePath(field)
		lines = append(lines, "label: "+label)
		if m := field.Map(); m != nil {
			if shape := m.GetField(d2ast.FlatUnquotedString("shape")); shape != nil && shape.Primary() != nil {
				lines = append(lines, "shape: "+shape.Primary().Value.ScalarString())
			}
		}
	case kindClass:
		group = "class"
	case kindVariable:
		group = "variable"
		if value := field.Primary(); value != nil {
			lines = append(lines, "value: "+value.Value.ScalarString())
		}
	case kindBoard:
		group = "board"
	default:
		return nil
	}

	count := len(sym.occurrences)
	references := "1 reference"
	if count != 1 {
		references = fmt.Sprintf("%d references", count)
	}
	lines = append(lines, references)
	return s.makeHover(name, group, strings.Join(lines, "\n"), sym.hit.rng)
}

func shapePath(field *d2ir.Field) string {
	var path []d2ast.String
	for field != nil {
		path = append(path, field.Name)
		m, ok := field.Parent().(*d2ir.Map)
		if !ok || m.Root() || d2ir.NodeBoardKind(m) != "" {
			break
		}
		field, _ = m.Parent().(*d2ir.Field)
	}
	slices.Reverse(path)
	return d2format.FormatKeyPath(path)
}
