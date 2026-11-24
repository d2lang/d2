package d2fonts

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/go-text/typesetting/font/opentype"
)

func TestFullWOFFPreservesOpenTypeTables(t *testing.T) {
	for _, style := range []FontStyle{FONT_STYLE_REGULAR, FONT_STYLE_BOLD} {
		f := SourceSansPro.Font(0, style)
		source := FontFaces.Get(f)
		uri := f.GetEncodedFull()
		woff, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, woffDataPrefix))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(woff, []byte("wOFF")) || len(woff) >= len(source) {
			t.Fatalf("%s: want a compressed full WOFF", style)
		}
		original, err := opentype.NewLoader(bytes.NewReader(source))
		if err != nil {
			t.Fatal(err)
		}
		converted, err := opentype.NewLoader(bytes.NewReader(woff))
		if err != nil {
			t.Fatal(err)
		}
		for _, tag := range original.Tables() {
			a, err := original.RawTable(tag)
			if err != nil {
				t.Fatal(err)
			}
			b, err := converted.RawTable(tag)
			if err != nil {
				t.Fatal(err)
			}
			if tag == opentype.MustNewTag("head") {
				// WOFF reconstructs an SFNT directory and its checksum.
				clear(a[8:12])
				clear(b[8:12])
			}
			if !bytes.Equal(a, b) {
				t.Fatalf("%s: changed table %s", style, tag)
			}
		}
	}
}

func TestFullWOFFRespectsRegisteredEncoding(t *testing.T) {
	custom := Font{Family: "full WOFF test"}
	const uri = "data:application/font-woff;base64,AAEA"
	FontEncodings.Set(custom, uri)
	defer FontEncodings.Delete(custom)
	if got := custom.GetEncodedFull(); got != uri {
		t.Fatal("re-encoded an unrecognized custom source")
	}
}
