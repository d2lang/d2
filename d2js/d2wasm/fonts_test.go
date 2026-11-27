//go:build js && wasm

package d2wasm

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/lib/font"
)

// The module carries one copy of each bundled face: the TTF. The registered
// whole-font encoding is that TTF under the historical WOFF media type, which
// GetEncodedFull re-encodes as a compressed WOFF on demand.
//
// The SVG renderer embeds a whole-font encoding only when a face cannot be
// subset. Whether font.SubsetWOFF fails depends on the face file alone, never
// on the characters requested, so one successful subset per face shows that
// no text set in a bundled face makes the renderer emit it. That is what
// allows the wasm build to drop the four separately encoded WOFFs that native
// builds register for SourceSansPro-Semibold and SourceCodePro-Bold, -Semibold
// and -Italic.
func TestBundledSVGFontEncodings(t *testing.T) {
	const prefix = "data:application/font-woff;base64,"
	for _, family := range []d2fonts.FontFamily{d2fonts.SourceSansPro, d2fonts.SourceCodePro, d2fonts.HandDrawn} {
		for _, style := range d2fonts.FontStyles {
			f := d2fonts.Font{Family: family, Style: style}
			ttf := d2fonts.FontFaces.Get(f)
			if len(ttf) == 0 || d2fonts.FontEncodings.Get(f) != prefix+base64.StdEncoding.EncodeToString(ttf) {
				t.Errorf("%s %s SVG data URL differs from its bundled TTF", family, style)
				continue
			}
			if _, err := font.SubsetWOFF(ttf, ""); err != nil {
				t.Errorf("%s %s cannot be subset, so SVGs would embed its whole-font encoding: %v", family, style, err)
			}
			subset := f.GetSVGEncodedSubset("Az09 ")
			woff, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(subset, "data:font/woff2;base64,"))
			if err != nil || !strings.HasPrefix(string(woff), "wOF2") || subset == f.GetEncodedFull() || subset == d2fonts.FontEncodings.Get(f) {
				t.Errorf("%s %s subset fell back to the whole-font encoding", family, style)
			}
		}
	}
}
