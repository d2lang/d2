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
	t.Run("DottedShapeAttributes", func(t *testing.T) {
		got := outline(t, `x.shape: circle
x.style.fill: red
x: Server {
  inner.part
  inner.part.style: {fill: red}
  inner.part.label: Details
  inner."style"
  inner.layers
  other.shape: circle
}
folder: {other.style.fill: blue; layers.shape: circle}
x -> y
(x -> y)[0].style.stroke: red
x -> y`)
		assert.Equal(t, 4, len(got))
		assert.Equal(t, "x", got[0].Name)
		assert.Equal(t, objectSymbol, got[0].Kind)
		assert.Equal(t, "Server", got[0].Detail)
		assert.Equal(t, lineRange(2, 0, 1), got[0].SelectionRange)
		assert.Equal(t, 4, len(got[0].Children))
		assert.Equal(t, "inner.part", got[0].Children[0].Name)
		assert.Equal(t, `inner."style"`, got[0].Children[1].Name)
		assert.Equal(t, "inner.layers", got[0].Children[2].Name)
		assert.Equal(t, "other", got[0].Children[3].Name)
		assert.Equal(t, "", got[0].Children[3].Detail)
		assert.Equal(t, lineRange(8, 2, 7), got[0].Children[3].SelectionRange)
		assert.Equal(t, 2, len(got[1].Children))
		assert.Equal(t, "other", got[1].Children[0].Name)
		assert.Equal(t, "layers", got[1].Children[1].Name)
		assert.Equal(t, objectSymbol, got[1].Children[1].Kind)
		assert.Equal(t, operatorSymbol, got[2].Kind)
		assert.Equal(t, operatorSymbol, got[3].Kind)
	})
	t.Run("ImplicitOutlineShapes", func(t *testing.T) {
		got := outline(t, `x.shape: circle
X.style.fill: red
"x".label: Label
a.b.icon: icon.svg
"a.b".icon: icon.svg
"shape".style.fill: red
style.fill: red`)
		assert.Equal(t, 4, len(got))
		for i, want := range []string{"x", "a.b", `"a.b"`, `"shape"`} {
			assert.Equal(t, want, got[i].Name)
			assert.Equal(t, objectSymbol, got[i].Kind)
			assert.Equal(t, "", got[i].Detail)
		}
		assert.Equal(t, lineRange(0, 0, 1), got[0].SelectionRange)
		assert.Equal(t, lineRange(3, 0, 3), got[1].SelectionRange)
	})
	t.Run("RepeatedContainers", func(t *testing.T) {
		got := outline(t, `x
x: Server {a: {one}}
x: {a: {two}; b}`)
		assert.Equal(t, 1, len(got))
		x := got[0]
		assert.Equal(t, "Server", x.Detail)
		assert.Equal(t, lineRange(0, 0, 1), x.SelectionRange)
		assert.Equal(t, lspRange{End: lspPosition{Line: 2, Character: 16}}, x.Range)
		assert.Equal(t, 2, len(x.Children))
		assert.Equal(t, "a", x.Children[0].Name)
		assert.Equal(t, "b", x.Children[1].Name)
		assert.Equal(t, 2, len(x.Children[0].Children))
		assert.Equal(t, "one", x.Children[0].Children[0].Name)
		assert.Equal(t, "two", x.Children[0].Children[1].Name)
	})
	t.Run("DottedNamespaces", func(t *testing.T) {
		got := outline(t, `layers.detail: {a}
layers: {detail: {b}; overview: {c}}
layers.detail.d.style.fill: red
vars.endpoint: localhost
vars: {region: us}
classes.web.style.fill: red
classes.web.style.stroke: blue`)
		assert.Equal(t, 3, len(got))
		layers := got[0]
		assert.Equal(t, "layers", layers.Name)
		assert.Equal(t, lineRange(0, 0, 6), layers.SelectionRange)
		assert.Equal(t, 2, len(layers.Children))
		detail := layers.Children[0]
		assert.Equal(t, packageSymbol, detail.Kind)
		assert.Equal(t, "detail", detail.Name)
		assert.Equal(t, lineRange(0, 7, 13), detail.SelectionRange)
		assert.Equal(t, 3, len(detail.Children))
		for i, name := range []string{"a", "b", "d"} {
			assert.Equal(t, name, detail.Children[i].Name)
			assert.Equal(t, objectSymbol, detail.Children[i].Kind)
		}
		assert.Equal(t, "overview", layers.Children[1].Name)
		assert.Equal(t, 2, len(got[1].Children))
		assert.Equal(t, "endpoint", got[1].Children[0].Name)
		assert.Equal(t, variableSymbol, got[1].Children[0].Kind)
		assert.Equal(t, "region", got[1].Children[1].Name)
		assert.Equal(t, 1, len(got[2].Children))
		assert.Equal(t, "web", got[2].Children[0].Name)
		assert.Equal(t, classSymbol, got[2].Children[0].Kind)
		assert.Equal(t, 0, len(got[2].Children[0].Children))
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

func TestCompletionHintAcceptsWithoutInsertingItsLabel(t *testing.T) {
	const uri = "untitled:hints"
	c := newTestClient(t)
	pos := openAtCursor(t, c, uri, "x.style.opacity:¦")
	got := decode[[]completionItem](t, c.request("textDocument/completion", pos))
	assert.Equal(t, 1, len(got))
	assert.True(t, strings.Contains(got[0].Label, "number"))
	assert.JSON(t, &textEdit{Range: lspRange{Start: pos.Position, End: pos.Position}}, got[0].TextEdit)
}

func TestInterpolatedAttributes(t *testing.T) {
	const uri = "untitled:interpolation"
	c := newTestClient(t)
	c.open(uri, `vars.suffix: blue
x.link: https://example.com/${suffix}
x.style.fill: "red${suffix}"`)
	assert.Equal(t, "[]", string(c.request("textDocument/documentLink", docParams(uri))))
	assert.Equal(t, "[]", string(c.request("textDocument/documentColor", docParams(uri))))
}

func TestShapeHoverLabelAttribute(t *testing.T) {
	c := newTestClient(t)
	pos := openAtCursor(t, c, "untitled:label", "¦x: Initial\nx.label: Display name")
	got := decode[hover](t, c.request("textDocument/hover", pos))
	assert.True(t, strings.Contains(got.Contents.Value, "label: Display name"))
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
