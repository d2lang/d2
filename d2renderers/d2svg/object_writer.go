package d2svg

import (
	"bufio"
	"bytes"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/d2lang/d2/d2themes"
	"github.com/d2lang/d2/lib/svg"
)

// Keep glyphs in drawing order. Changing the subset order can change embedded font bytes.
type svgUsage struct {
	fonts   [len(embeddedFontClasses)]strings.Builder
	classes map[string]bool
	// Reuse the decoder buffer when typed objects separate raw fragments.
	xmlBuffer bufio.Reader
}

type glyphRun struct {
	face int
	text string
}

type objectWriter struct {
	buf   *bytes.Buffer
	usage *svgUsage

	// An object may mix known elements with arbitrary nested SVG. In that
	// case discard its pending runs and read the complete object, so that
	// inherited fonts and nested spans are resolved together.
	start int
	// Consecutive opaque objects share one XML reader.
	rawStart, rawEnd int
	raw              bool
	runs             []glyphRun
}

func (w *objectWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.raw = true
	}
	return w.buf.Write(p)
}

func (w *objectWriter) classes(value string) {
	if w.usage == nil || w.usage.classes == nil || w.raw {
		return
	}
	for i := 0; i < len(value); i++ {
		if value[i] >= 128 || value[i] < ' ' {
			w.raw = true // Let the XML reader apply its class-tokenization guards.
			return
		}
	}
	for class := range strings.FieldsSeq(value) {
		if isPaletteClass(class) {
			w.usage.classes[class] = true
		}
	}
}

func (w *objectWriter) openGroup(classes []string, opacityStyle string) {
	value := strings.Join(classes, " ")
	w.classes(value)
	tag := svg.OpenElement("g", svg.Attr("class", value))
	if opacityStyle != "" {
		tag = strings.TrimSuffix(tag, ">") + opacityStyle + ">"
	}
	w.buf.WriteString(tag)
}

// element writes geometry or an explicitly styled label created here. Raw
// nested markup goes through Write so it keeps the full XML semantics.
func (w *objectWriter) element(el *d2themes.ThemableElement) {
	if w.usage != nil && !w.raw {
		w.buf.WriteString(el.RenderWithClasses(w.classes))
	} else {
		w.buf.WriteString(el.Render())
	}
}

// The text elements written here start with their explicit font class.
// Leave unusual XML text to the reader, which observes any replacements
// made by XML escaping before choosing the font subset.
func (w *objectWriter) glyphs(classes, text string) {
	if w.usage == nil || w.raw || text == "" {
		return
	}
	class, rest, _ := strings.Cut(classes, " ")
	face := slices.Index(embeddedFontClasses[:], class)
	if face < 0 || strings.IndexFunc(text, func(r rune) bool { return r == utf8.RuneError || r != '\n' && !unicode.IsPrint(r) }) >= 0 {
		w.raw = true
		return
	}
	// Multiple font classes on one element compete in stylesheet order.
	for extra := range strings.FieldsSeq(rest) {
		face = max(face, slices.Index(embeddedFontClasses[:], extra))
	}
	w.runs = append(w.runs, glyphRun{face, text})
}

func (w *objectWriter) text(el *d2themes.ThemableElement, text string) {
	w.glyphs(el.ClassName, text)
	el.SetText(text)
	w.element(el)
}

func (w *objectWriter) label(el *d2themes.ThemableElement, text string, height float64) {
	var visit func(string)
	if w.usage != nil && !w.raw {
		visit = func(line string) { w.glyphs(el.ClassName, line) }
	}
	el.SetInnerSVG(svg.TrustedFragment(renderText(text, el.X, height, visit)))
	w.element(el)
}

// finish marks a balanced fragment at the root of the SVG body. Keep raw
// objects together and flush them before recording the following object's text.
func (w *objectWriter) finish() {
	if w.usage == nil {
		return
	}
	end := w.buf.Len()
	if w.raw {
		if w.rawStart == w.rawEnd {
			w.rawStart = w.start
		}
		w.rawEnd = end
	} else if end != w.start {
		w.flushRaw()
		if w.usage != nil {
			for _, run := range w.runs {
				w.usage.fonts[run.face].WriteString(run.text)
			}
		}
	}
	w.start = end
	w.raw = false
	w.runs = w.runs[:0]
}

func (w *objectWriter) flushRaw() {
	if w.rawStart == w.rawEnd || w.usage == nil {
		return
	}
	source := bytes.NewReader(w.buf.Bytes()[w.rawStart:w.rawEnd])
	w.rawStart = w.rawEnd
	fonts, classes, ok := scanFontCorporaAndClasses(source, &w.usage.xmlBuffer)
	if !ok {
		// Unknown CSS or dynamic content also makes the whole-board font
		// scan fail. Keep legacy embedding without parsing those bytes twice.
		w.usage = nil
		return
	}
	for i, class := range embeddedFontClasses {
		w.usage.fonts[i].WriteString(fonts[class])
	}
	if classes == nil {
		// Palette tokenization can be ambiguous even when fonts are known.
		w.usage.classes = nil
	} else if w.usage.classes != nil {
		for class := range classes {
			w.usage.classes[class] = true
		}
	}
}

func (w *objectWriter) metadata(source string) (fontCorpora, map[string]bool) {
	w.flushRaw()
	if w.usage == nil {
		return nil, nil
	}
	fonts := make(fontCorpora)
	for i := range w.usage.fonts {
		if w.usage.fonts[i].Len() > 0 {
			fonts[embeddedFontClasses[i]] = w.usage.fonts[i].String()
		}
	}
	if !regularFaceTriggered(fonts, source) {
		return nil, nil
	}
	return fonts, w.usage.classes
}
