package lspserver

import "github.com/d2lang/d2/d2lsp"

func completions(doc document, pos lspPosition) []completionItem {
	items, err := d2lsp.GetCompletionItemsUTF16(doc.text, pos.Line, pos.Character)
	if err != nil {
		return []completionItem{}
	}
	out := make([]completionItem, 0, len(items))
	for _, item := range items {
		var kind int
		switch item.Kind {
		case d2lsp.KeywordCompletion:
			kind = 14 // Keyword
		case d2lsp.StyleCompletion:
			kind = 10 // Property
		case d2lsp.ShapeCompletion:
			kind = 12 // Value
		}
		completion := completionItem{
			Label: item.Label, Kind: kind, Detail: item.Detail, InsertText: item.InsertText,
		}
		// A hint has no insertable value. Without an explicit edit LSP
		// clients insert its descriptive label as diagram source.
		if item.InsertText == "" {
			completion.TextEdit = &textEdit{Range: lspRange{Start: pos, End: pos}}
		}
		out = append(out, completion)
	}
	return out
}
