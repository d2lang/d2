package lspserver

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/lib/localfile"
	"github.com/d2lang/util-go/assert"
)

func TestRenameWorkspaceEditVersions(t *testing.T) {
	dir := t.TempDir()
	diskPath := filepath.Join(dir, "b.d2")
	disk := pathToURI(diskPath)
	assert.WriteFile(t, diskPath, []byte("x"), 0o600)
	first, last := pathToURI(filepath.Join(dir, "a.d2")), pathToURI(filepath.Join(dir, "c.d2"))
	const firstText, lastText = "...@b\nx -> y", "...@b\nz -> x"
	for _, tc := range []struct {
		name      string
		supported bool
	}{{"DocumentChanges", true}, {"Changes", false}} {
		t.Run(tc.name, func(t *testing.T) {
			c := newBareTestClient(t)
			c.request("initialize", map[string]any{"capabilities": map[string]any{
				"workspace": map[string]any{"workspaceEdit": map[string]bool{"documentChanges": tc.supported}},
			}})
			c.notify("initialized", nil)
			c.open(last, "# initial")
			c.change(last, 9, lastText)
			c.open(first, "# initial")
			c.change(first, 7, firstText)
			params := renameParams{posParams(first, 1, 0), "new"}
			fields := decode[map[string]json.RawMessage](t, c.request("textDocument/rename", params))
			assert.Equal(t, 1, len(fields))
			if !tc.supported {
				changes := decode[map[string][]textEdit](t, fields["changes"])
				assert.Equal(t, 3, len(changes))
				assert.Equal(t, "...@b\nnew -> y", applyEdits(t, firstText, changes[first]))
				return
			}
			var got []struct {
				TextDocument struct {
					URI     string
					Version json.RawMessage
				}
				Edits []textEdit
			}
			assert.Success(t, json.Unmarshal(fields["documentChanges"], &got))
			assert.Equal(t, 3, len(got))
			for i, want := range []struct {
				uri, version, before, after string
			}{
				{first, "7", firstText, "...@b\nnew -> y"},
				{disk, "null", "x", "new"},
				{last, "9", lastText, "...@b\nz -> new"},
			} {
				assert.Equal(t, want.uri, got[i].TextDocument.URI)
				assert.Equal(t, want.version, string(got[i].TextDocument.Version))
				assert.Equal(t, want.after, applyEdits(t, want.before, got[i].Edits))
			}
		})
	}
}

func TestPrepareRename(t *testing.T) {
	const uri = "untitled:rename"
	t.Run("L63_Shape", func(t *testing.T) {
		c := newTestClient(t)
		pos := openAtCursor(t, c, uri, "api\nA¦PI -> db")
		got := decode[prepareRenameResult](t, c.request("textDocument/prepareRename", pos))
		assert.Equal(t, prepareRenameResult{lineRange(1, 0, 3), "API"}, got)
	})
	t.Run("L64_OnlyShapes", func(t *testing.T) {
		for _, source := range []string{
			"x.st¦yle.fill: red",
			"classes: {sv¦c: {}}",
			"vars: {co¦lor: red}",
			"a -¦> b",
		} {
			c := newTestClient(t)
			pos := openAtCursor(t, c, uri, source)
			assert.Equal(t, "null", string(c.request("textDocument/prepareRename", pos)))
			err := c.requestError("textDocument/rename", renameParams{pos, "new"})
			assert.Equal(t, -32803, err.Code)
			assert.Equal(t, "only shapes can be renamed", err.Message)
		}
	})
}

// Apply the same workspace edits as an editor. Sorting from the end preserves
// all earlier offsets and also lets the test reject overlapping edits.
func applyEdits(t *testing.T, text string, edits []textEdit) string {
	t.Helper()
	type byteEdit struct {
		start, end int
		text       string
	}
	ordered := make([]byteEdit, 0, len(edits))
	for _, edit := range edits {
		ordered = append(ordered, byteEdit{byteOffset(t, text, edit.Range.Start), byteOffset(t, text, edit.Range.End), edit.NewText})
	}
	slices.SortFunc(ordered, func(a, b byteEdit) int { return cmp.Compare(b.start, a.start) })
	end := len(text)
	for _, e := range ordered {
		if e.start > e.end || e.end > end {
			t.Fatalf("invalid or overlapping edit: %+v", e)
		}
		text = text[:e.start] + e.text + text[e.end:]
		end = e.start
	}
	return text
}

func byteOffset(t *testing.T, text string, pos lspPosition) int {
	t.Helper()
	var here lspPosition
	for offset, r := range text {
		if here == pos {
			return offset
		}
		if r == '\n' {
			here.Line++
			here.Character = 0
		} else {
			here.Character += utf16.RuneLen(r)
		}
	}
	if here == pos {
		return len(text)
	}
	t.Fatalf("invalid UTF-16 position %+v in %q", pos, text)
	return 0
}

