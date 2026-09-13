package d2ir

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/d2lang/util-go/mapfs"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2parser"
)

func TestVariableExpansionBudget(t *testing.T) {
	t.Parallel()

	budget, err := newVariableExpansionBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if budget.limit != DefaultMaxVariableExpansion {
		t.Fatalf("zero-value limit = %d, want %d", budget.limit, DefaultMaxVariableExpansion)
	}
	if err := budget.reserve(DefaultMaxVariableExpansion); err != nil {
		t.Fatal(err)
	}
	if err := budget.reserve(1); err == nil {
		t.Fatal("reservation beyond limit succeeded")
	}
	if budget.used != DefaultMaxVariableExpansion {
		t.Fatalf("failed reservation changed used budget to %d", budget.used)
	}
	if _, err := newVariableExpansionBudget(-1); err == nil {
		t.Fatal("negative limit succeeded")
	}
	if err := ReserveVariableExpansionAliases(context.Background(), nil); err == nil {
		t.Fatal("nil IR map reservation succeeded")
	}
	if err := ReserveVariableExpansionCopy(context.Background(), nil); err == nil {
		t.Fatal("nil IR map copy reservation succeeded")
	}
}

func TestVariableExpansionCopyUnitsIncludeSlices(t *testing.T) {
	t.Parallel()

	m := &Map{Fields: make([]*Field, 2), Edges: make([]*Edge, 3)}
	if got := variableExpansionNodeUnits(m); got != 6 {
		t.Fatalf("map copy units = %d, want 6", got)
	}
	array := &Array{Values: make([]Value, 4)}
	if got := variableExpansionNodeUnits(array); got != 5 {
		t.Fatalf("array copy units = %d, want 5", got)
	}
	field := &Field{Name: d2ast.FlatUnquotedString("name"), References: make([]*FieldReference, 3)}
	if got := variableExpansionNodeUnits(field); got != 9 {
		t.Fatalf("field copy units = %d, want 9", got)
	}
	edge := &Edge{
		ID: &EdgeID{
			SrcPath: make([]d2ast.String, 2),
			DstPath: make([]d2ast.String, 3),
		},
		References: make([]*EdgeReference, 2),
	}
	if got := variableExpansionNodeUnits(edge); got != 8 {
		t.Fatalf("edge copy units = %d, want 8", got)
	}
	number := &Scalar{Value: &d2ast.Number{Raw: strings.Repeat("9", 32)}}
	if got := variableExpansionNodeUnits(number); got != 33 {
		t.Fatalf("number copy units = %d, want 33", got)
	}
	value := "abcdefgh"
	stringScalar := &Scalar{Value: &d2ast.UnquotedString{Value: []d2ast.InterpolationBox{{String: &value}}}}
	if got := variableExpansionNodeUnits(stringScalar); got != 10 {
		t.Fatalf("string copy units = %d, want 10", got)
	}
}

