package d2cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/d2lang/util-go/xmain"
)

func TestFmtStdin(t *testing.T) {
	for _, tc := range []struct {
		name, input, output, diagnostic string
		flags                           []string
	}{
		{name: "unchanged", input: "client -> api\n", output: "client -> api\n"},
		{name: "changed", input: "client->api\n", output: "client -> api\n"},
		{name: "empty"},
		{name: "invalid", input: "client ->\n", diagnostic: "connection missing destination"},
		{name: "check unchanged", input: "client -> api\n", flags: []string{"--check"}},
		{name: "check changed", input: "client->api\n", flags: []string{"--check"}, diagnostic: "found 1 unformatted file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append(append([]string{}, tc.flags...), "-")
			stdout, err := runFmt(t, t.TempDir(), tc.input, args...)
			if tc.diagnostic == "" && err != nil {
				t.Fatal(err)
			}
			if tc.diagnostic != "" && (err == nil || !strings.Contains(err.Error(), tc.diagnostic)) {
				t.Fatalf("error = %v, want %q", err, tc.diagnostic)
			}
			if stdout != tc.output {
				t.Fatalf("stdout = %q, want %q", stdout, tc.output)
			}
		})
	}
}

func TestFmtMarkdownCLI(t *testing.T) {
	const input = "# Diagram\n\n- Request path:\n\n  ```d2\n  client->api\n  ```\n\n```sh\nclient->api\n```\n"
	const formatted = "# Diagram\n\n- Request path:\n\n  ```d2\n  client -> api\n  ```\n\n```sh\nclient->api\n```\n"
	directory := t.TempDir()
	const name = "README.md"
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	assertFile := func(want string) {
		t.Helper()
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("README.md = %q, want %q", got, want)
		}
	}

	preview, err := runFmt(t, directory, "", "--diff", name)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(preview, "--- a/README.md\n+++ b/README.md\n@@") ||
		!strings.Contains(preview, "-  client->api\n+  client -> api\n") {
		t.Fatalf("unexpected unified diff:\n%s", preview)
	}
	assertFile(input)

	stdout, err := runFmt(t, directory, "", "--check", "--diff", name)
	if err == nil || !strings.Contains(err.Error(), "found 1 unformatted file") {
		t.Fatalf("error = %v, want an unformatted file error", err)
	}
	if stdout != preview {
		t.Fatalf("--check --diff output = %q, want %q", stdout, preview)
	}
	assertFile(input)

	if stdout, err := runFmt(t, directory, "", name); err != nil || stdout != "" {
		t.Fatalf("format result = (%q, %v), want no output or error", stdout, err)
	}
	assertFile(formatted)

	if stdout, err := runFmt(t, directory, "", "--check", "--diff", name); err != nil || stdout != "" {
		t.Fatalf("post-format check = (%q, %v), want no output or error", stdout, err)
	}

	const invalid = "```d2\na->b\n```\n\n> ```d2\n> client ->\n> ```\n"
	if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	if stdout, err := runFmt(t, directory, "", name); err == nil || !strings.Contains(err.Error(), "README.md:6:3: connection missing destination") || stdout != "" {
		t.Fatalf("invalid input result = (%q, %v), want a diagnostic at README.md:6:3", stdout, err)
	}
	assertFile(invalid)
}

func runFmt(t *testing.T, directory, input string, args ...string) (string, error) {
	t.Helper()
	var stdout bytes.Buffer
	state := &xmain.TestState{
		Run:    Run,
		Args:   append([]string{"d2", "fmt"}, args...),
		PWD:    directory,
		Stdin:  strings.NewReader(input),
		Stdout: &stdout,
	}
	state.Start(t, t.Context())
	defer state.Cleanup(t)
	waitCtx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	err := state.Wait(waitCtx)
	return stdout.String(), err
}

func TestFmtHonorsCanceledContextBeforeWriting(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.d2")
	input := []byte("x:{y}\n")
	if err := os.WriteFile(inputPath, input, 0o600); err != nil {
		t.Fatal(err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	cancel()
	state := &xmain.TestState{
		Run:  Run,
		Args: []string{"d2", "fmt", inputPath},
		PWD:  directory,
	}
	state.Start(t, runCtx)
	defer state.Cleanup(t)
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer waitCancel()
	if err := state.Wait(waitCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("d2 fmt error = %v, want context.Canceled", err)
	}

	got, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(input) {
		t.Fatalf("d2 fmt rewrote input after cancellation: got %q, want %q", got, input)
	}
}
