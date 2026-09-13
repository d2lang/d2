package textmeasure

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestReplaceSubstitutionsMarkdownBounded(t *testing.T) {
	t.Parallel()

	input := "before ${x} `code ${x}`\n\n```\n${x}\n```\nafter ${x}"
	want := "before value `code ${x}`\n\n```\n${x}\n```\nafter value"
	got, used, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), input, map[string]string{"x": "value"}, 12)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("replacement = %q, want %q", got, want)
	}
	if used != 12 {
		t.Fatalf("replacement work = %d, want 12", used)
	}

	got, _, err = ReplaceSubstitutionsMarkdownBounded(context.Background(), "${x}", map[string]string{
		"x": strings.Repeat("x", 1<<20),
	}, 32)
	if err == nil || got != "" {
		t.Fatalf("oversized replacement = (%q, %v), want rejection before result", got, err)
	}

	got, used, err = ReplaceSubstitutionsMarkdownBounded(context.Background(), "${x} ${missing}", map[string]string{
		"x": "${y}",
		"y": "resolved",
	}, 32)
	if err != nil {
		t.Fatal(err)
	}
	if got != "${y} ${missing}" || used != 5 {
		t.Fatalf("single-pass replacement = (%q, %d), want (%q, 5)", got, used, "${y} ${missing}")
	}

	got, used, err = ReplaceSubstitutionsMarkdownBounded(context.Background(), "${a}b}", map[string]string{
		"a}b": "value",
	}, 6)
	if err != nil {
		t.Fatal(err)
	}
	if got != "value" || used != 6 {
		t.Fatalf("quoted-key replacement = (%q, %d), want (%q, 6)", got, used, "value")
	}

	got, used, err = ReplaceSubstitutionsMarkdownBounded(context.Background(), "${a}b}", map[string]string{
		"a":   "short",
		"a}b": "long",
	}, 6)
	if err != nil {
		t.Fatal(err)
	}
	if got != "long" || used != 6 {
		t.Fatalf("overlapping replacement = (%q, %d), want (%q, 6)", got, used, "long")
	}
}

func TestMarkdownMatcherBoundsSuffixCandidateWork(t *testing.T) {
	t.Parallel()

	variables := make(map[string]string, 128)
	for i := 1; i <= 128; i++ {
		variables["a"+strings.Repeat("}${a", i-1)] = ""
	}
	input := strings.Repeat("${a}", 1_024)
	_, _, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), input, variables, 512)
	if err == nil || !strings.Contains(err.Error(), "Markdown substitution expansion limit exceeded") {
		t.Fatalf("suffix candidate work error = %v, want expansion limit", err)
	}

	got, used, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), "${a}", map[string]string{"a": ""}, 1)
	if err != nil {
		t.Fatalf("exact candidate boundary rejected: %v", err)
	}
	if got != "" || used != 1 {
		t.Fatalf("exact candidate boundary = (%q, %d), want empty output and 1 work unit", got, used)
	}
}

func TestReplaceSubstitutionsMarkdownChargesIdentityReplacementWork(t *testing.T) {
	t.Parallel()

	got, used, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), "${x}", map[string]string{"x": "${x}"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if got != "${x}" || used != 5 {
		t.Fatalf("identity replacement = (%q, %d), want (%q, 5)", got, used, "${x}")
	}
	if _, _, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), "${x}", map[string]string{"x": "${x}"}, 4); err == nil {
		t.Fatal("identity replacement succeeded below its work boundary")
	}
}

func TestReplaceSubstitutionsMarkdownLeavesUnknownPlaceholdersUnchanged(t *testing.T) {
	t.Parallel()

	var input strings.Builder
	for i := 0; i < 32; i++ {
		fmt.Fprintf(&input, "${unknown-%d} ", i)
	}
	want := input.String()
	got, used, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), want, map[string]string{"known": "value"}, 32)
	if err != nil {
		t.Fatal(err)
	}
	if got != want || used != 0 {
		t.Fatalf("unknown placeholders = (%q, %d), want unchanged input", got, used)
	}
}

