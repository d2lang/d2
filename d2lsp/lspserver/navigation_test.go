package lspserver

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/d2lang/util-go/assert"
)

// Mark the cursor with ¦ in test diagrams and count its UTF-16 position.
func openAtCursor(t *testing.T, c *testClient, uri, source string) textDocumentPositionParams {
	t.Helper()
	before, after, ok := strings.Cut(source, "¦")
	assert.True(t, ok)
	line := strings.Count(before, "\n")
	currentLine := before[strings.LastIndexByte(before, '\n')+1:]
	pos := posParams(uri, line, len(utf16.Encode([]rune(currentLine))))
	c.open(uri, before+after)
	return pos
}

func TestDefinitions(t *testing.T) {
	const uri = "untitled:def"
	for _, tc := range []struct {
		name, source string
		want         lspRange
	}{
		{"L46_Shape", "x\ny\nx¦ -> y", lineRange(0, 0, 1)},
		{"L47_Nested", "a: {b}\na.b¦ -> c", lineRange(0, 4, 5)},
		{"L48_Parent", "y\na: {x -> _.y¦}", lineRange(0, 0, 1)},
		{"L51_Class", "classes: {service: {shape: page}}\nx.class: serv¦ice", lineRange(0, 10, 17)},
		{"L51_ClassArray", "classes: {a: {}; b: {}}\nx.class: [a; ¦b]", lineRange(0, 17, 18)},
		{"L52_Variable", "vars: {env: prod}\nx: ${e¦nv}", lineRange(0, 7, 10)},
		{"L52_NestedVariable", "vars: {a: {b: value}}\nx: ${a.¦b}", lineRange(0, 11, 12)},
		{"L54_Partial", "x\nbroken: @missing\nx¦ -> y", lineRange(0, 0, 1)},
		{"ClassInBoard", "classes: {svc: {}}\nlayers: {detail: {\n  classes: {svc: {}}\n  x.class: s¦vc\n}}", lineRange(2, 12, 15)},
		{"VariableInBoard", "vars: {env: root}\nlayers: {detail: {\n  vars: {env: board}\n  x: ${e¦nv}\n}}", lineRange(2, 9, 12)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			pos := openAtCursor(t, c, uri, tc.source)
			got := decode[*location](t, c.request("textDocument/definition", pos))
			assert.True(t, got != nil)
			assert.Equal(t, location{URI: uri, Range: tc.want}, *got)
		})
	}
	for _, tc := range []struct{ name, source string }{
		{"L48_ParentMarker", "y\na: {x -> ¦_.y}"},
		{"L53_Keyword", "x.sh¦ape: diamond"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			pos := openAtCursor(t, c, uri, tc.source)
			assert.Equal(t, "null", string(c.request("textDocument/definition", pos)))
		})
	}
}

func TestImportedDefinitions(t *testing.T) {
	dir := t.TempDir()
	main := pathToURI(filepath.Join(dir, "main.d2"))
	otherFile := filepath.Join(dir, "other.d2")
	other := pathToURI(otherFile)
	assert.WriteFile(t, otherFile, []byte("# exported shapes\nservice: Service"), 0o600)
	for _, tc := range []struct {
		name, source string
		want         lspRange
	}{
		{"L49_ImportedShape", "...@other\ns¦ervice -> client", lineRange(1, 0, 7)},
		{"L50_Import", "...@ot¦her\nservice -> client", lspRange{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			pos := openAtCursor(t, c, main, tc.source)
			got := decode[*location](t, c.request("textDocument/definition", pos))
			assert.True(t, got != nil)
			assert.Equal(t, location{URI: other, Range: tc.want}, *got)
		})
	}
}

func (c *testClient) references(pos textDocumentPositionParams, includeDeclaration bool) []location {
	c.t.Helper()
	params := referenceParams{textDocumentPositionParams: pos}
	params.Context.IncludeDeclaration = includeDeclaration
	return decode[[]location](c.t, c.request("textDocument/references", params))
}

