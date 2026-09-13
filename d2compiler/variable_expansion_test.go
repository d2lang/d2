package d2compiler_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
)

func TestVariableExpansionLimits(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		dsl   string
		limit int64
	}{
		{
			name:  "unquoted scalar bytes",
			dsl:   "vars: {x: 12345678}\nout: ${x}${x}${x}${x}${x}",
			limit: 32,
		},
		{
			name:  "whole scalar bytes",
			dsl:   "vars: {x: 12345678}\none: ${x}\ntwo: ${x}",
			limit: 8,
		},
		{
			name:  "quoted scalar bytes",
			dsl:   "vars: {x: 12345678}\nout: \"${x}${x}${x}${x}${x}\"",
			limit: 32,
		},
		{
			name:  "Markdown scalar bytes",
			dsl:   "vars: {x: 12345678}\nout: |md\n${x}${x}${x}${x}${x}\n|",
			limit: 32,
		},
		{
			name:  "array members",
			dsl:   "vars: {x: [a; b; c; d]}\nout.class: [...${x}; ...${x}; ...${x}]",
			limit: 8,
		},
		{
			name:  "map spread copy",
			dsl:   "vars: {x: {a; b; c; d}}\nout: {...${x}}",
			limit: 4,
		},
		{
			name:  "referenced composite DAG",
			dsl:   compositeAliasSource(7, true, ""),
			limit: 64,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := d2compiler.Compile("expansion.d2", strings.NewReader(tc.dsl), &d2compiler.CompileOptions{
				MaxVariableExpansion: tc.limit,
			})
			assertVariableExpansionError(t, err, tc.limit)
		})
	}
}

func TestVariableExpansionDefaultLimit(t *testing.T) {
	t.Parallel()

	value := strings.Repeat("x", 32_769)
	_, _, err := d2compiler.Compile(
		"expansion-default.d2",
		strings.NewReader(fmt.Sprintf("vars: {x: %s}\nout: ${x}${x}", value)),
		nil,
	)
	assertVariableExpansionError(t, err, 65_536)
}

func TestVariableExpansionBoundaryAndValidation(t *testing.T) {
	t.Parallel()

	const source = "vars: {x: 12345678}\nout: ${x}${x}${x}${x}"
	if _, _, err := d2compiler.Compile("expansion-boundary.d2", strings.NewReader(source), &d2compiler.CompileOptions{
		MaxVariableExpansion: 32,
	}); err != nil {
		t.Fatalf("exact-boundary expansion rejected: %v", err)
	}

	_, _, err := d2compiler.Compile("expansion-negative.d2", strings.NewReader("x"), &d2compiler.CompileOptions{
		MaxVariableExpansion: -1,
	})
	if err == nil || !strings.Contains(err.Error(), "MaxVariableExpansion must not be negative") {
		t.Fatalf("negative limit error = %v", err)
	}
}

func TestUnusedCompositeAliasBoardCopiesAreBounded(t *testing.T) {
	t.Parallel()

	for _, boardKind := range []string{"layers", "scenarios", "steps"} {
		boardKind := boardKind
		t.Run(boardKind, func(t *testing.T) {
			t.Parallel()
			boards := fmt.Sprintf("%s: {first: {a}; second: {b}}", boardKind)
			_, _, err := d2compiler.Compile("board-control.d2", strings.NewReader(boards), &d2compiler.CompileOptions{
				MaxVariableExpansion: 256,
			})
			if err != nil {
				t.Fatalf("ordinary boards rejected: %v", err)
			}

			_, _, err = d2compiler.Compile(
				"board-expansion.d2",
				strings.NewReader(compositeAliasSource(8, false, boards)),
				&d2compiler.CompileOptions{MaxVariableExpansion: 256},
			)
			assertVariableExpansionError(t, err, 256)
		})
	}
}

