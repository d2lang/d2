package d2format_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
)

func TestImportFormatPreservesSelection(t *testing.T) {
	files := fstest.MapFS{
		"NULL.d2":  {Data: []byte("x: uppercase filename")},
		"theme.d2": {Data: []byte("d2: selected\nunrelated: other")},
		"STYLE.d2": {Data: []byte("x: uppercase filename")},
		"style.d2": {Data: []byte("x: lowercase filename")},
	}
	for _, tc := range []struct{ source, label string }{
		{"x: @theme.d2.d2", "selected"},
		{`x: @"theme".d2`, "selected"},
		{`x: @theme."d2"`, "selected"},
		{"...@STYLE", "uppercase filename"},
		{`...@"NULL"`, "uppercase filename"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			source := tc.source
			for pass := 0; pass < 2; pass++ {
				g, _, err := d2compiler.Compile("main.d2", strings.NewReader(source), &d2compiler.CompileOptions{FS: files})
				if err != nil {
					t.Fatal(err)
				}
				if len(g.Objects) != 1 || g.Objects[0].Label.Value != tc.label {
					t.Fatalf("expected one object labeled %q for %q", tc.label, source)
				}
				m, err := d2parser.Parse("main.d2", strings.NewReader(source), nil)
				if err != nil {
					t.Fatal(err)
				}
				formatted := d2format.Format(m)
				if pass == 1 && formatted != source {
					t.Fatalf("formatting twice changed the import: %q -> %q", source, formatted)
				}
				source = formatted
			}
		})
	}
}
