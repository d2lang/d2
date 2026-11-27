//go:build js && wasm

package d2fonts

import (
	_ "embed"
	"encoding/base64"

	"github.com/d2lang/d2/lib/syncmap"
)

// The files of ttf/ as one brotli stream; see ttf_stream.go.
//
//go:embed ttf_stream.br
var bundledTTFStream []byte

func init() {
	FontFaces = syncmap.New[Font, []byte]()
	FontEncodings = syncmap.New[Font, string]()

	faces, err := decodeBundledTTFStream(bundledTTFStream)
	if err != nil {
		panic(err)
	}

	for _, face := range []struct {
		filename string
		family   FontFamily
		style    FontStyle
	}{
		{"SourceSansPro-Regular.ttf", SourceSansPro, FONT_STYLE_REGULAR},
		{"SourceSansPro-Bold.ttf", SourceSansPro, FONT_STYLE_BOLD},
		{"SourceSansPro-Semibold.ttf", SourceSansPro, FONT_STYLE_SEMIBOLD},
		{"SourceSansPro-Italic.ttf", SourceSansPro, FONT_STYLE_ITALIC},
		{"SourceCodePro-Regular.ttf", SourceCodePro, FONT_STYLE_REGULAR},
		{"SourceCodePro-Bold.ttf", SourceCodePro, FONT_STYLE_BOLD},
		{"SourceCodePro-Semibold.ttf", SourceCodePro, FONT_STYLE_SEMIBOLD},
		{"SourceCodePro-Italic.ttf", SourceCodePro, FONT_STYLE_ITALIC},
		{"FuzzyBubbles-Regular.ttf", HandDrawn, FONT_STYLE_REGULAR},
		{"FuzzyBubbles-Bold.ttf", HandDrawn, FONT_STYLE_BOLD},
	} {
		ttf, ok := faces[face.filename]
		if !ok {
			panic("d2fonts: " + face.filename + " is not in ttf_stream.br")
		}
		// SVG embeds a subset cut from the TTF. The whole-font encoding is the
		// TTF itself under the historical WOFF media type; a WOFF copy of every
		// face would add its weight to the module for a fallback that subsetting
		// a bundled face never takes.
		uri := "data:application/font-woff;base64," + base64.StdEncoding.EncodeToString(ttf)
		font := Font{Family: face.family, Style: face.style}
		FontFaces.Set(font, ttf)
		FontEncodings.Set(font, uri)
	}

	// HandDrawn uses its regular and bold faces for italic and semibold.
	for _, pair := range []struct{ alias, source FontStyle }{
		{FONT_STYLE_ITALIC, FONT_STYLE_REGULAR},
		{FONT_STYLE_SEMIBOLD, FONT_STYLE_BOLD},
	} {
		alias := Font{Family: HandDrawn, Style: pair.alias}
		source := Font{Family: HandDrawn, Style: pair.source}
		FontFaces.Set(alias, FontFaces.Get(source))
		FontEncodings.Set(alias, FontEncodings.Get(source))
	}
	registerAppendixFaces()
	if err := registerBundledFontFaces(); err != nil {
		panic(err)
	}
}
