package d2ir

import (
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2parser"
)

func TestGlobExpansionBudgetDefault(t *testing.T) {
	budget, err := newGlobExpansionBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if budget.limit != DefaultMaxGlobExpansion {
		t.Fatalf("zero-value limit = %d, want %d", budget.limit, DefaultMaxGlobExpansion)
	}
	for range DefaultMaxGlobExpansion {
		if err := budget.reserve(1); err != nil {
			t.Fatal(err)
		}
	}
	if err := budget.reserve(1); err == nil {
		t.Fatal("reservation beyond the default glob expansion limit succeeded")
	}

	budget, err = newGlobExpansionBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	for range maxGlobCreatedFields {
		if err := budget.reserveField(); err != nil {
			t.Fatal(err)
		}
	}
	if err := budget.reserveField(); err == nil {
		t.Fatal("reservation beyond the glob-created field limit succeeded")
	}
}

func TestGlobExpansionBudgetDoesNotCountExplicitFields(t *testing.T) {
	var explicit strings.Builder
	for i := range 100 {
		fmt.Fprintf(&explicit, "field-%d\n", i)
	}
	ast, err := d2parser.Parse("explicit-fields.d2", strings.NewReader(explicit.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Compile(ast, &CompileOptions{MaxGlobExpansion: 1}); err != nil {
		t.Fatalf("explicit fields consumed the glob expansion budget: %v", err)
	}

}

func TestNormalGlobExpansionFitsDefaultWorkBudget(t *testing.T) {
	var source strings.Builder
	source.WriteString("*.style.fill: red\n")
	for i := range 2_000 {
		fmt.Fprintf(&source, "field-%d\n", i)
	}
	ast, err := d2parser.Parse("normal-glob.d2", strings.NewReader(source.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Compile(ast, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRecursiveGlobFeedbackIsBounded(t *testing.T) {
	const source = "**.a\n**.b\n**.c\nx\n"
	ast, err := d2parser.Parse("glob-feedback.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Compile(ast, &CompileOptions{MaxGlobExpansion: 64})
	want := "glob expansion exceeds limit of 64 work units"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Compile() error = %v, want %q", err, want)
	}
	if !strings.Contains(err.Error(), "glob-feedback.d2:") {
		t.Fatalf("Compile() error = %v, want source location", err)
	}
}

func TestTwoRuleRecursiveGlobFeedbackIsBoundedByDefault(t *testing.T) {
	const source = "**.a\n**.b\nx\n"
	ast, err := d2parser.Parse("two-rule-glob-feedback.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Compile(ast, nil)
	want := fmt.Sprintf("glob expansion exceeds limit of %d work units", DefaultMaxGlobExpansion)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Compile() error = %v, want %q", err, want)
	}
}

func TestGlobGeneratedSubstitutionPlaceholdersConsumeBudget(t *testing.T) {
	const limit = 16
	const onePlaceholder = "vars: {x: {p}}\n*.a: { ...${x} }\nz\n"
	ast, err := d2parser.Parse("one-glob-placeholder.d2", strings.NewReader(onePlaceholder), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Compile(ast, &CompileOptions{MaxGlobExpansion: limit}); err != nil {
		t.Fatalf("one-placeholder boundary failed: %v", err)
	}

	const twoPlaceholders = "vars: {x: {p}; y: {q}}\n*.a: { ...${x}; ...${y} }\nz\n"
	ast, err = d2parser.Parse("two-glob-placeholders.d2", strings.NewReader(twoPlaceholders), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Compile(ast, &CompileOptions{MaxGlobExpansion: limit})
	if err == nil || !strings.Contains(err.Error(), "glob expansion exceeds limit of 16 work units") {
		t.Fatalf("two-placeholder Compile() error = %v, want glob work limit", err)
	}
}

func TestTripleGlobSkipsUnresolvedSubstitutionPlaceholder(t *testing.T) {
	const source = "...${missing}\n***.a\nx\n"
	ast, err := d2parser.Parse("triple-glob-placeholder.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Compile(ast, nil)
	if err == nil || !strings.Contains(err.Error(), `could not resolve variable "missing"`) {
		t.Fatalf("Compile() error = %v, want unresolved-variable error", err)
	}
}

func TestLiteralGlobBodyWorkConsumesBudget(t *testing.T) {
	var source strings.Builder
	source.WriteString("*.a: {")
	for range 64 {
		source.WriteString(" b: v;")
	}
	source.WriteString(" }\nz\n")

	ast, err := d2parser.Parse("literal-glob-body.d2", strings.NewReader(source.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Compile(ast, &CompileOptions{MaxGlobExpansion: 64})
	if err == nil || !strings.Contains(err.Error(), "glob expansion exceeds limit of 64 work units") {
		t.Fatalf("Compile() error = %v, want glob work limit", err)
	}
}

func TestGlobArrayNodeWorkConsumesBudget(t *testing.T) {
	const array = "[[${x}]]"
	normalSource := "vars: {x: value}\nitems: " + array + "\n"
	ast, err := d2parser.Parse("normal-array.d2", strings.NewReader(normalSource), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Compile(ast, &CompileOptions{MaxGlobExpansion: 1}); err != nil {
		t.Fatalf("normal array consumed glob work budget: %v", err)
	}

	globSource := "vars: {x: value}\n*.items: " + array + "\nz\n"
	for _, tc := range []struct {
		limit    int64
		location string
	}{
		{limit: 6, location: "glob-array.d2:2:11"},
		{limit: 7, location: "glob-array.d2:2:12"},
	} {
		ast, err = d2parser.Parse("glob-array.d2", strings.NewReader(globSource), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = Compile(ast, &CompileOptions{MaxGlobExpansion: tc.limit})
		want := fmt.Sprintf("glob expansion exceeds limit of %d work units", tc.limit)
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), tc.location) {
			t.Fatalf("Compile() error = %v, want %q at %s", err, want, tc.location)
		}
	}

	ast, err = d2parser.Parse("glob-array.d2", strings.NewReader(globSource), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Compile(ast, &CompileOptions{MaxGlobExpansion: 12}); err != nil {
		t.Fatalf("exact glob-array work boundary failed: %v", err)
	}
}

func TestGlobExpansionRejectsNegativeLimit(t *testing.T) {
	ast, err := d2parser.Parse("glob-negative.d2", strings.NewReader("x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Compile(ast, &CompileOptions{MaxGlobExpansion: -1})
	if err == nil || err.Error() != "MaxGlobExpansion must not be negative" {
		t.Fatalf("Compile() error = %v, want negative-limit error", err)
	}
}

func TestDefaultRecursiveGlobFeedbackLimitMessage(t *testing.T) {
	const source = "**.a\n**.b\n**.c\nx\n"
	ast, err := d2parser.Parse("glob-feedback-default.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Compile(ast, nil)
	want := fmt.Sprintf("glob expansion exceeds limit of %d work units", DefaultMaxGlobExpansion)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Compile() error = %v, want %q", err, want)
	}
}
