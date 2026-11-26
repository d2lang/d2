package textmeasure

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

func ReplaceSubstitutionsMarkdown(mdText string, variables map[string]string) string {
	result, _, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), mdText, variables, int64(^uint64(0)>>1))
	if err != nil {
		return mdText
	}
	return result
}

// ReplaceSubstitutionsMarkdownBounded replaces variables outside Markdown code
// spans and blocks while bounding candidate-match work and bytes inserted by
// replacements. It checks the bound before each allocating replacement.
func ReplaceSubstitutionsMarkdownBounded(ctx context.Context, mdText string, variables map[string]string, maxExpansion int64) (string, int64, error) {
	return ResolveSubstitutionsMarkdownBounded(ctx, mdText, variables, maxExpansion, nil)
}

// ResolveSubstitutionsMarkdownBounded resolves D2 references in Markdown prose
// and link destinations and titles.
// The callback receives the original spelling between the braces. Literal names
// in variables also match, including historical names with an unquoted closing
// brace. The longest match wins, and resolve can override its stored value.
func ResolveSubstitutionsMarkdownBounded(ctx context.Context, mdText string, variables map[string]string, maxExpansion int64, resolve func(key string) (string, bool)) (string, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxExpansion < 0 {
		return "", 0, errors.New("negative Markdown substitution expansion limit")
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	if !strings.Contains(mdText, "${") || (len(variables) == 0 && resolve == nil && !strings.Contains(mdText, `\${`)) {
		return mdText, 0, nil
	}
	matcher, err := newMarkdownSubstitutionMatcher(ctx, variables)
	if err != nil {
		return "", 0, err
	}
	matcher.resolve = resolve
	source := []byte(mdText)
	doc, pc, err := parseMarkdownSubstitutions(ctx, source, mdText)
	if err != nil {
		return "", 0, err
	}

	matcher.source, matcher.references = mdText, pc

	type substitution struct {
		start  int
		stop   int
		newVal string
	}
	var substitutions []substitution
	var expansion int64
	replace := func(segment text.Index, escapes *markdownEscapes) error {
		// Owned and empty values have no source text to edit.
		if segment.Start == segment.Stop {
			return nil
		}
		original := mdText[segment.Start:segment.Stop]
		matcher.segmentStart = segment.Start
		value, used, err := matcher.replaceBounded(ctx, original, maxExpansion-expansion, escapes)
		if err != nil {
			return err
		}
		expansion += used
		if value != original {
			substitutions = append(substitutions, substitution{start: segment.Start, stop: segment.Stop, newVal: value})
		}
		return nil
	}
	replaceParts := func(parts text.MultiLineValue, escapes *markdownEscapes) error {
		indices := parts.Indices()
		if escapes == &referenceEscapes && len(indices) > 0 {
			// A shortcut's visible code is literal. Apply that same rule to
			// its lookup key so changing the definition cannot break the link.
			start, stop := indices[0].Start, indices[len(indices)-1].Stop
			if strings.Contains(mdText[start:stop], "`") && strings.Contains(mdText[start:stop], "${") {
				indices, err = referenceProseIndices(ctx, mdText[start:stop], start, indices)
				if err != nil {
					return err
				}
			}
		}
		for _, segment := range indices {
			if err := replace(segment, escapes); err != nil {
				return err
			}
		}
		return nil
	}
	replaceLink := func(dest text.SingleLineValue, title text.MultiLineValue, ref *ast.ReferenceLink) error {
		if ref != nil {
			// Shortcut and collapsed references use their visible text as the key.
			if ref.ReferenceLinkKind == ast.ReferenceLinkKindFull {
				return replaceParts(ref.Value, &referenceEscapes)
			}
			return nil
		}
		if err := replace(dest.Index(), &destinationEscapes); err != nil {
			return err
		}
		return replaceParts(title, &titleEscapes)
	}

	shortcutDepth := 0
	err = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if err := ctx.Err(); err != nil {
			return ast.WalkStop, err
		}
		if !entering {
			if referenceUsesText(n) {
				shortcutDepth--
			}
			return ast.WalkContinue, nil
		}

		if referenceUsesText(n) {
			shortcutDepth++
		}
		if isCodeNode(n) {
			return ast.WalkSkipChildren, nil
		}

		switch n := n.(type) {
		case *ast.Text:
			if shortcutDepth > 0 {
				return ast.WalkContinue, replace(n.Value.Index(), &referenceEscapes)
			}
			return ast.WalkContinue, replace(n.Value.Index(), nil)
		case *ast.AutoLink:
			return ast.WalkContinue, replace(n.Text.Index(), &autolinkEscapes)
		case *ast.Link:
			return ast.WalkContinue, replaceLink(n.Destination, n.Title, n.Reference)
		case *ast.Image:
			return ast.WalkContinue, replaceLink(n.Destination, n.Title, n.Reference)
		case *ast.LinkReferenceDefinition:
			// Every use shares these source offsets. Edit the definition once.
			if err := replaceParts(n.Label, &referenceEscapes); err != nil {
				return ast.WalkStop, err
			}
			return ast.WalkContinue, replaceLink(n.Destination, n.Title, nil)
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return "", 0, err
	}

	if len(substitutions) == 0 {
		return mdText, expansion, nil
	}

	sort.Slice(substitutions, func(i, j int) bool {
		return substitutions[i].start < substitutions[j].start
	})

	resultLen := len(source)
	for _, sub := range substitutions {
		resultLen += len(sub.newVal) - (sub.stop - sub.start)
	}
	var result strings.Builder
	result.Grow(resultLen)
	start := 0
	for _, sub := range substitutions {
		result.Write(source[start:sub.start])
		result.WriteString(sub.newVal)
		start = sub.stop
	}
	result.Write(source[start:])

	return result.String(), expansion, nil
}

// A label can span several source segments in a Markdown block quote or list.
// Goldmark supplies code span boundaries and the original segments retain
// indentation and block prefixes outside the label.
func referenceProseIndices(ctx context.Context, label string, start int, segments []text.Index) ([]text.Index, error) {
	const prefix = "x " // Parse the label as inline paragraph content.
	src := prefix + label
	doc, _, err := parseMarkdownSubstitutions(ctx, []byte(src), src)
	if err != nil {
		return nil, err
	}
	var code []text.Index
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if span, ok := n.(*ast.CodeSpan); entering && ok {
			indices := span.Value.Indices()
			if len(indices) > 0 {
				code = append(code, text.NewIndex(start+indices[0].Start-len(prefix), start+indices[len(indices)-1].Stop-len(prefix)))
			}
		}
		return ast.WalkContinue, nil
	})
	if len(code) == 0 {
		return segments, nil
	}
	var prose []text.Index
	for _, segment := range segments {
		for len(code) > 0 && code[0].Start < segment.Stop {
			if segment.Start < code[0].Start {
				prose = append(prose, text.NewIndex(segment.Start, code[0].Start))
			}
			segment.Start = max(segment.Start, code[0].Stop)
			if code[0].Stop > segment.Stop {
				break
			}
			code = code[1:]
		}
		if segment.Start < segment.Stop {
			prose = append(prose, segment)
		}
	}
	return prose, nil
}

