package textmeasure

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
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
	if ctx == nil {
		ctx = context.Background()
	}
	if maxExpansion < 0 {
		return "", 0, errors.New("negative Markdown substitution expansion limit")
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	if len(variables) == 0 || !strings.Contains(mdText, "${") {
		return mdText, 0, nil
	}
	matcher, err := newMarkdownSubstitutionMatcher(ctx, variables)
	if err != nil {
		return "", 0, err
	}
	source := []byte(mdText)
	reader := text.NewReader(source)
	doc := markdownRenderer.Parser().Parse(reader)

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

		if textNode, ok := n.(*ast.Text); ok {
			segment := textNode.Segment
			originalText := string(segment.Value(source))
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
	case ast.KindCodeBlock, ast.KindFencedCodeBlock, ast.KindCodeSpan:
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
	states []markdownSubstitutionState
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
	for i := 0; i < len(s); {
		if i&1023 == 0 {
			if err := ctx.Err(); err != nil {
				return "", 0, err
			}
		}
		replacement, ok := bestAtStart[i]
		if !ok {
			i++
			continue
		}
		if int64(len(replacement.value)) > maxExpansion-used {
			return "", 0, errors.New("Markdown substitution expansion limit exceeded")
		}
		used += int64(len(replacement.value))
		replacements = append(replacements, replacement)
		i = replacement.stop
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
