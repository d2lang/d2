package lspserver

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/util-go/assert"
)

func TestDocumentSymbols(t *testing.T) {
	const uri = "untitled:symbols"
	outline := func(t *testing.T, text string) []documentSymbol {
		t.Helper()
		c := newTestClient(t)
		c.open(uri, text)
		return decode[[]documentSymbol](t, c.request("textDocument/documentSymbol", docParams(uri)))
	}
	t.Run("L31_ShapesAndEdges", func(t *testing.T) {
		got := outline(t, "a: A {\n  b\n}\na -> c: calls")
		assert.Equal(t, 2, len(got))
		assert.Equal(t, "a", got[0].Name)
		assert.Equal(t, 19, got[0].Kind)
		assert.Equal(t, "A", got[0].Detail)
		assert.Equal(t, 1, len(got[0].Children))
		assert.Equal(t, "b", got[0].Children[0].Name)
		assert.Equal(t, "a -> c", got[1].Name)
		assert.Equal(t, 25, got[1].Kind)
		assert.Equal(t, "calls", got[1].Detail)
	})
	t.Run("L32_Classify", func(t *testing.T) {
		got := outline(t, "vars: {a: {b: value}}\nclasses: {service: {style.fill: red}}\nlayers: {detail: {box}}\nstyle: {fill: red}")
		assert.Equal(t, 3, len(got))
		for i, want := range []struct {
			name, child     string
			kind, childKind int
		}{{"vars", "a", 13, 13}, {"classes", "service", 3, 5}, {"layers", "detail", 3, 4}} {
			assert.Equal(t, want.name, got[i].Name)
			assert.Equal(t, want.kind, got[i].Kind)
			assert.Equal(t, 1, len(got[i].Children))
			assert.Equal(t, want.child, got[i].Children[0].Name)
			assert.Equal(t, want.childKind, got[i].Children[0].Kind)
		}
		assert.Equal(t, 13, got[0].Children[0].Children[0].Kind)
		assert.Equal(t, 0, len(got[1].Children[0].Children))
		assert.Equal(t, 19, got[2].Children[0].Children[0].Kind)
	})
	t.Run("L33_SelectionRanges", func(t *testing.T) {
		got := outline(t, "container: {\n  \"日本\"\n  x -> y\n}\nlayers: {detail: {a}}")
		var check func([]documentSymbol)
		check = func(symbols []documentSymbol) {
			for _, s := range symbols {
				assert.True(t, s.Name != "")
				assert.True(t, comparePosition(s.Range.Start, s.SelectionRange.Start) <= 0)
				assert.True(t, comparePosition(s.SelectionRange.Start, s.SelectionRange.End) <= 0)
				assert.True(t, comparePosition(s.SelectionRange.End, s.Range.End) <= 0)
				check(s.Children)
			}
		}
		assert.Equal(t, 2, len(got))
		assert.Equal(t, lineRange(0, 0, 9), got[0].SelectionRange)
		check(got)
	})
	t.Run("L34_SyntaxErrors", func(t *testing.T) {
		got := outline(t, "a\nb: {")
		assert.True(t, len(got) > 0)
		assert.Equal(t, "a", got[0].Name)
	})
}

func TestFolding(t *testing.T) {
	const uri = "untitled:fold"
	for _, tc := range []struct {
		name, text string
		want       []foldingRange
	}{
		{"L35_Map", "a: {\n  b\n}", []foldingRange{{StartLine: 0, EndLine: 1}}},
		{"L36_SingleLine", "a: {b}", []foldingRange{}},
		{"L36_BlockString", "a: ||\n  b\n||", []foldingRange{{StartLine: 0, EndLine: 1}}},
		{"L36_BlockComment", "\"\"\"\none\ntwo\n\"\"\"", []foldingRange{{StartLine: 0, EndLine: 3, Kind: "comment"}}},
		{"L36_CommentRun", "# first\n# second\n# third\n\n# alone", []foldingRange{{StartLine: 0, EndLine: 2, Kind: "comment"}}},
		{"Array", "vars: {a: [\n  one\n  two\n]}", []foldingRange{{StartLine: 0, EndLine: 2}, {StartLine: 0, EndLine: 2}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			c.open(uri, tc.text)
			got := decode[[]foldingRange](t, c.request("textDocument/foldingRange", docParams(uri)))
			assert.JSON(t, tc.want, got)
		})
	}
}

