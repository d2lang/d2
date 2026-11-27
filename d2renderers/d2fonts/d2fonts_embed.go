//go:build !js || !wasm

// WebAssembly builds load the same TTF faces from ttf_stream.br instead
// (d2fonts_embed_wasm.go).

package d2fonts

import (
	"embed"
	"encoding/base64"
	"strings"

	"github.com/d2lang/d2/lib/syncmap"
)

//go:embed ttf/*.ttf
var fontFacesFS embed.FS

func init() {
	FontFaces = syncmap.New[Font, []byte]()
	FontEncodings = syncmap.New[Font, string]()

	for _, face := range []struct {
		filename string
		family   FontFamily
		style    FontStyle
		woff     string
	}{
		{"SourceSansPro-Regular.ttf", SourceSansPro, FONT_STYLE_REGULAR, ""},
		{"SourceSansPro-Bold.ttf", SourceSansPro, FONT_STYLE_BOLD, ""},
		{"SourceSansPro-Semibold.ttf", SourceSansPro, FONT_STYLE_SEMIBOLD, sourceSansProSemiboldBase64},
		{"SourceSansPro-Italic.ttf", SourceSansPro, FONT_STYLE_ITALIC, ""},
		{"SourceCodePro-Regular.ttf", SourceCodePro, FONT_STYLE_REGULAR, ""},
		{"SourceCodePro-Bold.ttf", SourceCodePro, FONT_STYLE_BOLD, sourceCodeProBoldBase64},
		{"SourceCodePro-Semibold.ttf", SourceCodePro, FONT_STYLE_SEMIBOLD, sourceCodeProSemiboldBase64},
		{"SourceCodePro-Italic.ttf", SourceCodePro, FONT_STYLE_ITALIC, sourceCodeProItalicBase64},
		{"FuzzyBubbles-Regular.ttf", HandDrawn, FONT_STYLE_REGULAR, ""},
		{"FuzzyBubbles-Bold.ttf", HandDrawn, FONT_STYLE_BOLD, ""},
	} {
		ttf, err := fontFacesFS.ReadFile("ttf/" + face.filename)
		if err != nil {
			panic(err)
		}
		uri := strings.TrimSuffix(face.woff, "\n")
		if uri == "" {
			// These six SVG encodings use the original TTF bytes with the historical WOFF media type.
			uri = "data:application/font-woff;base64," + base64.StdEncoding.EncodeToString(ttf)
		}
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
