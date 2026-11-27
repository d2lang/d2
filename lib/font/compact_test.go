package font_test

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"

	textfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"

	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/lib/font"
)

// Decode both WOFFs with an independent OpenType reader. Glyph IDs include
// the components pulled in for accented glyphs even when they have no cmap entry.
func TestCompactSubsetWOFF(t *testing.T) {
	const mixed = "cache hit: 0123456789 ÀßéЖΩ e\u0301 😀"
	for _, family := range d2fonts.FontFamilies {
		for _, style := range d2fonts.FontStyles {
			f := d2fonts.Font{Family: family, Style: style}
			source := d2fonts.FontFaces.Get(f)
			t.Run(string(family)+"/"+string(style), func(t *testing.T) {
				t.Run("bundled", func(t *testing.T) { checkCompactSubset(t, source, "Azé 0123", true) })
				t.Run("mixed", func(t *testing.T) { checkCompactSubset(t, source, mixed, true) })
				t.Run("custom", func(t *testing.T) { checkCompactSubset(t, bytes.Clone(source), mixed, false) })
			})
		}
	}
}

func checkCompactSubset(t *testing.T, source []byte, corpus string, bundled bool) {
	t.Helper()
	original := bytes.Clone(source)
	want, err := font.Sfnt2Woff(font.UTF8CutFont(bytes.Clone(source), corpus))
	if err != nil {
		t.Fatal(err)
	}
	got, err := font.SubsetWOFF(source, corpus)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(source, original) || len(got) >= len(want) {
		t.Fatalf("source preserved = %t, old/new WOFF bytes = %d/%d", bytes.Equal(source, original), len(want), len(got))
	}

	var faces [2]*textfont.Face
	var glyphCounts [2]uint16
	var names [2][]byte
	for i, font := range [][]byte{want, got} {
		loader, err := opentype.NewLoader(bytes.NewReader(font))
		if err != nil {
			t.Fatal(err)
		}
		maxp, err := loader.RawTable(opentype.MustNewTag("maxp"))
		if err != nil {
			t.Fatal(err)
		}
		names[i], err = loader.RawTable(opentype.MustNewTag("name"))
		if err != nil {
			t.Fatal(err)
		}
		glyphCounts[i] = binary.BigEndian.Uint16(maxp[4:6])
		faces[i], err = textfont.ParseTTF(bytes.NewReader(font))
		if err != nil {
			t.Fatal(err)
		}
	}
	if bundled {
		var postScriptNames [2]string
		for i, data := range names {
			table, _, err := tables.ParseName(data)
			if err != nil {
				t.Fatal(err)
			}
			postScriptNames[i] = table.Name(6)
		}
		if binary.BigEndian.Uint16(names[1][2:]) != 1 || postScriptNames[0] == "" || postScriptNames[0] != postScriptNames[1] {
			t.Errorf("bundled subset should keep only its PostScript name %q, got %q", postScriptNames[0], postScriptNames[1])
		}
	} else if !bytes.Equal(names[1], names[0]) {
		t.Error("custom font name table changed")
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
		if a.HorizontalAdvance(glyph) != b.HorizontalAdvance(glyph) {
			t.Fatalf("advance changed for glyph %d", glyph)
		}
		if glyph == 0 && bundled {
			// TestBundledSubsetMissingGlyph covers the conditional outline.
			continue
		}
		x, xOK := a.GlyphExtents(glyph)
		y, yOK := b.GlyphExtents(glyph)
		if x != y || xOK != yOK ||
			!reflect.DeepEqual(a.GlyphData(glyph), b.GlyphData(glyph)) {
			t.Fatalf("metrics or outline changed for glyph %d", glyph)
		}
	}
}

func TestBundledSubsetMissingGlyph(t *testing.T) {
	f := d2fonts.Font{Family: d2fonts.SourceSansPro, Style: d2fonts.FONT_STYLE_REGULAR}
	source := d2fonts.FontFaces.Get(f)
	original, err := textfont.ParseTTF(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		corpus string
		keep   bool
	}{
		{"Az é", false},
		{"", true},
		{" ", true},
		{"🎃", true},
		{"A\u0378B", true},
	} {
		data, err := font.SubsetWOFF(source, tc.corpus)
		if err != nil {
			t.Fatal(err)
		}
		face, err := textfont.ParseTTF(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("load subset for %q: %v", tc.corpus, err)
		}
		if face.HorizontalAdvance(0) != original.HorizontalAdvance(0) {
			t.Errorf("corpus %q changed the missing glyph advance", tc.corpus)
		}
		outline, _ := face.GlyphDataOutline(0)
		if tc.keep {
			want, _ := original.GlyphDataOutline(0)
			if !reflect.DeepEqual(outline, want) {
				t.Errorf("corpus %q lost its missing glyph outline", tc.corpus)
			}
		} else if len(outline.Segments) != 0 {
			t.Errorf("corpus %q retained an unused missing glyph outline", tc.corpus)
		}
	}
}