func TestClassApplicationCartesianExpansionIsBounded(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	source.WriteString("vars: {\n  a0: {class: c}\n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&source, "  a%d: {left: ${a%d}; right: ${a%d}}\n", i, i-1, i-1)
	}
	source.WriteString("  b0: {label: leaf}\n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&source, "  b%d: {left: ${b%d}; right: ${b%d}}\n", i, i-1, i-1)
	}
	source.WriteString("}\nclasses: {c: ${b5}}\nout: ${a5}\n")

	_, _, err := d2compiler.Compile("class-expansion.d2", strings.NewReader(source.String()), &d2compiler.CompileOptions{
		MaxVariableExpansion: 2_048,
	})
	assertVariableExpansionError(t, err, 2_048)
}

func TestMarkdownNestedVariableNamesAreBoundedBeforeFlattening(t *testing.T) {
	t.Parallel()

	longName := strings.Repeat("x", 64)
	source := fmt.Sprintf("vars: {%s: {%s: {leaf: value}}}\nout: |md\n${%s.%s.leaf}\n|", longName, longName, longName, longName)
	_, _, err := d2compiler.Compile("markdown-variable-name.d2", strings.NewReader(source), &d2compiler.CompileOptions{
		MaxVariableExpansion: 64,
	})
	assertVariableExpansionError(t, err, 64)
}

func TestMarkdownVariableMatcherConstructionIsCumulativeAcrossBlocks(t *testing.T) {
	t.Parallel()

	var prefix strings.Builder
	prefix.WriteString("vars: {\n")
	for i := 0; i < 128; i++ {
		fmt.Fprintf(&prefix, "  variable-%03d: value\n", i)
	}
	prefix.WriteString("}\n")
	compile := func(blocks int) error {
		var source strings.Builder
		source.WriteString(prefix.String())
		for i := 0; i < blocks; i++ {
			fmt.Fprintf(&source, "out-%d: |md\n${unknown}\n|\n", i)
		}
		_, _, err := d2compiler.Compile("markdown-matcher-work.d2", strings.NewReader(source.String()), &d2compiler.CompileOptions{
			MaxVariableExpansion: 4_096,
		})
		return err
	}
	if err := compile(1); err != nil {
		t.Fatalf("single Markdown block control rejected: %v", err)
	}
	err := compile(4)
	assertVariableExpansionError(t, err, 4_096)

	var noPlaceholders strings.Builder
	noPlaceholders.WriteString(prefix.String())
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&noPlaceholders, "plain-%d: |md\nno variables here\n|\n", i)
	}
	if _, _, err := d2compiler.Compile("markdown-no-matcher-work.d2", strings.NewReader(noPlaceholders.String()), &d2compiler.CompileOptions{
		MaxVariableExpansion: 1,
	}); err != nil {
		t.Fatalf("Markdown without placeholders consumed matcher work: %v", err)
	}
}

func TestMarkdownSubstitutionSupportsQuotedVariableNamesWithClosingBraces(t *testing.T) {
	t.Parallel()

	const source = "vars:{ \"a}b\": value }\nout: |md\n${a}b}\n|"
	g, _, err := d2compiler.Compile("markdown-quoted-variable.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, ok := g.Root.HasChild([]string{"out"})
	if !ok {
		t.Fatal("compiled graph has no out object")
	}
	if got := strings.TrimSpace(out.Label.Value); got != "value" {
		t.Fatalf("out label = %q, want %q", got, "value")
	}
}

func compositeAliasSource(depth int, referenced bool, suffix string) string {
	var source strings.Builder
	source.WriteString("vars: {\n  v0: {leaf}\n")
	for i := 1; i <= depth; i++ {
		fmt.Fprintf(&source, "  v%d: {left: ${v%d}; right: ${v%d}}\n", i, i-1, i-1)
	}
	source.WriteString("}\n")
	if referenced {
		fmt.Fprintf(&source, "out: ${v%d}\n", depth)
	}
	if suffix != "" {
		source.WriteString(suffix)
		source.WriteByte('\n')
	}
	return source.String()
}

func assertVariableExpansionError(t *testing.T, err error, limit int64) {
	t.Helper()
	want := fmt.Sprintf("variable substitution expansion exceeds limit of %d work units", limit)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Compile error = %v, want error containing %q", err, want)
	}
}
