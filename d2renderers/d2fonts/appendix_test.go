package d2fonts

import (
	"bytes"
	"encoding/base64"
	"slices"
	"strings"
	"testing"

	textfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/harfbuzz"
)

func TestAppendixFacesPreserveShaping(t *testing.T) {
	for _, tc := range []struct {
		style    FontStyle
		text     string
		maxBytes int
	}{
		{FONT_STYLE_REGULAR, "AVATAR To Wo office ffi ffl $123.45 ﬀ ²", 24000},
		{FONT_STYLE_BOLD, "0123456789109876543210 o H ﬀ ²", 24000},
	} {
		original := FontFaces.Get(SourceSansPro.Font(0, tc.style))
		woff, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(AppendixEncoding(tc.style), woffDataPrefix))
		if err != nil {
			t.Fatal(err)
		}
		if len(woff) > tc.maxBytes {
			t.Fatalf("%s appendix subset grew to %d bytes", tc.style, len(woff))
		}
		type glyph struct {
			name                                 string
			cluster                              int
			xAdvance, yAdvance, xOffset, yOffset harfbuzz.Position
		}
		shape := func(data []byte) []glyph {
			f, err := textfont.ParseTTF(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			hb := harfbuzz.NewBuffer()
			hb.AddRunes([]rune(tc.text), 0, -1)
			hb.GuessSegmentProperties()
			hb.Shape(harfbuzz.NewFont(f), nil)
			var glyphs []glyph
			for i, g := range hb.Info {
				p := hb.Pos[i]
				glyphs = append(glyphs, glyph{f.GlyphName(g.Glyph), g.Cluster, p.XAdvance, p.YAdvance, p.XOffset, p.YOffset})
			}
			return glyphs
		}
		if !slices.Equal(shape(original), shape(woff)) {
			t.Fatalf("%s appendix glyph selection or positioning changed", tc.style)
		}
	}
}

func TestAppendixFacesRespectRegisteredEncoding(t *testing.T) {
	f := SourceSansPro.Font(0, FONT_STYLE_REGULAR)
	original := FontEncodings.Get(f)
	defer FontEncodings.Set(f, original)
	const replacement = "https://example.org/custom.woff"
	FontEncodings.Set(f, replacement)
	if got := AppendixEncoding(f.Style); got != replacement {
		t.Fatal("overrode custom appendix font encoding")
	}
}