func TestReplaceSubstitutionsMarkdownIgnoresUnreferencedVariablesInLinearTime(t *testing.T) {
	t.Parallel()

	variables := make(map[string]string, 10_000)
	for i := 0; i < 10_000; i++ {
		variables[fmt.Sprintf("unused-%d", i)] = ""
	}
	ctx := &markdownCancelAfterErrContext{Context: context.Background(), cancelAt: 100}
	got, used, err := ReplaceSubstitutionsMarkdownBounded(ctx, "no substitutions here", variables, 32)
	if err != nil {
		t.Fatalf("unreferenced variables consumed scan work: %v", err)
	}
	if got != "no substitutions here" || used != 0 {
		t.Fatalf("replacement = (%q, %d), want unchanged input", got, used)
	}
	if ctx.calls >= ctx.cancelAt {
		t.Fatalf("context checked %d times for unreferenced variables, want fewer than %d", ctx.calls, ctx.cancelAt)
	}
}

func TestMarkdownMatcherNoMatchStorageIsSparse(t *testing.T) {
	matcher, err := newMarkdownSubstitutionMatcher(context.Background(), map[string]string{
		"one":   "1",
		"two":   "2",
		"three": "3",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range matcher.states {
		if state.terminal && state.next != nil {
			t.Fatal("terminal trie state eagerly allocated a transition map")
		}
	}

	input := "${unknown} " + strings.Repeat("plain text without matches ", 1<<15)
	allocations := testing.AllocsPerRun(5, func() {
		got, used, err := matcher.replaceBounded(context.Background(), input, 32)
		if err != nil || got != input || used != 0 {
			panic("unexpected no-match result")
		}
	})
	if allocations > 1 {
		t.Fatalf("no-match scan allocated %.1f objects, want at most 1 independent of source length", allocations)
	}
}

func TestReplaceSubstitutionsMarkdownBoundedCancellation(t *testing.T) {
	t.Parallel()

	ctx := &markdownCancelAfterErrContext{Context: context.Background(), cancelAt: 5}
	_, _, err := ReplaceSubstitutionsMarkdownBounded(ctx, "${x}", map[string]string{"x": "value"}, 32)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("replacement error = %v, want context.Canceled", err)
	}
	if ctx.calls < ctx.cancelAt {
		t.Fatalf("context Err called %d times, want at least %d", ctx.calls, ctx.cancelAt)
	}
}

func TestReplaceSubstitutionsMarkdownMatchesManyPatternsInLinearTime(t *testing.T) {
	const helperEnv = "D2_MARKDOWN_SUBSTITUTION_COMPLEXITY_HELPER"
	if os.Getenv(helperEnv) == "1" {
		const count = 10_000
		variables := make(map[string]string, count)
		var input strings.Builder
		for i := 0; i < count; i++ {
			variables[fmt.Sprintf("known-%d", i)] = "value"
			fmt.Fprintf(&input, "${unknown-%d} *separator* ", i)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		want := input.String()
		got, used, err := ReplaceSubstitutionsMarkdownBounded(ctx, want, variables, 32)
		if err != nil {
			t.Fatal(err)
		}
		if got != want || used != 0 {
			t.Fatalf("many-pattern replacement used %d bytes or changed unknown placeholders", used)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReplaceSubstitutionsMarkdownMatchesManyPatternsInLinearTime$")
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("many-pattern Markdown substitution exceeded bounded runtime: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("many-pattern Markdown substitution failed: %v\n%s", err, output)
	}
}

type markdownCancelAfterErrContext struct {
	context.Context
	calls    int
	cancelAt int
}

func (c *markdownCancelAfterErrContext) Err() error {
	c.calls++
	if c.calls >= c.cancelAt {
		return context.Canceled
	}
	return nil
}
