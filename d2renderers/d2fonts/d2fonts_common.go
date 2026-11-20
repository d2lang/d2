// Package d2fonts holds the fonts used to measure and render diagrams.
package d2fonts

import (
	"encoding/base64"
	"fmt"
	"strings"
	"sync"

	"github.com/d2lang/d2/internal/bundledfont"
	"github.com/d2lang/d2/lib/font"
	fontlib "github.com/d2lang/d2/lib/font"
	"github.com/d2lang/d2/lib/syncmap"
)

type FontFamily string
type FontStyle string

type Font struct {
	Family FontFamily
	Style  FontStyle
	Size   int
}

func (f FontFamily) Font(size int, style FontStyle) Font {
	return Font{
		Family: f,
		Style:  style,
		Size:   size,
	}
}

func (f Font) GetEncodedSubset(corpus string) string {
	var unique strings.Builder
	uniqueMap := make(map[rune]bool)
	for _, char := range corpus {
		if _, exists := uniqueMap[char]; !exists {
			uniqueMap[char] = true
			unique.WriteRune(char)
		}
	}
	uniqueChars := unique.String()

	FontFamiliesMu.Lock()
	defer FontFamiliesMu.Unlock()
	face := FontFaces.Get(f)
	// Only bundled fonts are remembered: a caller's font may be registered
	// afresh for every diagram and must not be kept alive by this cache.
	cacheable := bundledfont.Is(face)
	var key encodedSubsetKey
	if cacheable {
		key = encodedSubsetKey{face: &face[0], chars: uniqueChars}
		if encoded, ok := encodedSubsets[key]; ok {
			return encoded
		}
	}
	fontBuf := make([]byte, len(face))
	copy(fontBuf, face)
	fontBuf = font.UTF8CutFont(fontBuf, uniqueChars)

	fontBuf, err := fontlib.Sfnt2Woff(fontBuf)
	if err != nil {
		// If subset fails, return full encoding
		return FontEncodings.Get(f)
	}

	encoded := fmt.Sprintf("data:application/font-woff;base64,%v", base64.StdEncoding.EncodeToString(fontBuf))
	if cacheable {
		// The key is retained too, and characters the font lacks lengthen it
		// without adding anything to the subset.
		size := len(encoded) + len(uniqueChars)
		if encodedSubsetsSize+size > maxEncodedSubsetsSize {
			clear(encodedSubsets)
			encodedSubsetsSize = 0
		}
		encodedSubsets[key] = encoded
		encodedSubsetsSize += size
	}
	return encoded
}

// encodedSubsetKey identifies a subset by a bundled font file's backing array
// and the characters retained, in order of first use.
type encodedSubsetKey struct {
	face  *byte
	chars string
}

// A subset is a pure function of the font file and its characters, and
// cutting, compressing and encoding one is most of the cost of embedding
// fonts. Boards of one diagram, successive renders while watching a file and
// the layouts of one test all repeat the same subsets. FontFamiliesMu guards
// the cache, which is emptied whenever its keys and values outgrow the budget.
const maxEncodedSubsetsSize = 8 << 20

var (
	encodedSubsets     = make(map[encodedSubsetKey]string)
	encodedSubsetsSize int
)

const (
	FONT_SIZE_XS   = 13
	FONT_SIZE_S    = 14
	FONT_SIZE_M    = 16
	FONT_SIZE_L    = 20
	FONT_SIZE_XL   = 24
	FONT_SIZE_XXL  = 28
	FONT_SIZE_XXXL = 32

	FONT_STYLE_REGULAR  FontStyle = "regular"
	FONT_STYLE_BOLD     FontStyle = "bold"
	FONT_STYLE_SEMIBOLD FontStyle = "semibold"
	FONT_STYLE_ITALIC   FontStyle = "italic"

	SourceSansPro FontFamily = "SourceSansPro"
	SourceCodePro FontFamily = "SourceCodePro"
	HandDrawn     FontFamily = "HandDrawn"
)

