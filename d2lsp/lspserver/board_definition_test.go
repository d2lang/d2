package lspserver

import (
	"testing"
)

// The layer and scenario share a name, and astral characters before the usage
// occupy two UTF-16 code units each.
func TestDefinitionInsideBoard(t *testing.T) {
	const uri = "untitled:board"
	c := newTestClient(t)
	c.open(uri, `vars.color: red
layers.詳細: {
  vars.color: blue
  classes.accent: {style.fill: blue}
  "😀😀😀".class: accent
}
scenarios: {詳細: {
  vars.color: green
  "😀😀😀".style.fill: ${color}
}}`)
	for _, tc := range []struct {
		pos lspPosition
		def lspRange
	}{
		{lspPosition{4, 22}, lineRange(3, 10, 16)},
		{lspPosition{8, 28}, lineRange(7, 7, 12)},
	} {
		got := decode[*location](t, c.request("textDocument/definition", posParams(uri, tc.pos.Line, tc.pos.Character)))
		if got == nil || got.URI != uri || got.Range != tc.def {
			t.Fatalf("definition at %+v = %+v, want %+v", tc.pos, got, tc.def)
		}
	}
}
