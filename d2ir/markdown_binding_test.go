package d2ir_test

import (
	"testing"
	"testing/fstest"

	"github.com/d2lang/d2/d2ir"
)

func TestMarkdownImportBindsEachReferenceOnce(t *testing.T) {
	opts := d2ir.CompileOptions{FS: fstest.MapFS{
		"leaf.d2": {Data: []byte(`vars: {literal: '${p}'; p: leaf}
plain: "${literal}/${region}/${zone}"
markdown: |md ${literal}/${region}/${zone} |`)},
		"east.d2": {Data: []byte(`vars: {region: east}; ...@leaf`)},
		"west.d2": {Data: []byte(`vars: {region: west}; ...@leaf`)},
	}}
	root, err := compileTween(t, `vars: {zone: outer}
a: @east
b: @west`, opts)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"a": "${p}/east/outer", "b": "${p}/west/outer"} {
		for _, label := range []string{"plain", "markdown"} {
			if got := tweenText(t, root, id, label); got != want {
				t.Errorf("%s.%s = %q, want %q", id, label, got, want)
			}
		}
	}
}

func TestImportedMarkdownVariableKeepsLiteralSubstitution(t *testing.T) {
	opts := d2ir.CompileOptions{FS: fstest.MapFS{
		"lib.d2": {Data: []byte(`vars: {region: '${zone}'; msg: |md ${region} |}
note: ${msg}`)},
	}}
	root, err := compileTween(t, `vars: {zone: importer}
note: @lib.note`, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := tweenText(t, root, "note"), "${zone}"; got != want {
		t.Errorf("imported Markdown label = %q, want %q", got, want)
	}
}

func TestPartiallyResolvedMarkdownUsesSelectedImportScope(t *testing.T) {
	opts := d2ir.CompileOptions{FS: fstest.MapFS{
		"lib.d2": {Data: []byte(`vars: {env: original; formula: '${zone}'}
plain: "${env} ${zone}"
markdown: |md ${env} ${zone} |
complete: |md ${env} ${formula} |
onlyInLibrary: |md ${formula} ${zone} |`)},
	}}
	root, err := compileTween(t, `vars: {env: importing; zone: outer}
plain: @lib.plain
markdown: @lib.markdown
complete: @lib.complete
onlyInLibrary: @lib.onlyInLibrary`, opts)
	if err != nil {
		t.Fatal(err)
	}
	for label, want := range map[string]string{
		"plain": "importing outer", "markdown": "importing outer",
		"complete": "original ${zone}", "onlyInLibrary": "${zone} outer",
	} {
		if got := tweenText(t, root, label); got != want {
			t.Errorf("%s = %q, want %q", label, got, want)
		}
	}
}
