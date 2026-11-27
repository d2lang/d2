// Package d2fonts holds the fonts used to measure and render diagrams.
package d2fonts

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/d2lang/d2/internal/bundledfont"
	"github.com/d2lang/d2/lib/font"
	"github.com/d2lang/d2/lib/syncmap"
	"github.com/d2lang/d2/lib/xsort"
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

const (
	woffDataPrefix  = "data:application/font-woff;base64,"
	woff2DataPrefix = "data:font/woff2;base64,"
)

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
	return f.encodedSubset(corpus, false)
}

// GetSVGEncodedSubset returns a data URL for a font embedded by the SVG renderer.
func (f Font) GetSVGEncodedSubset(corpus string) string {
	return f.encodedSubset(corpus, true)
}

func (f Font) encodedSubset(corpus string, svg bool) string {
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
	xsort.Ordered(chars)
	uniqueChars := string(chars)

	FontFamiliesMu.Lock()
	defer FontFamiliesMu.Unlock()
	face := FontFaces.Get(f)
	// Only bundled fonts are remembered: a caller's font may be registered
	// afresh for every diagram and must not be kept alive by this cache.
	cacheable := bundledfont.Is(face)
	var key encodedSubsetKey
	if cacheable {
		key = encodedSubsetKey{face: &face[0], chars: uniqueChars, svg: svg}
		if encoded, ok := encodedSubsets[key]; ok {
			return encoded
		}
	}
	var fontBuf []byte
	var err error
	prefix := woffDataPrefix
	if svg && cacheable {
		fontBuf, err = font.SubsetWOFF2(face, uniqueChars)
		if err == nil {
			prefix = woff2DataPrefix
		}
	}
	if fontBuf == nil {
		fontBuf, err = font.SubsetWOFF(face, uniqueChars)
	}
	if err != nil {
		// If subset fails, return full encoding
		return f.GetEncodedFull()
	}

	encoded := prefix + base64.StdEncoding.EncodeToString(fontBuf)
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
	svg   bool
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

func encodeFontFace(ttf []byte) (string, error) {
	// The legacy WOFF encoder adjusts the input checksum. Keep the original
	// face available for measuring, comparing and serializing compiled diagrams.
	woff, err := font.Sfnt2Woff(bytes.Clone(ttf))
	if err != nil {
		return "", fmt.Errorf("failed to encode ttf to woff: %v", err)
	}
	return woffDataPrefix + base64.StdEncoding.EncodeToString(woff), nil
}

func AddFontStyle(f Font, style FontStyle, ttf []byte) error {
	encoded, err := encodeFontFace(ttf)
	if err != nil {
		return err
	}
	FontFaces.Set(f, ttf)
	FontEncodings.Set(f, encoded)
	return nil
}

func AddFontFamily(name string, regularTTF, italicTTF, boldTTF, semiboldTTF []byte) (*FontFamily, error) {
	FontFamiliesMu.Lock()
	defer FontFamiliesMu.Unlock()
	family := FontFamily(name)
	faces := [...]struct {
		style    FontStyle
		ttf      []byte
		encoding string
	}{
		{style: FONT_STYLE_REGULAR, ttf: regularTTF},
		{style: FONT_STYLE_ITALIC, ttf: italicTTF},
		{style: FONT_STYLE_BOLD, ttf: boldTTF},
		{style: FONT_STYLE_SEMIBOLD, ttf: semiboldTTF},
	}
	// Prepare every face before replacing an existing family. A failure in a
	// later style must leave earlier diagrams' measurement and encoding intact.
	for i := range faces {
		face := &faces[i]
		if face.ttf == nil {
			fallback := Font{Family: SourceSansPro, Style: face.style}
			face.ttf = FontFaces.Get(fallback)
			face.encoding = FontEncodings.Get(fallback)
		} else {
			encoded, err := encodeFontFace(face.ttf)
			if err != nil {
				return nil, err
			}
			face.encoding = encoded
		}
	}
	for _, face := range faces {
		f := Font{Family: family, Style: face.style}
		FontFaces.Set(f, face.ttf)
		FontEncodings.Set(f, face.encoding)
	}
	if !slices.Contains(FontFamilies, family) {
		FontFamilies = append(FontFamilies, family)
	}
	return &family, nil
}
