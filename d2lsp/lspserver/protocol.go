package lspserver

type lspPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspRange struct {
	Start lspPosition `json:"start"`
	End   lspPosition `json:"end"`
}

type location struct {
	URI   string   `json:"uri"`
	Range lspRange `json:"range"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type textDocumentPositionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     lspPosition            `json:"position"`
}

type didOpenParams struct {
	TextDocument struct {
		URI     string `json:"uri"`
		Version int32  `json:"version"`
		Text    string `json:"text"`
	} `json:"textDocument"`
}

type didChangeParams struct {
	TextDocument struct {
		URI     string `json:"uri"`
		Version int32  `json:"version"`
	} `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}

type documentParams struct { // didSave, didClose, documentSymbol, foldingRange, documentLink, formatting, documentColor
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type diagnostic struct {
	Range              lspRange             `json:"range"`
	Severity           int                  `json:"severity"`
	Source             string               `json:"source"`
	Message            string               `json:"message"`
	RelatedInformation []relatedInformation `json:"relatedInformation,omitempty"`
}

type relatedInformation struct {
	Location location `json:"location"`
	Message  string   `json:"message"`
}

type publishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     int32        `json:"version"`
	Diagnostics []diagnostic `json:"diagnostics"`
}

type textEdit struct {
	Range   lspRange `json:"range"`
	NewText string   `json:"newText"`
}

type workspaceEdit struct {
	Changes map[string][]textEdit `json:"changes"`
}

type completionItem struct {
	Label      string `json:"label"`
	Kind       int    `json:"kind,omitempty"`
	Detail     string `json:"detail,omitempty"`
	InsertText string `json:"insertText,omitempty"`
}

type hover struct {
	Contents markupContent `json:"contents"`
	Range    *lspRange     `json:"range,omitempty"`
}

type markupContent struct {
	Kind  string `json:"kind"` // "markdown" or "plaintext"
	Value string `json:"value"`
}

type documentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          lspRange         `json:"range"`
	SelectionRange lspRange         `json:"selectionRange"`
	Children       []documentSymbol `json:"children,omitempty"`
}

type foldingRange struct {
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Kind      string `json:"kind,omitempty"`
}

type documentLink struct {
	Range  lspRange `json:"range"`
	Target string   `json:"target"`
}

type referenceParams struct {
	textDocumentPositionParams
	Context struct {
		IncludeDeclaration bool `json:"includeDeclaration"`
	} `json:"context"`
}

type renameParams struct {
	textDocumentPositionParams
	NewName string `json:"newName"`
}

type prepareRenameResult struct {
	Range       lspRange `json:"range"`
	Placeholder string   `json:"placeholder"`
}

type lspColor struct {
	Red   float64 `json:"red"`
	Green float64 `json:"green"`
	Blue  float64 `json:"blue"`
	Alpha float64 `json:"alpha"`
}

type colorInformation struct {
	Range lspRange `json:"range"`
	Color lspColor `json:"color"`
}

type colorPresentationParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Color        lspColor               `json:"color"`
	Range        lspRange               `json:"range"`
}

type colorPresentation struct {
	Label    string    `json:"label"`
	TextEdit *textEdit `json:"textEdit,omitempty"`
}
