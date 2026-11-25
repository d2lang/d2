//go:build js && wasm

package d2wasm

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2fonts"
)

func TestBundledSVGFontEncodings(t *testing.T) {
	// These four URLs contain separately encoded WOFF files. The checksums
	// cover the exact data URLs in d2fonts/encoded, excluding the trailing LF.
	woffDigests := map[d2fonts.Font]string{
		{Family: d2fonts.SourceSansPro, Style: d2fonts.FONT_STYLE_SEMIBOLD}: "42f1adcf985750c625c7745000182f01c9e31d49045a785de3cd57267951ead9",
		{Family: d2fonts.SourceCodePro, Style: d2fonts.FONT_STYLE_BOLD}:     "ac6848b8885d0bac5fa1615940d330d4709183324b30c4b467100c08d1df353e",
		{Family: d2fonts.SourceCodePro, Style: d2fonts.FONT_STYLE_SEMIBOLD}: "10419913ae992638aaf2da85a08dc0aab214110046bde4cc48ff17bf7759da17",
		{Family: d2fonts.SourceCodePro, Style: d2fonts.FONT_STYLE_ITALIC}:   "d204aed9b79cb0c1d8cc0d926ed8a56008ee9c86c5c503aa7ae9f15a96dd2891",
	}
	for _, family := range []d2fonts.FontFamily{d2fonts.SourceSansPro, d2fonts.SourceCodePro, d2fonts.HandDrawn} {
		for _, style := range d2fonts.FontStyles {
			font := d2fonts.Font{Family: family, Style: style}
			uri := d2fonts.FontEncodings.Get(font)
			if want, ok := woffDigests[font]; ok {
				if got := fmt.Sprintf("%x", sha256.Sum256([]byte(uri))); got != want {
					t.Errorf("%s %s WOFF data URL SHA-256 = %s, want %s", family, style, got, want)
				}
				continue
			}
			ttf := d2fonts.FontFaces.Get(font)
			if len(ttf) == 0 || uri != "data:application/font-woff;base64,"+base64.StdEncoding.EncodeToString(ttf) {
				t.Errorf("%s %s SVG data URL differs from its bundled TTF", family, style)
			}
		}
	}
}
