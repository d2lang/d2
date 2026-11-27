package d2format

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	markdown "github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2parser"
)

var markdownFenceSourcesKey = parser.NewContextKey()
var markdownParser = parser.New(parser.WithBlockParsers(util.Prioritized[parser.BlockParser](
	markdownFenceParser{parser.NewFencedCodeBlockParser()}, 699,
)))

// Goldmark exposes code content positions, with list and quote markers already
// removed. Record the container boundary separately so the formatter can restore
// those markers when it rewrites the code.
type markdownFenceParser struct{ parser.BlockParser }

type markdownFenceSource struct {
	indent     int
	containers []text.Segment
}

func (p markdownFenceParser) Open(parent markdown.Node, reader text.Reader, ctx parser.Context) (markdown.Node, parser.State) {
	indent := ctx.BlockOffset()
	node, state := p.BlockParser.Open(parent, reader, ctx)
	if block, ok := node.(*markdown.CodeBlock); ok {
		if language, _ := block.Language(reader.Source()); language == "d2" {
			sources := ctx.Get(markdownFenceSourcesKey).(map[*markdown.CodeBlock]*markdownFenceSource)
			sources[block] = &markdownFenceSource{indent: indent}
		}
	}
	return node, state
}

func (p markdownFenceParser) Continue(node markdown.Node, reader text.Reader, ctx parser.Context) parser.State {
	_, container := reader.Position()
	state := p.BlockParser.Continue(node, reader, ctx)
	sources := ctx.Get(markdownFenceSourcesKey).(map[*markdown.CodeBlock]*markdownFenceSource)
	if fence := sources[node.(*markdown.CodeBlock)]; fence != nil && state&parser.Continue != 0 {
		fence.containers = append(fence.containers, container)
	}
	return state
}

