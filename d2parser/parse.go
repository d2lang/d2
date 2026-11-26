package d2parser

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	tunicode "golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/internal/d2string"
	"github.com/d2lang/util-go/go2"
)

// MaxNestingDepth is the maximum number of nested maps and arrays accepted by
// the parser.
const MaxNestingDepth = 128

type ParseOptions struct {
	// UTF16Pos would be used with input received from a browser where the browser will send the text as UTF-8 but
	// JavaScript keeps strings in memory as UTF-16 and so needs UTF-16 indexes into the text to line up errors correctly.
	// So you want to read UTF-8 still but adjust the indexes to pretend the input is utf16.
	UTF16Pos bool

	ParseError *ParseError
}

// Parse parses a .d2 Map in r.
//
// The returned Map always represents a valid .d2 file. All encountered errors will be in
// []error.
//
// The map may be compiled via Compile even if there are errors to keep language tooling
// operational. Though autoformat should not run.
//
// If UTF16Pos is true, positions will be recorded in UTF-16 codeunits as required by LSP
// and browser clients. See
// https://microsoft.github.io/language-server-protocol/specifications/specification-current/#textDocuments
// TODO: update godocs
func Parse(path string, r io.Reader, opts *ParseOptions) (*d2ast.Map, error) {
	return ParseContext(context.Background(), path, r, opts)
}

