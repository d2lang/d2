package lspserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/d2lang/util-go/assert"
)

func TestReferencesAndRenameFromSharedFile(t *testing.T) {
	dir := t.TempDir()
	shared := pathToURI(filepath.Join(dir, "shared.d2"))
	first := pathToURI(filepath.Join(dir, "first.d2"))
	second := pathToURI(filepath.Join(dir, "second.d2"))
	standalone := pathToURI(filepath.Join(dir, "standalone.d2"))
	const sharedText = "a: Shared"
	const firstText = "...@shared\na -> first"
	const secondText = "...@middle\nsecond.near: a"
	assert.WriteFile(t, filepath.Join(dir, "middle.d2"), []byte("...@shared"), 0o600)
	c := newTestClient(t)
	c.open(standalone, "a")
	c.open(first, firstText)
	c.open(second, secondText)
	c.open(shared, sharedText)
	for _, p := range []textDocumentPositionParams{posParams(shared, 0, 0), posParams(first, 1, 0)} {
		assert.Equal(t, 3, len(c.references(p, true)))
		changes := c.rename(p, "renamed")
		assert.Equal(t, 3, len(changes))
		assert.Equal(t, "renamed: Shared", applyEdits(t, sharedText, changes[shared]))
		assert.Equal(t, "...@shared\nrenamed -> first", applyEdits(t, firstText, changes[first]))
		assert.Equal(t, "...@middle\nsecond.near: renamed", applyEdits(t, secondText, changes[second]))
		assert.Equal(t, -32803, c.requestError("textDocument/rename", renameParams{p, "second"}).Code)
	}
}

func TestRenameLiteralGlobPrefix(t *testing.T) {
	const uri = "untitled:glob"
	const source = "group: {a; b}\ngroup.*.style.fill: red"
	c := newTestClient(t)
	c.open(uri, source)
	p := posParams(uri, 0, 0)
	assert.Equal(t, 2, len(c.references(p, true)))
	assert.Equal(t, "x: {a; b}\nx.*.style.fill: red", applyEdits(t, source, c.rename(p, "x")[uri]))
	// Wildcards remain generic on rename, even with one matching shape.
	assert.Equal(t, "group: {x; b}\ngroup.*.style.fill: red", applyEdits(t, source, c.rename(posParams(uri, 0, 8), "x")[uri]))
}

func TestRenameRemovedBoardOverride(t *testing.T) {
	for _, op := range []string{"null", "suspend"} {
		c := newTestClient(t)
		const uri = "untitled:remove"
		c.open(uri, "a\nscenarios.one: {a: "+op+"}")
		err := c.requestError("textDocument/rename", renameParams{posParams(uri, 0, 0), "b"})
		assert.Equal(t, -32803, err.Code)
		assert.True(t, strings.Contains(err.Message, "removed or suspended"))
	}
}

func TestRenameSelectiveImport(t *testing.T) {
	dir := t.TempDir()
	shared := pathToURI(filepath.Join(dir, "shared.d2"))
	main := pathToURI(filepath.Join(dir, "main.d2"))
	c := newTestClient(t)
	c.open(shared, "a: {child}")
	c.open(main, "box: @shared.a")
	p := posParams(shared, 0, 0)
	err := c.requestError("textDocument/rename", renameParams{p, "renamed"})
	assert.Equal(t, -32803, err.Code)
	assert.True(t, strings.Contains(err.Message, "selective import"))
}

func TestRenameLocalShapeWhileAnotherImporterIsIncomplete(t *testing.T) {
	dir := t.TempDir()
	shared := pathToURI(filepath.Join(dir, "shared.d2"))
	first := pathToURI(filepath.Join(dir, "first.d2"))
	second := pathToURI(filepath.Join(dir, "second.d2"))
	const source = "...@shared\nlocal"
	c := newTestClient(t)
	c.open(shared, "common")
	c.open(first, source)
	c.open(second, "...@shared\nother ->")

	changes := c.rename(posParams(first, 1, 0), "renamed")
	assert.Equal(t, 1, len(changes))
	assert.Equal(t, "...@shared\nrenamed", applyEdits(t, source, changes[first]))
	err := c.requestError("textDocument/rename", renameParams{posParams(shared, 0, 0), "renamed"})
	assert.True(t, strings.Contains(err.Message, second))
}

