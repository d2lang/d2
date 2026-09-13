package d2cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/d2lang/util-go/xmain"
	"github.com/d2lang/util-go/xos"
)

const legacyPluginMarkerEnv = "D2_TEST_LEGACY_PLUGIN_MARKER"

// A copy of this test executable stands in for a legacy plugin on PATH.
func TestMain(m *testing.M) {
	if marker := os.Getenv(legacyPluginMarkerEnv); marker != "" && strings.HasPrefix(filepath.Base(os.Args[0]), "d2plugin-") {
		if err := os.WriteFile(marker, []byte("executed"), 0600); err != nil {
			os.Exit(2)
		}
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestLegacyLayoutExecutablesAreIgnored(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "executed")
	for _, name := range []string{"external", "dagre", "elk", "tala"} {
		installLegacyLayoutExecutable(t, directory, name)
	}
	t.Setenv("PATH", directory)
	t.Setenv(legacyPluginMarkerEnv, marker)
	if err := os.WriteFile(filepath.Join(directory, "input.d2"), []byte("a -> b"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "external.d2"), []byte("vars: {d2-config: {layout-engine: external}}\na -> b"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		args      []string
		env       []string
		wantError string
	}{
		{name: "help", args: []string{"--help"}},
		{name: "version", args: []string{"--version"}},
		{name: "list", args: []string{"layout"}},
		{name: "builtin help", args: []string{"layout", "TaLa"}},
		{name: "external help", args: []string{"layout", "external"}, wantError: "External d2plugin executables are no longer supported"},
		{name: "external flag", args: []string{"--layout=external", "input.d2", "output.svg"}, wantError: "not a supported built-in layout engine"},
		{name: "external environment", args: []string{"input.d2", "output.svg"}, env: []string{"D2_LAYOUT=external"}, wantError: "not a supported built-in layout engine"},
		{name: "external source", args: []string{"external.d2", "output.svg"}, wantError: "not a supported built-in layout engine"},
		{name: "external option", args: []string{"--external-option=value", "input.d2", "output.svg"}, wantError: "unknown flag: --external-option"},
		{name: "dagre", args: []string{"-lDAGRE", "input.d2", "output.svg"}},
		{name: "elk", args: []string{"--layout=ELK", "input.d2", "output.svg"}},
		{name: "tala", args: []string{"--layout=TaLa", "input.d2", "output.svg"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, err := runLayoutCLI(t, directory, tc.env, tc.args...)
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want %q", err, tc.wantError)
			}
			if tc.name == "list" {
				for _, name := range []string{"dagre", "elk", "tala"} {
					if !strings.Contains(output, name+" (built-in)") {
						t.Fatalf("missing %s in layout help: %s", name, output)
					}
				}
				if strings.Contains(output, "external") {
					t.Fatalf("legacy executable listed: %s", output)
				}
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("legacy executable ran (marker stat: %v)", err)
			}
		})
	}
}

func TestBuiltinLayoutFlagOrder(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "input.d2"), []byte("a -> b\na -> c"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, flag, value string }{
		{"dagre", "--dagre-nodesep", "100"},
		{"elk", "--elk-nodeNodeBetweenLayers", "100"},
		{"tala", "--tala-seeds", "7,11"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := []string{tc.flag, tc.value, "--layout", tc.name, "input.d2", "before.svg"}
			after := []string{"--layout", tc.name, tc.flag, tc.value, "input.d2", "after.svg"}
			for _, args := range [][]string{before, after} {
				if _, err := runLayoutCLI(t, directory, nil, args...); err != nil {
					t.Fatal(err)
				}
			}
			a, err := os.ReadFile(filepath.Join(directory, "before.svg"))
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(filepath.Join(directory, "after.svg"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a, b) {
				t.Fatal("layout flag order changed output")
			}
		})
	}
}

func runLayoutCLI(t *testing.T, directory string, env []string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var stdout bytes.Buffer
	state := &xmain.TestState{Run: Run, Env: xos.NewEnv(env), Args: append([]string{"d2"}, args...), PWD: directory, Stdout: &stdout}
	state.Start(t, ctx)
	defer state.Cleanup(t)
	err := state.Wait(ctx)
	return stdout.String(), err
}

func installLegacyLayoutExecutable(t *testing.T, directory, name string) {
	t.Helper()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binaryName := "d2plugin-" + name
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(filepath.Join(directory, binaryName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		t.Fatal(copyErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}
