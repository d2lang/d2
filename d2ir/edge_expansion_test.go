package d2ir

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2parser"
)

func TestEdgeExpansionBudget(t *testing.T) {
	t.Parallel()

	budget, err := newEdgeExpansionBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if budget.limit != DefaultMaxEdgeExpansion {
		t.Fatalf("zero-value limit = %d, want %d", budget.limit, DefaultMaxEdgeExpansion)
	}
	for i := int64(0); i < DefaultMaxEdgeExpansion; i++ {
		if err := budget.reserve(); err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
	}
	if err := budget.reserve(); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("limit of %d endpoint pairs", DefaultMaxEdgeExpansion)) {
		t.Fatalf("overflow reserve error = %v, want edge expansion limit", err)
	}
	if budget.used != DefaultMaxEdgeExpansion {
		t.Fatalf("failed reservation changed used budget to %d", budget.used)
	}
	if _, err := newEdgeExpansionBudget(-1); err == nil || !strings.Contains(err.Error(), "MaxEdgeExpansion must not be negative") {
		t.Fatalf("negative limit error = %v", err)
	}
}

func TestEdgeExpansionWorkBudget(t *testing.T) {
	t.Parallel()

	budget, err := newEdgeExpansionWorkBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if budget.limit != DefaultMaxEdgeExpansionWork {
		t.Fatalf("zero-value limit = %d, want %d", budget.limit, DefaultMaxEdgeExpansionWork)
	}

	budget, err = newEdgeExpansionWorkBudget(3)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := budget.reserve(); err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
	}
	if err := budget.reserve(); err == nil || !strings.Contains(err.Error(), "work limit of 3 endpoint-pair examinations") {
		t.Fatalf("overflow reserve error = %v, want edge expansion work limit", err)
	}
	if budget.used != 3 {
		t.Fatalf("failed reservation changed used budget to %d", budget.used)
	}
	if _, err := newEdgeExpansionWorkBudget(-1); err == nil || !strings.Contains(err.Error(), "MaxEdgeExpansionWork must not be negative") {
		t.Fatalf("negative limit error = %v", err)
	}
}

func TestEdgeGlobExpansionBoundary(t *testing.T) {
	t.Parallel()

	const source = "a\nb\nc\n* -> *\n"
	if got, err := compileEdgeExpansion(t, source, 9, context.Background()); err != nil {
		t.Fatalf("boundary compile: %v", err)
	} else if got := got.EdgeCountRecursive(); got != 6 {
		t.Fatalf("edge count = %d, want 6", got)
	}

	if _, err := compileEdgeExpansion(t, source, 8, context.Background()); err == nil || !strings.Contains(err.Error(), "edge glob expansion exceeds limit of 8 endpoint pairs") {
		t.Fatalf("over-limit compile error = %v, want edge expansion limit", err)
	}
}

func TestEdgeSelectorGlobExpansionBoundary(t *testing.T) {
	t.Parallel()

	const source = "a\nb\nc\n(* -> *)[*].style.opacity: 0\n"
	if got, err := compileEdgeExpansion(t, source, 9, context.Background()); err != nil {
		t.Fatalf("boundary compile: %v", err)
	} else if got := got.EdgeCountRecursive(); got != 0 {
		t.Fatalf("edge count = %d, want 0", got)
	}

	if _, err := compileEdgeExpansion(t, source, 8, context.Background()); err == nil || !strings.Contains(err.Error(), "edge glob expansion exceeds limit of 8 endpoint pairs") {
		t.Fatalf("over-limit compile error = %v, want edge expansion limit", err)
	}
}

func TestEdgeSelectorGlobExpansionDoesNotRechargeUniqueFanout(t *testing.T) {
	t.Parallel()

	const source = "(* -> *)[*].style.opacity: 0\na\nb\nc\n"
	if _, err := compileEdgeExpansion(t, source, 9, context.Background()); err != nil {
		t.Fatalf("lazy selector compile: %v", err)
	}
}

func TestEdgeSelectorGlobExpansionChargesLazyReplayWork(t *testing.T) {
	t.Parallel()

	const source = "(* -> *)[*].style.opacity: 0\na\nb\nc\n"
	if _, err := compileEdgeExpansionWithWork(t, source, 9, 9, context.Background()); err == nil || !strings.Contains(err.Error(), "work limit of 9 endpoint-pair examinations") {
		t.Fatalf("lazy selector compile error = %v, want edge expansion work limit", err)
	}
}

func TestEdgeGlobExpansionChargesEachChainSegment(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	for i := 0; i < 32; i++ {
		fmt.Fprintf(&source, "n%d\n", i)
	}
	source.WriteString("* -> * -> *\n")

	if _, err := compileEdgeExpansion(t, source.String(), DefaultMaxEdgeExpansion, context.Background()); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("edge glob expansion exceeds limit of %d endpoint pairs", DefaultMaxEdgeExpansion)) {
		t.Fatalf("multi-segment chain compile error = %v, want edge expansion limit", err)
	}
}

func TestEdgeGlobExpansionDefaultRejectsCompleteGraphFanout(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&source, "n%d\n", i)
	}
	source.WriteString("* -> *\n")

	if _, err := compileEdgeExpansion(t, source.String(), 0, context.Background()); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("edge glob expansion exceeds limit of %d endpoint pairs", DefaultMaxEdgeExpansion)) {
		t.Fatalf("complete graph compile error = %v, want default edge expansion limit", err)
	}
}

