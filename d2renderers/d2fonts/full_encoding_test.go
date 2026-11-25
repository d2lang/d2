package d2fonts

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/go-text/typesetting/font/opentype"

	"github.com/d2lang/d2/lib/font"
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
		legacy, err := font.Sfnt2Woff(bytes.Clone(source))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(woff, []byte("wOFF")) || len(woff) >= len(legacy) {
			t.Fatalf("%s: want a compressed full WOFF", style)
		}
		// Both WOFFs reconstruct the same SFNT directory and checksum.
		// Every decompressed table, including head, must be identical.
		original, err := opentype.NewLoader(bytes.NewReader(legacy))
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

func TestSubsetFallsBackToFullEncoding(t *testing.T) {
	f := Font{Family: "outline fallback"}
	t.Cleanup(func() {
		FontFaces.Delete(f)
		FontEncodings.Delete(f)
	})

	// Isolate the SFNT wrapper from any particular outline data. CFF stores
	// the OTTO flavor in the file header, not in the head table's version.
	face := make([]byte, 12+16+54+2)
	copy(face, "OTTO")
	binary.BigEndian.PutUint16(face[4:], 1)
	copy(face[12:], "head")
	binary.BigEndian.PutUint32(face[20:], 28)
	binary.BigEndian.PutUint32(face[24:], 54)
	binary.BigEndian.PutUint32(face[28:], 0x00010000)
	if err := AddFontStyle(f, f.Style, face); err != nil {
		t.Fatal(err)
	}
	want := FontEncodings.Get(f)
	got := f.GetEncodedSubset("Read pool")
	if got != want {
		t.Fatal("unsupported outlines did not use the registered full face")
	}
	woff, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(got, woffDataPrefix))
	if err != nil || !bytes.HasPrefix(woff, []byte("wOFFOTTO")) {
		t.Fatalf("full WOFF lost the OpenType flavor: %v", err)
	}

	// Applications can register a full encoding without a source for subsets.
	FontFaces.Delete(f)
	if got := f.GetEncodedSubset("Read pool"); got != want {
		t.Fatal("encoding-only registration lost its full font")
	}
}
