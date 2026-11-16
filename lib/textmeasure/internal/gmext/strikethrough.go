// Code in this file is copied from github.com/yuin/goldmark v1.8.5
// (extension package), Copyright (c) 2019 Yusuke Inuzuka, MIT License.
// See THIRD_PARTY_NOTICES.txt.
//
// D2 vendors only the extensions it uses instead of importing
// github.com/yuin/goldmark/extension. That package compiles its linkify URL
// regexps at init, and their {1,256} repetition recurses deeply enough in
// regexp/syntax to exhaust the smaller stack of a Safari 27 Web Worker, which
// kills the D2 WASM module before it starts (d2lang/d2#2956).

package gmext

import (
	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type strikethroughDelimiterProcessor struct {
}

func (p *strikethroughDelimiterProcessor) IsDelimiter(b byte) bool {
	return b == '~'
}

func (p *strikethroughDelimiterProcessor) CanOpenCloser(opener, closer *parser.Delimiter) bool {
	return opener.Char == closer.Char
}

func (p *strikethroughDelimiterProcessor) OnMatch(consumes int) gast.Node {
	return ast.NewStrikethrough()
}

var defaultStrikethroughDelimiterProcessor = &strikethroughDelimiterProcessor{}

type strikethroughParser struct {
}

var defaultStrikethroughParser = &strikethroughParser{}

// NewStrikethroughParser return a new InlineParser that parses
// strikethrough expressions.
func NewStrikethroughParser() parser.InlineParser {
	return defaultStrikethroughParser
}

func (s *strikethroughParser) Trigger() []byte {
	return []byte{'~'}
}

func (s *strikethroughParser) Parse(parent gast.Node, block text.Reader, pc parser.Context) gast.Node {
	before := block.PrecendingCharacter()
	line, segment := block.PeekLine()
	node := parser.ScanDelimiter(line, before, 1, defaultStrikethroughDelimiterProcessor)
	if node == nil || node.OriginalLength > 2 || before == '~' {
		return nil
	}

	node.Segment = segment.WithStop(segment.Start + node.OriginalLength)
	block.Advance(node.OriginalLength)
	pc.PushDelimiter(node)
	return node
}

func (s *strikethroughParser) CloseBlock(parent gast.Node, pc parser.Context) {
	// nothing to do
}

// StrikethroughHTMLRenderer is a renderer.NodeRenderer implementation that
// renders Strikethrough nodes.
type StrikethroughHTMLRenderer struct {
	html.Config
}

// NewStrikethroughHTMLRenderer returns a new StrikethroughHTMLRenderer.
func NewStrikethroughHTMLRenderer(opts ...html.Option) renderer.NodeRenderer {
	r := &StrikethroughHTMLRenderer{
		Config: html.NewConfig(),
	}
	for _, opt := range opts {
		opt.SetHTMLOption(&r.Config)
	}
	return r
}

// RegisterFuncs implements renderer.NodeRenderer.RegisterFuncs.
func (r *StrikethroughHTMLRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindStrikethrough, r.renderStrikethrough)
}

// StrikethroughAttributeFilter defines attribute names which dd elements can have.
var StrikethroughAttributeFilter = html.GlobalAttributeFilter

func (r *StrikethroughHTMLRenderer) renderStrikethrough(
	w util.BufWriter, source []byte, n gast.Node, entering bool) (gast.WalkStatus, error) {
	if entering {
		if n.Attributes() != nil {
			_, _ = w.WriteString("<del")
			html.RenderAttributes(w, n, StrikethroughAttributeFilter)
			_ = w.WriteByte('>')
		} else {
			_, _ = w.WriteString("<del>")
		}
	} else {
		_, _ = w.WriteString("</del>")
	}
	return gast.WalkContinue, nil
}

type strikethrough struct {
}

// Strikethrough is an extension that allow you to use strikethrough expression like '~~text~~' .
var Strikethrough = &strikethrough{}

func (e *strikethrough) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(NewStrikethroughParser(), 500),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(NewStrikethroughHTMLRenderer(), 500),
	))
}
