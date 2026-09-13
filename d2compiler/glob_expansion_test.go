package d2compiler_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
)

func TestCompilePropagatesGlobExpansionLimit(t *testing.T) {
	_, _, err := d2compiler.Compile("glob-feedback.d2", strings.NewReader("**.a\n**.b\n**.c\nx\n"), &d2compiler.CompileOptions{
		MaxGlobExpansion: 64,
	})
	if err == nil || !strings.Contains(err.Error(), "glob expansion exceeds limit of 64 work units") {
		t.Fatalf("Compile() error = %v, want glob expansion limit", err)
	}
}
