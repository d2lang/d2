//go:build !js || !wasm

package d2fonts

import (
	"embed"
	"encoding/base64"
	"strings"

	"github.com/d2lang/d2/lib/syncmap"
)

//go:embed encoded/SourceSansPro-Regular.txt
var sourceSansProRegularBase64 string

//go:embed encoded/SourceSansPro-Bold.txt
var sourceSansProBoldBase64 string

//go:embed encoded/SourceSansPro-Italic.txt
var sourceSansProItalicBase64 string

//go:embed encoded/SourceCodePro-Regular.txt
var sourceCodeProRegularBase64 string

//go:embed encoded/FuzzyBubbles-Regular.txt
var fuzzyBubblesRegularBase64 string

//go:embed encoded/FuzzyBubbles-Bold.txt
var fuzzyBubblesBoldBase64 string

// These four SVG encodings contain WOFF data. The other encodings already
// contain the full TTF, so each native binary only needs one copy of those faces.
//
//go:embed ttf/SourceSansPro-Semibold.ttf ttf/SourceCodePro-Bold.ttf
//go:embed ttf/SourceCodePro-Semibold.ttf ttf/SourceCodePro-Italic.ttf
var fontFacesFS embed.FS

func init() {
	FontEncodings = syncmap.New[Font, string]()

	FontEncodings.Set(
		Font{
			Family: SourceSansPro,
			Style:  FONT_STYLE_REGULAR,
		},
		sourceSansProRegularBase64)

	FontEncodings.Set(
		Font{
			Family: SourceSansPro,
			Style:  FONT_STYLE_BOLD,
		},
		sourceSansProBoldBase64)

	FontEncodings.Set(
		Font{
			Family: SourceSansPro,
			Style:  FONT_STYLE_SEMIBOLD,
		},
		sourceSansProSemiboldBase64)

	FontEncodings.Set(
		Font{
			Family: SourceSansPro,
			Style:  FONT_STYLE_ITALIC,
		},
		sourceSansProItalicBase64)

	FontEncodings.Set(
		Font{
			Family: SourceCodePro,
			Style:  FONT_STYLE_REGULAR,
		},
		sourceCodeProRegularBase64)

	FontEncodings.Set(
		Font{
			Family: SourceCodePro,
			Style:  FONT_STYLE_BOLD,
		},
		sourceCodeProBoldBase64)

	FontEncodings.Set(
		Font{
			Family: SourceCodePro,
			Style:  FONT_STYLE_SEMIBOLD,
		},
		sourceCodeProSemiboldBase64)

	FontEncodings.Set(
		Font{
			Family: SourceCodePro,
			Style:  FONT_STYLE_ITALIC,
		},
		sourceCodeProItalicBase64)

	FontEncodings.Set(
		Font{
			Family: HandDrawn,
			Style:  FONT_STYLE_REGULAR,
		},
		fuzzyBubblesRegularBase64)

	FontEncodings.Set(
		Font{
			Family: HandDrawn,
			Style:  FONT_STYLE_ITALIC,
			// This font has no italic, so just reuse regular
		}, fuzzyBubblesRegularBase64)
	FontEncodings.Set(
		Font{
			Family: HandDrawn,
			Style:  FONT_STYLE_BOLD,
		}, fuzzyBubblesBoldBase64)
	FontEncodings.Set(
		Font{
			Family: HandDrawn,
			Style:  FONT_STYLE_SEMIBOLD,
			// This font has no semibold, so just reuse bold
		}, fuzzyBubblesBoldBase64)

	FontEncodings.Range(func(k Font, v string) bool {
		FontEncodings.Set(k, strings.TrimSuffix(v, "\n"))
		return true
	})

	FontFaces = syncmap.New[Font, []byte]()

	b, err := decodeEmbeddedTTF(sourceSansProRegularBase64)
	if err != nil {
		panic(err)
	}
	FontFaces.Set(Font{
		Family: SourceSansPro,
		Style:  FONT_STYLE_REGULAR,
	}, b)

	b, err = decodeEmbeddedTTF(sourceCodeProRegularBase64)
	if err != nil {
		panic(err)
	}
	FontFaces.Set(Font{
		Family: SourceCodePro,
		Style:  FONT_STYLE_REGULAR,
	}, b)

	b, err = fontFacesFS.ReadFile("ttf/SourceCodePro-Bold.ttf")
	if err != nil {
		panic(err)
	}
	FontFaces.Set(Font{
		Family: SourceCodePro,
		Style:  FONT_STYLE_BOLD,
	}, b)

	b, err = fontFacesFS.ReadFile("ttf/SourceCodePro-Semibold.ttf")
	if err != nil {
		panic(err)
	}
	FontFaces.Set(Font{
		Family: SourceCodePro,
		Style:  FONT_STYLE_SEMIBOLD,
	}, b)

	b, err = fontFacesFS.ReadFile("ttf/SourceCodePro-Italic.ttf")
	if err != nil {
		panic(err)
	}
	FontFaces.Set(Font{
		Family: SourceCodePro,
		Style:  FONT_STYLE_ITALIC,
	}, b)

	b, err = decodeEmbeddedTTF(sourceSansProBoldBase64)
	if err != nil {
		panic(err)
	}
	FontFaces.Set(Font{
		Family: SourceSansPro,
		Style:  FONT_STYLE_BOLD,
	}, b)

	b, err = fontFacesFS.ReadFile("ttf/SourceSansPro-Semibold.ttf")
	if err != nil {
		panic(err)
	}
	FontFaces.Set(Font{
		Family: SourceSansPro,
		Style:  FONT_STYLE_SEMIBOLD,
	}, b)

	b, err = decodeEmbeddedTTF(sourceSansProItalicBase64)
	if err != nil {
		panic(err)
	}
	FontFaces.Set(Font{
		Family: SourceSansPro,
		Style:  FONT_STYLE_ITALIC,
	}, b)

	b, err = decodeEmbeddedTTF(fuzzyBubblesRegularBase64)
	if err != nil {
		panic(err)
	}
	FontFaces.Set(Font{
		Family: HandDrawn,
		Style:  FONT_STYLE_REGULAR,
	}, b)
	FontFaces.Set(Font{
		Family: HandDrawn,
		Style:  FONT_STYLE_ITALIC,
	}, b)

	b, err = decodeEmbeddedTTF(fuzzyBubblesBoldBase64)
	if err != nil {
		panic(err)
	}
	FontFaces.Set(Font{
		Family: HandDrawn,
		Style:  FONT_STYLE_BOLD,
	}, b)
	FontFaces.Set(Font{
		Family: HandDrawn,
		Style:  FONT_STYLE_SEMIBOLD,
	}, b)

	if err := registerBundledFontFaces(); err != nil {
		panic(err)
	}
}

func decodeEmbeddedTTF(dataURI string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURI, "data:application/font-woff;base64,"))
}
