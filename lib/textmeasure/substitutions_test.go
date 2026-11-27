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
		got, used, err := matcher.replaceBounded(context.Background(), input, 32, nil)
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

func TestResolveSubstitutionsMarkdownBounded(t *testing.T) {
	t.Parallel()
	const input = "${ENV} / ${cfg.\"a}b\"} / ${'can''t}stop'} / ${don't} / ${a}b} / ${missing}\n\n`code ${ENV}`\n\n```\n${ENV}\n```"
	const want = "local / dotted / quoted / bare / longest / ${missing}\n\n`code ${ENV}`\n\n```\n${ENV}\n```"
	values := map[string]string{"ENV": "local", `cfg."a}b"`: "dotted", "'can''t}stop'": "quoted", "don't": "bare", "a}b": "longest"}
	visited := map[string]int{}
	resolve := func(key string) (string, bool) {
		visited[key]++
		value, ok := values[key]
		return value, ok
	}
	got, used, err := ResolveSubstitutionsMarkdownBounded(context.Background(), input,
		map[string]string{"ENV": "outer", "a": "short", "a}b": "legacy"}, 100, resolve)
	if err != nil || got != want {
		t.Fatalf("result = %q, %v; want %q", got, err, want)
	}
	if used != int64(6+len("localdottedquotedbarelongest")) {
		t.Errorf("work = %d", used)
	}
	if visited["ENV"] != 1 || visited["a"] != 0 || visited["a}b"] != 1 {
		t.Errorf("resolved keys = %v", visited)
	}
}

func TestMarkdownReferenceBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ input, want string }{
		{`${ "some } name" }`, ` "some } name" `},
		{`${ 'some}name'}`, ` 'some}name'`},
		{`${its"name}after`, `its"name`},
		{`${a\}b}`, `a\}b`},
		{`${"escaped\"}quote"}`, `"escaped\"}quote"`},
		{`${'slash\'}`, `'slash\'`},
	} {
		end, next, err := markdownReferenceEnd(context.Background(), tc.input, 2)
		if err != nil || end <= 2 || next != end || tc.input[2:end-1] != tc.want {
			t.Errorf("boundaries of %q = (%d, %d, %v), want %q", tc.input, end, next, err, tc.want)
		}
	}
}

func TestResolveSubstitutionsMarkdownBudgetAndMalformedReferences(t *testing.T) {
	t.Parallel()
	resolve := func(key string) (string, bool) { return "value", key == "x" }
	got, used, err := ResolveSubstitutionsMarkdownBounded(context.Background(), "${unfinished ${x} ${missing}", nil, 6, resolve)
	if err != nil || got != "${unfinished value ${missing}" || used != 6 {
		t.Fatalf("result = %q, work %d, error %v", got, used, err)
	}
	_, _, err = ResolveSubstitutionsMarkdownBounded(context.Background(), "${x}", nil, 5, resolve)
	if err == nil {
		t.Fatal("expected expansion limit")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = ResolveSubstitutionsMarkdownBounded(ctx, "${x}", nil, 6, resolve)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("result error = %v, want context.Canceled", err)
	}
}

func TestMarkdownLinkSubstitutions(t *testing.T) {
	t.Parallel()
	const input = "[${name}](${url} '${name}')\n\n" +
		"[first][ref] and ![second][ref]\n\n[ref]: <${url}> \"multi\n${name}\"\n\n" +
		"![alt](${url}) <https://example.com/${name}> [unknown](${missing})\n\n" +
		"`[code](${url})`\n\n```\n[code](${url})\n```"
	const want = "[Guide](https://example.com/guide 'Guide')\n\n" +
		"[first][ref] and ![second][ref]\n\n[ref]: <https://example.com/guide> \"multi\nGuide\"\n\n" +
		"![alt](https://example.com/guide) <https://example.com/Guide> [unknown](${missing})\n\n" +
		"`[code](${url})`\n\n```\n[code](${url})\n```"
	vars := map[string]string{"url": "https://example.com/guide", "name": "Guide"}
	// The reference destination and title each cost one substitution even with two uses.
	budget := int64(3*(1+len(vars["url"])) + 4*(1+len(vars["name"])))
	got, used, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), input, vars, budget)
	if err != nil || got != want || used != budget {
		t.Fatalf("result = %q, work = %d, error = %v; want %q, work %d", got, used, err, want, budget)
	}
	if _, _, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), input, vars, budget-1); err == nil {
		t.Fatal("expected link substitutions to honor the expansion limit")
	}
}

