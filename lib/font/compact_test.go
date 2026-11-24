package font

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	textfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
)

// Decode both WOFFs with an independent OpenType reader. Glyph IDs include
// the components pulled in for accented glyphs even when they have no cmap entry.
func TestCompactSubsetWOFF(t *testing.T) {
	const corpus = "cache hit: 0123456789 ÀßéЖΩ e\u0301 😀"
	names, err := filepath.Glob("../../d2renderers/d2fonts/ttf/*.ttf")
	if err != nil || len(names) == 0 {
		t.Fatalf("locate bundled fonts: %v (%d files)", err, len(names))
	}
	for _, name := range names {
		t.Run(filepath.Base(name), func(t *testing.T) {
			source, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			original := bytes.Clone(source)
			want, err := Sfnt2Woff(UTF8CutFont(bytes.Clone(source), corpus))
			if err != nil {
				t.Fatal(err)
			}
			got, err := SubsetWOFF(source, corpus)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(source, original) || len(got) >= len(want) {
				t.Fatalf("source preserved = %t, old/new WOFF bytes = %d/%d", bytes.Equal(source, original), len(want), len(got))
			}

			var faces [2]*textfont.Face
			var glyphCounts [2]uint16
			for i, font := range [][]byte{want, got} {
				loader, err := opentype.NewLoader(bytes.NewReader(font))
				if err != nil {
					t.Fatal(err)
				}
				maxp, err := loader.RawTable(opentype.MustNewTag("maxp"))
				if err != nil {
					t.Fatal(err)
				}
				glyphCounts[i] = binary.BigEndian.Uint16(maxp[4:6])
				faces[i], err = textfont.ParseTTF(bytes.NewReader(font))
				if err != nil {
					t.Fatal(err)
				}
			}
			if glyphCounts[0] != glyphCounts[1] {
				t.Fatalf("glyph count changed from %d to %d", glyphCounts[0], glyphCounts[1])
			}
			a, b := faces[0].Cmap.Iter(), faces[1].Cmap.Iter()
			for a.Next() {
				if !b.Next() {
					t.Fatal("short cmap")
				}
				ar, ag := a.Char()
				br, bg := b.Char()
				if ar != br || ag != bg {
					t.Fatalf("cmap entry changed from (%U, %d) to (%U, %d)", ar, ag, br, bg)
				}
			}
			if b.Next() {
				t.Fatal("extra cmap entries")
			}
			for glyph := textfont.GID(0); glyph < textfont.GID(glyphCounts[0]); glyph++ {
				a, b := faces[0], faces[1]
				x, xOK := a.GlyphExtents(glyph)
				y, yOK := b.GlyphExtents(glyph)
				if x != y || xOK != yOK || a.HorizontalAdvance(glyph) != b.HorizontalAdvance(glyph) ||
					!reflect.DeepEqual(a.GlyphData(glyph), b.GlyphData(glyph)) {
					t.Fatalf("metrics or outline changed for glyph %d", glyph)
				}
			}
		})
	}
}
