package d2fonts

import (
	_ "embed"
	"encoding/base64"
	"sync"
)

// The generator retains the original kerning and substitution rules.
//
//go:embed encoded/D2Appendix-Regular.woff2
var appendixRegular []byte

//go:embed encoded/D2Appendix-Bold.woff2
var appendixBold []byte

var appendixFaces = [...]struct {
	style    FontStyle
	woff2    []byte
	original string
	encoding func() string
}{
	{style: FONT_STYLE_REGULAR, woff2: appendixRegular},
	{style: FONT_STYLE_BOLD, woff2: appendixBold},
}

func registerAppendixFaces() {
	for i := range appendixFaces {
		f := &appendixFaces[i]
		f.original = FontEncodings.Get(SourceSansPro.Font(0, f.style))
		f.encoding = sync.OnceValue(func() string {
			return woff2DataPrefix + base64.StdEncoding.EncodeToString(f.woff2)
		})
	}
}

// AppendixEncoding supplies the regular face for ASCII footnotes or the bold
// face for their numbers. Callers decide whether their text fits that range.
// An application that registered its own encoding still gets that exact face.
func AppendixEncoding(style FontStyle) string {
	f := SourceSansPro.Font(0, style)
	for _, a := range appendixFaces {
		if a.style == style && a.original == FontEncodings.Get(f) {
			return a.encoding()
		}
	}
	return f.GetEncodedFull()
}
