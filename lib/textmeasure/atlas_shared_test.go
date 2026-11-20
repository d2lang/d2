package textmeasure

import (
	"reflect"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2fonts"
)

func TestRulersShareBundledAtlases(t *testing.T) {
	spec := d2fonts.SourceSansPro.Font(17, d2fonts.FONT_STYLE_BOLD)
	sizeless := spec
	sizeless.Size = SIZELESS_FONT_SIZE
	first, err := NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	first.addFontSize(spec)
	second.addFontSize(spec)
	a, b := first.atlases[spec], second.atlases[spec]
	if reflect.ValueOf(a.mapping).Pointer() != reflect.ValueOf(b.mapping).Pointer() {
		t.Fatal("rulers measured a bundled face twice instead of sharing its atlas")
	}
	if a.face == b.face || a.face != first.faces[spec] || b.face != second.faces[spec] {
		t.Fatal("a shared atlas must keep each ruler's own face")
	}
	// The shared metrics are exactly those of an atlas built from scratch.
	fresh := NewAtlas(first.ttfs[sizeless].newFace(float64(spec.Size)), Runes)
	if !reflect.DeepEqual(fresh.mapping, a.mapping) || fresh.ascent != a.ascent || fresh.descent != a.descent || fresh.lineHeight != a.lineHeight {
		t.Fatal("shared atlas differs from a freshly built one")
	}
}

func TestRuntimeFontAtlasesAreNotRetained(t *testing.T) {
	bundled, ok := d2fonts.FontFaces.Lookup(d2fonts.Font{Family: d2fonts.SourceSansPro, Style: d2fonts.FONT_STYLE_REGULAR})
	if !ok {
		t.Fatal("missing bundled font")
	}
	// A caller's font, even one with identical contents, may be supplied anew
	// for every diagram. Sharing its atlas would keep every copy alive.
	parsed, err := parseFont(append([]byte(nil), bundled...))
	if err != nil {
		t.Fatal(err)
	}
	before := sharedAtlasesSize.Load()
	first := parsed.sharedAtlas(parsed.newFace(19), 19)
	second := parsed.sharedAtlas(parsed.newFace(19), 19)
	if sharedAtlasesSize.Load() != before {
		t.Fatal("an atlas for a font supplied at run time was retained")
	}
	if reflect.ValueOf(first.mapping).Pointer() == reflect.ValueOf(second.mapping).Pointer() {
		t.Fatal("atlases for a font supplied at run time were shared")
	}
}

func TestSharedAtlasesFollowRunes(t *testing.T) {
	spec := d2fonts.SourceSansPro.Font(21, d2fonts.FONT_STYLE_REGULAR)
	first, err := NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	first.addFontSize(spec)
	if first.atlases[spec].contains('→') {
		t.Fatal("the arrow is expected to be outside the default rune set")
	}

	// Runes is exported. A ruler created after a caller extends it measures
	// the new set, as it did before atlases were shared.
	runes := Runes
	t.Cleanup(func() { Runes = runes })
	Runes = append(append([]rune(nil), runes...), '→')
	second, err := NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	second.addFontSize(spec)
	if !second.atlases[spec].contains('→') {
		t.Fatal("a ruler created after Runes changed was served the old atlas")
	}
}