// During substitution, D2 owns the complete reference in prose. Otherwise a
// key such as ${__version__} is split across Markdown emphasis nodes. Actual
// code spans and blocks are consumed by Goldmark before reaching this parser.
var substitutionParser = newMarkdownParser(parser.WithInlineParsers(util.Prioritized[parser.InlineParser](referenceInlineParser{}, 100)))
var referenceParserKey = parser.NewContextKey()

func parseMarkdownSubstitutions(ctx context.Context, src []byte, input string) (ast.Node, parser.Context, error) {
	state := &referenceParserState{ctx: ctx, source: input}
	pc := parser.NewContext()
	pc.Set(referenceParserKey, state)
	doc := substitutionParser.Parse(src, parser.WithContext(pc))
	return doc, pc, state.err
}

type referenceParserState struct {
	ctx    context.Context
	source string
	next   int
	err    error
}

type referenceInlineParser struct{}

func (referenceInlineParser) Trigger() []byte { return []byte{'$', '\\'} }
func (referenceInlineParser) Parse(_ ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, segment := block.PeekLine()
	start := segment.Start
	if len(line) >= 1 && line[0] == '\\' {
		line = line[1:]
		start++
	}
	if len(line) < 2 || line[0] != '$' || line[1] != '{' {
		return nil
	}
	state := pc.Get(referenceParserKey).(*referenceParserState)
	if segment.Start < state.next || state.err != nil {
		return nil
	}
	var end int
	end, state.next, state.err = markdownReferenceEnd(state.ctx, state.source, start+2)
	if end == 0 || end > segment.Stop || !strings.ContainsAny(state.source[start+2:end-1], markdownSyntax) {
		return nil
	}
	block.Advance(end - segment.Start)
	return ast.NewText(text.NewSingleLineValueFromSegment(segment.WithStop(end), block.Decoder()))
}

