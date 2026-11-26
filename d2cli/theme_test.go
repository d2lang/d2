package d2cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/d2lang/util-go/xmain"
	"github.com/d2lang/util-go/xos"
)

func TestCLIThemeNames(t *testing.T) {
	const source = "vars: {d2-config: {theme-id: 6; dark-theme-id: 201}}\na -> b"
	want, err := imageCLI(t, t.TempDir(), source, "--theme=300", "--dark-theme=200", "-")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		env  []string
		args []string
	}{
		{"names from environment", []string{"D2_THEME=Terminal", "D2_DARK_THEME=Dark Mauve"}, nil},
		{"flags override environment", []string{"D2_THEME=Origami", "D2_DARK_THEME=Dark Flagship Terrastruct"}, []string{"-t", "terminal", "--dark-theme=dark-mauve"}},
		{"prefixed numeric flags", nil, []string{"-t", "0454", "--dark-theme=0xc8"}},
		{"zero-padded decimal environment", []string{"D2_THEME=0300", "D2_DARK_THEME=0200"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := imageCLIWithEnv(t, t.TempDir(), source, tc.env, append(tc.args, "-")...)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatal("rendered SVG differs from --theme=300 --dark-theme=200")
			}
		})
	}
}

func TestPlayThemeName(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.d2")
	if err := os.WriteFile(input, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	state := &xmain.TestState{
		Run:    Run,
		PWD:    dir,
		Env:    xos.NewEnv([]string{"BROWSER=0"}),
		Args:   []string{"d2", "play", "--theme=Terminal", input},
		Stderr: &stderr,
	}
	state.Start(t, t.Context())
	defer state.Cleanup(t)
	if err := state.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "&theme=300&") {
		t.Fatalf("expected the numeric theme ID in the playground URL, got %s", stderr.String())
	}
}
