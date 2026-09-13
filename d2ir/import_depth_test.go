package d2ir_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/d2lang/d2/d2ir"
	"github.com/d2lang/d2/d2parser"
)

const importDepthHelperEnv = "D2_TEST_IMPORT_DEPTH_HELPER"

func TestImportDepthLimit(t *testing.T) {
	if os.Getenv(importDepthHelperEnv) == "1" {
		compileImportChain(t, 10_000, true)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestImportDepthLimit$")
	cmd.Env = append(os.Environ(), importDepthHelperEnv+"=1")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("deep import subprocess did not stop safely: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("deep import subprocess failed: %v\n%s", err, output)
	}
}

func TestImportDepthBoundary(t *testing.T) {
	compileImportChain(t, d2ir.MaxImportDepth, false)
	compileImportChain(t, d2ir.MaxImportDepth+1, true)
}

func compileImportChain(t *testing.T, depth int, wantLimitError bool) {
	t.Helper()
	files := make(fstest.MapFS, depth)
	for index := 0; index < depth; index++ {
		contents := "leaf"
		if index+1 < depth {
			contents = fmt.Sprintf("...@level-%05d", index+1)
		}
		files[fmt.Sprintf("level-%05d.d2", index)] = &fstest.MapFile{Data: []byte(contents)}
	}

	ast, err := d2parser.Parse("index.d2", strings.NewReader("...@level-00000"), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = d2ir.Compile(ast, &d2ir.CompileOptions{FS: files})
	if wantLimitError {
		if err == nil || !strings.Contains(err.Error(), "maximum import depth") {
			t.Fatalf("Compile error = %v, want maximum import depth error", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("Compile error at supported import depth %d: %v", depth, err)
	}
}
