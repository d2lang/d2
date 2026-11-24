package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUninstallRejectsInstallFlags(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the generated installer is a POSIX shell script")
	}
	for _, flag := range []string{"--version=v0.8.2", "--force", "--verify-provenance"} {
		for _, args := range [][]string{{"--uninstall", flag}, {flag, "--uninstall"}} {
			t.Run(strings.Join(args, "_"), func(t *testing.T) {
				dir := t.TempDir()
				cache := filepath.Join(dir, "cache")
				prefix := filepath.Join(dir, "prefix")
				cmd := exec.Command("sh", append([]string{"../../install.sh", "--prefix=" + prefix}, args...)...)
				cmd.Env = []string{"PATH=/usr/bin:/bin", "TMPDIR=" + dir, "XDG_CACHE_HOME=" + cache}
				out, err := cmd.CombinedOutput()
				name, _, _ := strings.Cut(flag, "=")
				if err == nil || !strings.Contains(string(out), name+" cannot be used with --uninstall") {
					t.Fatalf("expected usage error for %s, got %v\n%s", name, err, out)
				}
				for _, path := range []string{cache, prefix} {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatalf("installer touched %s before rejecting flags: %v", path, err)
					}
				}
			})
		}
	}
}

func TestUninstallAcceptsInstallDefaultsFromEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the generated installer is a POSIX shell script")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "d2"), []byte("#!/bin/sh\necho v0.9.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "../../install.sh", "--uninstall", "--method=homebrew",
		"--prefix="+filepath.Join(dir, "prefix"), "--dry-run", "--trace")
	cmd.Env = []string{
		"PATH=" + dir + ":/usr/bin:/bin", "TMPDIR=" + dir, "XDG_CACHE_HOME=" + filepath.Join(dir, "cache"),
		"VERSION=v0.8.2", "FORCE=1", "VERIFY_PROVENANCE=1",
	}
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "exec: brew remove d2") {
		t.Fatalf("expected a Homebrew uninstall dry run, got %v\n%s", err, out)
	}
}
