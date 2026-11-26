package lspserver

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/d2lang/util-go/assert"

	"github.com/d2lang/d2/d2ast"
)

func TestIncompleteEdgeWhileTyping(t *testing.T) {
	var log bytes.Buffer
	s := New(Options{Log: &log})
	doc := document{uri: "untitled:edge", version: 1, text: "service\na ->"}
	snap := s.analyze(t.Context(), doc)
	s.ensureIR(t.Context(), doc, snap)
	assert.True(t, snap.parseErr != nil && snap.ir == nil)
	assert.Equal(t, "service", symbols(snap.ast)[0].Name)
	assert.Equal(t, "", log.String())
	doc.version++
	doc.text += " b"
	snap = s.analyze(t.Context(), doc)
	s.ensureIR(t.Context(), doc, snap)
	assert.True(t, snap.ir != nil)
}

func TestLazySnapshot(t *testing.T) {
	s := New(Options{})
	doc := document{uri: "untitled:cache", version: 1, text: "vars: {env: prod}\nx: \"environment: ${env}\""}
	snap := s.analyze(t.Context(), doc)
	assert.True(t, !snap.attemptedIR && snap.ir == nil)
	s.ensureIR(t.Context(), doc, snap)
	assert.True(t, snap.ir != nil)
	// Compiling coalesces this scalar. The editor AST still needs the
	// substitution and its authored range for go to definition.
	nodes := nodePathAt(snap.ast, lspPosition{Line: 1, Character: 19})
	sawSubstitution := false
	for _, node := range nodes {
		if _, ok := node.(*d2ast.Substitution); ok {
			sawSubstitution = true
		}
	}
	assert.True(t, sawSubstitution)
	ir := snap.ir
	cached := s.analyze(t.Context(), doc)
	s.ensureIR(t.Context(), doc, cached)
	assert.True(t, cached == snap && cached.ir == ir)
}

func TestRenameSharedBoardOwners(t *testing.T) {
	c := newTestClient(t)
	const uri = "untitled:copies"
	const source = "a\nscenarios: {one: {a.style.fill: red}; two: {a.style.fill: blue; taken}}"
	c.open(uri, source)
	pos := posParams(uri, 1, 18)
	got := applyEdits(t, source, c.rename(pos, "updated")[uri])
	assert.Equal(t, "updated\nscenarios: {one: {updated.style.fill: red}; two: {updated.style.fill: blue; taken}}", got)
	assert.Equal(t, -32803, c.requestError("textDocument/rename", renameParams{pos, "taken"}).Code)
}

func TestRenameEscapedEndpoints(t *testing.T) {
	dir := t.TempDir()
	main := pathToURI(filepath.Join(dir, "main.d2"))
	imported := pathToURI(filepath.Join(dir, "other.d2"))
	const mainText = "...@other\ncost\\#.child\n\"cost#\" -> cost\\# # keep\ncost\\#x"
	const importedText = "co\\\n  st\\#: Price"
	c := newTestClient(t)
	c.open(imported, importedText)
	c.open(main, mainText)
	changes := c.rename(posParams(main, 1, 2), "fee")
	assert.Equal(t, "...@other\nfee.child\nfee -> fee # keep\ncost\\#x", applyEdits(t, mainText, changes[main]))
	assert.Equal(t, "fee: Price", applyEdits(t, importedText, changes[imported]))
}
