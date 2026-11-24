// Package d2fonts holds the fonts used to measure and render diagrams.
package d2fonts

import (
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/d2lang/d2/internal/bundledfont"
	"github.com/d2lang/d2/lib/font"
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

const woffDataPrefix = "data:application/font-woff;base64,"

// Some bundled data URLs historically contain a TTF with a WOFF MIME type.
// Index by the original URL so callers overriding FontEncodings retain exactly
// their registered value. Only bundled data is cached.
var fullWoffEncodings = map[string]func() string{}

func registerFullWoffEncoding(uri string) {
	if !strings.HasPrefix(uri, woffDataPrefix+"AAEA") {
		return
	}
	fullWoffEncodings[uri] = sync.OnceValue(func() string {
		ttf, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, woffDataPrefix))
		if err != nil {
			return uri
		}
		woff, err := font.FullWOFF(ttf)
		if err != nil {
			return uri
		}
		return woffDataPrefix + base64.StdEncoding.EncodeToString(woff)
	})
}

// GetEncodedFull returns the complete embedded face, including its shaping
// tables, in a compressed WOFF when one is available.
func (f Font) GetEncodedFull() string {
	uri := FontEncodings.Get(f)
	if compressed := fullWoffEncodings[uri]; compressed != nil {
		return compressed()
	}
	return uri
}

func (f Font) GetEncodedSubset(corpus string) string {
	seen := make(map[rune]bool)
	var chars []rune
	for _, char := range corpus {
		if !seen[char] {
			seen[char] = true
			chars = append(chars, char)
		}
	}
	// The subsetter orders glyphs by ID. Share the result across boards
	// that rearrange labels while using the same characters.
	slices.Sort(chars)
	uniqueChars := string(chars)

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
	fontBuf, err := font.SubsetWOFF(face, uniqueChars)
	if err != nil {
		// If subset fails, return full encoding
		return f.GetEncodedFull()
	}

	encoded := fmt.Sprintf("data:application/font-woff;base64,%v", base64.StdEncoding.EncodeToString(fontBuf))
	if cacheable {
		// The key is retained too, and characters the font lacks lengthen it
		// without adding anything to the subset.
		size := len(encoded) + len(uniqueChars)
		if size > maxEncodedSubsetsSize {
			return encoded
		}
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
// and its sorted set of characters.
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

func AddFontStyle(f Font, style FontStyle, ttf []byte) error {
	FontFaces.Set(f, ttf)

	woff, err := font.Sfnt2Woff(ttf)
	if err != nil {
		return fmt.Errorf("failed to encode ttf to woff: %v", err)
	}
	encodedWoff := fmt.Sprintf("data:application/font-woff;base64,%v", base64.StdEncoding.EncodeToString(woff))
	FontEncodings.Set(f, encodedWoff)

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
