package d2fonts

import (
	"bytes"
	"testing"
)

func TestReplacingFontFamily(t *testing.T) {
	const family FontFamily = "replacement-test"
	count := len(FontFamilies)
	t.Cleanup(func() {
		FontFamilies = FontFamilies[:count]
		for _, style := range FontStyles {
			f := Font{Family: family, Style: style}
			FontFaces.Delete(f)
			FontEncodings.Delete(f)
		}
	})
	regular := Font{Family: family, Style: FONT_STYLE_REGULAR}
	first := FontFaces.Get(Font{Family: SourceCodePro, Style: FONT_STYLE_REGULAR})
	second := FontFaces.Get(Font{Family: SourceSansPro, Style: FONT_STYLE_REGULAR})
	if _, err := AddFontFamily(string(family), first, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	initial := FontEncodings.Get(regular)
	if _, err := AddFontFamily(string(family), second, []byte("invalid font"), nil, nil); err == nil {
		t.Fatal("registered invalid italic font")
	}
	if !bytes.Equal(FontFaces.Get(regular), first) || FontEncodings.Get(regular) != initial {
		t.Fatal("failed registration partially replaced regular font")
	}
	if _, err := AddFontFamily(string(family), nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(FontFamilies) != count+1 || !bytes.Equal(FontFaces.Get(regular), second) {
		t.Fatal("replacing the family must reuse its entry and restore the bundled fallback")
	}
}