func TestMarkdownLinkSubstitutionsPreserveURLAndTitle(t *testing.T) {
	t.Parallel()
	const input = `[Guide](${url} '${title}')`
	values := map[string]string{
		"url":   "https://example.com/My Guide)/v1?q=1&copy;=2",
		"title": "What's new (v1)",
	}
	out, used, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), input, values, 200)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := RenderMarkdown(out)
	if err != nil {
		t.Fatal(err)
	}
	if want := `<a href="https://example.com/My%20Guide)/v1?q=1&amp;copy;=2" title="What's new (v1)">Guide</a>`; !strings.Contains(rendered, want) {
		t.Errorf("rendered %q, want %q", rendered, want)
	}
	if _, _, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), input, values, used-1); err == nil {
		t.Fatal("escaped bytes must count toward the expansion limit")
	}
}

func TestMarkdownReferenceLabelSubstitutions(t *testing.T) {
	t.Parallel()
	const input = "[${name}], [${name}][], [guide][${name}], ![alt][${name}]\n\n[${name}]: /guide"
	for _, name := range []string{"Runbook", "Build [v2]", `A\B`} {
		out, used, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), input, map[string]string{"name": name}, 1000)
		if err != nil {
			t.Fatal(err)
		}
		rendered, err := RenderMarkdown(out)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(rendered, `href="/guide"`) != 3 || !strings.Contains(rendered, `src="/guide"`) {
			t.Errorf("substituting %q produced %s from %q", name, rendered, out)
		}
		if _, _, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), input, map[string]string{"name": name}, used-1); err == nil {
			t.Fatal("reference labels must respect the expansion limit")
		}
	}
}

func TestMarkdownEscapedSubstitution(t *testing.T) {
	t.Parallel()
	const input = "\\${env} \\\\${env} \\\\\\${env} ${env} `\\${env}`"
	const want = `<p>${env} \staging \${env} staging <code>\${env}</code></p>`
	out, _, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), input, map[string]string{"env": "staging"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := RenderMarkdown(out)
	if err != nil || strings.TrimSpace(rendered) != want {
		t.Errorf("rendered %q, %v; want %q", rendered, err, want)
	}
}

func TestMarkdownPunctuationInVariableKeys(t *testing.T) {
	t.Parallel()
	const input = "**${'build[prod]'}** ${__version__} `[${__version__}]`\n\n```\n${'build[prod]'}\n```"
	const want = "**ready** v2.4 `[${__version__}]`\n\n```\n${'build[prod]'}\n```"
	keys := map[string]string{"'build[prod]'": "ready", "__version__": "v2.4"}
	out, used, err := ResolveSubstitutionsMarkdownBounded(context.Background(), input, nil, 11, func(key string) (string, bool) { val, ok := keys[key]; return val, ok })
	if err != nil || out != want || used != 11 {
		t.Errorf("substitution = %q, %d, %v; want %q, 11", out, used, err, want)
	}
}

func TestMarkdownReferenceKeysPreserveInlineCode(t *testing.T) {
	input := "[${env} `${env}`] and [explicit][${env} `${env}`]\n\n[${env} `${env}`]: /guide"
	out := ReplaceSubstitutionsMarkdown(input, map[string]string{"env": "stage"})
	rendered, err := RenderMarkdown(out)
	if err != nil || strings.Count(rendered, `href="/guide"`) != 2 || !strings.Contains(rendered, "stage <code>${env}</code>") {
		t.Errorf("rendering %q gave %q, %v", out, rendered, err)
	}
}

