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