// ParseContext parses a .d2 Map in r and stops promptly when ctx is canceled.
func ParseContext(ctx context.Context, path string, r io.Reader, opts *ParseOptions) (*d2ast.Map, error) {
	if opts == nil {
		opts = &ParseOptions{
			UTF16Pos: false,
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	p := &parser{
		ctx:  ctx,
		path: path,

		utf16Pos: opts.UTF16Pos,
		err:      opts.ParseError,
	}
	br := bufio.NewReader(r)
	p.reader = br

	bom, err := br.Peek(2)
	if err == nil {
		// 0xFFFE is invalid UTF-8 so this is safe.
		// Also a different BOM is used for UTF-8.
		// See https://unicode.org/faq/utf_bom.html#bom4
		if bom[0] == 0xFF && bom[1] == 0xFE {
			p.utf16Pos = true

			buf := make([]byte, br.Buffered())
			io.ReadFull(br, buf)

			mr := io.MultiReader(bytes.NewBuffer(buf), r)
			tr := transform.NewReader(mr, tunicode.UTF16(tunicode.LittleEndian, tunicode.UseBOM).NewDecoder())
			br.Reset(tr)
		}
	}

	if p.err == nil {
		p.err = &ParseError{}
	}

	m := p.parseMap(true)
	if p.contextErr != nil {
		return m, p.contextErr
	}
	if !p.err.Empty() {
		return m, p.err
	}
	return m, nil
}

// ParseKey parses a single key path.
func ParseKey(key string) (*d2ast.KeyPath, error) {
	p := &parser{
		reader: strings.NewReader(key),
		err:    &ParseError{},
	}

	k := p.parseKey(stringContext{beforeEdge: true})
	if k != nil {
		p.requireEnd("key")
	}
	if !p.err.Empty() {
		return nil, fmt.Errorf("failed to parse key %q: %w", key, p.err)
	}
	if k == nil {
		return nil, fmt.Errorf("empty key: %q", key)
	}
	return k, nil
}

// ParseMapKey parses one map entry, including its optional value.
func ParseMapKey(mapKey string) (*d2ast.Key, error) {
	p := &parser{
		reader: strings.NewReader(mapKey),
		err:    &ParseError{},
	}

	mk := p.parseMapKey()
	if mk != nil {
		p.requireEnd("map key")
	}
	if !p.err.Empty() {
		return nil, fmt.Errorf("failed to parse map key %q: %w", mapKey, p.err)
	}
	if mk == nil {
		return nil, fmt.Errorf("empty map key: %q", mapKey)
	}
	return mk, nil
}

// ParseValue parses a single scalar, map, array or import.
func ParseValue(value string) (d2ast.Value, error) {
	p := &parser{
		reader: strings.NewReader(value),
		err:    &ParseError{},
	}

	v := p.parseValue(false)
	if v.Unbox() != nil {
		p.requireEnd("value")
	}
	if !p.err.Empty() {
		return nil, fmt.Errorf("failed to parse value %q: %w", value, p.err)
	}
	if v.Unbox() == nil {
		return nil, fmt.Errorf("empty value: %q", value)
	}
	return v.Unbox(), nil
}

func (p *parser) requireEnd(kind string) {
	if !p.err.Empty() {
		return
	}
	for {
		start := p.pos
		r, eof := p.read()
		if eof {
			return
		}
		if !unicode.IsSpace(r) {
			p.errorf(start, p.pos, "unexpected %q after %s", r, kind)
			return
		}
	}
}

// TODO: refactor parser to keep entire file in memory as []rune
//   - trivial to then convert positions
//   - lookahead is gone, just forward back as much as you want :)
//   - streaming parser isn't really helpful.
//   - just read into a string even and decode runes forward/back as needed
//   - the whole file essentially exists within the parser as the AST anyway...
//
// TODO: ast struct that combines map & errors and pass that around
type parser struct {
	ctx context.Context

	path     string
	pos      d2ast.Position
	utf16Pos bool

	reader    io.RuneReader
	readerPos d2ast.Position

	readahead      []rune
	readaheadIndex int
	lookahead      []rune
	lookaheadPos   d2ast.Position

	ioerr bool
	err   *ParseError

	inEdgeGroup bool

	depth        int
	nestingDepth int
	readCount    uint
	contextErr   error
	halted       bool
}

// TODO: rename to Error and make existing Error a private type errorWithRange
type ParseError struct {
	// Errors from globs need to be deduplicated
	ErrorsLookup map[d2ast.Error]struct{} `json:"-"`
	Errors       []d2ast.Error            `json:"errs"`
}

func Errorf(n d2ast.Node, f string, v ...interface{}) error {
	f = "%v: " + f
	v = append([]interface{}{n.GetRange()}, v...)
	return d2ast.Error{
		Range:   n.GetRange(),
		Message: fmt.Sprintf(f, v...),
	}
}

func (pe *ParseError) Empty() bool {
	if pe == nil {
		return true
	}
	return len(pe.Errors) == 0
}

func (pe *ParseError) Error() string {
	var sb strings.Builder
	for i, err := range pe.Errors {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(err.Error())
	}
	return sb.String()
}

func (p *parser) errorf(start d2ast.Position, end d2ast.Position, f string, v ...interface{}) {
	if p.halted || p.contextErr != nil {
		return
	}
	r := d2ast.Range{
		Path:  p.path,
		Start: start,
		End:   end,
	}
	f = "%v: " + f
	v = append([]interface{}{r}, v...)
	p.err.Errors = append(p.err.Errors, d2ast.Error{
		Range:   r,
		Message: fmt.Sprintf(f, v...),
	})
}

func (p *parser) enterNesting(start d2ast.Position) bool {
	if p.nestingDepth >= MaxNestingDepth {
		p.errorf(start, p.pos, "maximum nesting depth of %d exceeded", MaxNestingDepth)
		p.halted = true
		return false
	}
	p.nestingDepth++
	return true
}

func (p *parser) leaveNesting() {
	p.nestingDepth--
}

// _readRune reads the next rune from the underlying reader or from the p.readahead buffer.
func (p *parser) _readRune() (r rune, eof bool) {
	if p.halted || p.contextErr != nil {
		return 0, true
	}
	p.readCount++
	if p.ctx != nil && p.readCount%256 == 1 {
		if err := p.ctx.Err(); err != nil {
			p.contextErr = err
			return 0, true
		}
	}
	if p.readaheadIndex < len(p.readahead) {
		r = p.readahead[p.readaheadIndex]
		p.readaheadIndex++
		if p.readaheadIndex == len(p.readahead) {
			p.readahead = p.readahead[:0]
			p.readaheadIndex = 0
		}
		return r, false
	}

	if p.ioerr {
		p.rewind()
		return 0, true
	}

	p.readerPos = p.lookaheadPos

	r, _, err := p.reader.ReadRune()
	if err != nil {
		p.ioerr = true
		if err != io.EOF {
			p.err.Errors = append(p.err.Errors, d2ast.Error{
				Range: d2ast.Range{
					Path:  p.path,
					Start: p.readerPos,
					End:   p.readerPos,
				},
				Message: fmt.Sprintf("io error: %v", err),
			})
		}
		p.rewind()
		return 0, true
	}
	return r, false
}

func (p *parser) read() (r rune, eof bool) {
	r, eof = p._readRune()
	if eof {
		return 0, true
	}
	p.pos = p.pos.Advance(r, p.utf16Pos)
	p.lookaheadPos = p.pos
	return r, false
}

func (p *parser) replay(r rune) {
	p.pos = p.pos.Subtract(r, p.utf16Pos)

	// This is more complex than it needs to be to allow reusing the buffer underlying
	// p.lookahead.
	newcap := len(p.lookahead) + 1
	if newcap > cap(p.lookahead) {
		lookahead2 := make([]rune, newcap)
		copy(lookahead2[1:], p.lookahead)
		p.lookahead = lookahead2
	} else {
		p.lookahead = p.lookahead[:newcap]
		copy(p.lookahead[1:], p.lookahead)
	}
	p.lookahead[0] = r

	p.rewind()
}

// peek returns the next rune without advancing the parser.
// You *must* call commit or rewind afterwards.
func (p *parser) peek() (r rune, eof bool) {
	r, eof = p._readRune()
	if eof {
		return 0, true
	}

	p.lookahead = append(p.lookahead, r)
	p.lookaheadPos = p.lookaheadPos.Advance(r, p.utf16Pos)
	return r, false
}

// TODO: this can replace multiple peeks i think, just return []rune instead
func (p *parser) peekn(n int) (s string, eof bool) {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		r, eof := p.peek()
		if eof {
			return sb.String(), true
		}
		sb.WriteRune(r)
	}
	return sb.String(), false
}

func (p *parser) readNotSpace() (r rune, eof bool) {
	for {
		r, eof = p.read()
		if eof {
			return 0, true
		}
		if unicode.IsSpace(r) {
			continue
		}
		return r, false
	}
}

// peekNotSpace returns the next non space rune without advancing the parser.
//
// newline is set if the next non space character is on a different line
// than the current line.
//
// TODO: everywhere this is used, we support newline escapes and so can just
// add the logic here and it should *just* work
// except line comments iirc
// not entirely sure, maybe i can put it into peek somehow
func (p *parser) peekNotSpace() (r rune, newlines int, eof bool) {
	for {
		r, eof = p.peek()
		if eof {
			return 0, 0, true
		}
		if unicode.IsSpace(r) {
			if r == '\n' {
				newlines++
			}
			continue
		}
		return r, newlines, false
	}
}

// commit advances p.pos by all peeked bytes and then resets the p.lookahead buffer.
func (p *parser) commit() {
	p.pos = p.lookaheadPos
	p.lookahead = p.lookahead[:0]
}

// rewind copies p.lookahead to the front of p.readahead and then resets the p.lookahead buffer.
// All peeked bytes will again be available via p.eat or p.peek.
// TODO:
// peek
// peekn
// peekNotSpace
// commit
// rewind
//
// TODO: make each parse function read its delimiter and return nil if not as expected
// TODO: lookahead *must* always be empty in between parse calls. you either commit or
//
//	rewind in each function. if you don't, you pass a hint.
//
// TODO: omg we don't need two buffers, just a single lookahead and an index...
// TODO: get rid of lookaheadPos or at least never use directly. maybe rename to beforePeekPos?
//
//	or better yet keep positions in the lookahead buffer.
//	ok so plan here is to get rid of lookaheadPos and add a rewindPos that stores
//	the pos to rewind to.
func (p *parser) rewind() {
	if len(p.lookahead) == 0 {
		return
	}

	// This is more complex than it needs to be to allow reusing the buffer underlying
	// p.readahead.
	unread := p.readahead[p.readaheadIndex:]
	newcap := len(p.lookahead) + len(unread)
	if cap(p.readahead) < newcap {
		readahead2 := make([]rune, newcap)
		copy(readahead2[len(p.lookahead):], unread)
		p.readahead = readahead2
	} else {
		p.readahead = p.readahead[:newcap]
		copy(p.readahead[len(p.lookahead):], unread)
	}
	copy(p.readahead, p.lookahead)
	p.readaheadIndex = 0

	p.lookahead = p.lookahead[:0]
	p.lookaheadPos = p.pos
}

// TODO: remove isFileMap like in printer. can't rn as we have to subtract delim
func (p *parser) parseMap(isFileMap bool) *d2ast.Map {
	m := &d2ast.Map{
		Range: d2ast.Range{
			Path:  p.path,
			Start: p.pos,
		},
	}
	defer m.Range.End.From(&p.pos)

	if !isFileMap {
		m.Range.Start = m.Range.Start.Subtract('{', p.utf16Pos)
		if !p.enterNesting(m.Range.Start) {
			return m
		}
		defer p.leaveNesting()
		p.depth++
		defer dec(&p.depth)
	}

	for {
		r, eof := p.readNotSpace()
		if eof {
			if !isFileMap {
				p.errorf(m.Range.Start, p.readerPos, "maps must be terminated with }")
			}
			return m
		}

		switch r {
		case ';':
			continue
		case '}':
			if isFileMap {
				p.errorf(p.pos.Subtract(r, p.utf16Pos), p.pos, "unexpected map termination character } in file map")
				continue
			}
			return m
		}

		n := p.parseMapNode(r)
		if n.Unbox() != nil {
			m.Nodes = append(m.Nodes, n)
			// TODO: all subsequent not comment characters on the current line (or till ;)
			// need to be considered errors.
			// TODO: add specific msg for each bad rune type
		}

		if n.BlockComment != nil {
			// Anything after a block comment is ok.
			continue
		}

		after := p.pos
		for {
			r, newlines, eof := p.peekNotSpace()
			if eof || newlines != 0 || r == ';' || r == '}' || r == '#' {
				p.rewind()
				break
			}
			p.commit()
		}

		// TODO: maybe better idea here is to make parseUnquotedString aware of its delimiters
		// better and so it would read technically invalid characters and just complain.
		// TODO: that way broken syntax will be parsed more "intently". would work better with
		// language tooling I think though not sure. yes definitely, eaterr!
		if after != p.pos {
			if n.Unbox() != nil {
				if n.MapKey != nil && n.MapKey.Value.Unbox() != nil {
					ps := ""
					if _, ok := n.MapKey.Value.Unbox().(*d2ast.BlockString); ok {
						ps = ". See https://d2lang.com/tour/text#advanced-block-strings."
					}
					p.errorf(after, p.pos, "unexpected text after %v%s", n.MapKey.Value.Unbox().Type(), ps)
				} else {
					p.errorf(after, p.pos, "unexpected text after %v", n.Unbox().Type())
				}
			} else {
				p.errorf(after, p.pos, "invalid text beginning unquoted key")
			}
		}
	}
}

func (p *parser) parseMapNode(r rune) d2ast.MapNodeBox {
	var box d2ast.MapNodeBox

	switch r {
	case '#':
		box.Comment = p.parseComment()
		return box
	case '"':
		s, eof := p.peekn(2)
		if eof {
			break
		}
		if s != `""` {
			p.rewind()
			break
		}
		p.commit()
		box.BlockComment = p.parseBlockComment()
		return box
	case '.':
		box.Substitution, box.Import = p.parseSpread(false)
		if box.Substitution != nil || box.Import != nil {
			return box
		}
	}

	p.replay(r)
	box.MapKey = p.parseMapKey()
	return box
}

// The first dot has already been read. Only consume a complete spread prefix.
func (p *parser) parseSpread(inArray bool) (*d2ast.Substitution, *d2ast.Import) {
	prefix, eof := p.peekn(3)
	if !eof {
		switch prefix {
		case "..$":
			start := p.pos.Subtract('.', p.utf16Pos)
			if p.consumeSubstitutionOpen() {
				return p.parseSubstitution(start, true), nil
			}
		case "..@":
			p.commit()
			return nil, p.parseImport(true, inArray)
		}
	}
	p.rewind()
	return nil, nil
}

func (p *parser) parseComment() *d2ast.Comment {
	c := &d2ast.Comment{
		Range: d2ast.Range{
			Path:  p.path,
			Start: p.pos.Subtract('#', p.utf16Pos),
		},
	}
	defer c.Range.End.From(&p.pos)

	var sb strings.Builder
	defer func() {
		c.Value = sb.String()
	}()
	p.parseCommentLine(c, &sb)

	for {
		r, newlines, eof := p.peekNotSpace()
		if eof {
			return c
		}
		if r != '#' || newlines >= 2 {
			p.rewind()
			return c
		}
		p.commit()

		if newlines == 1 {
			sb.WriteByte('\n')
		}

		p.parseCommentLine(c, &sb)
	}
}

func (p *parser) parseCommentLine(c *d2ast.Comment, sb *strings.Builder) {
	firstRune := true
	for {
		r, eof := p.peek()
		if eof {
			return
		}
		if r == '\n' {
			p.rewind()
			return
		}
		p.commit()

		if firstRune {
			firstRune = false
			if r == ' ' {
				continue
			}
		}
		sb.WriteRune(r)
	}
}

func (p *parser) parseBlockComment() *d2ast.BlockComment {
	bc := &d2ast.BlockComment{
		Range: d2ast.Range{
			Path:  p.path,
			Start: p.pos.SubtractString(`"""`, p.utf16Pos),
		},
	}
	defer bc.Range.End.From(&p.pos)

	p.depth++
	defer dec(&p.depth)

	for {
		r, eof := p.peek()
		if eof {
			p.errorf(bc.Range.Start, p.readerPos, `block comments must be terminated with """`)
			return bc
		}

		if !unicode.IsSpace(r) {
			p.rewind()
			break
		}
		p.commit()
		if r == '\n' {
			break
		}
	}

	var closed bool
	bc.Value, closed = p.readBlockBody("", `"""`)
	if !closed {
		p.errorf(bc.Range.Start, p.readerPos, `block comments must be terminated with """`)
	}
	return bc
}

// readBlockBody consumes through the first complete delimiter. The prefix
// table lets partial matches overlap without rereading any of the body.
func (p *parser) readBlockBody(indent, delimiter string) (string, bool) {
	end := []rune(delimiter)
	prefix := make([]int, len(end))
	advance := func(n int, r rune) int {
		for n > 0 && r != end[n] {
			n = prefix[n-1]
		}
		if r == end[n] {
			n++
		}
		return n
	}
	for i := 1; i < len(end); i++ {
		prefix[i] = advance(prefix[i-1], end[i])
	}

	var body strings.Builder
	body.WriteString(indent)
	matched := 0
	for matched != len(end) {
		r, eof := p.read()
		if eof {
			break
		}
		body.WriteRune(r)
		matched = advance(matched, r)
	}
	text := body.String()
	closed := matched == len(end)
	if closed {
		text = text[:len(text)-len(delimiter)]
	}
	return trimCommonIndent(trimSpaceAfterLastNewline(text)), closed
}

func trimSpaceAfterLastNewline(s string) string {
	lastNewline := strings.LastIndexByte(s, '\n')
	if lastNewline == -1 {
		return strings.TrimRightFunc(s, unicode.IsSpace)
	}

	lastLine := s[lastNewline+1:]
	lastLine = strings.TrimRightFunc(lastLine, unicode.IsSpace)
	if len(lastLine) == 0 {
		return s[:lastNewline]
	}
	return s[:lastNewline+1] + lastLine
}

func (p *parser) parseMapKey() (mk *d2ast.Key) {
	mk = &d2ast.Key{
		Range: d2ast.Range{
			Path:  p.path,
			Start: p.pos,
		},
	}
	defer mk.Range.End.From(&p.pos)

	// An optional filter marker precedes a key or connection.
	r, eof := p.peek()
	if eof {
		return nil
	}
	if r == '!' {
		r, eof := p.peek()
		if !eof && r == '&' {
			p.commit()
			mk.NotAmpersand = true
		} else {
			p.rewind()
		}
	} else if r == '&' {
		p.commit()
		mk.Ampersand = true
	} else {
		p.rewind()
	}

	markerEnd := p.pos
	errorsBefore := len(p.err.Errors)
	defer func() {
		if mk.Key == nil && len(mk.Edges) == 0 {
			if (mk.Ampersand || mk.NotAmpersand) && len(p.err.Errors) == errorsBefore {
				p.errorf(mk.Range.Start, markerEnd, "filter requires a key or connection")
			}
			mk = nil
		}
	}()

	r, eof = p.peek()
	if eof {
		return mk
	}
	if r == '(' {
		p.commit()
		p.parseEdgeGroup(mk)
		return mk
	}
	p.rewind()

	k, scopedGroup := p.parseKeyPath(stringContext{beforeEdge: true}, true)
	mk.Key = k
	if scopedGroup {
		p.parseEdgeGroup(mk)
		return mk
	}

	r, newlines, eof := p.peekNotSpace()
	if eof {
		return mk
	}
	if newlines > 0 {
		p.rewind()
		return mk
	}
	switch r {
	case '(':
		p.commit()
		if k != nil {
			p.errorf(p.pos.Subtract('(', p.utf16Pos), p.pos, "expected . before scoped connection")
		}
		p.parseEdgeGroup(mk)
		return mk
	case '<', '>', '-':
		p.rewind()
		mk.Key = nil
		p.parseEdges(mk, k)
		p.parseMapKeyValue(mk)
		return mk
	default:
		p.rewind()
		p.parseMapKeyValue(mk)
		return mk
	}
}

func (p *parser) parseMapKeyValue(mk *d2ast.Key) {
	r, newlines, eof := p.peekNotSpace()
	if eof {
		return
	}
	if newlines > 0 {
		p.rewind()
		return
	}

	switch r {
	case '{':
		p.rewind()
		if mk.Key == nil && len(mk.Edges) == 0 {
			return
		}
	case ':':
		p.commit()
		if mk.Key == nil && len(mk.Edges) == 0 {
			p.errorf(mk.Range.Start, p.pos, "map value without key")
		}
	default:
		p.rewind()
		return
	}
	mk.Value = p.parseValue(false)
	if mk.Value.Unbox() == nil {
		p.errorf(p.pos.Subtract(':', p.utf16Pos), p.pos, "missing value after colon")
	}

	sb := mk.Value.ScalarBox()
	// If the value is a scalar, then check if it's the primary value.
	if sb.Unbox() != nil {
		r, newlines, eof := p.peekNotSpace()
		if eof || newlines > 0 || r != '{' {
			p.rewind()
			return
		}
		// Next character is on the same line without ; separator so it must mean
		// our current value is the Primary and the next is the Value.
		p.commit()
		p.replay(r)
		mk.Primary = sb
		mk.Value = p.parseValue(false)
	}
}

func (p *parser) parseEdgeGroup(mk *d2ast.Key) {
	groupStart := p.pos.Subtract('(', p.utf16Pos)
	// Only endpoint keys use the group's closing parenthesis as a delimiter.
	p.inEdgeGroup = true
	src := p.parseKey(stringContext{beforeEdge: true})
	p.parseEdges(mk, src)
	p.inEdgeGroup = false

	r, newlines, eof := p.peekNotSpace()
	if eof || newlines > 0 || r != ')' {
		p.rewind()
		p.errorf(groupStart, p.pos, "edge groups must be terminated with )")
		return
	}
	p.commit()
	if len(mk.Edges) == 0 {
		p.errorf(groupStart, p.pos, "edge groups must contain a connection")
	}

	r, newlines, eof = p.peekNotSpace()
	if eof || newlines > 0 {
		p.rewind()
		return
	}
	if r == '[' {
		p.commit()
		mk.EdgeIndex = p.parseEdgeIndex()
	} else {
		p.rewind()
	}

	r, newlines, eof = p.peekNotSpace()
	if eof || newlines > 0 {
		p.rewind()
		return
	}
	if r == '.' {
		p.commit()
		dotEnd := p.pos
		mk.EdgeKey = p.parseKey(stringContext{})
		if mk.EdgeKey == nil {
			p.errorAfterDot(dotEnd)
		}
	} else {
		p.rewind()
	}

	p.parseMapKeyValue(mk)
}

func (p *parser) parseEdgeIndex() *d2ast.EdgeIndex {
	ei := &d2ast.EdgeIndex{
		Range: d2ast.Range{
			Path:  p.path,
			Start: p.pos.Subtract('[', p.utf16Pos),
		},
	}
	defer ei.Range.End.From(&p.pos)

	var sb strings.Builder
	for {
		r, eof := p.peek()
		// Leave statement, value and comment delimiters for the caller on error.
		if eof || strings.ContainsRune("\n;:{}#", r) {
			p.rewind()
			p.errorf(ei.Range.Start, p.pos, "unterminated edge index")
			return nil
		}
		p.commit()
		if r == ']' {
			break
		}
		sb.WriteRune(r)
	}

	index := strings.TrimSpace(sb.String())
	if index == "*" {
		ei.Glob = true
		return ei
	}
	// Parse unsigned digits within the range of EdgeIndex.Int.
	n, err := strconv.ParseUint(index, 10, strconv.IntSize-1)
	if errors.Is(err, strconv.ErrRange) {
		p.errorf(ei.Range.Start, p.pos, "edge index is too large")
		return nil
	}
	if err != nil {
		p.errorf(ei.Range.Start, p.pos, "edge index must be a nonnegative decimal integer or *")
		return nil
	}
	i := int(n)
	ei.Int = &i
	return ei
}

func (p *parser) parseEdges(mk *d2ast.Key, src *d2ast.KeyPath) {
	for {
		e := &d2ast.Edge{
			Range: d2ast.Range{
				Path: p.path,
			},
			Src: src,
		}
		if src != nil {
			e.Range.Start = src.Range.Start
		} else {
			e.Range.Start = p.pos
		}

		r, newlines, eof := p.peekNotSpace()
		if eof {
			return
		}
		if newlines > 0 {
			p.rewind()
			return
		}
		if r == '<' || r == '*' {
			e.SrcArrow = string(r)
		} else if r != '-' {
			p.rewind()
			return
		}
		if src == nil {
			p.errorf(p.lookaheadPos.Subtract(r, p.utf16Pos), p.lookaheadPos, "connection missing source")
			e.Range.Start = p.lookaheadPos.Subtract(r, p.utf16Pos)
		}
		p.commit()

		if !p.parseEdge(e) {
			return
		}

		dst := p.parseKey(stringContext{beforeEdge: true})
		if dst == nil {
			p.errorf(e.Range.Start, p.pos, "connection missing destination")
		} else {
			e.Dst = dst
			e.Range.End = e.Dst.Range.End
		}
		mk.Edges = append(mk.Edges, e)
		src = dst
	}
}

func (p *parser) parseEdge(e *d2ast.Edge) (ok bool) {
	defer e.Range.End.From(&p.pos)

	// parseEdges has consumed the first arrow character. An unheaded
	// connection starting with '-' needs at least one more hyphen.
	firstCharEnd := p.pos
	singleHyphen := e.SrcArrow == ""
	for {
		r, eof := p.peek()
		if eof {
			p.errorf(e.Range.Start, p.readerPos, "unterminated connection")
			return false
		}
		switch r {
		case '>', '*':
			e.DstArrow = string(r)
			p.commit()
			return true
		case '\\':
			p.commit()
			r, newlines, eof := p.peekNotSpace()
			if eof {
				continue
			}
			if newlines == 0 {
				p.rewind()
				p.errorf(e.Range.Start, p.readerPos, "only newline escapes are allowed in connections")
				return false
			}
			if newlines > 1 {
				p.rewind()
				continue
			}
			p.commit()
			p.replay(r)
		case '-':
			p.commit()
			singleHyphen = false
		default:
			p.rewind()
			if singleHyphen {
				p.errorf(firstCharEnd.Subtract('-', p.utf16Pos), firstCharEnd, "use -- for an undirected connection")
			}
			return true
		}
	}
}

type stringContext struct {
	inKey      bool
	inArray    bool // a closing bracket ends this array element
	beforeEdge bool // an edge operator can follow this endpoint
}

func (p *parser) parseKey(ctx stringContext) *d2ast.KeyPath {
	k, _ := p.parseKeyPath(ctx, false)
	return k
}

func (p *parser) errorAfterDot(dotEnd d2ast.Position) {
	p.errorf(dotEnd.Subtract('.', p.utf16Pos), dotEnd, "expected a key segment after .")
}

// An entry key may end in .( instead of another path segment. In that case
// parseKeyPath consumes the opening parenthesis for parseEdgeGroup.
func (p *parser) parseKeyPath(ctx stringContext, allowScopedGroup bool) (k *d2ast.KeyPath, scopedGroup bool) {
	ctx.inKey = true
	k = &d2ast.KeyPath{
		Range: d2ast.Range{
			Path:  p.path,
			Start: p.pos,
		},
	}

	defer func() {
		if len(k.Path) == 0 {
			k = nil
		} else {
			k.Range.End = k.Path[len(k.Path)-1].Unbox().GetRange().End
			for _, part := range k.Path {
				if part.Unbox() != nil {
					if len(part.Unbox().ScalarString()) > 518 {
						p.errorf(k.Range.Start, k.Range.End, "key length %d exceeds maximum allowed length of 518", len(part.Unbox().ScalarString()))
						break
					}
				}
			}
		}
	}()

	var dotEnd d2ast.Position
	for {
		r, newlines, eof := p.peekNotSpace()
		if allowScopedGroup && len(k.Path) > 0 && !eof && newlines == 0 && r == '(' {
			p.commit()
			return k, true
		}
		p.rewind()

		var sb d2ast.StringBox
		if !eof && newlines == 0 && r != '(' && r != '.' {
			sb = p.parseString(ctx)
		}
		s := sb.Unbox()
		if s == nil {
			if len(k.Path) > 0 {
				p.errorAfterDot(dotEnd)
			}
			return k, false
		}
		if sb.UnquotedString != nil && strings.HasPrefix(s.ScalarString(), "@") {
			p.errorf(s.GetRange().Start, s.GetRange().End, "%s is not a valid import, did you mean ...%[2]s?", s.ScalarString())
		}

		if len(k.Path) == 0 {
			k.Range.Start = s.GetRange().Start
		}
		k.Path = append(k.Path, &sb)

		r, newlines, eof = p.peekNotSpace()
		if eof {
			return k, false
		}
		if newlines > 0 || r != '.' {
			p.rewind()
			return k, false
		}
		p.commit()
		dotEnd = p.pos
	}
}

func (p *parser) parseString(ctx stringContext) d2ast.StringBox {
	var box d2ast.StringBox

	r, newlines, eof := p.peekNotSpace()
	if eof || newlines > 0 {
		p.rewind()
		return box
	}
	p.commit()

	switch r {
	case '"':
		box.DoubleQuotedString = p.parseDoubleQuotedString(ctx.inKey)
		return box
	case '\'':
		box.SingleQuotedString = p.parseSingleQuotedString()
		return box
	case '|':
		box.BlockString = p.parseBlockString()
		return box
	default:
		p.replay(r)
		box.UnquotedString = p.parseUnquotedString(ctx)
		return box
	}
}

func (p *parser) parseUnquotedString(ctx stringContext) (s *d2ast.UnquotedString) {
	s = &d2ast.UnquotedString{
		Range: d2ast.Range{
			Path:  p.path,
			Start: p.pos,
		},
	}
	// TODO: fix unquoted end whitespace handling to peekNotSpace
	lastNonSpace := p.pos
	defer s.Range.End.From(&lastNonSpace)
	// Hash-prefixed values include complete substitutions and escapes in their
	// token range. Other bare strings retain their existing source positions.
	hashPrefixed := false

	var sb strings.Builder
	var rawb strings.Builder
	hasRaw := false
	lastPatternIndex := 0
	defer func() {
		sv := strings.TrimRightFunc(sb.String(), unicode.IsSpace)
		rawv := sv
		if hasRaw {
			raw := rawb.String()
			rawv = strings.TrimRightFunc(raw, unicode.IsSpace)
			// Keep an escape's operand so the formatter cannot turn the last
			// backslash into a line continuation that consumes the next key.
			if len(rawv) < len(raw) && strings.HasSuffix(rawv, `\`) {
				backslashes := len(rawv) - len(strings.TrimRight(rawv, `\`))
				if backslashes%2 == 1 {
					_, size := utf8.DecodeRuneInString(raw[len(rawv):])
					rawv = raw[:len(rawv)+size]
				}
			}
		}
		if s.Pattern != nil {
			if lastPatternIndex < len(sv) {
				s.Pattern = append(s.Pattern, sv[lastPatternIndex:])
			}
		}
		if sv == "" {
			if len(s.Value) > 0 {
				return
			}
			s = nil
			// TODO: this should be in the parent and instead they check the delimiters first
			// 			 or last really. only in parseMapNode && parseArrayNode
			// TODO: give specific descriptions for each kind of special character that could have caused this.
			return
		}
		s.Value = append(s.Value, d2ast.InterpolationBox{String: &sv, StringRaw: &rawv})
	}()

	// Preserve all four peeks, including their I/O/error behavior, without
	// allocating a temporary string for this fixed prefix check.
	importSpread := true
	for _, want := range "...@" {
		r, eof := p.peek()
		if eof {
			importSpread = false
			break
		}
		if r != want {
			importSpread = false
		}
	}
	p.rewind()
	if importSpread {
		p.errorf(p.pos, p.pos.AdvanceString("...@", p.utf16Pos), "unquoted strings cannot begin with ...@ as that's import spread syntax")
	}

	for {
		r, eof := p.peek()
		if eof {
			return s
		}

		if p.inEdgeGroup && r == ')' {
			if p.endsEdgeGroup() {
				p.rewind()
				return s
			}
			p.rewind()
			p.peek()
			p.commit()
			lastNonSpace = p.pos
			sb.WriteRune(r)
			if hasRaw {
				rawb.WriteRune(r)
			}
			continue
		}

		switch r {
		case '\n', ';', '{', '}':
			p.rewind()
			return s
		case '#':
			// After a colon the first hash can introduce a label. At other
			// positions it begins a comment, including at array-entry start.
			if ctx.inKey || ctx.inArray || p.pos != s.Range.Start {
				p.rewind()
				return s
			}
			next, eof := p.peek()
			p.rewind()
			if eof || unicode.IsSpace(next) || strings.ContainsRune("#;{}", next) {
				return s
			}
			hashPrefixed = true
			p.peek()
		case ']':
			if ctx.inArray {
				p.rewind()
				return s
			}
		}
		if ctx.inKey {
			switch r {
			case ':', '.':
				p.rewind()
				return s
			case '<':
				if ctx.beforeEdge {
					edge := p.startsLeftArrow(sb.Len() != 0)
					p.rewind()
					if edge {
						return s
					}
					p.peek()
				}
			case '-':
				// Only the first character of an arrow belongs to the edge parser.
				// Otherwise consume the hyphen as text and scan the next rune normally.
				next, eof := p.peek()
				p.rewind()
				if !eof && (next == '-' || next == '>' || next == '*') {
					return s
				}
				p.peek()
			}
		}

		if r == '*' {
			if sb.Len() == 0 {
				s.Pattern = append(s.Pattern, "*")
			} else {
				// An interpolation can start a shorter text fragment between
				// wildcards. A prior offset outside it belongs to the old fragment.
				if lastPatternIndex > sb.Len() {
					lastPatternIndex = 0
				}
				s.Pattern = append(s.Pattern, sb.String()[lastPatternIndex:], "*")
			}
			lastPatternIndex = len(sb.String()) + 1
		}

		p.commit()

		if !unicode.IsSpace(r) {
			lastNonSpace = p.pos
		}

		if !ctx.inKey && r == '$' {
			start := p.pos.Subtract('$', p.utf16Pos)
			if p.consumeSubstitutionOpen() {
				if sb.Len() > 0 {
					sv := sb.String()
					rawv := sv
					if hasRaw {
						rawv = rawb.String()
					}
					s.Value = append(s.Value, d2ast.InterpolationBox{String: &sv, StringRaw: &rawv})
					sb.Reset()
					rawb.Reset()
					hasRaw = false
				}
				subst := p.parseSubstitution(start, false)
				s.Value = append(s.Value, d2ast.InterpolationBox{Substitution: subst})
				if hashPrefixed {
					lastNonSpace = p.pos
				}
				continue
			}
		}

		if r != '\\' {
			sb.WriteRune(r)
			if hasRaw {
				rawb.WriteRune(r)
			}
			continue
		}

		r2, eof := p.read()
		if eof {
			p.errorf(p.pos.Subtract('\\', p.utf16Pos), p.readerPos, "unfinished escape sequence")
			return s
		}

		if r2 == '\n' {
			r, newlines, eof := p.peekNotSpace()
			if eof || newlines > 0 {
				p.rewind()
				return s
			}
			p.commit()
			p.replay(r)
			continue
		}

		// Raw and decoded text are identical until the first real escape.
		// Copy that prefix once; ordinary tokens need only the decoded builder.
		if !hasRaw {
			rawb.WriteString(sb.String())
			hasRaw = true
		}
		sb.WriteRune(d2string.DecodeEscape(r2))
		rawb.WriteByte('\\')
		rawb.WriteRune(r2)
		if hashPrefixed {
			lastNonSpace = p.pos
		}
	}
}

// The closing parenthesis is already peeked. Parentheses within endpoint
// text (for instance get()) are otherwise part of the unquoted key.
func (p *parser) endsEdgeGroup() bool {
	next, newlines, eof := p.peekNotSpace()
	return eof || newlines > 0 || strings.ContainsRune(";#{}[]:.", next)
}

// A bare '<' is also a valid left-pointing connection when there are
// endpoints on either side. Explicit arrowheads and shafts still start an
// operator even if an endpoint is missing.
func (p *parser) startsLeftArrow(hasSource bool) bool {
	next, eof := p.peek()
	if !eof && next == '\\' {
		// A backslash continues an arrow only across a line break. Other
		// escapes belong to the surrounding literal key.
		for {
			r, eof := p.peek()
			if eof || !unicode.IsSpace(r) {
				return false
			}
			if r == '\n' {
				return true
			}
		}
	}
	if !eof && strings.ContainsRune("->*", next) {
		return true
	}
	if !hasSource {
		return false
	}
	for !eof && next != '\n' && unicode.IsSpace(next) {
		next, eof = p.peek()
	}
	if !eof && p.inEdgeGroup && next == ')' {
		return !p.endsEdgeGroup()
	}
	return !eof && !strings.ContainsRune("\n;#{}:.(@", next)
}

func (p *parser) parseDoubleQuotedString(inKey bool) *d2ast.DoubleQuotedString {
	s := &d2ast.DoubleQuotedString{
		Range: d2ast.Range{
			Path:  p.path,
			Start: p.pos.Subtract('"', p.utf16Pos),
		},
	}
	defer s.Range.End.From(&p.pos)

	var sb strings.Builder
	var rawb strings.Builder
	defer func() {
		if sb.Len() > 0 {
			sv := sb.String()
			rawv := rawb.String()
			s.Value = append(s.Value, d2ast.InterpolationBox{String: &sv, StringRaw: &rawv})
		}
	}()

	for {
		r, eof := p.peek()
		if eof {
			p.errorf(s.Range.Start, p.readerPos, `double quoted strings must be terminated with "`)
			return s
		}
		if r == '\n' {
			p.rewind()
			p.errorf(s.Range.Start, p.pos, `double quoted strings must be terminated with "`)
			return s
		}

		p.commit()
		if !inKey && r == '$' {
			start := p.pos.Subtract('$', p.utf16Pos)
			if p.consumeSubstitutionOpen() {
				subst := p.parseSubstitution(start, false)
				if sb.Len() > 0 {
					s.Value = append(s.Value, d2ast.InterpolationBox{String: go2.Pointer(sb.String())})
					sb.Reset()
				}
				s.Value = append(s.Value, d2ast.InterpolationBox{Substitution: subst})
				continue
			}
		}

		if r == '"' {
			return s
		}

		if r != '\\' {
			sb.WriteRune(r)
			rawb.WriteRune(r)
			continue
		}

		r2, eof := p.read()
		if eof {
			p.errorf(p.pos.Subtract('\\', p.utf16Pos), p.readerPos, "unfinished escape sequence")
			p.errorf(s.Range.Start, p.readerPos, `double quoted strings must be terminated with "`)
			return s
		}

		if r2 == '\n' {
			// TODO: deindent
			continue
		}
		sb.WriteRune(d2string.DecodeEscape(r2))
		rawb.WriteByte('\\')
		rawb.WriteRune(r2)
	}
}

func (p *parser) parseSingleQuotedString() *d2ast.SingleQuotedString {
	s := &d2ast.SingleQuotedString{
		Range: d2ast.Range{
			Path:  p.path,
			Start: p.pos.Subtract('\'', p.utf16Pos),
		},
	}
	defer s.Range.End.From(&p.pos)

	var sb strings.Builder
	defer func() {
		s.Value = sb.String()
	}()

	for {
		r, eof := p.peek()
		if eof {
			p.errorf(s.Range.Start, p.readerPos, `single quoted strings must be terminated with '`)
			return s
		}
		if r == '\n' {
			p.rewind()
			p.errorf(s.Range.Start, p.pos, `single quoted strings must be terminated with '`)
			return s
		}
		p.commit()

		if r == '\'' {
			r, eof = p.peek()
			if eof {
				return s
			}
			if r == '\'' {
				p.commit()
				sb.WriteByte('\'')
				continue
			}
			p.rewind()
			return s
		}

		if r != '\\' {
			sb.WriteRune(r)
			continue
		}

		r2, eof := p.peek()
		if eof {
			continue
		}

		switch r2 {
		case '\n':
			p.commit()
			continue
		default:
			sb.WriteRune(r)
			p.rewind()
		}
	}
}

func (p *parser) parseBlockString() *d2ast.BlockString {
	bs := &d2ast.BlockString{
		Range: d2ast.Range{
			Path:  p.path,
			Start: p.pos.Subtract('|', p.utf16Pos),
		},
	}
	defer bs.Range.End.From(&p.pos)

	p.depth++
	defer dec(&p.depth)

	var fence, tag strings.Builder
	for {
		r, eof := p.peek()
		if eof || unicode.IsSpace(r) {
			bs.Quote = fence.String()
			bs.Tag = tag.String()
			if eof {
				p.errorf(bs.Range.Start, p.readerPos, `block string must be terminated with %v`, bs.Quote+"|")
				return bs
			}
			p.rewind()
			break
		}
		p.commit()
		if tag.Len() == 0 && !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			fence.WriteRune(r)
		} else {
			tag.WriteRune(r)
		}
	}
	if bs.Tag == "" {
		// TODO: no and fix compiler to not set text/markdown shape always.
		//       reason being not all multiline text is markdown by default.
		//       for example markdown edge labels or other random text.
		//       maybe we can be smart about this at some point and only set
		//       if the block string is being interpreted as markdown.
		bs.Tag = "md"
	}

	// Skip non newline whitespace.
	var indent string
	for {
		r, eof := p.peek()
		if eof {
			p.errorf(bs.Range.Start, p.readerPos, `block string must be terminated with %v`, bs.Quote+"|")
			return bs
		}
		if !unicode.IsSpace(r) {
			// Non whitespace characters on the first line have an implicit indent.
			indent = p.getIndent()
			p.rewind()
			break
		}
		p.commit()
		if r == '\n' {
			break
		}
	}

	var closed bool
	bs.Value, closed = p.readBlockBody(indent, bs.Quote+"|")
	if !closed {
		p.errorf(bs.Range.Start, p.readerPos, `block string must be terminated with %v`, bs.Quote+"|")
	}
	return bs
}

func (p *parser) parseArray() *d2ast.Array {
	a := &d2ast.Array{
		Range: d2ast.Range{
			Path:  p.path,
			Start: p.pos.Subtract('[', p.utf16Pos),
		},
	}
	defer a.Range.End.From(&p.readerPos)
	if !p.enterNesting(a.Range.Start) {
		return a
	}
	defer p.leaveNesting()

	p.depth++
	defer dec(&p.depth)

	for {
		r, eof := p.readNotSpace()
		if eof {
			p.errorf(a.Range.Start, p.readerPos, "arrays must be terminated with ]")
			return a
		}

		switch r {
		case ';':
			continue
		case ']':
			return a
		}

		n := p.parseArrayNode(r)
		if n.Unbox() != nil {
			a.Nodes = append(a.Nodes, n)
		}

		if n.BlockComment != nil {
			// Anything after a block comment is ok.
			continue
		}

		after := p.pos
		for {
			r, newlines, eof := p.peekNotSpace()
			if eof || newlines != 0 || r == ';' || r == ']' || r == '#' {
				p.rewind()
				break
			}
			p.commit()
		}

		if after != p.pos {
			if n.Unbox() != nil {
				p.errorf(after, p.pos, "unexpected text after %v", n.Unbox().Type())
			} else {
				p.errorf(after, p.pos, "invalid text beginning unquoted string")
			}
		}
	}
}

func (p *parser) parseArrayNode(r rune) d2ast.ArrayNodeBox {
	var box d2ast.ArrayNodeBox

	switch r {
	case '#':
		box.Comment = p.parseComment()
		return box
	case '"':
		s, eof := p.peekn(2)
		if eof {
			break
		}
		if s != `""` {
			p.rewind()
			break
		}
		p.commit()
		box.BlockComment = p.parseBlockComment()
		return box
	case '.':
		box.Substitution, box.Import = p.parseSpread(true)
		if box.Substitution != nil || box.Import != nil {
			return box
		}
	}

	p.replay(r)
	vbox := p.parseValue(true)
	if vbox.UnquotedString != nil && vbox.UnquotedString.ScalarString() == "" &&
		!(len(vbox.UnquotedString.Value) > 0 && vbox.UnquotedString.Value[0].Substitution != nil) {
		p.errorf(p.pos, p.pos.Advance(r, p.utf16Pos), "unquoted strings cannot start on %q", r)
	}
	box.Null = vbox.Null
	box.Suspension = vbox.Suspension
	box.Boolean = vbox.Boolean
	box.Number = vbox.Number
	box.UnquotedString = vbox.UnquotedString
	box.DoubleQuotedString = vbox.DoubleQuotedString
	box.SingleQuotedString = vbox.SingleQuotedString
	box.BlockString = vbox.BlockString
	box.Array = vbox.Array
	box.Map = vbox.Map
	box.Import = vbox.Import
	return box
}

func (p *parser) parseValue(inArray bool) d2ast.ValueBox {
	var box d2ast.ValueBox

	r, newlines, eof := p.peekNotSpace()
	if eof || newlines > 0 {
		p.rewind()
		return box
	}
	p.commit()

	switch r {
	case '[':
		box.Array = p.parseArray()
		return box
	case '{':
		box.Map = p.parseMap(false)
		return box
	case '@':
		box.Import = p.parseImport(false, inArray)
		return box
	}

	p.replay(r)
	sb := p.parseString(stringContext{inArray: inArray})
	if sb.DoubleQuotedString != nil {
		box.DoubleQuotedString = sb.DoubleQuotedString
		return box
	}
	if sb.SingleQuotedString != nil {
		box.SingleQuotedString = sb.SingleQuotedString
		return box
	}
	if sb.BlockString != nil {
		box.BlockString = sb.BlockString
		return box
	}

	if sb.UnquotedString == nil {
		return box
	}

	s := sb.UnquotedString
	if len(s.Value) != 1 || s.Value[0].String == nil {
		box.UnquotedString = s
		return box
	}

	literal := *s.Value[0].String
	if strings.EqualFold(literal, "null") {
		box.Null = &d2ast.Null{
			Range: s.Range,
		}
		return box
	}
	if strings.EqualFold(literal, "suspend") {
		box.Suspension = &d2ast.Suspension{
			Range: s.Range,
			Value: true,
		}
		return box
	}
	if strings.EqualFold(literal, "unsuspend") {
		box.Suspension = &d2ast.Suspension{
			Range: s.Range,
			Value: false,
		}
		return box
	}

	if strings.EqualFold(literal, "true") {
		box.Boolean = &d2ast.Boolean{
			Range: s.Range,
			Value: true,
		}
		return box
	}

	if strings.EqualFold(literal, "false") {
		box.Boolean = &d2ast.Boolean{
			Range: s.Range,
			Value: false,
		}
		return box
	}

	// TODO: only if matches regex
	rat, ok := big.NewRat(0, 1).SetString(literal)
	if ok {
		box.Number = &d2ast.Number{
			Range: s.Range,
			Raw:   literal,
			Value: rat,
		}
		return box
	}

	box.UnquotedString = s
	return box
}

// The dollar sign is already consumed or part of a peeked spread prefix.
// Preserve the existing horizontal whitespace permitted before the brace.
func (p *parser) consumeSubstitutionOpen() bool {
	r, newlines, eof := p.peekNotSpace()
	if eof || newlines > 0 || r != '{' {
		p.rewind()
		return false
	}
	p.commit()
	return true
}

func (p *parser) parseSubstitution(start d2ast.Position, spread bool) *d2ast.Substitution {
	subst := &d2ast.Substitution{
		Range: d2ast.Range{
			Path:  p.path,
			Start: start,
		},
		Spread: spread,
	}
	defer subst.Range.End.From(&p.pos)

	k := p.parseKey(stringContext{})
	if k != nil {
		subst.Path = k.Path
	} else {
		p.errorf(subst.Range.Start, p.pos, "substitution requires a variable name")
	}

	r, newlines, eof := p.peekNotSpace()
	if eof {
		p.errorf(subst.Range.Start, p.readerPos, "substitutions must be terminated by }")
		return subst
	}
	if newlines > 0 || r != '}' {
		p.rewind()
		p.errorf(subst.Range.Start, p.pos, "substitutions must be terminated by }")
		return subst
	}
	p.commit()

	return subst
}

func (p *parser) parseImport(spread, inArray bool) *d2ast.Import {
	imp := &d2ast.Import{
		Range: d2ast.Range{
			Path:  p.path,
			Start: p.pos.SubtractString("$", p.utf16Pos),
		},
		Spread: spread,
	}
	defer imp.Range.End.From(&p.pos)

	if imp.Spread {
		imp.Range.Start = imp.Range.Start.SubtractString("...", p.utf16Pos)
	}

	var pre strings.Builder
	for {
		r, eof := p.peek()
		if eof {
			break
		}
		if r != '.' && r != '/' {
			p.rewind()
			break
		}
		pre.WriteRune(r)
		p.commit()
	}
	imp.Pre = pre.String()

	k := p.parseKey(stringContext{inArray: inArray})
	if k == nil {
		p.errorf(imp.Range.Start, p.pos, "import requires a path")
		return imp
	}
	if k.Path[0].UnquotedString != nil && len(k.Path) > 1 && k.Path[1].UnquotedString != nil && k.Path[1].Unbox().ScalarString() == "d2" {
		k.Path = append(k.Path[:1], k.Path[2:]...)
	}
	imp.Path = k.Path
	return imp
}

// func marshalKey(k *d2ast.Key) string {
// 	var sb strings.Builder
// 	for i, s := range k.Path {
// 		// TODO: Need to encode specials and quotes.
// 		sb.WriteString(s.Unbox().ScalarString())
// 		if i < len(k.Path)-1 {
// 			sb.WriteByte('.')
// 		}
// 	}
// 	return sb.String()
// }

func dec(i *int) {
	*i -= 1
}

func (p *parser) getIndent() string {
	return strings.Repeat(" ", p.depth*2)
}

func trimCommonIndent(s string) string {
	lines := strings.Split(s, "\n")
	common := -1
	for _, line := range lines {
		width, rest := splitLeadingIndent(line, -1)
		if rest == "" {
			continue
		}
		if width == 0 {
			return s
		}
		if common < 0 || width < common {
			common = width
		}
	}
	if common < 0 {
		return s
	}
	for i, line := range lines {
		_, lines[i] = splitLeadingIndent(line, common)
	}
	return strings.Join(lines, "\n")
}

// splitLeadingIndent removes up to limit columns (all of them if negative).
// A tab occupies two columns; removing only its first leaves one space.
func splitLeadingIndent(s string, limit int) (width int, rest string) {
	for i, r := range s {
		if width == limit || !unicode.IsSpace(r) {
			return width, s[i:]
		}
		width++
		if r == '\t' {
			if width == limit {
				return width, " " + s[i+1:]
			}
			width++
		}
	}
	return width, ""
}