func TestMarkdownLiteralPlaceholderAndReferenceLookup(t *testing.T) {
	for _, tc := range []struct{ input, value, want string }{
		{`\${__name__}`, "replaced", `<p>${__name__}</p>`},
		{"[${name}]\n\n[Run \\[v2\\]]: /go", "Run [v2]", `<p><a href="/go">Run [v2]</a></p>`},
		{"${name}\n\n[Run \\[v2\\]]: /go", "[go](/other)", `<p><a href="/other">go</a></p>`},
	} {
		out := ReplaceSubstitutionsMarkdown(tc.input, map[string]string{"name": tc.value, "__name__": tc.value})
		rendered, err := RenderMarkdown(out)
		if err != nil || strings.TrimSpace(rendered) != tc.want {
			t.Errorf("%q rendered as %q from %q, want %q, err %v", tc.input, rendered, out, tc.want, err)
		}
	}
	out, used, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), `\${__name__}`, nil, 200)
	if err != nil || used == 0 {
		t.Fatalf("escaped literal returned %q, %d, %v", out, used, err)
	}
	if _, _, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), `\${__name__}`, nil, used-1); err == nil {
		t.Fatal("escaped literal must respect work limit")
	}
}

func TestMarkdownNestedReferenceImage(t *testing.T) {
	input := "[![${name}][]][${page}]\n\n[${name}]: /image\n[${page}]: /guide"
	out := ReplaceSubstitutionsMarkdown(input, map[string]string{"name": "Run [v2]", "page": "Guide [v2]"})
	rendered, err := RenderMarkdown(out)
	if err != nil || !strings.Contains(rendered, `<a href="/guide"><img src="/image" alt="Run [v2]"`) {
		t.Errorf("nested reference image rendered %q from %q, %v", rendered, out, err)
	}
}

func TestMarkdownReferenceIdentity(t *testing.T) {
	const defs = "[${first}]: /api \"API guide\"\n[${second}]: /ops \"Ops guide\"\n[runbook]: /static"
	vars := map[string]string{"first": "Runbook", "second": "RUNBOOK", "suffix": "!"}
	for _, tc := range []struct{ name, use, want string }{
		{"shortcut", `[${first}]${suffix} [${second}] [runbook]`, `<a href="/api" title="API guide">Runbook</a>! <a href="/ops" title="Ops guide">RUNBOOK</a> <a href="/static">runbook</a>`},
		{"collapsed", `[${first}][] [${second}][ ] [runbook][]`, `<a href="/api" title="API guide">Runbook</a> <a href="/ops" title="Ops guide">RUNBOOK</a> <a href="/static">runbook</a>`},
		{"full", `[API][${first}] [Ops][${second}] [static][runbook]`, `<a href="/api" title="API guide">API</a> <a href="/ops" title="Ops guide">Ops</a> <a href="/static">static</a>`},
		{"images", `![${first}][] ![alt][${second}]`, `<img src="/api" alt="Runbook" title="API guide" /> <img src="/ops" alt="alt" title="Ops guide" />`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, input := range []string{tc.use + "\n\n" + defs, defs + "\n\n" + tc.use} {
				out, used, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), input, vars, 1024)
				rendered, renderErr := RenderMarkdown(out)
				if err != nil || renderErr != nil || !strings.Contains(rendered, tc.want) {
					t.Fatalf("rendering %q gave %q, errors: %v, %v", out, rendered, err, renderErr)
				}
				if used > 0 {
					if _, _, err := ReplaceSubstitutionsMarkdownBounded(context.Background(), input, vars, used-1); err == nil {
						t.Fatal("a reference must fit within the expansion budget")
					}
				}
			}
		})
	}
}

func TestMultilineMarkdownReferenceIdentity(t *testing.T) {
	for _, tail := range []string{"", "[]", "[\n> ]"} {
		input := "> [Launch\n> ${stage}]" + tail + "\n\n> [Launch\n> ${stage}]: /guide"
		out := ReplaceSubstitutionsMarkdown(input, map[string]string{"stage": "prod"})
		rendered, err := RenderMarkdown(out)
		if err != nil || !strings.Contains(rendered, `href="/guide"`) || !strings.Contains(rendered, "prod</a>") {
			t.Fatalf("reference in a block quote: %q rendered as %q, %v", out, rendered, err)
		}
	}
}