func referenceUsesText(n ast.Node) bool {
	var ref *ast.ReferenceLink
	switch n := n.(type) {
	case *ast.Link:
		ref = n.Reference
	case *ast.Image:
		ref = n.Reference
	}
	return ref != nil && ref.ReferenceLinkKind != ast.ReferenceLinkKindFull
}

func isCodeNode(n ast.Node) bool {
	switch n.Kind() {
	case ast.KindCodeBlock, ast.KindCodeSpan:
		return true
	}
	return false
}

// An interpolated URL or title is data inside Markdown syntax. Escape only
// the inserted bytes, leaving the author's delimiters and escapes intact.
type markdownEscapes [256]string

const markdownSyntax = "`*_[]<>~&\\|"

var literalKeyEscapes = func() (escapes markdownEscapes) {
	for _, ch := range markdownSyntax {
		escapes[ch] = "&#" + strconv.Itoa(int(ch)) + ";"
	}
	return escapes
}()

// Reference labels compare source spellings. Use the same escapes on the
// definition and each use so brackets and backslashes still match.
var referenceEscapes = markdownEscapes{'[': `\[`, ']': `\]`, '\\': `\\`, '\n': "&#10;", '\r': "&#13;"}

var destinationEscapes = markdownEscapes{
	' ': "%20", '\t': "%09", '\n': "%0A", '\r': "%0D",
	'(': `\(`, ')': `\)`, '<': `\<`, '>': `\>`, '\\': `\\`, '&': `\&`,
}

var titleEscapes = markdownEscapes{
	'"': `\"`, '\'': `\'`, '(': `\(`, ')': `\)`, '\\': `\\`, '&': `\&`,
	'\n': "&#10;", '\r': "&#13;",
}

// CommonMark autolinks do not interpret backslash escapes.
var autolinkEscapes = markdownEscapes{
	' ': "%20", '\t': "%09", '\n': "%0A", '\r': "%0D", '<': "%3C", '>': "%3E",
}

func (escapes *markdownEscapes) escape(ctx context.Context, value string, remaining int64) (string, error) {
	var result strings.Builder
	start := 0
	remaining -= int64(len(value))
	for i := 0; i < len(value); i++ {
		if i&1023 == 0 {
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}
		escaped := escapes[value[i]]
		if escaped == "" {
			continue
		}
		remaining -= int64(len(escaped) - 1)
		if remaining < 0 {
			return "", errors.New("Markdown substitution expansion limit exceeded")
		}
		result.WriteString(value[start:i])
		result.WriteString(escaped)
		start = i + 1
	}
	if start == 0 {
		return value, nil
	}
	result.WriteString(value[start:])
	return result.String(), nil
}

type markdownSubstitutionState struct {
	next          map[byte]int
	fail          int
	outputLink    int
	terminal      bool
	patternLength int
	value         string
}

type markdownSubstitutionMatcher struct {
	states       []markdownSubstitutionState
	resolve      func(string) (string, bool)
	source       string
	references   parser.Context
	segmentStart int
}

func newMarkdownSubstitutionMatcher(ctx context.Context, vars map[string]string) (*markdownSubstitutionMatcher, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	matcher := &markdownSubstitutionMatcher{
		states: []markdownSubstitutionState{{outputLink: -1}},
	}
	// Build one byte-oriented Aho-Corasick matcher for the whole Markdown
	// document. Complete ${key} patterns are necessary because quoted D2
	// variable names may themselves contain a closing brace.
	states := matcher.states
	for key, value := range vars {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pattern := "${" + key + "}"
		state := 0
		for i := 0; i < len(pattern); i++ {
			if i&1023 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			next, ok := states[state].next[pattern[i]]
			if !ok {
				next = len(states)
				if states[state].next == nil {
					states[state].next = make(map[byte]int)
				}
				states[state].next[pattern[i]] = next
				states = append(states, markdownSubstitutionState{outputLink: -1})
			}
			state = next
		}
		states[state].terminal = true
		states[state].patternLength = len(pattern)
		states[state].value = value
	}

	queue := make([]int, 0, len(states)-1)
	for _, next := range states[0].next {
		queue = append(queue, next)
	}
	for head := 0; head < len(queue); head++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		state := queue[head]
		for b, next := range states[state].next {
			failure := states[state].fail
			for failure != 0 {
				if _, ok := states[failure].next[b]; ok {
					break
				}
				failure = states[failure].fail
			}
			if candidate, ok := states[failure].next[b]; ok {
				states[next].fail = candidate
			}
			failure = states[next].fail
			if states[failure].terminal {
				states[next].outputLink = failure
			} else {
				states[next].outputLink = states[failure].outputLink
			}
			queue = append(queue, next)
		}
	}
	matcher.states = states
	return matcher, nil
}

