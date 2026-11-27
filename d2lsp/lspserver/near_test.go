package lspserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/util-go/assert"
)

func TestNearShapeReference(t *testing.T) {
	const uri = "untitled:near"
	for _, tc := range []struct {
		name, source, want string
		pos                lspPosition
		count              int
	}{
		{"nested", "a\nnote: {near: a}", "b\nnote: {near: b}", lspPosition{0, 0}, 2},
		{"forward", "note.near: a\na", "note.near: b\nb", lspPosition{0, 11}, 2},
		{"pathPrefix", "notes.a\ntext.near: notes.a", "b.a\ntext.near: b.a", lspPosition{1, 12}, 2},
		{"scoped", "a\nbox.a\nbox.note.near: a", "b\nbox.a\nbox.note.near: b", lspPosition{2, 15}, 2},
		{"literalBoardKeyword", "box.steps\nnote.near: box.steps", "box.b\nnote.near: box.b", lspPosition{1, 17}, 2},
		{"class", "a\nclasses: {p: {near: a}}\nnote.class: p", "b\nclasses: {p: {near: b}}\nnote.class: p", lspPosition{0, 0}, 2},
		{"inlineBlockPrefix", "a.z\nnote.near: | a.z |", "b.z\nnote.near: b.z", lspPosition{1, 13}, 2},
		{"blockLeaf", "a.z\nnote.near: |\n  a.z\n|", "a.b\nnote.near: a.b", lspPosition{2, 4}, 2},
		{"foldedPrefix", "a.z\nnote.near: a.\\\n  z", "b.z\nnote.near: b.z", lspPosition{1, 11}, 2},
		{"foldedLeaf", "a.z\nnote.near: a.\\\n  z", "a.b\nnote.near: a.b", lspPosition{2, 2}, 2},
		{"escapedInnerEnd", "a.\"z#\".c\nnote.near: 'a.z\\#.c'", "a.b.c\nnote.near: a.b.c", lspPosition{1, 17}, 2},
		{"quoted", "a\nnote.near: \"a\"", "b\nnote.near: b", lspPosition{1, 12}, 2},
		{"attributePositions", "right\nnote.icon.near: right\nnote.label.near: right", "b\nnote.icon.near: right\nnote.label.near: right", lspPosition{0, 0}, 1},
		{"board", "a\nlayers.child: {a; note.near: a}", "a\nlayers.child: {b; note.near: b}", lspPosition{1, 29}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			c.open(uri, tc.source)
			pos := posParams(uri, tc.pos.Line, tc.pos.Character)
			assert.Equal(t, tc.count, len(c.references(pos, true)))
			if tc.name == "forward" {
				def := decode[*location](t, c.request("textDocument/definition", pos))
				assert.True(t, def != nil)
				assert.Equal(t, lineRange(1, 0, 1), def.Range)
			}
			got := applyEdits(t, tc.source, c.rename(pos, "b")[uri])
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRenameNearLiteral(t *testing.T) {
	const uri = "untitled:near"
	const source = "box.a\nnote.near: box.a"
	for _, name := range []string{"main.go", "style", "price${x}", "o'neil", "a.\"b\"", "日本😀"} {
		t.Run(name, func(t *testing.T) {
			c := newTestClient(t)
			c.open(uri, source)
			text := applyEdits(t, source, c.rename(posParams(uri, 0, 4), name)[uri])
			graph, _, err := d2compiler.Compile("", strings.NewReader(text), nil)
			assert.Success(t, err)
			near, ok := graph.Root.Children["note"].NearTarget()
			assert.True(t, ok)
			assert.Equal(t, name, near.IDVal)
		})
	}
}

func TestNearConstant(t *testing.T) {
	const uri = "untitled:near"
	c := newTestClient(t)
	const source = "top-left\nnote.near: top-left\nvars: {v: top-left}\nother.near: top-right"
	c.open(uri, source)
	assert.Equal(t, 2, len(c.references(posParams(uri, 0, 0), true)))
	got := applyEdits(t, source, c.rename(posParams(uri, 0, 0), "b")[uri])
	assert.Equal(t, "b\nnote.near: b\nvars: {v: top-left}\nother.near: top-right", got)
	assert.Equal(t, "null", string(c.request("textDocument/prepareRename", posParams(uri, 3, 17))))
}

func TestNearSharedImport(t *testing.T) {
	dir := t.TempDir()
	uri := pathToURI(filepath.Join(dir, "main.d2"))
	shared := pathToURI(filepath.Join(dir, "shared.d2"))
	c := newTestClient(t)
	c.open(shared, "note.near: a")
	const source = "layers.one: {a; ...@shared}\nlayers.two: {a; ...@shared}"
	c.open(uri, source)
	pos := posParams(uri, 0, 13)
	assert.JSON(t, []location{{URI: uri, Range: lineRange(0, 13, 14)}, {URI: shared, Range: lineRange(0, 11, 12)}}, c.references(pos, true))
	err := c.requestError("textDocument/rename", renameParams{pos, "new"})
	assert.Equal(t, -32803, err.Code)
}

func TestNearSharedConstant(t *testing.T) {
	dir := t.TempDir()
	uri := pathToURI(filepath.Join(dir, "main.d2"))
	shared := pathToURI(filepath.Join(dir, "shared.d2"))
	c := newTestClient(t)
	c.open(shared, "note.near: top-left")
	c.open(uri, "layers.one: {top-left; ...@shared}\nlayers.two: {...@shared}")
	err := c.requestError("textDocument/rename", renameParams{posParams(uri, 0, 13), "different"})
	assert.Equal(t, -32803, err.Code)
}

func TestRenameCapturesConstantNear(t *testing.T) {
	for _, tc := range []struct {
		source  string
		capture bool
	}{
		{"server\nnote.near: top-left", true},
		{"server\nvars.corner: top-left\nnote.near: ${corner}", true},
		{"server\nscenarios.one: {note.near: top-left}", true},
		{"server\nlayers.one: {note.near: top-left}", false},
		{"box.server\nnote.near: top-left", false},
		{"server\nnote.near: bottom-left", false},
	} {
		c := newTestClient(t)
		const uri = "untitled:near-capture"
		c.open(uri, tc.source)
		pos := posParams(uri, 0, strings.Index(tc.source, "server"))
		if tc.capture {
			err := c.requestError("textDocument/rename", renameParams{pos, "TOP-LEFT"})
			assert.Equal(t, -32803, err.Code)
			assert.True(t, strings.Contains(err.Message, "placement constant"))
		} else {
			assert.Equal(t, strings.Replace(tc.source, "server", "TOP-LEFT", 1), applyEdits(t, tc.source, c.rename(pos, "TOP-LEFT")[uri]))
		}
	}
}