func TestVariableExpansionCopyReservationsAreCumulative(t *testing.T) {
	t.Parallel()

	ast, err := d2parser.Parse("copy-budget.d2", strings.NewReader("x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	ir, _, err := Compile(ast, &CompileOptions{MaxVariableExpansion: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := ReserveVariableExpansionCopy(context.Background(), ir); err != nil {
		t.Fatalf("first copy reservation: %v", err)
	}
	if err := ReserveVariableExpansionCopy(context.Background(), ir); err == nil {
		t.Fatal("second copy reservation reset the compilation budget")
	}
}

func TestVariableExpansionCustomLimitAboveDefault(t *testing.T) {
	t.Parallel()

	value := strings.Repeat("x", int(DefaultMaxVariableExpansion/2+1))
	source := fmt.Sprintf("vars: {x: %s}\nout: ${x}${x}", value)
	ast, err := d2parser.Parse("custom-limit.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Compile(ast, nil)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("limit of %d work units", DefaultMaxVariableExpansion)) {
		t.Fatalf("default Compile error = %v, want default expansion limit", err)
	}

	const customLimit = DefaultMaxVariableExpansion + 2
	ast, err = d2parser.Parse("custom-limit.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Compile(ast, &CompileOptions{MaxVariableExpansion: customLimit}); err != nil {
		t.Fatalf("custom limit %d rejected exact-boundary expansion: %v", customLimit, err)
	}
}

func TestVariableExpansionStopsBeforeCleanup(t *testing.T) {
	const helperEnv = "D2_TEST_VARIABLE_EXPANSION_CLEANUP_HELPER"
	if os.Getenv(helperEnv) == "1" {
		var source strings.Builder
		source.WriteString("vars: {\n  v0: {leaf}\n")
		for i := 1; i <= 28; i++ {
			fmt.Fprintf(&source, "  v%d: {left: ${v%d}; right: ${v%d}}\n", i, i-1, i-1)
		}
		source.WriteString("}\nout: ${v28}\n")
		ast, err := d2parser.Parse("cleanup-helper.d2", strings.NewReader(source.String()), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = Compile(ast, &CompileOptions{MaxVariableExpansion: 64})
		if err == nil || !strings.Contains(err.Error(), "variable substitution expansion exceeds limit of 64 work units") {
			t.Fatalf("Compile error = %v, want expansion limit", err)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestVariableExpansionStopsBeforeCleanup$")
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("rejected expansion continued into exponential cleanup: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("cleanup helper failed: %v\n%s", err, output)
	}
}

func TestCompileBoundsCopyCostOfRepeatedNumericAlias(t *testing.T) {
	t.Parallel()

	compile := func(number string) error {
		var source strings.Builder
		fmt.Fprintf(&source, "vars: {\n  v0: {number: %s}\n", number)
		for i := 1; i <= 4; i++ {
			fmt.Fprintf(&source, "  v%d: {left: ${v%d}; right: ${v%d}}\n", i, i-1, i-1)
		}
		source.WriteString("}\nout: ${v4}\n")
		ast, err := d2parser.Parse("numeric-alias.d2", strings.NewReader(source.String()), nil)
		if err != nil {
			return err
		}
		_, _, err = Compile(ast, &CompileOptions{MaxVariableExpansion: 8_192})
		return err
	}
	if err := compile("1"); err != nil {
		t.Fatalf("small-number structural control rejected: %v", err)
	}
	err := compile(strings.Repeat("9", 256))
	if err == nil || !strings.Contains(err.Error(), "variable substitution expansion exceeds limit of 8192 work units") {
		t.Fatalf("large-number Compile error = %v, want copy-cost expansion limit", err)
	}
}

func TestCompileBoundsNestedCopyCostOfArraySpreadAliases(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	fmt.Fprintf(&source, "vars: {items: [{number: %s}]}\nout: [", strings.Repeat("9", 256))
	for i := 0; i < 8; i++ {
		source.WriteString("...${items}; ")
	}
	source.WriteString("]\n")
	ast, err := d2parser.Parse("array-alias.d2", strings.NewReader(source.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Compile(ast, &CompileOptions{MaxVariableExpansion: 1_024})
	if err == nil || !strings.Contains(err.Error(), "variable substitution expansion exceeds limit of 1024 work units") {
		t.Fatalf("Compile error = %v, want nested array-spread expansion limit", err)
	}
}

func TestCompileRechecksArrayAliasesAfterForwardScalarGrowth(t *testing.T) {
	t.Parallel()

	compile := func(payload string) error {
		var source strings.Builder
		source.WriteString("vars: {\n  seed: ${bomb}\n  values: [${seed}]\n  copies: [")
		for i := 0; i < 8; i++ {
			source.WriteString("...${values}; ")
		}
		fmt.Fprintf(&source, "]\n  bomb: ${payload}${payload}\n  payload: %s\n}\n", payload)
		ast, err := d2parser.Parse("forward-array-alias.d2", strings.NewReader(source.String()), nil)
		if err != nil {
			return err
		}
		_, _, err = Compile(ast, &CompileOptions{MaxVariableExpansion: 4_096})
		return err
	}
	if err := compile("x"); err != nil {
		t.Fatalf("small forward-growth control rejected: %v", err)
	}
	err := compile(strings.Repeat("x", 256))
	if err == nil || !strings.Contains(err.Error(), "variable substitution expansion exceeds limit of 4096 work units") {
		t.Fatalf("large forward-growth Compile error = %v, want expansion limit", err)
	}
}

func TestCompileRechecksDistinctScalarsSharingForwardGrownValue(t *testing.T) {
	t.Parallel()

	compile := func(payload string) error {
		var source strings.Builder
		source.WriteString("vars: {\n  seed: ${bomb}\n")
		for i := 0; i < 8; i++ {
			fmt.Fprintf(&source, "  copy-%d: ${seed}\n", i)
		}
		fmt.Fprintf(&source, "  bomb: ${payload}${payload}\n  payload: %s\n}\n", payload)
		ast, err := d2parser.Parse("forward-scalar-alias.d2", strings.NewReader(source.String()), nil)
		if err != nil {
			return err
		}
		_, _, err = Compile(ast, &CompileOptions{MaxVariableExpansion: 4_096})
		return err
	}
	if err := compile("x"); err != nil {
		t.Fatalf("small shared-scalar control rejected: %v", err)
	}
	err := compile(strings.Repeat("x", 256))
	if err == nil || !strings.Contains(err.Error(), "variable substitution expansion exceeds limit of 4096 work units") {
		t.Fatalf("large shared-scalar Compile error = %v, want expansion limit", err)
	}
}

func TestCompileCancellationDuringIRConstruction(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	for i := 0; i < 128; i++ {
		fmt.Fprintf(&source, "object-%d\n", i)
	}
	ast, err := d2parser.Parse("canceled.d2", strings.NewReader(source.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &cancelAfterErrContext{Context: context.Background(), cancelAt: 16}
	_, _, err = Compile(ast, &CompileOptions{Context: ctx})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Compile error = %v, want context.Canceled", err)
	}
	if ctx.calls < ctx.cancelAt {
		t.Fatalf("context Err called %d times, want at least %d", ctx.calls, ctx.cancelAt)
	}
}

func TestCompileArrayStopsOnCancellation(t *testing.T) {
	t.Parallel()

	values := strings.Repeat("value; ", 128)
	ast, err := d2parser.Parse("array-canceled.d2", strings.NewReader("items: ["+values+"]"), nil)
	if err != nil {
		t.Fatal(err)
	}
	array := ast.Nodes[0].MapKey.Value.Array
	dst := &Array{}
	ctx := &cancelAfterErrContext{Context: context.Background(), cancelAt: 4}
	c := &compiler{
		ctx:               ctx,
		variableExpansion: &variableExpansionBudget{limit: DefaultMaxVariableExpansion},
	}
	c.compileArray(dst, array, ast)
	if !errors.Is(c.contextErr, context.Canceled) {
		t.Fatalf("compileArray error = %v, want context.Canceled", c.contextErr)
	}
	if len(dst.Values) >= len(array.Nodes) {
		t.Fatalf("compileArray materialized %d of %d values after cancellation", len(dst.Values), len(array.Nodes))
	}
}

func TestCachedImportASTCloneStopsOnCancellation(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	for i := 0; i < 128; i++ {
		fmt.Fprintf(&source, "object-%d: {child: value}\n", i)
	}
	ast, err := d2parser.Parse("clone-canceled.d2", strings.NewReader(source.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &cancelAfterErrContext{Context: context.Background(), cancelAt: 16}
	cloned, err := cloneASTMapContext(ctx, ast)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("clone error = %v, want context.Canceled", err)
	}
	if cloned != nil {
		t.Fatal("canceled AST clone returned a partial tree")
	}
}

func TestCachedImportASTArrayCloneCancellationNeverReturnsPartialValues(t *testing.T) {
	t.Parallel()

	ast, err := d2parser.Parse("array-clone-canceled.d2", strings.NewReader("items: [1; 2; 3; 4; 5; 6; 7; 8]"), nil)
	if err != nil {
		t.Fatal(err)
	}
	observedCancellation := false
	for cancelAt := 1; cancelAt <= 64; cancelAt++ {
		ctx := &cancelAfterErrContext{Context: context.Background(), cancelAt: cancelAt}
		cloned, cloneErr := cloneASTMapContext(ctx, ast)
		if errors.Is(cloneErr, context.Canceled) {
			observedCancellation = true
			if cloned != nil {
				t.Fatalf("cancelAt %d returned a partial AST clone", cancelAt)
			}
			continue
		}
		if cloneErr != nil {
			t.Fatalf("cancelAt %d clone error = %v", cancelAt, cloneErr)
		}
	}
	if !observedCancellation {
		t.Fatal("array AST clone never observed cancellation")
	}
}

func TestImportedArraySpreadChargesInsertedMembers(t *testing.T) {
	t.Parallel()

	files, err := mapfs.New(map[string]string{
		"library.d2": "items: [a; b; c; d]",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()

	compile := func(source string) *Map {
		ast, err := d2parser.Parse("index.d2", strings.NewReader(source), nil)
		if err != nil {
			t.Fatal(err)
		}
		ir, _, err := Compile(ast, &CompileOptions{FS: files, MaxVariableExpansion: 1_024})
		if err != nil {
			t.Fatal(err)
		}
		return ir
	}

	nonSpread := compile("out: [@library.items]")
	spread := compile("out: [...@library.items]")
	nonSpreadUsed := variableExpansionBudgetFor(nonSpread).used
	spreadUsed := variableExpansionBudgetFor(spread).used
	if got := spreadUsed - nonSpreadUsed; got != 21 {
		t.Fatalf("array import spread charged %d extra work units, want 21", got)
	}
}

func TestImportedVariableExpansionUsesCompilationBudget(t *testing.T) {
	t.Parallel()

	var library strings.Builder
	library.WriteString("vars: {\n  v0: {leaf}\n")
	for i := 1; i <= 7; i++ {
		fmt.Fprintf(&library, "  v%d: {left: ${v%d}; right: ${v%d}}\n", i, i-1, i-1)
	}
	library.WriteString("}\nout: ${v7}\n")
	files, err := mapfs.New(map[string]string{
		"index.d2":   "...@library",
		"library.d2": library.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	ast, err := d2parser.Parse("index.d2", strings.NewReader("...@library"), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Compile(ast, &CompileOptions{FS: files, MaxVariableExpansion: 64})
	if err == nil || !strings.Contains(err.Error(), "variable substitution expansion exceeds limit of 64 work units") {
		t.Fatalf("Compile error = %v, want shared import expansion limit", err)
	}
}

func TestRepeatedImportPeeksUseCompilationBudget(t *testing.T) {
	t.Parallel()

	var library strings.Builder
	for i := 0; i < 64; i++ {
		fmt.Fprintf(&library, "child-%d\n", i)
	}
	files, err := mapfs.New(map[string]string{
		"library.d2": library.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()

	compile := func(source string) error {
		ast, err := d2parser.Parse("index.d2", strings.NewReader(source), nil)
		if err != nil {
			return err
		}
		_, _, err = Compile(ast, &CompileOptions{FS: files, MaxVariableExpansion: 8_192})
		return err
	}
	if err := compile("x: { ...@library }\nleaf\n"); err != nil {
		t.Fatalf("ordinary import rejected: %v", err)
	}

	var source strings.Builder
	source.WriteString("x: { ...@library }\n")
	for i := 0; i < 24; i++ {
		fmt.Fprintf(&source, "leaf-%d\n", i)
	}
	source.WriteString("** -> **\n")
	err = compile(source.String())
	if err == nil || !strings.Contains(err.Error(), "variable substitution expansion exceeds limit of 8192 work units") {
		t.Fatalf("repeated import peek error = %v, want expansion limit", err)
	}
}

type cancelAfterErrContext struct {
	context.Context
	calls    int
	cancelAt int
}

func (c *cancelAfterErrContext) Err() error {
	c.calls++
	if c.calls >= c.cancelAt {
		return context.Canceled
	}
	return nil
}
