package d2fonts

import (
	"testing"

	"github.com/d2lang/d2/internal/bundledfont"
)

func TestEncodedSubsetsAreRememberedForBundledFontsOnly(t *testing.T) {
	bundled := Font{Family: SourceSansPro, Style: FONT_STYLE_REGULAR}
	data, ok := FontFaces.Lookup(bundled)
	if !ok || !bundledfont.Is(data) {
		t.Fatal("missing bundled font")
	}
	if bundledfont.Is(append([]byte(nil), data...)) {
		t.Fatal("a copy of a bundled font was taken for the embedded file")
	}
	cache := func() (entries, size int) {
		FontFamiliesMu.Lock()
		defer FontFamiliesMu.Unlock()
		return len(encodedSubsets), encodedSubsetsSize
	}

	// The cache outlives the test, which may run more than once in a process.
	FontFamiliesMu.Lock()
	clear(encodedSubsets)
	encodedSubsetsSize = 0
	FontFamiliesMu.Unlock()

	// The first request is remembered, and its key counts toward the budget.
	entries, size := cache()
	want := bundled.GetEncodedSubset("subset cache")
	if n, s := cache(); n != entries+1 || s != size+len(want)+len("subet cah") {
		t.Fatalf("first subset: %d entries and %d bytes, want %d and %d", n, s, entries+1, size+len(want)+len("subet cah"))
	}
	entries++

	// The same characters in the same first-use order are one subset, however
	// often they repeat.
	if got := bundled.GetEncodedSubset("subset cache subset"); got != want {
		t.Fatal("repeated characters changed the encoded subset")
	}
	if n, _ := cache(); n != entries {
		t.Fatal("an equal subset was encoded and stored twice")
	}

	// A font registered at run time encodes identically but is not retained.
	families := len(FontFamilies)
	family, err := AddFontFamily("subset-cache-test", append([]byte(nil), data...), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		FontFamiliesMu.Lock()
		defer FontFamiliesMu.Unlock()
		FontFamilies = FontFamilies[:families]
		for _, style := range FontStyles {
			FontFaces.Delete(Font{Family: *family, Style: style})
			FontEncodings.Delete(Font{Family: *family, Style: style})
		}
	})
	custom := Font{Family: *family, Style: FONT_STYLE_REGULAR}
	if got := custom.GetEncodedSubset("subset cache"); got != want {
		t.Fatal("identical font data produced a different subset")
	}
	if n, _ := cache(); n != entries {
		t.Fatal("a subset of a font supplied at run time was retained")
	}

	// Another bundled font is another subset of the same characters.
	bold := Font{Family: SourceSansPro, Style: FONT_STYLE_BOLD}
	if got := bold.GetEncodedSubset("subset cache"); got == want {
		t.Fatal("two bundled fonts were given the same subset")
	}
	if n, _ := cache(); n != entries+1 {
		t.Fatal("a second font's subset was not stored under its own key")
	}
}