func TestRenameIncompleteImporter(t *testing.T) {
	dir := t.TempDir()
	shared := pathToURI(filepath.Join(dir, "shared.d2"))
	main := pathToURI(filepath.Join(dir, "main.d2"))
	c := newTestClient(t)
	c.open(shared, "shared")
	c.open(main, "...@shared\nshared ->")
	err := c.requestError("textDocument/rename", renameParams{posParams(shared, 0, 0), "renamed"})
	assert.Equal(t, -32803, err.Code)
	assert.True(t, strings.Contains(err.Message, main))
}

func TestRenameReloadsClosedImport(t *testing.T) {
	dir := t.TempDir()
	main := pathToURI(filepath.Join(dir, "main.d2"))
	path := filepath.Join(dir, "shared.d2")
	shared := pathToURI(path)
	assert.WriteFile(t, path, []byte("a: Shared"), 0o600)
	c := newTestClient(t)
	c.open(main, "...@shared\na -> other")
	p := posParams(main, 1, 0)
	assert.True(t, decode[*hover](t, c.request("textDocument/hover", p)) != nil)
	const changed = "# externally inserted\na: Shared"
	assert.WriteFile(t, path, []byte(changed), 0o600)
	edits := c.rename(p, "new")
	assert.Equal(t, "# externally inserted\nnew: Shared", applyEdits(t, changed, edits[shared]))
}

func TestRenameSelectiveReexport(t *testing.T) {
	dir := t.TempDir()
	shared := pathToURI(filepath.Join(dir, "shared.d2"))
	main := pathToURI(filepath.Join(dir, "main.d2"))
	assert.WriteFile(t, filepath.Join(dir, "middle.d2"), []byte("...@shared"), 0o600)
	c := newTestClient(t)
	c.open(shared, "foo.a\nbar.a")
	c.open(main, "box: @middle.bar.a")
	err := c.requestError("textDocument/rename", renameParams{posParams(shared, 1, 4), "b"})
	assert.True(t, strings.Contains(err.Message, "selective import"))
	edits := c.rename(posParams(shared, 0, 4), "b")
	assert.Equal(t, "foo.b\nbar.a", applyEdits(t, "foo.a\nbar.a", edits[shared]))
}

func TestRenameBesideRemovedBoardShape(t *testing.T) {
	for _, rule := range []string{"bar.a: null", "bar: {a: suspend}", "bar: {(a -> destination)[0]: null}"} {
		c := newTestClient(t)
		const uri = "untitled:separate"
		text := "foo.a\nbar.a\nscenarios.detail: {" + rule + "}"
		c.open(uri, text)
		p := posParams(uri, 0, 4)
		assert.Equal(t, strings.Replace(text, "foo.a", "foo.b", 1), applyEdits(t, text, c.rename(p, "b")[uri]))
	}
}

func TestRenameRemovedEdgeEndpoints(t *testing.T) {
	const uri = "untitled:removed-edge"
	for _, source := range []string{
		"a -> b\nscenarios.one: {(a -> b)[0]: null}",
		"a -> b\n(a -> b)[0]: null",
		"box: {a -> b}\nscenarios.one: {box.(a -> b)[0]: null}",
	} {
		c := newTestClient(t)
		c.open(uri, source)
		err := c.requestError("textDocument/rename", renameParams{posParams(uri, 0, strings.Index(source, "a ->")), "new"})
		assert.Equal(t, -32803, err.Code)
		assert.True(t, strings.Contains(err.Message, "removed or suspended"))
	}
	// A suspended endpoint still present in the index can be edited normally.
	c := newTestClient(t)
	const source = "a -> b\nsteps.one: {(a -> b)[0]: suspend}\nsteps.two: {c}"
	c.open(uri, source)
	changes := c.rename(posParams(uri, 0, 5), "new")
	assert.Equal(t, strings.ReplaceAll(source, "-> b", "-> new"), applyEdits(t, source, changes[uri]))
}

func TestRenameWithRemovedEdgeInImporter(t *testing.T) {
	dir := t.TempDir()
	shared := pathToURI(filepath.Join(dir, "shared.d2"))
	main := pathToURI(filepath.Join(dir, "main.d2"))
	c := newTestClient(t)
	c.open(shared, "a -> b")
	c.open(main, "...@shared\nscenarios.one: {(a -> b)[0]: null}")
	err := c.requestError("textDocument/rename", renameParams{posParams(shared, 0, 0), "new"})
	assert.Equal(t, -32803, err.Code)
	assert.True(t, strings.Contains(err.Message, "removed or suspended"))
}
