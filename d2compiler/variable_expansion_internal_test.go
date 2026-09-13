package d2compiler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2ir"
	"github.com/d2lang/d2/d2parser"
)

func TestGraphMaterializationBoundsUnusedAliasesAcrossBoards(t *testing.T) {
	t.Parallel()

	for _, boardKind := range []string{"layers", "scenarios", "steps"} {
		boardKind := boardKind
		t.Run(boardKind, func(t *testing.T) {
			t.Parallel()
			var source strings.Builder
			source.WriteString("vars: {\n  v0: {leaf}\n")
			for i := 1; i <= 10; i++ {
				fmt.Fprintf(&source, "  v%d: {left: {}; right: {}}\n", i)
			}
			fmt.Fprintf(&source, "}\n%s: {first: {one}; second: {two}}", boardKind)
			ast, ir := compileExpansionIR(t, source.String(), 8_192)

			if _, err := compileIRContext(context.Background(), ast, ir); err != nil {
				t.Fatalf("ordinary boards rejected: %v", err)
			}

			ast, ir = compileExpansionIR(t, source.String(), 8_192)
			vars := ir.GetField(d2ast.FlatUnquotedString("vars")).Map()
			shared := vars.GetField(d2ast.FlatUnquotedString("v0")).Composite
			for i := 1; i <= 10; i++ {
				level := vars.GetField(d2ast.FlatUnquotedString(fmt.Sprintf("v%d", i)))
				level.Map().GetField(d2ast.FlatUnquotedString("left")).Composite = shared
				level.Map().GetField(d2ast.FlatUnquotedString("right")).Composite = shared
				shared = level.Composite
			}
			_, err := compileIRContext(context.Background(), ast, ir)
			if err == nil || !strings.Contains(err.Error(), "variable substitution expansion exceeds limit of 8192 work units") {
				t.Fatalf("compileIR error = %v, want expansion limit", err)
			}
		})
	}
}

func TestCompileCancellationDuringGraphMaterialization(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	for i := 0; i < 128; i++ {
		fmt.Fprintf(&source, "object-%d\n", i)
	}
	ast, ir := compileExpansionIR(t, source.String(), 0)
	ctx := &graphCancelAfterErrContext{Context: context.Background(), cancelAt: 16}
	_, err := compileIRContext(ctx, ast, ir)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("compileIR error = %v, want context.Canceled", err)
	}
	if ctx.calls < ctx.cancelAt {
		t.Fatalf("context Err called %d times, want at least %d", ctx.calls, ctx.cancelAt)
	}
}

func TestClassLookupBuildsOneReusableIndex(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	source.WriteString("classes: {\n")
	for i := 0; i < 2_048; i++ {
		fmt.Fprintf(&source, "  class-%d: {style.fill: red}\n", i)
	}
	source.WriteString("}\n")
	_, ir := compileExpansionIR(t, source.String(), 0)
	c := &compiler{ctx: context.Background()}
	for pass := 0; pass < 4; pass++ {
		for i := 0; i < 2_048; i++ {
			if got := c.getClassMap(ir, fmt.Sprintf("CLASS-%d", i)); got == nil {
				t.Fatalf("class-%d not found", i)
			}
		}
		if got := c.getClassMap(ir, fmt.Sprintf("missing-%d", pass)); got != nil {
			t.Fatalf("missing class resolved to %p", got)
		}
	}
	if got := len(c.classMapsByRoot); got != 1 {
		t.Fatalf("class lookup built %d root indexes, want 1", got)
	}
}

func TestClassLookupIndexConstructionIsCancelable(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	source.WriteString("classes: {\n")
	for i := 0; i < 1_024; i++ {
		fmt.Fprintf(&source, "  class-%d: {style.fill: red}\n", i)
	}
	source.WriteString("}\n")
	_, ir := compileExpansionIR(t, source.String(), 0)
	ctx := &graphCancelAfterErrContext{Context: context.Background(), cancelAt: 32}
	c := &compiler{ctx: ctx}
	if got := c.getClassMap(ir, "class-1023"); got != nil {
		t.Fatalf("canceled class lookup returned %p", got)
	}
	if !errors.Is(c.fatalErr, context.Canceled) {
		t.Fatalf("class lookup error = %v, want context.Canceled", c.fatalErr)
	}
	if len(c.classMapsByRoot) != 0 {
		t.Fatal("canceled class lookup retained a partial index")
	}
}

func compileExpansionIR(t *testing.T, source string, limit int64) (*d2ast.Map, *d2ir.Map) {
	t.Helper()
	ast, err := d2parser.Parse("expansion-internal.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	ir, _, err := d2ir.Compile(ast, &d2ir.CompileOptions{MaxVariableExpansion: limit})
	if err != nil {
		t.Fatal(err)
	}
	return ast, ir
}

type graphCancelAfterErrContext struct {
	context.Context
	calls    int
	cancelAt int
}

func (c *graphCancelAfterErrContext) Err() error {
	c.calls++
	if c.calls >= c.cancelAt {
		return context.Canceled
	}
	return nil
}
