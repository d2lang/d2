package lspserver

import (
	"strings"
	"testing"

	"github.com/d2lang/util-go/assert"
)

func TestRenameComputedNear(t *testing.T) {
	const uri = "untitled:placement"
	for _, tc := range []struct{ name, text string }{
		{"variable", "¦a\nvars.anchor: a\nnote.near: ${anchor}"},
		{"interpolation", "box.¦a\nvars.child: a\nnote.near: box.${child}"},
		{"quoted interpolation", "box.¦a\nvars.child: a\nnote.near: \"box.${child}\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestClient(t)
			pos := openAtCursor(t, client, uri, tc.text)
			refs := client.references(pos, false)
			assert.Equal(t, 1, len(refs))
			line := strings.Split(tc.text, "\n")[2]
			assert.Equal(t, lineRange(2, strings.Index(line, ": ")+2, len(line)), refs[0].Range)
			err := client.requestError("textDocument/rename", renameParams{pos, "renamed"})
			assert.Equal(t, -32803, err.Code)
			assert.True(t, strings.Contains(err.Message, "computed"))
		})
	}
}
