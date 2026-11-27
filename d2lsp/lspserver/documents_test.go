package lspserver

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/d2lang/util-go/assert"
)

const badDirection = `direction must be one of up, down, right, left, got "LR"`

func TestDocumentDiagnostics(t *testing.T) {
	const uri = "untitled:Test"
	t.Run("L13_Valid", func(t *testing.T) {
		c := newTestClient(t)
		c.open(uri, "x -> y")
		got := c.waitDiagnosticParams(uri)
		assert.Equal(t, int32(1), got.Version)
		assert.True(t, got.Diagnostics != nil)
		assert.Equal(t, 0, len(got.Diagnostics))
	})
	t.Run("L14_SemanticError", func(t *testing.T) {
		c := newTestClient(t)
		c.open(uri, "direction: LR")
		assert.JSON(t, []diagnostic{{Range: lineRange(0, 11, 13), Severity: 1, Source: "d2", Message: badDirection}}, c.waitDiagnostics(uri))
	})
	t.Run("L15_SyntaxError", func(t *testing.T) {
		c := newTestClient(t)
		c.open(uri, "x: {")
		got := c.waitDiagnostics(uri)
		assert.Equal(t, 1, len(got))
		assert.Equal(t, "maps must be terminated with }", got[0].Message)
	})
	t.Run("L16_ThreeErrors", func(t *testing.T) {
		c := newTestClient(t)
		c.open(uri, "a.direction: LR\nb.direction: LR\nc.direction: LR")
		got := c.waitDiagnostics(uri)
		assert.Equal(t, 3, len(got))
		for line, d := range got {
			assert.Equal(t, badDirection, d.Message)
			assert.Equal(t, lineRange(line, 13, 15), d.Range)
		}
	})
	t.Run("L17_ClearAndVersion", func(t *testing.T) {
		c := newTestClient(t)
		c.open(uri, "direction: LR")
		assert.Equal(t, 1, len(c.waitDiagnostics(uri)))
		c.change(uri, 2, "direction: bad", "direction: right")
		got := c.waitDiagnosticParams(uri)
		assert.Equal(t, int32(2), got.Version)
		assert.JSON(t, []diagnostic{}, got.Diagnostics)
	})
	t.Run("L18_Debounce", func(t *testing.T) {
		c := newTestClient(t)
		c.open(uri, "initial")
		c.waitDiagnostics(uri)
		c.change(uri, 2, "direction: LR")
		c.change(uri, 3, "fixed")
		got := c.waitDiagnosticParams(uri)
		assert.Equal(t, int32(3), got.Version)
		assert.JSON(t, []diagnostic{}, got.Diagnostics)
		c.expectQuiet()
	})
	t.Run("IgnoreStaleVersion", func(t *testing.T) {
		c := newTestClient(t)
		c.open(uri, "initial")
		c.waitDiagnostics(uri)
		c.change(uri, 3, "direction: right")
		c.change(uri, 2, "direction: LR")
		got := c.waitDiagnosticParams(uri)
		assert.Equal(t, int32(3), got.Version)
		assert.JSON(t, []diagnostic{}, got.Diagnostics)
	})
	t.Run("L19_CloseWhileChangePending", func(t *testing.T) {
		c := newTestClient(t)
		c.open(uri, "direction: LR")
		c.waitDiagnostics(uri)
		c.change(uri, 2, "direction: bad")
		c.notify("textDocument/didClose", docParams(uri))
		assert.JSON(t, []diagnostic{}, c.waitDiagnostics(uri))
		c.expectQuiet()
	})
	t.Run("L20_UTF16", func(t *testing.T) {
		for _, tc := range []struct {
			text string
			want lspRange
		}{
			{"\"日本\": x\ndirection: LR", lineRange(1, 11, 13)},
			{"\"😀\"; direction: LR", lineRange(0, 17, 19)},
		} {
			c := newTestClient(t)
			c.open(uri, tc.text)
			got := c.waitDiagnostics(uri)
			assert.Equal(t, 1, len(got))
			assert.Equal(t, tc.want, got[0].Range)
		}
	})
	t.Run("L24_MissingImport", func(t *testing.T) {
		c := newTestClient(t)
		file := pathToURI(filepath.Join(t.TempDir(), "a.d2"))
		c.open(file, "x: @missing")
		got := c.waitDiagnostics(file)
		assert.Equal(t, 1, len(got))
		assert.True(t, strings.Contains(got[0].Message, "missing.d2"))
	})
	t.Run("L25_UntitledImportsDisabled", func(t *testing.T) {
		c := newTestClient(t)
		c.open(uri, "x: @missing")
		got := c.waitDiagnostics(uri)
		assert.Equal(t, 1, len(got))
		assert.True(t, strings.Contains(got[0].Message, "imports are disabled"))
	})
	t.Run("L26_FileTooLarge", func(t *testing.T) {
		c := newTestClient(t)
		c.open(uri, strings.Repeat("x", (2<<20)+1))
		got := c.waitDiagnostics(uri)
		assert.Equal(t, 1, len(got))
		assert.Equal(t, 3, got[0].Severity)
		assert.Equal(t, 0, got[0].Range.Start.Line)
		assert.Equal(t, "file is too large for D2 language features", got[0].Message)
		assert.Equal(t, "null", string(c.request("textDocument/hover", posParams(uri, 0, 0))))
	})
}

