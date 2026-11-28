package d2cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/d2lang/util-go/xmain"
)

func TestValidateCompilesInput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "child.d2"), []byte("child"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, source, wantError string }{
		{"semantic_error", "a.style.opacity: 7", "opacity"},
		{"valid_import", "...@child", ""},
		{"missing_import", "...@missing", "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".d2")
			if err := os.WriteFile(path, []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			var stdout bytes.Buffer
			state := &xmain.TestState{Run: Run, Args: []string{"d2", "validate", path}, PWD: dir, Stdout: &stdout}
			state.Start(t, t.Context())
			defer state.Cleanup(t)
			err := state.Wait(t.Context())
			if tc.wantError == "" {
				if err != nil || !strings.Contains(stdout.String(), "is valid D2") {
					t.Fatalf("validation: %v, output %q", err, stdout.String())
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) || stdout.Len() != 0 {
				t.Fatalf("validation: %v, output %q", err, stdout.String())
			}
		})
	}
}
