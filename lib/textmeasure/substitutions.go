package textmeasure

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark/v2/ast"
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

// ResolveSubstitutionsMarkdownBounded resolves D2 references in Markdown prose.
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
	if (len(variables) == 0 && resolve == nil) || !strings.Contains(mdText, "${") {
		return mdText, 0, nil
	}
	matcher, err := newMarkdownSubstitutionMatcher(ctx, variables)
	if err != nil {
		return "", 0, err
	}
	matcher.resolve = resolve
	source := []byte(mdText)
	doc := markdownParser.Parse(source)

	type substitution struct {
		start  int
		stop   int
		newVal string
	}
	var substitutions []substitution
	var expansion int64
	var walkErr error

	err = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if err := ctx.Err(); err != nil {
			return ast.WalkStop, err
		}
		if !entering {
			return ast.WalkContinue, nil
		}

		if isCodeNode(n) {
			return ast.WalkSkipChildren, nil
		}

		// Replacements need byte offsets in the original Markdown source.
		if textNode, ok := n.(*ast.Text); ok && !textNode.Value.IsOwned() {
			segment := textNode.Value.Index()
			originalText := textNode.Value.Str(source)
			newText, used, err := matcher.replaceBounded(ctx, originalText, maxExpansion-expansion)
			if err != nil {
				walkErr = err
				return ast.WalkStop, err
			}
			expansion += used

			if originalText != newText {
				substitutions = append(substitutions, substitution{
					start:  segment.Start,
					stop:   segment.Stop,
					newVal: newText,
				})
			}
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		if walkErr != nil {
			return "", 0, walkErr
		}
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

func isCodeNode(n ast.Node) bool {
	switch n.Kind() {
	case ast.KindCodeBlock, ast.KindCodeSpan:
		return true
	}
	return false
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
	states  []markdownSubstitutionState
	resolve func(string) (string, bool)
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

func (matcher *markdownSubstitutionMatcher) replaceBounded(ctx context.Context, s string, maxExpansion int64) (string, int64, error) {
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
