package textmeasure

import (
	"cmp"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/lib/xsort"
)

// MarkdownStrikeChanged reports whether only the struck ranges of a Markdown
// label changed. Compare the rendered markup so code spans, escaped delimiters,
// and link destinations retain their normal Markdown semantics.
func MarkdownStrikeChanged(from, to string) bool {
	if from == to || !mayContainStrike(from) && !mayContainStrike(to) {
		return false
	}
	a, sa, ok := markdownWithoutStrike(from)
	if !ok {
		return false
	}
	b, sb, ok := markdownWithoutStrike(to)
	return ok && a == b && !slices.Equal(sa, sb)
}

func mayContainStrike(source string) bool {
	return strings.ContainsAny(source, "~<")
}

// PlainMarkdownStrikes identifies struck literal byte ranges in a matching
// single paragraph. An empty slice means a match without strikes; nil means
// different wording or formatting.
func PlainMarkdownStrikes(plain, markdown string) [][2]int {
	if plain == "" || strings.ContainsAny(plain, "\r\n") {
		return nil
	}
	rendered, err := RenderMarkdown(markdown)
	if err != nil {
		return nil
	}
	body, ok := strings.CutPrefix(strings.TrimSpace(rendered), "<p>")
	body, closed := strings.CutSuffix(body, "</p>")
	if !ok || !closed {
		return nil
	}
	tokens := html.NewTokenizer(strings.NewReader(body))
	spans := make([][2]int, 0)
	var offset, start, depth int
	for {
		switch kind := tokens.Next(); kind {
		case html.ErrorToken:
			if tokens.Err() == io.EOF && offset == len(plain) && depth == 0 {
				return spans
			}
			return nil
		case html.TextToken:
			text := string(tokens.Text())
			if !strings.HasPrefix(plain[offset:], text) {
				return nil
			}
			offset += len(text)
		case html.StartTagToken, html.EndTagToken:
			tag, attrs := tokens.TagName()
			if attrs || (string(tag) != "del" && string(tag) != "s" && string(tag) != "strike") {
				return nil
			}
			if kind == html.StartTagToken {
				if depth == 0 {
					start = offset
				}
				depth++
			} else {
				depth--
				if depth < 0 {
					return nil
				}
				if depth == 0 && offset > start {
					spans = append(spans, [2]int{start, offset})
				}
			}
		default:
			return nil
		}
	}
}

// StrikeAdvances measures a plain run once, retaining kerning across span
// boundaries. The positions can be reused at any sampled label width.
func (r *Ruler) StrikeAdvances(text string, spans [][2]int, font d2fonts.Font) [][2]float64 {
	face := font
	face.Size = SIZELESS_FONT_SIZE
	if r == nil || font.Size <= 0 || r.ttfs[face] == nil {
		return nil
	}
	r.MeasurePrecise(font, "")
	offset := 0
	advanceTo := func(end int) float64 {
		r.buf = append(r.buf, text[offset:end]...)
		r.drawBuf(font)
		offset = end
		return r.Dot.X - r.Orig.X
	}
	out := make([][2]float64, 0, len(spans))
	for _, span := range spans {
		if span[0] < offset || span[1] <= span[0] || span[1] > len(text) ||
			!utf8.RuneStart(text[span[0]]) || (span[1] < len(text) && !utf8.RuneStart(text[span[1]])) {
			return nil
		}
		left, right := advanceTo(span[0]), advanceTo(span[1])
		if right > left {
			out = append(out, [2]float64{left, right})
		}
	}
	width := advanceTo(len(text))
	if width <= 0 {
		return nil
	}
	n := 0
	var end float64
	for _, span := range out {
		left, right := max(end, span[0]/width), min(1., span[1]/width)
		if right > left {
			out[n] = [2]float64{left, right}
			end = right
			n++
		}
	}
	return out[:n]
}

