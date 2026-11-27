package d2fonts

import (
	"encoding/base64"
	"sync"

	"github.com/d2lang/d2/lib/font"
)

// These faces are generated with the original kerning and substitution rules.
// D2's general-purpose subsetter omits those tables.
var appendixFaces = [...]struct {
	style    FontStyle
	file     string
	original string
	encoding func() string
}{
	{style: FONT_STYLE_REGULAR, file: "D2Appendix-Regular.ttf"},
	{style: FONT_STYLE_BOLD, file: "D2Appendix-Bold.ttf"},
}

func registerAppendixFaces(files func(string) []byte) {
	for i := range appendixFaces {
		f := &appendixFaces[i]
		ttf := files(f.file)
		if len(ttf) == 0 {
			panic("d2fonts: missing bundled " + f.file)
		}
		f.original = FontEncodings.Get(SourceSansPro.Font(0, f.style))
		f.encoding = sync.OnceValue(func() string {
			data, err := font.FullWOFF(ttf)
			if err != nil {
				panic("d2fonts: encode " + f.file + ": " + err.Error())
			}
			return woffDataPrefix + base64.StdEncoding.EncodeToString(data)
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
