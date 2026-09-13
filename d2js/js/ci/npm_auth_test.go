package ci_test

import (
	"embed"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Include the shell implementation and its call site in the test inputs so
// edits to either invalidate Go's test cache.
//
//go:embed npm-auth.sh build.sh
var npmAuthSources embed.FS

func TestMain(m *testing.M) {
	code := m.Run()
	runtime.KeepAlive(npmAuthSources)
	os.Exit(code)
}

func TestNPMAuthConfigIsPrivateAndRemoved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell credential helper")
	}
	tempDir := t.TempDir()
	helper := filepath.Join(tempDir, "npm-auth.sh")
	source, err := npmAuthSources.ReadFile("npm-auth.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, source, 0o700); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sh", "-c", `. "$1"
umask 000
NPM_TOKEN=unit-test-token
create_npm_auth_config
[ "${NPM_TOKEN+x}" != x ]
printf '%s' "$NPM_AUTH_CONFIG"`, "sh", helper)
	cmd.Env = append(os.Environ(), "TMPDIR="+tempDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("create npm auth config: %v: %s", err, output)
	}
	config := strings.TrimSpace(string(output))
	t.Cleanup(func() { _ = os.Remove(config) })

	info, err := os.Stat(config)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("npm auth config mode = %04o, want 0600", got)
	}
	contents, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "//registry.npmjs.org/:_authToken=unit-test-token\n" {
		t.Fatal("npm auth config did not contain exactly the scoped registry credential")
	}

	checkout := filepath.Join(tempDir, "checkout")
	if err := os.Mkdir(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	projectConfig := filepath.Join(checkout, ".npmrc")
	if err := os.WriteFile(projectConfig, []byte("project-setting=true\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	cleanup := exec.Command("sh", "-c", `. "$1"
NPM_AUTH_CONFIG=$2
remove_npm_auth_config`, "sh", helper, config)
	cleanup.Dir = checkout
	if output, err := cleanup.CombinedOutput(); err != nil {
		t.Fatalf("remove npm auth config: %v: %s", err, output)
	}
	if _, err := os.Stat(config); !os.IsNotExist(err) {
		t.Fatalf("npm auth config remains after cleanup: %v", err)
	}
	if contents, err := os.ReadFile(projectConfig); err != nil || string(contents) != "project-setting=true\n" {
		t.Fatalf("cleanup changed the checkout's existing .npmrc: contents=%q err=%v", contents, err)
	}
}

func TestNPMAuthConfigExitCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell credential helper")
	}
	tempDir := t.TempDir()
	helper := filepath.Join(tempDir, "npm-auth.sh")
	source, err := npmAuthSources.ReadFile("npm-auth.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, source, 0o700); err != nil {
		t.Fatal(err)
	}

	record := filepath.Join(tempDir, "config-path")
	cmd := exec.Command("sh", "-c", `set -eu
. "$1"
trap remove_npm_auth_config EXIT
NPM_TOKEN=unit-test-token
create_npm_auth_config
printf '%s' "$NPM_AUTH_CONFIG" >"$2"
false`, "sh", helper, record)
	cmd.Env = append(os.Environ(), "TMPDIR="+tempDir)
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("failure fixture unexpectedly succeeded: %s", output)
	}
	config, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(string(config)); !os.IsNotExist(err) {
		t.Fatalf("npm auth config remains after error exit: %v", err)
	}
}
