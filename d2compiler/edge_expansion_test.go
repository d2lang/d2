package d2compiler_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
)

func TestCompileEdgeExpansionLimit(t *testing.T) {
	t.Parallel()

	_, _, err := d2compiler.Compile(
		"edge-expansion.d2",
		strings.NewReader("a\nb\nc\n* -> *\n"),
		&d2compiler.CompileOptions{MaxEdgeExpansion: 8},
	)
	if err == nil || !strings.Contains(err.Error(), "edge glob expansion exceeds limit of 8 endpoint pairs") {
		t.Fatalf("Compile error = %v, want edge expansion limit", err)
	}
}

func TestCompileEdgeSelectorExpansionLimit(t *testing.T) {
	t.Parallel()

	_, _, err := d2compiler.Compile(
		"edge-selector-expansion.d2",
		strings.NewReader("a\nb\nc\n(* -> *)[*].style.opacity: 0\n"),
		&d2compiler.CompileOptions{MaxEdgeExpansion: 8},
	)
	if err == nil || !strings.Contains(err.Error(), "edge glob expansion exceeds limit of 8 endpoint pairs") {
		t.Fatalf("Compile error = %v, want edge expansion limit", err)
	}
}

func TestCompileEdgeExpansionWorkLimit(t *testing.T) {
	t.Parallel()

	_, _, err := d2compiler.Compile(
		"edge-expansion-work.d2",
		strings.NewReader("(* -> *)[*].style.opacity: 0\na\nb\nc\n"),
		&d2compiler.CompileOptions{
			MaxEdgeExpansion:     9,
			MaxEdgeExpansionWork: 9,
		},
	)
	if err == nil || !strings.Contains(err.Error(), "work limit of 9 endpoint-pair examinations") {
		t.Fatalf("Compile error = %v, want edge expansion work limit", err)
	}
}

func TestCompileEdgeExpansionDoesNotChargeExplicitEdges(t *testing.T) {
	t.Parallel()

	_, _, err := d2compiler.Compile(
		"explicit-edges.d2",
		strings.NewReader("a -> b\na -> b\na -> b\n"),
		&d2compiler.CompileOptions{MaxEdgeExpansion: 1},
	)
	if err != nil {
		t.Fatalf("Compile explicit edges: %v", err)
	}
}
