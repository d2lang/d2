package release

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStandaloneInstallDetectsConcurrentPrefixChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the generated installer is a POSIX shell script")
	}
	temp := t.TempDir()
	prefix := filepath.Join(temp, "prefix")
	cache := filepath.Join(temp, "cache")
	tools := filepath.Join(temp, "tools")
	releases := filepath.Join(cache, "d2", "release")
	for _, dir := range []string{tools, releases} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	archive := func(version string) (name, digest string) {
		t.Helper()
		root := t.TempDir()
		tree := "d2-" + version
		if err := os.CopyFS(filepath.Join(root, tree), os.DirFS("template")); err != nil {
			t.Fatal(err)
		}
		bin := filepath.Join(root, tree, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(bin, "d2"), "#!/bin/sh\necho "+version+"\n")
		name = tree + "-linux-amd64.tar.gz"
		path := filepath.Join(releases, name)
		if out, err := exec.Command("tar", "-czf", path, "-C", root, tree).CombinedOutput(); err != nil {
			t.Fatalf("create fixture: %v\n%s", err, out)
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return name, fmt.Sprintf("%x", sha256.Sum256(payload))
	}

	const first, competing = "v0.7.0", "v0.7.1"
	firstArchive, firstDigest := archive(first)
	competingArchive, competingDigest := archive(competing)
	installer, err := filepath.Abs("../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(tools, "curl"), fakeReleaseCurl)
	// A different installation advertising the requested version shadows the target prefix.
	write(filepath.Join(tools, "d2"), "#!/bin/sh\necho "+first+"\n")
	// Complete the second install after the first has inspected the prefix but before it
	// publishes. Nesting the calls fixes the order without sleeps or timing assumptions.
	write(filepath.Join(tools, "make"), `#!/bin/sh
set -eu
if [ "$1" = install ] && [ -n "${INTERLEAVE-}" ]; then
  INTERLEAVE= TAG=$COMPETING ARCHIVE=$COMPETING_ARCHIVE DIGEST=$COMPETING_DIGEST \
    sh "$INSTALLER" --method=standalone --prefix="$TARGET_PREFIX" --version="$COMPETING"
fi
exec /usr/bin/make "$@"
`)
	run := func(args ...string) (string, error) {
		t.Helper()
		cmd := exec.Command("sh", append([]string{installer, "--method=standalone", "--prefix=" + prefix}, args...)...)
		cmd.Env = []string{
			"PATH=" + tools + ":/usr/bin:/bin", "TMPDIR=" + temp, "XDG_CACHE_HOME=" + cache,
			"OS=linux", "ARCH=amd64", "CALLS=" + filepath.Join(temp, "calls"),
			"TAG=" + first, "ARCHIVE=" + firstArchive, "DIGEST=" + firstDigest,
			"COMPETING=" + competing, "COMPETING_ARCHIVE=" + competingArchive, "COMPETING_DIGEST=" + competingDigest,
			"INTERLEAVE=1", "INSTALLER=" + installer, "TARGET_PREFIX=" + prefix,
		}
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	out, err := run("--version=" + first)
	if err == nil || !strings.Contains(out, "changed during this operation") {
		t.Fatalf("expected the stale installer to request a retry: %v\n%s", err, out)
	}
	exe := filepath.Join(prefix, "bin", "d2")
	if out, err := exec.Command(exe, "--version").CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != competing {
		t.Fatalf("competing installation: got %q, %v", out, err)
	}
	if out, err := run("--uninstall"); err != nil {
		t.Fatalf("uninstall with another d2 on PATH: %v\n%s", err, out)
	}
	if _, err := os.Lstat(exe); !os.IsNotExist(err) {
		t.Fatalf("executable after uninstall: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(prefix, "lib", "d2"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("release trees after uninstall: %v, %v", entries, err)
	}
}
