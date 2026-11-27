package lspserver

import "testing"

func TestVariableDefinitionScope(t *testing.T) {
	const uri = "untitled:scope"
	for _, tc := range []struct {
		name, source string
		want         lspRange
	}{
		{"nested", "vars.one: root\nbox: {vars.one: local; x: ${o¦ne}}", lineRange(1, 11, 14)},
		{"dotted", "vars.one: root\nbox.vars.one: local\nbox.child.x: ${o¦ne}", lineRange(1, 9, 12)},
		{"edge label", "vars.one: root\nbox: {vars.one: local; a -> b: ${o¦ne}}", lineRange(1, 11, 14)},
		{"extend outer", "vars.one: root\nbox.vars.one: \"${o¦ne}-local\"", lineRange(0, 5, 8)},
		{"array", "vars.one: root\nbox: {vars.one: local; a.tooltip: [${o¦ne}]}", lineRange(1, 11, 14)},
		{"board", "vars.one: root\nscenarios.s: {vars.one: board; box: {vars.one: local; x: ${o¦ne}}}", lineRange(1, 42, 45)},
		{"nested variable path", "vars.db.host: outer\nbox.vars.db.host: local\nbox.x: ${db.h¦ost}", lineRange(1, 12, 16)},
		{"map spread", "vars.template: {a}\nbox: {vars.template: {b}; ...${tem¦plate}}", lineRange(1, 11, 19)},
		{"exact spelling before local fallback", "vars.style: root\nbox: {vars.\"style\": local; x: ${st¦yle}}", lineRange(0, 5, 10)},
		{"quoted spelling before local fallback", "vars.\"style\": root\nbox: {vars.style: local; x: ${\"st¦yle\"}}", lineRange(0, 5, 12)},
		{"local fallback", "box: {vars.style: local; x: ${\"st¦yle\"}}", lineRange(0, 11, 16)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			pos := openAtCursor(t, c, uri, tc.source)
			got := decode[*location](t, c.request("textDocument/definition", pos))
			if got == nil || got.URI != uri || got.Range != tc.want {
				t.Fatalf("definition = %+v, want %+v", got, tc.want)
			}
		})
	}
}