var FontSizes = []int{
	FONT_SIZE_XS,
	FONT_SIZE_S,
	FONT_SIZE_M,
	FONT_SIZE_L,
	FONT_SIZE_XL,
	FONT_SIZE_XXL,
	FONT_SIZE_XXXL,
}

var FontStyles = []FontStyle{
	FONT_STYLE_REGULAR,
	FONT_STYLE_BOLD,
	FONT_STYLE_SEMIBOLD,
	FONT_STYLE_ITALIC,
}

var FontFamilies = []FontFamily{
	SourceSansPro,
	SourceCodePro,
	HandDrawn,
}

var FontFamiliesMu sync.Mutex

var FontEncodings syncmap.SyncMap[Font, string]
var FontFaces syncmap.SyncMap[Font, []byte]

var D2_FONT_TO_FAMILY = map[string]FontFamily{
	"default": SourceSansPro,
	"mono":    SourceCodePro,
}

func AddFontStyle(font Font, style FontStyle, ttf []byte) error {
	FontFaces.Set(font, ttf)

	woff, err := fontlib.Sfnt2Woff(ttf)
	if err != nil {
		return fmt.Errorf("failed to encode ttf to woff: %v", err)
	}
	encodedWoff := fmt.Sprintf("data:application/font-woff;base64,%v", base64.StdEncoding.EncodeToString(woff))
	FontEncodings.Set(font, encodedWoff)

	return nil
}

func AddFontFamily(name string, regularTTF, italicTTF, boldTTF, semiboldTTF []byte) (*FontFamily, error) {
	FontFamiliesMu.Lock()
	defer FontFamiliesMu.Unlock()
	customFontFamily := FontFamily(name)

	regularFont := Font{
		Family: customFontFamily,
		Style:  FONT_STYLE_REGULAR,
	}
	if regularTTF != nil {
		err := AddFontStyle(regularFont, FONT_STYLE_REGULAR, regularTTF)
		if err != nil {
			return nil, err
		}
	} else {
		fallbackFont := Font{
			Family: SourceSansPro,
			Style:  FONT_STYLE_REGULAR,
		}
		FontFaces.Set(regularFont, FontFaces.Get(fallbackFont))
		FontEncodings.Set(regularFont, FontEncodings.Get(fallbackFont))
	}

	italicFont := Font{
		Family: customFontFamily,
		Style:  FONT_STYLE_ITALIC,
	}
	if italicTTF != nil {
		err := AddFontStyle(italicFont, FONT_STYLE_ITALIC, italicTTF)
		if err != nil {
			return nil, err
		}
	} else {
		fallbackFont := Font{
			Family: SourceSansPro,
			Style:  FONT_STYLE_ITALIC,
		}
		FontFaces.Set(italicFont, FontFaces.Get(fallbackFont))
		FontEncodings.Set(italicFont, FontEncodings.Get(fallbackFont))
	}

	boldFont := Font{
		Family: customFontFamily,
		Style:  FONT_STYLE_BOLD,
	}
	if boldTTF != nil {
		err := AddFontStyle(boldFont, FONT_STYLE_BOLD, boldTTF)
		if err != nil {
			return nil, err
		}
	} else {
		fallbackFont := Font{
			Family: SourceSansPro,
			Style:  FONT_STYLE_BOLD,
		}
		FontFaces.Set(boldFont, FontFaces.Get(fallbackFont))
		FontEncodings.Set(boldFont, FontEncodings.Get(fallbackFont))
	}

	semiboldFont := Font{
		Family: customFontFamily,
		Style:  FONT_STYLE_SEMIBOLD,
	}
	if semiboldTTF != nil {
		err := AddFontStyle(semiboldFont, FONT_STYLE_SEMIBOLD, semiboldTTF)
		if err != nil {
			return nil, err
		}
	} else {
		fallbackFont := Font{
			Family: SourceSansPro,
			Style:  FONT_STYLE_SEMIBOLD,
		}
		FontFaces.Set(semiboldFont, FontFaces.Get(fallbackFont))
		FontEncodings.Set(semiboldFont, FontEncodings.Get(fallbackFont))
	}

	FontFamilies = append(FontFamilies, customFontFamily)

	return &customFontFamily, nil
}
