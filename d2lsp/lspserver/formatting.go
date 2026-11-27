package lspserver

import (
	"strings"

	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
)

func formatDocument(doc document) []textEdit {
	ast, err := d2parser.Parse(doc.path, strings.NewReader(doc.text), nil)
	if err != nil {
		return nil
	}
	formatted := d2format.Format(ast)
	if formatted == doc.text {
		return []textEdit{}
	}
	return []textEdit{{
		Range:   lspRange{End: lspPosition{Line: strings.Count(doc.text, "\n") + 1}},
		NewText: formatted,
	}}
}
