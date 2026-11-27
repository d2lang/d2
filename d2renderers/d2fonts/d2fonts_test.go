package d2fonts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/d2lang/d2/lib/font"
	"github.com/d2lang/util-go/assert"
	"github.com/d2lang/util-go/diff"
)

func TestCutFont(t *testing.T) {
	f := Font{
		Family: SourceCodePro,
		Style:  FONT_STYLE_BOLD,
	}
	face := FontFaces.Get(f)
	fontBuf := make([]byte, len(face))
	copy(fontBuf, face)
	fontBuf = font.UTF8CutFont(fontBuf, " 1")
	err := diff.Testdata(filepath.Join("testdata", "d2fonts", "cut"), ".txt", fontBuf)
	assert.Success(t, err)
}

// The bundled SVG encodings are public. Embedding TTF bytes and constructing
// the URI at startup must preserve the original media type and Base64 bytes.
func TestBundledTTFEncodings(t *testing.T) {
	for _, tc := range []struct {
		font Font
		file string
	}{
		{Font{Family: SourceSansPro, Style: FONT_STYLE_REGULAR}, "SourceSansPro-Regular.txt"},
		{Font{Family: SourceSansPro, Style: FONT_STYLE_BOLD}, "SourceSansPro-Bold.txt"},
		{Font{Family: SourceSansPro, Style: FONT_STYLE_ITALIC}, "SourceSansPro-Italic.txt"},
		{Font{Family: SourceCodePro, Style: FONT_STYLE_REGULAR}, "SourceCodePro-Regular.txt"},
		{Font{Family: HandDrawn, Style: FONT_STYLE_REGULAR}, "FuzzyBubbles-Regular.txt"},
		{Font{Family: HandDrawn, Style: FONT_STYLE_BOLD}, "FuzzyBubbles-Bold.txt"},
	} {
		want, err := os.ReadFile(filepath.Join("encoded", tc.file))
		if err != nil {
			t.Fatal(err)
		}
		if got := FontEncodings.Get(tc.font); got != strings.TrimSuffix(string(want), "\n") {
			t.Errorf("encoding for %v differs from %s", tc.font, tc.file)
		}
	}
}