func markdownWithoutStrike(source string) (string, [][2]int, bool) {
	rendered, err := RenderMarkdown(source)
	if err != nil {
		return "", nil, false
	}
	tokens := html.NewTokenizer(strings.NewReader(rendered))
	var plain strings.Builder
	var spans [][2]int
	depth := 0
	for {
		kind := tokens.Next()
		switch kind {
		case html.ErrorToken:
			return plain.String(), spans, tokens.Err() == io.EOF
		case html.StartTagToken, html.EndTagToken:
			tag, _ := tokens.TagName()
			switch string(tag) {
			case "del", "s", "strike":
				if kind == html.StartTagToken {
					depth++
				} else if depth > 0 {
					depth--
				}
				continue
			}
		case html.TextToken:
			if depth > 0 && len(tokens.Raw()) > 0 {
				start, end := plain.Len(), plain.Len()+len(tokens.Raw())
				if n := len(spans); n > 0 && spans[n-1][1] == start {
					spans[n-1][1] = end
				} else {
					spans = append(spans, [2]int{start, end})
				}
			}
		}
		plain.Write(tokens.Raw())
	}
}

// LayoutMarkdownStrikeFrame lays out the decorated endpoint. A transition between
// two different sets of strikes also needs the old positions to retain shared ink.
func LayoutMarkdownStrikeFrame(from, to string, progress float64, ruler *Ruler, family, mono *d2fonts.FontFamily, size int) (*MarkdownLayout, error) {
	layout := func(source string) (*MarkdownLayout, error) {
		return LayoutMarkdown(source, ruler, family, mono, size)
	}
	if progress <= 0 {
		return layout(from)
	}
	if progress >= 1 {
		return layout(to)
	}
	var previous *MarkdownLayout
	if mayContainStrike(from) {
		var err error
		previous, err = layout(from)
		if err != nil {
			return nil, err
		}
		if !mayContainStrike(to) {
			return tweenMarkdownStrike(previous, nil, progress), nil
		}
	}
	current, err := layout(to)
	if err != nil {
		return nil, err
	}
	return tweenMarkdownStrike(previous, current, progress), nil
}

// tweenMarkdownStrike uses the same measured ink rectangles as the static
// Markdown painter. Intersecting portions stay inked when a strike is extended,
// shortened, moved to another word, or split into separate spans.
func tweenMarkdownStrike(from, to *MarkdownLayout, progress float64) *MarkdownLayout {
	type boundary struct {
		y, x     float64
		paint    MarkdownColorRole
		from, to int
	}
	var edges []boundary
	for i, layout := range []*MarkdownLayout{from, to} {
		if layout == nil {
			continue
		}
		for _, p := range layout.Primitives {
			if p.Kind != MarkdownRectPrimitive || p.Decoration != MarkdownTextDecorationLineThrough || p.Width <= 0 {
				continue
			}
			left := boundary{y: p.Y, x: p.X, paint: p.FillRole}
			right := boundary{y: p.Y, x: p.X + p.Width, paint: p.FillRole}
			if i == 0 {
				left.from, right.from = 1, -1
			} else {
				left.to, right.to = 1, -1
			}
			edges = append(edges, left, right)
		}
	}
	xsort.Func(edges, func(a, b boundary) int {
		return cmp.Or(cmp.Compare(a.y, b.y), cmp.Compare(a.paint, b.paint), cmp.Compare(a.x, b.x))
	})
	base := to
	if base == nil {
		base = from
	}
	frame := *base
	frame.Primitives = make([]MarkdownPrimitive, 0, len(base.Primitives))
	for _, p := range base.Primitives {
		if p.Kind != MarkdownRectPrimitive || p.Decoration != MarkdownTextDecorationLineThrough {
			frame.Primitives = append(frame.Primitives, p)
		}
	}
	old, next := 0, 0
	for i, e := range edges {
		old, next = old+e.from, next+e.to
		if i+1 == len(edges) || old == 0 && next == 0 {
			continue
		}
		width := edges[i+1].x - e.x
		switch {
		case old == 0:
			width *= progress
		case next == 0:
			width *= 1 - progress
		}
		if width > 0 {
			frame.Primitives = append(frame.Primitives, MarkdownPrimitive{
				Kind: MarkdownRectPrimitive, X: e.x, Y: e.y, Width: width, Height: 1,
				FillRole: e.paint, Decoration: MarkdownTextDecorationLineThrough,
			})
		}
	}
	return &frame
}