// FormatMarkdown formats the D2 fenced code blocks in source. Other Markdown
// content, including both fence markers, is preserved. Errors are positioned in
// the Markdown document, and no output is returned if a block cannot be formatted.
func FormatMarkdown(ctx context.Context, path string, source []byte) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	doc := newMarkdownSource(source)
	var output bytes.Buffer
	var updated []markdownFenceEdit
	copied := 0
	err := walkD2Fences(ctx, doc, func(block *markdown.CodeBlock, fence *markdownFenceSource) error {
		segments := block.Value.Segments()
		if len(segments) == 0 {
			return nil
		}
		openingLine := doc.lineAt(block.Pos())
		body := doc.fenceBody(openingLine, segments)
		ast, err := d2parser.ParseContext(ctx, path, strings.NewReader(body), nil)
		if err != nil {
			return doc.fenceError(openingLine, segments, err)
		}
		formatted := Format(ast)
		lastLine := openingLine + len(segments)
		if doc.lineEnding(lastLine) == "" {
			formatted = strings.TrimSuffix(formatted, "\n")
		}
		if formatted == body {
			return nil
		}

		start, end := doc.starts[openingLine+1], doc.lineEnd(lastLine)
		updated = append(updated, markdownFenceEdit{
			position: output.Len() + block.Pos() - copied,
			original: doc.position(openingLine, block.Pos()),
			body:     formatted,
		})
		output.Write(source[copied:start])
		doc.writeFenceBody(&output, fence, openingLine, segments, formatted)
		copied = end
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(updated) == 0 {
		return source, nil
	}
	output.Write(source[copied:])
	if err := checkFormattedFences(ctx, path, output.Bytes(), updated); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func walkD2Fences(ctx context.Context, doc markdownSource, visit func(*markdown.CodeBlock, *markdownFenceSource) error) error {
	pc := parser.NewContext()
	sources := make(map[*markdown.CodeBlock]*markdownFenceSource)
	pc.Set(markdownFenceSourcesKey, sources)
	root := markdownParser.Parse(doc.input, parser.WithContext(pc))
	return markdown.Walk(root, func(node markdown.Node, entering bool) (markdown.WalkStatus, error) {
		if err := ctx.Err(); err != nil {
			return markdown.WalkStop, err
		}
		if block, ok := node.(*markdown.CodeBlock); entering && ok {
			if fence := sources[block]; fence != nil {
				return markdown.WalkContinue, visit(block, fence)
			}
		}
		return markdown.WalkContinue, nil
	})
}

type markdownSource struct {
	raw    []byte
	input  []byte
	starts []int
}

func newMarkdownSource(source []byte) markdownSource {
	doc := markdownSource{raw: source, input: source, starts: []int{0}}
	copied := false
	for i, b := range source {
		switch {
		case b == '\n':
			doc.starts = append(doc.starts, i+1)
		case b == '\r' && (i+1 == len(source) || source[i+1] != '\n'):
			// CommonMark treats standalone CR as a line ending. Keep the source
			// offsets unchanged when providing a corresponding LF to Goldmark.
			if !copied {
				doc.input = bytes.Clone(source)
				copied = true
			}
			doc.input[i] = '\n'
			doc.starts = append(doc.starts, i+1)
		}
	}
	return doc
}

func (doc markdownSource) lineAt(offset int) int {
	return sort.SearchInts(doc.starts, offset+1) - 1
}

func (doc markdownSource) lineEnd(line int) int {
	if line+1 < len(doc.starts) {
		return doc.starts[line+1]
	}
	return len(doc.raw)
}

func (doc markdownSource) lineEnding(line int) string {
	if line+1 >= len(doc.starts) {
		return ""
	}
	end := doc.starts[line+1]
	if end >= 2 && doc.raw[end-2] == '\r' && doc.raw[end-1] == '\n' {
		return "\r\n"
	}
	return string(doc.raw[end-1])
}

func (doc markdownSource) fenceBody(openingLine int, segments []text.Segment) string {
	var body strings.Builder
	for i, segment := range segments {
		line := openingLine + 1 + i
		ending := doc.lineEnding(line)
		end := segment.Stop - len(ending)
		body.WriteString(strings.Repeat(" ", segment.Padding))
		if segment.Start < end {
			body.Write(doc.raw[segment.Start:end])
		}
		if ending != "" {
			body.WriteByte('\n')
		}
	}
	return body.String()
}

func (doc markdownSource) writeFenceBody(output *bytes.Buffer, fence *markdownFenceSource, openingLine int, segments []text.Segment, body string) {
	if body == "" {
		return
	}
	prefixes := make([][]byte, len(segments))
	var linePrefix []byte
	for i, segment := range segments {
		line := openingLine + 1 + i
		if len(bytes.TrimSpace(doc.raw[segment.Start:segment.Stop])) == 0 {
			// Blank lines need not have enough indentation to carry code.
			continue
		}
		prefixes[i] = doc.fenceLinePrefix(line, segment, fence.containers[i], fence.indent)
		if linePrefix == nil {
			linePrefix = prefixes[i]
		}
	}
	ending := doc.lineEnding(openingLine)
	for i, line := range strings.SplitAfter(body, "\n") {
		if line == "" {
			break
		}
		lineEnding := ending
		if i < len(segments) {
			originalLine := openingLine + 1 + i
			segment := segments[i]
			if line == "\n" && segment.Padding == 0 && segment.Len() == len(doc.lineEnding(originalLine)) {
				output.Write(doc.raw[doc.starts[originalLine]:doc.lineEnd(originalLine)])
				continue
			}
			if prefixes[i] != nil {
				linePrefix = prefixes[i]
			}
			if originalEnding := doc.lineEnding(originalLine); originalEnding != "" {
				lineEnding = originalEnding
			}
		}
		output.Write(linePrefix)
		if content, newline := strings.CutSuffix(line, "\n"); newline {
			output.WriteString(content)
			output.WriteString(lineEnding)
		} else {
			output.WriteString(content)
		}
	}
}

func (doc markdownSource) fenceLinePrefix(line int, body, container text.Segment, indent int) []byte {
	start := doc.starts[line]
	consumed := body.Start - container.Start + container.Padding
	if body.Padding == 0 && consumed >= indent {
		return doc.raw[start:body.Start]
	}
	prefix := bytes.Clone(doc.raw[start:container.Start])
	if container.Padding > 0 && len(prefix) > 0 && prefix[len(prefix)-1] == '\t' {
		// The final tab was only partly consumed by the container. Keep
		// its consumed columns separate from the fence's own indentation.
		prefix = prefix[:len(prefix)-1]
		columns := 0
		for _, b := range prefix {
			if b == '\t' {
				columns += util.TabWidth(columns)
			} else {
				columns++
			}
		}
		prefix = append(prefix, bytes.Repeat([]byte{' '}, max(0, util.TabWidth(columns)-container.Padding))...)
	}
	return append(prefix, bytes.Repeat([]byte{' '}, indent)...)
}

type markdownFenceEdit struct {
	position int
	original d2ast.Position
	body     string
}

func checkFormattedFences(ctx context.Context, path string, output []byte, updated []markdownFenceEdit) error {
	// A D2 block string can contain an indented run of the fence character.
	// If formatting moves that run to closing-fence indentation, the Markdown
	// structure would change. Check what the updated Markdown actually parses.
	doc := newMarkdownSource(output)
	next := 0
	err := walkD2Fences(ctx, doc, func(block *markdown.CodeBlock, _ *markdownFenceSource) error {
		if next < len(updated) && block.Pos() == updated[next].position && doc.fenceBody(doc.lineAt(block.Pos()), block.Value.Segments()) == updated[next].body {
			next++
		}
		return nil
	})
	if err != nil {
		return err
	}
	if next < len(updated) {
		at := d2ast.Range{Path: path, Start: updated[next].original}
		return fmt.Errorf("%s: formatting would change the Markdown fence; try a longer enclosing fence", at)
	}
	return nil
}

func (doc markdownSource) position(line, offset int) d2ast.Position {
	return d2ast.Position{Line: line, Column: offset - doc.starts[line], Byte: offset}
}

func (doc markdownSource) fenceError(openingLine int, segments []text.Segment, err error) error {
	var parsed *d2parser.ParseError
	if !errors.As(err, &parsed) {
		return err
	}
	mapPosition := func(pos d2ast.Position) d2ast.Position {
		line := openingLine + 1 + pos.Line
		if pos.Line >= len(segments) {
			end := segments[len(segments)-1].Stop
			return doc.position(doc.lineAt(end), end)
		}
		segment := segments[pos.Line]
		column := max(0, pos.Column-segment.Padding)
		return doc.position(line, min(segment.Start+column, doc.lineEnd(line)))
	}
	remapped := &d2parser.ParseError{Errors: make([]d2ast.Error, 0, len(parsed.Errors))}
	for _, problem := range parsed.Errors {
		message := strings.TrimPrefix(problem.Message, problem.Range.String()+": ")
		problem.Range.Start = mapPosition(problem.Range.Start)
		problem.Range.End = mapPosition(problem.Range.End)
		problem.Message = problem.Range.String() + ": " + message
		remapped.Errors = append(remapped.Errors, problem)
	}
	return remapped
}