func TestDocumentLinks(t *testing.T) {
	t.Run("L37_ImportsOnDiskAndInMemory", func(t *testing.T) {
		dir := t.TempDir()
		main := pathToURI(filepath.Join(dir, "main.d2"))
		other := filepath.Join(dir, "other.d2")
		overlayURI := pathToURI(filepath.Join(dir, "new.d2"))
		assert.WriteFile(t, other, []byte("x"), 0o600)
		c := newTestClient(t)
		c.open(overlayURI, "x")
		c.open(main, "x: @other\ny: @new")
		got := decode[[]documentLink](t, c.request("textDocument/documentLink", docParams(main)))
		assert.JSON(t, []documentLink{
			{Range: lineRange(0, 3, 9), Target: pathToURI(other)},
			{Range: lineRange(1, 3, 7), Target: overlayURI},
		}, got)
	})
	t.Run("L38_Missing", func(t *testing.T) {
		main := pathToURI(filepath.Join(t.TempDir(), "main.d2"))
		c := newTestClient(t)
		c.open(main, "x: @missing")
		assert.Equal(t, "[]", string(c.request("textDocument/documentLink", docParams(main))))
	})
	t.Run("L39_URLs", func(t *testing.T) {
		const uri, url, icon = "untitled:links", "https://example.com", "https://icons.d2lang.com/a.svg"
		c := newTestClient(t)
		c.open(uri, "x.link: "+url+"\nx.icon: "+icon)
		got := decode[[]documentLink](t, c.request("textDocument/documentLink", docParams(uri)))
		assert.JSON(t, []documentLink{
			{Range: lineRange(0, 8, 8+len(url)), Target: url},
			{Range: lineRange(1, 8, 8+len(icon)), Target: icon},
		}, got)
	})
	t.Run("L40_BoardPath", func(t *testing.T) {
		const uri = "untitled:links"
		c := newTestClient(t)
		c.open(uri, "x.link: layers.y")
		assert.Equal(t, "[]", string(c.request("textDocument/documentLink", docParams(uri))))
	})
}

func TestKeywordHover(t *testing.T) {
	const uri = "untitled:hover"
	for _, tc := range []struct {
		name, text, word, group string
		start                   int
	}{
		{"L41_Shape", "x.shape: circle", "shape", "attribute", 2},
		{"L42_StrokeWidth", "x.style.stroke-width: 2", "stroke-width", "style", 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			c.open(uri, tc.text)
			got := decode[hover](t, c.request("textDocument/hover", posParams(uri, 0, tc.start+1)))
			assert.Equal(t, "markdown", got.Contents.Kind)
			assert.True(t, strings.HasPrefix(got.Contents.Value, "**"+tc.word+"** ("+tc.group+")\n\n"))
			assert.True(t, got.Range != nil)
			assert.Equal(t, lineRange(0, tc.start, tc.start+len(tc.word)), *got.Range)
		})
	}
	t.Run("L43_QuotedName", func(t *testing.T) {
		c := newTestClient(t)
		c.open(uri, `"shape"`)
		result := decode[*hover](t, c.request("textDocument/hover", posParams(uri, 0, 3)))
		assert.True(t, result == nil || !strings.Contains(result.Contents.Value, "(attribute)"))
	})
	t.Run("L44_AllReservedKeywords", func(t *testing.T) {
		for word := range d2ast.ReservedKeywords {
			doc, ok := keywordDocs[word]
			if !ok || doc.group == "" || doc.text == "" {
				t.Errorf("missing documentation for keyword %q", word)
			}
		}
	})
	t.Run("Plaintext", func(t *testing.T) {
		c := newBareTestClient(t)
		c.request("initialize", json.RawMessage(`{"capabilities":{"textDocument":{"hover":{"contentFormat":["plaintext"]}}}}`))
		c.notify("initialized", nil)
		c.open(uri, "x.shape: circle")
		got := decode[hover](t, c.request("textDocument/hover", posParams(uri, 0, 4)))
		assert.Equal(t, "plaintext", got.Contents.Kind)
		assert.True(t, strings.HasPrefix(got.Contents.Value, "shape (attribute)\n\n"))
		assert.False(t, strings.Contains(got.Contents.Value, "**"))
	})
}