func TestReferences(t *testing.T) {
	const uri = "untitled:refs"
	for _, tc := range []struct {
		name, source string
		declaration  bool
		want         []lspRange
	}{
		{"L55_DistinctScopes", "x¦\nx.a\na.x\nx -> y", true, []lspRange{lineRange(0, 0, 1), lineRange(1, 0, 1), lineRange(3, 0, 1)}},
		{"L56_OmitDeclaration", "x¦\nx.a\na.x\nx -> y", false, []lspRange{lineRange(1, 0, 1), lineRange(3, 0, 1)}},
		{"L57_Case", "API -> db\na¦pi.shape: hexagon", true, []lspRange{lineRange(0, 0, 3), lineRange(1, 0, 3)}},
		{"L58_GeneratedByGlob", "a¦\n*.style.fill: red", true, []lspRange{lineRange(0, 0, 1)}},
		{"L59_ScenarioCopies", "a¦ -> b\nscenarios: {bright: {\n  a.style.fill: red\n}}", true, []lspRange{lineRange(0, 0, 1), lineRange(2, 2, 3)}},
		{"L60_Edge", "a -¦> b\n(a -> b)[0].style.stroke: red", true, []lspRange{lineRange(0, 0, 6), lineRange(1, 1, 7)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			pos := openAtCursor(t, c, uri, tc.source)
			got := c.references(pos, tc.declaration)
			want := make([]location, len(tc.want))
			for i, r := range tc.want {
				want[i] = location{URI: uri, Range: r}
			}
			assert.JSON(t, want, got)
		})
	}
	t.Run("L58_GlobCursor", func(t *testing.T) {
		c := newTestClient(t)
		pos := openAtCursor(t, c, uri, "a\n¦*.style.fill: red")
		assert.Equal(t, 0, len(c.references(pos, true)))
	})
}

func TestL61_ShapeHover(t *testing.T) {
	c := newTestClient(t)
	pos := openAtCursor(t, c, "untitled:shape", "cluster: {orders: Orders {shape: cylinder}}\ncluster.or¦ders -> api")
	got := decode[*hover](t, c.request("textDocument/hover", pos))
	assert.True(t, got != nil)
	for _, part := range []string{"**cluster.orders** (shape)", "label: Orders", "shape: cylinder", "2 references"} {
		if !strings.Contains(got.Contents.Value, part) {
			t.Errorf("missing %q in hover %q", part, got.Contents.Value)
		}
	}
	assert.True(t, got.Range != nil)
	assert.Equal(t, lineRange(1, 8, 14), *got.Range)
}

func TestL62_BrokenFileNavigation(t *testing.T) {
	c := newTestClient(t)
	pos := openAtCursor(t, c, "untitled:broken", "¦]\n{\n(a ->)[:\n[")
	assert.Equal(t, "null", string(c.request("textDocument/definition", pos)))
	assert.Equal(t, "null", string(c.request("textDocument/hover", pos)))
	assert.Equal(t, 0, len(c.references(pos, true)))
}

func TestDeclaredSymbolHover(t *testing.T) {
	for _, tc := range []struct{ source, heading, value string }{
		{"vars: {e¦nv: prod}", "(variable)", "value: prod"},
		{"classes: {s¦vc: {shape: page}}", "(class)", "svc"},
		{"layers: {det¦ail: {a}}", "(board)", "detail"},
	} {
		c := newTestClient(t)
		pos := openAtCursor(t, c, "untitled:hover", tc.source)
		got := decode[*hover](t, c.request("textDocument/hover", pos))
		assert.True(t, got != nil)
		for _, part := range []string{tc.heading, tc.value, "reference"} {
			if !strings.Contains(got.Contents.Value, part) {
				t.Errorf("missing %q in hover %q", part, got.Contents.Value)
			}
		}
	}
}

func TestImportedDefinitionTracksUnsavedChanges(t *testing.T) {
	dir := t.TempDir()
	main := pathToURI(filepath.Join(dir, "main.d2"))
	other := pathToURI(filepath.Join(dir, "other.d2"))
	c := newTestClient(t)
	c.open(other, "service")
	pos := openAtCursor(t, c, main, "...@other\ns¦ervice -> client")
	definition := func() location {
		return decode[location](t, c.request("textDocument/definition", pos))
	}
	assert.Equal(t, location{other, lineRange(0, 0, 7)}, definition())
	c.change(other, 2, "# moved\nservice")
	assert.Equal(t, location{other, lineRange(1, 0, 7)}, definition())
}