// An unresolved shortcut can become a link to a literal definition after
// interpolation. Escape brackets only if that produces a defined reference.
func (matcher *markdownSubstitutionMatcher) matchesLiteralReference(ctx context.Context, start, stop int, value string) bool {
	start += matcher.segmentStart
	stop += matcher.segmentStart
	if matcher.references == nil || start == 0 || stop >= len(matcher.source) || matcher.source[start-1] != '[' || matcher.source[stop] != ']' {
		return false
	}
	if len(value) > 999 || !strings.ContainsAny(value, `[]\`) {
		return false
	}
	// CommonMark limits reference labels to 999 characters.
	escaped, err := referenceEscapes.escape(ctx, value, 999)
	if err != nil {
		return false
	}
	_, found := matcher.references.LinkDefinition(util.ToLinkReference([]byte(escaped)))
	return found
}

func escapedMarkdownDollar(s string, i int) bool {
	start := i
	for start > 0 && s[start-1] == '\\' {
		start--
	}
	return (i-start)%2 != 0
}

// Keep punctuation in a literal placeholder through Markdown rendering and
// import replay. Simple names can retain their original Markdown spelling.
func escapedMarkdownReference(ctx context.Context, key string, remaining int64) (string, error) {
	if !strings.ContainsAny(key, markdownSyntax) {
		return "", nil
	}
	const prefix = "&#36;{"
	remaining -= int64(len(prefix) + 1)
	if int64(len(key)) > remaining {
		return "", errors.New("Markdown substitution expansion limit exceeded")
	}
	key, err := literalKeyEscapes.escape(ctx, key, remaining)
	if err != nil {
		return "", err
	}
	return prefix + key + "}", nil
}

func (matcher *markdownSubstitutionMatcher) replaceBounded(ctx context.Context, s string, maxExpansion int64, escapes *markdownEscapes) (string, int64, error) {
	type replacement struct {
		start int
		stop  int
		value string
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if maxExpansion < 0 {
		return "", 0, errors.New("negative Markdown substitution expansion limit")
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	if len(s) == 0 || matcher == nil || len(matcher.states) == 0 {
		return s, 0, nil
	}
	states := matcher.states
	var bestAtStart map[int]replacement
	var used int64
	state := 0
	for i := 0; i < len(s); i++ {
		if i&1023 == 0 {
			if err := ctx.Err(); err != nil {
				return "", 0, err
			}
		}
		b := s[i]
		for state != 0 {
			if _, ok := states[state].next[b]; ok {
				break
			}
			state = states[state].fail
		}
		if next, ok := states[state].next[b]; ok {
			state = next
		} else {
			state = 0
		}

		for candidate := state; candidate != -1; candidate = states[candidate].outputLink {
			match := states[candidate]
			if !match.terminal {
				continue
			}
			if used >= maxExpansion {
				return "", 0, errors.New("Markdown substitution expansion limit exceeded")
			}
			used++
			if used&1023 == 0 {
				if err := ctx.Err(); err != nil {
					return "", 0, err
				}
			}
			start := i + 1 - match.patternLength
			if start < 0 {
				continue
			}
			stop := i + 1
			existing, ok := bestAtStart[start]
			if !ok || stop-start > existing.stop-existing.start {
				if bestAtStart == nil {
					bestAtStart = make(map[int]replacement)
				}
				bestAtStart[start] = replacement{start: start, stop: stop, value: match.value}
			}
		}
	}

	var replacements []replacement
	var nextReference int
	for i := 0; i < len(s); {
		if i&1023 == 0 {
			if err := ctx.Err(); err != nil {
				return "", 0, err
			}
		}
		if strings.HasPrefix(s[i:], "${") && escapedMarkdownDollar(s, i) {
			if (escapes == nil || escapes == &referenceEscapes) && i >= nextReference {
				end, next, err := markdownReferenceEnd(ctx, s, i+2)
				if err != nil {
					return "", 0, err
				}
				nextReference = next
				if end > 0 {
					literal, err := escapedMarkdownReference(ctx, s[i+2:end-1], maxExpansion-used)
					if err != nil {
						return "", 0, err
					}
					if literal != "" {
						replacements = append(replacements, replacement{start: i - 1, stop: end, value: literal})
						used += int64(len(literal))
					}
					i = end
					continue
				}
			}
			i++
			continue
		}
		match, found := bestAtStart[i]
		legacyEnd := match.stop
		if matcher.resolve != nil && i >= nextReference && strings.HasPrefix(s[i:], "${") {
			end, next, err := markdownReferenceEnd(ctx, s, i+2)
			if err != nil {
				return "", 0, err
			}
			nextReference = next
			if end > match.stop {
				if value, ok := matcher.resolve(s[i+2 : end-1]); ok {
					if used >= maxExpansion {
						return "", 0, errors.New("Markdown substitution expansion limit exceeded")
					}
					used++
					match = replacement{start: i, stop: end, value: value}
					found = true
				}
			}
		}
		if !found {
			i++
			continue
		}
		// Exact literal matches already paid their candidate cost above.
		if matcher.resolve != nil && match.stop == legacyEnd {
			if value, ok := matcher.resolve(s[i+2 : match.stop-1]); ok {
				match.value = value
			}
		}
		if int64(len(match.value)) > maxExpansion-used {
			return "", 0, errors.New("Markdown substitution expansion limit exceeded")
		}
		matchEscapes := escapes
		if matchEscapes == nil && matcher.matchesLiteralReference(ctx, match.start, match.stop, match.value) {
			matchEscapes = &referenceEscapes
		}
		if matchEscapes != nil && match.value != s[match.start:match.stop] {
			var err error
			match.value, err = matchEscapes.escape(ctx, match.value, maxExpansion-used)
			if err != nil {
				return "", 0, err
			}
		}
		used += int64(len(match.value))
		replacements = append(replacements, match)
		i = match.stop
	}
	if len(replacements) == 0 {
		return s, 0, nil
	}

	resultLen := len(s)
	const maxInt = int(^uint(0) >> 1)
	for _, replacement := range replacements {
		delta := len(replacement.value) - (replacement.stop - replacement.start)
		if delta > 0 && resultLen > maxInt-delta {
			return "", 0, errors.New("Markdown substitution result is too large")
		}
		resultLen += delta
	}
	var result strings.Builder
	result.Grow(resultLen)
	written := 0
	for _, replacement := range replacements {
		if err := ctx.Err(); err != nil {
			return "", 0, err
		}
		result.WriteString(s[written:replacement.start])
		result.WriteString(replacement.value)
		written = replacement.stop
	}
	result.WriteString(s[written:])
	return result.String(), used, nil
}

// markdownReferenceEnd locates a closing brace outside quoted or escaped key
// segments. The resolver validates the key path. next lets the caller continue
// in one forward pass even when the candidate is incomplete.
func markdownReferenceEnd(ctx context.Context, s string, start int) (end, next int, err error) {
	var quote byte
	segmentStart := true
	nextCheck := start
	for i := start; i < len(s); i++ {
		if i >= nextCheck {
			nextCheck = i + 1024
			if err := ctx.Err(); err != nil {
				return 0, 0, err
			}
		}
		c := s[i]
		if c == '\n' {
			return 0, i + 1, nil
		}
		if c == '\\' && quote != '\'' {
			i++
			segmentStart = false
			continue
		}
		if quote != 0 {
			if c == quote {
				if quote == '\'' && i+1 < len(s) && s[i+1] == '\'' {
					i++
				} else {
					quote = 0
				}
			}
			continue
		}
		switch c {
		case '}':
			return i + 1, i + 1, nil
		case '{':
			if i > start && s[i-1] == '$' {
				return 0, i - 1, nil
			}
			return 0, i + 1, nil
		case '.':
			segmentStart = true
		case '"', '\'':
			if segmentStart {
				quote = c
			}
			segmentStart = false
		default:
			if segmentStart {
				r, size := utf8.DecodeRuneInString(s[i:])
				segmentStart = unicode.IsSpace(r)
				i += size - 1
			}
		}
	}
	return 0, len(s), nil
}
