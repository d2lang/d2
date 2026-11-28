package d2svg

import (
	"bufio"
	"bytes"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/d2lang/d2/d2target"
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
	// Only standalone default exports use compact marker IDs. Frames and
	// legends keep the IDs used by animation compaction and custom CSS.
	compactMarkerIDs bool
	buf              *bytes.Buffer
	usage            *svgUsage

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
		if isThemeClass(class) {
			w.usage.classes[class] = true
		}
	}
}

func (w *objectWriter) openGroup(classes []string, opacityStyle string, attrs ...svg.Attribute) {
	value := strings.Join(classes, " ")
	w.classes(value)
	tag := svg.OpenElement("g", append([]svg.Attribute{svg.Attr("class", value)}, attrs...)...)
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

func (w *objectWriter) plainStrike(el *d2themes.ThemableElement, label d2target.Text, theme *d2themes.Theme, linked bool) {
	label.MarkdownStrike.PlainRules(el.X-float64(label.LabelWidth)/2, el.Y, float64(label.LabelWidth), float64(label.FontSize), func(x, y, width, height float64) {
		rule := d2themes.NewThemableElement("rect", theme)
		rule.X, rule.Y, rule.Width, rule.Height = x, y, width, height
		rule.Fill = el.Fill
		if linked {
			rule.ClassName = "text-link"
		}
		w.element(rule)
	})
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

// Let the browser draw the same underline (including descender gaps) as the
// static label. Each centered line gets its own font-relative clip.
func (w *objectWriter) animatedUnderline(el *d2themes.ThemableElement, text string, height float64, progress *float64) {
	if progress == nil {
		w.label(el, text, height)
		return
	}
	el.ClassName = strings.ReplaceAll(el.ClassName, " text-underline", "")
	if *progress <= 0 || *progress >= 1 {
		if *progress >= 1 {
			el.ClassName += " text-underline"
		}
		w.label(el, text, height)
		return
	}
	decoratedClip, plainClip := svg.UnderlineClips(*progress)
	plain := el.Copy()
	plain.Style += ";clip-path:" + plainClip
	ink := el.Copy()
	ink.ClassName += " text-underline"
	// Both clipped halves must receive pointer events for an enclosing label link.
	ink.Style += ";clip-path:" + decoratedClip + ";user-select:none"
	ink.AddAttributes(svg.Attr("aria-hidden", "true"))
	lineHeight := height / float64(1+strings.Count(text, "\n"))
	for line := range strings.SplitSeq(text, "\n") {
		w.text(plain, line)
		w.text(ink, line)
		plain.Y += lineHeight
		ink.Y += lineHeight
	}
}

// sketch writes rough.js output for a shape, passing its error through.
func (w *objectWriter) sketch(out string, err error) error {
	if err == nil {
		w.Write([]byte(out))
	}
	return err
}

// paths writes el once for each piece of SVG path data.
func (w *objectWriter) paths(el *d2themes.ThemableElement, data []string) {
	for _, d := range data {
		el.D = d
		w.element(el)
	}
}