func (c *testClient) rename(pos textDocumentPositionParams, name string) map[string][]textEdit {
	c.t.Helper()
	return decode[workspaceEdit](c.t, c.request("textDocument/rename", renameParams{pos, name})).Changes
}

func TestRename(t *testing.T) {
	const uri = "untitled:rename"
	t.Run("L65_AllMentions", func(t *testing.T) {
		c := newTestClient(t)
		source := "x\nx.a\nx -> z"
		c.open(uri, source)
		changes := c.rename(posParams(uri, 0, 0), "y2")
		assert.Equal(t, 1, len(changes))
		assert.Equal(t, 3, len(changes[uri]))
		assert.Equal(t, "y2\ny2.a\ny2 -> z", applyEdits(t, source, changes[uri]))
	})
	for _, tc := range []struct{ test, name string }{
		{"L66_Spaces", "my service"}, {"L67_Dot", "main.go"}, {"L68_Keyword", "style"}, {"LiteralParentMarker", "_"},
		{"Dollar", "$"}, {"SubstitutionSpelling", "${env}"}, {"NullSpelling", "NuLl"}, {"QuoteInName", "a.\"b\""},
	} {
		t.Run(tc.test, func(t *testing.T) {
			c := newTestClient(t)
			const source = "x\nx -> x"
			c.open(uri, source)
			edits := c.rename(posParams(uri, 0, 0), tc.name)[uri]
			assert.Equal(t, 3, len(edits))
			if tc.name == "style" || tc.name == "NuLl" {
				assert.Equal(t, `"`+tc.name+`"`, edits[0].NewText)
			}
			text := applyEdits(t, source, edits)
			graph, _, err := d2compiler.Compile("", strings.NewReader(text), nil)
			assert.Success(t, err)
			assert.Equal(t, 1, len(graph.Objects))
			if tc.name == "NuLl" {
				// Graph IDs canonicalize null regardless of the authored spelling.
				ast, err := d2parser.Parse("", strings.NewReader(text), nil)
				assert.Success(t, err)
				assert.Equal(t, tc.name, ast.Nodes[0].MapKey.Key.Path[0].Unbox().ScalarString())
			} else {
				assert.Equal(t, tc.name, graph.Objects[0].IDVal)
			}
		})
	}
	t.Run("L69_SiblingCollision", func(t *testing.T) {
		c := newTestClient(t)
		pos := openAtCursor(t, c, uri, "box: {x¦; taken}")
		for _, name := range []string{"taken", "TAKEN"} {
			err := c.requestError("textDocument/rename", renameParams{pos, name})
			assert.Equal(t, -32803, err.Code)
			assert.True(t, strings.Contains(err.Message, "already exists"))
		}
	})
	t.Run("L70_ParentReference", func(t *testing.T) {
		c := newTestClient(t)
		source := "y\na: {😀 -> _.y}"
		c.open(uri, source)
		edits := c.rename(posParams(uri, 0, 0), "new name")[uri]
		got := applyEdits(t, source, edits)
		assert.Equal(t, "new name\na: {😀 -> _.new name}", got)
	})
	t.Run("L71_ImportedShape", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "editor project")
		assert.Success(t, os.Mkdir(dir, 0o700))
		mainPath, otherPath := filepath.Join(dir, "main.d2"), filepath.Join(dir, "other.d2")
		mainURI := pathToURI(mainPath)
		// Clients may percent-escape ordinary letters. Use their original URI in edits.
		otherURI := strings.TrimSuffix(pathToURI(otherPath), "other.d2") + "%6fther.d2"
		const mainText, otherText = "...@other\n😀 -> x", "x: Original"
		c := newTestClient(t)
		c.open(otherURI, otherText)
		c.open(mainURI, mainText)
		changes := c.rename(posParams(mainURI, 1, 6), "new")
		assert.Equal(t, 2, len(changes))
		gotMain := applyEdits(t, mainText, changes[mainURI])
		gotOther := applyEdits(t, otherText, changes[otherURI])
		assert.Equal(t, "...@other\n😀 -> new", gotMain)
		assert.Equal(t, "new: Original", gotOther)
		assert.WriteFile(t, otherPath, []byte(gotOther), 0o600)
		_, _, err := d2compiler.Compile(mainPath, strings.NewReader(gotMain), &d2compiler.CompileOptions{FS: localfile.Unrestricted()})
		assert.Success(t, err)
	})
	t.Run("L72_InvalidName", func(t *testing.T) {
		c := newTestClient(t)
		c.open(uri, "x")
		for _, name := range []string{"", "line\nfeed", "carriage\rreturn"} {
			err := c.requestError("textDocument/rename", renameParams{posParams(uri, 0, 0), name})
			assert.Equal(t, -32803, err.Code)
			assert.Equal(t, "invalid name", err.Message)
		}
	})
}