func TestEdgeSelectorGlobExpansionDefaultRejectsFanout(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	for i := 0; i < 128; i++ {
		fmt.Fprintf(&source, "n%d\n", i)
	}
	source.WriteString("(* -> *)[*].style.opacity: 0\n")

	if _, err := compileEdgeExpansion(t, source.String(), 0, context.Background()); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("edge glob expansion exceeds limit of %d endpoint pairs", DefaultMaxEdgeExpansion)) {
		t.Fatalf("edge selector compile error = %v, want default edge expansion limit", err)
	}
}

func TestEdgeGlobExpansionDefaultSparseStarBoundary(t *testing.T) {
	t.Parallel()

	compileStar := func(leaves int) (*Map, error) {
		var source strings.Builder
		source.WriteString("root\n")
		for i := 0; i < leaves; i++ {
			fmt.Fprintf(&source, "leaf%d\n", i)
		}
		source.WriteString("root -> *\n")
		return compileEdgeExpansion(t, source.String(), 0, context.Background())
	}

	// The root plus DefaultMaxEdgeExpansion-1 leaves examines exactly the
	// default number of endpoint pairs.
	m, err := compileStar(int(DefaultMaxEdgeExpansion - 1))
	if err != nil {
		t.Fatalf("default boundary compile: %v", err)
	}
	if got := m.EdgeCountRecursive(); got != int(DefaultMaxEdgeExpansion-1) {
		t.Fatalf("edge count = %d, want %d", got, DefaultMaxEdgeExpansion-1)
	}

	if _, err := compileStar(int(DefaultMaxEdgeExpansion)); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("edge glob expansion exceeds limit of %d endpoint pairs", DefaultMaxEdgeExpansion)) {
		t.Fatalf("over-limit sparse star error = %v, want default edge expansion limit", err)
	}
}

func TestEdgeGlobExpansionObservesCancellation(t *testing.T) {
	t.Parallel()

	base := compileIndexTestSource(t, "a\nb\nc\n")
	key, err := d2parser.ParseMapKey("* -> *")
	if err != nil {
		t.Fatal(err)
	}

	// Locate a deterministic cancellation point after expansion has started but
	// before all nine endpoint pairs have been examined. This avoids depending
	// on the number of context checks performed by field lookup internals.
	for cancelAt := 1; cancelAt < 128; cancelAt++ {
		root := base.Copy(nil).(*Map)
		ctx := &cancelAfterErrContext{Context: context.Background(), cancelAt: cancelAt}
		c := &compiler{
			err:               &d2parser.ParseError{},
			ctx:               ctx,
			variableExpansion: &variableExpansionBudget{limit: DefaultMaxVariableExpansion},
			edgeExpansion:     &edgeExpansionBudget{limit: 100},
			globContextStack:  [][]*globContext{{}},
		}
		refctx := &RefContext{Key: key, Edge: key.Edges[0], ScopeMap: root}
		_, _ = root.CreateEdge(NewEdgeIDs(key)[0], refctx, c)
		if errors.Is(c.contextErr, context.Canceled) && c.edgeExpansion.used > 0 && c.edgeExpansion.used < 9 {
			return
		}
	}
	t.Fatal("could not observe cancellation during endpoint-pair expansion")
}

func TestEdgeSelectorGlobExpansionObservesCancellation(t *testing.T) {
	t.Parallel()

	base := compileIndexTestSource(t, "a\nb\nc\n")
	key, err := d2parser.ParseMapKey("(* -> *)[*].style.opacity")
	if err != nil {
		t.Fatal(err)
	}

	for cancelAt := 1; cancelAt < 128; cancelAt++ {
		root := base.Copy(nil).(*Map)
		ctx := &cancelAfterErrContext{Context: context.Background(), cancelAt: cancelAt}
		c := &compiler{
			err:               &d2parser.ParseError{},
			ctx:               ctx,
			variableExpansion: &variableExpansionBudget{limit: DefaultMaxVariableExpansion},
			edgeExpansion:     &edgeExpansionBudget{limit: 100},
			globContextStack:  [][]*globContext{{}},
		}
		refctx := &RefContext{Key: key, Edge: key.Edges[0], ScopeMap: root}
		_ = root.getEdgesForCompile(NewEdgeIDs(key)[0], refctx, c)
		if errors.Is(c.contextErr, context.Canceled) && c.edgeExpansion.used > 0 && c.edgeExpansion.used < 9 {
			return
		}
	}
	t.Fatal("could not observe cancellation during selector endpoint-pair expansion")
}

func compileEdgeExpansion(t *testing.T, source string, limit int64, ctx context.Context) (*Map, error) {
	t.Helper()
	return compileEdgeExpansionWithWork(t, source, limit, 0, ctx)
}

func compileEdgeExpansionWithWork(t *testing.T, source string, limit, workLimit int64, ctx context.Context) (*Map, error) {
	t.Helper()
	ast, err := d2parser.Parse("edge-expansion.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := Compile(ast, &CompileOptions{
		Context:              ctx,
		MaxEdgeExpansion:     limit,
		MaxEdgeExpansionWork: workLimit,
	})
	return m, err
}