// Wait beyond the debounce period to catch duplicate or stale publications.
func (c *testClient) expectQuiet() {
	c.t.Helper()
	assert.Equal(c.t, 0, len(c.pending))
	select {
	case body := <-c.inbox:
		c.t.Fatalf("unexpected server message: %s", body)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestImportDiagnostics(t *testing.T) {
	dir := t.TempDir()
	a, bPath := pathToURI(filepath.Join(dir, "a.d2")), filepath.Join(dir, "b.d2")
	b := pathToURI(bPath)

	t.Run("L21_Overlay", func(t *testing.T) {
		assert.WriteFile(t, bPath, []byte("direction: LR"), 0o600)
		c := newTestClient(t)
		c.open(b, "direction: right")
		c.waitDiagnostics(b)
		c.open(a, "x: @b")
		assert.JSON(t, []diagnostic{}, c.waitDiagnostics(a))
	})

	t.Run("L22_RelatedInformation", func(t *testing.T) {
		assert.WriteFile(t, bPath, []byte("direction: LR"), 0o600)
		c := newTestClient(t)
		c.open(a, "x: @b")
		assert.JSON(t, []diagnostic{{
			Range: lineRange(0, 3, 5), Severity: 1, Source: "d2",
			Message:            "b.d2:1:12: " + badDirection,
			RelatedInformation: []relatedInformation{{Location: location{URI: b, Range: lineRange(0, 11, 13)}, Message: badDirection}},
		}}, c.waitDiagnostics(a))
		c.expectQuiet()
	})

	t.Run("L23_RecheckImporter", func(t *testing.T) {
		c := newTestClient(t)
		c.open(b, "direction: LR")
		c.waitDiagnostics(b)
		c.open(a, "x: @b")
		assert.Equal(t, 1, len(c.waitDiagnostics(a)))
		c.change(b, 2, "direction: right")
		got := c.waitDiagnosticParams(a)
		assert.Equal(t, int32(1), got.Version)
		assert.JSON(t, []diagnostic{}, got.Diagnostics)
	})

	t.Run("OpenURIWithAlternateSpelling", func(t *testing.T) {
		alias := strings.TrimSuffix(b, "b.d2") + "%62.d2"
		c := newTestClient(t)
		c.open(alias, "direction: LR")
		c.open(b, "direction: right")
		c.open(a, "x: @b")
		got := c.waitDiagnostics(a)
		assert.Equal(t, 1, len(got))
		assert.Equal(t, alias, got[0].RelatedInformation[0].Location.URI)
		links := decode[[]documentLink](t, c.request("textDocument/documentLink", docParams(a)))
		assert.Equal(t, alias, links[0].Target)

		c.notify("textDocument/didClose", docParams(alias))
		assert.JSON(t, []diagnostic{}, c.waitDiagnostics(a))
		links = decode[[]documentLink](t, c.request("textDocument/documentLink", docParams(a)))
		assert.Equal(t, b, links[0].Target)
	})

	t.Run("WatchedFiles", func(t *testing.T) {
		assert.WriteFile(t, bPath, []byte("direction: LR"), 0o600)
		c := newTestClient(t)
		c.open(a, "x: @b")
		assert.Equal(t, 1, len(c.waitDiagnostics(a)))
		assert.WriteFile(t, bPath, []byte("direction: right"), 0o600)
		c.notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []map[string]any{{"uri": b, "type": 2}}})
		assert.JSON(t, []diagnostic{}, c.waitDiagnostics(a))
	})
}

func TestFormatting(t *testing.T) {
	const uri = "untitled:format"
	for _, tc := range []struct {
		name, text string
		want       []textEdit
	}{
		{"L27_Format", "x->y", []textEdit{{Range: lspRange{End: lspPosition{Line: 1}}, NewText: "x -> y\n"}}},
		{"L28_Formatted", "x -> y\n", []textEdit{}},
		{"L29_Invalid", "x: {", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			c.open(uri, tc.text)
			got := decode[[]textEdit](t, c.request("textDocument/formatting", docParams(uri)))
			assert.JSON(t, tc.want, got)
		})
	}
}

func TestRenameConflictingBuffers(t *testing.T) {
	dir := t.TempDir()
	path := pathToURI(filepath.Join(dir, "b.d2"))
	alias := strings.TrimSuffix(path, "b.d2") + "%62.d2"
	importer := pathToURI(filepath.Join(dir, "main.d2"))
	c := newTestClient(t)
	c.open(alias, "a")
	c.open(path, "q")
	c.open(importer, "...@b\na -> x")
	for _, pos := range []textDocumentPositionParams{
		posParams(path, 0, 0), posParams(alias, 0, 0), posParams(importer, 1, 0),
	} {
		err := c.requestError("textDocument/rename", renameParams{pos, "renamed"})
		assert.Equal(t, -32803, err.Code)
		assert.True(t, strings.Contains(err.Message, "different contents"))
	}
	// An unrelated shape remains renameable. Matching alias texts also allow
	// renaming the shared shape again.
	assert.Equal(t, 1, len(c.rename(posParams(importer, 1, 5), "other")))
	c.change(path, 2, "a")
	assert.Equal(t, 2, len(c.rename(posParams(importer, 1, 0), "renamed")))
}

func TestL30_Completion(t *testing.T) {
	const uri = "untitled:complete"
	for _, tc := range []struct {
		text, label string
		kind        int
	}{{"x.shape: ", "cylinder", 12}, {"x.style.", "opacity", 10}, {"", "", 0}} {
		c := newTestClient(t)
		c.open(uri, tc.text)
		got := decode[[]completionItem](t, c.request("textDocument/completion", posParams(uri, 0, len(tc.text))))
		if tc.text == "" {
			assert.JSON(t, []completionItem{}, got)
			continue
		}
		kind := 0
		for _, item := range got {
			if item.Label == tc.label {
				kind = item.Kind
				break
			}
		}
		assert.Equal(t, tc.kind, kind)
	}
}
