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

func TestStandaloneUpgradePreservesWorkingInstallation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the generated installer is a POSIX shell script")
	}
	const oldVersion = "v0.7.0"
	for _, tc := range []struct {
		name, version string
		force         bool
		failPublish   bool
	}{
		{name: "upgrade", version: "v0.7.1"},
		{name: "forced_reinstall", version: oldVersion, force: true},
		{name: "upgrade_publish_fails", version: "v0.7.1", failPublish: true},
		{name: "forced_reinstall_publish_fails", version: oldVersion, force: true, failPublish: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			temp := t.TempDir()
			prefix := filepath.Join(temp, "prefix")
			cache := filepath.Join(temp, "cache")
			tools := filepath.Join(temp, "tools")
			if err := os.Mkdir(tools, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(tools, "curl"), []byte(fakeReleaseCurl), 0o755); err != nil {
				t.Fatal(err)
			}
			const mv = `#!/bin/sh
for dest do :; done
if [ "$dest" = "${PUBLISH_DEST-}" ] && [ ! -f "$PUBLISH_MARKER" ]; then
  touch "$PUBLISH_MARKER"
  exit 72
fi
exec /bin/mv "$@"
`
			if err := os.WriteFile(filepath.Join(tools, "mv"), []byte(mv), 0o755); err != nil {
				t.Fatal(err)
			}

			// Use the release Makefile and its install/uninstall recipes. The archive
			// is in the cache and fakeReleaseCurl supplies its release metadata.
			archiveEnv := func(version, revision string) []string {
				t.Helper()
				src := t.TempDir()
				tree := "d2-" + version
				root := filepath.Join(src, tree)
				if err := os.CopyFS(root, os.DirFS("template")); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
					t.Fatal(err)
				}
				binary := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = --version ]; then echo %s; else echo %s; fi\n", version, revision)
				if err := os.WriteFile(filepath.Join(root, "bin/d2"), []byte(binary), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "man/d2.1"), []byte(revision), 0o644); err != nil {
					t.Fatal(err)
				}
				// Fail once, after the real recipe has written the binary and manpage.
				// Record that point so an unrelated early failure cannot pass the test.
				script := filepath.Join(root, "scripts/install.sh")
				body, err := os.ReadFile(script)
				if err != nil {
					t.Fatal(err)
				}
				body = append(body, `
if [ -n "${FAIL_MARKER-}" ] && [ ! -f "$FAIL_MARKER" ]; then
  touch "$FAIL_MARKER"
  exit 71
fi
`...)
				if err := os.WriteFile(script, body, 0o755); err != nil {
					t.Fatal(err)
				}
				dir := filepath.Join(cache, "d2", "release")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				archive := tree + "-linux-amd64.tar.gz"
				path := filepath.Join(dir, archive)
				if output, err := exec.Command("tar", "-czf", path, "-C", src, tree).CombinedOutput(); err != nil {
					t.Fatalf("archive fixture: %v\n%s", err, output)
				}
				payload, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				return []string{"TAG=" + version, "ARCHIVE=" + archive, fmt.Sprintf("DIGEST=%x", sha256.Sum256(payload))}
			}

			run := func(env []string, args ...string) (string, error) {
				cmd := exec.Command("sh", append([]string{"../../install.sh", "--method=standalone", "--prefix=" + prefix}, args...)...)
				cmd.Env = append([]string{
					"PATH=" + tools + ":" + filepath.Join(prefix, "bin") + ":/usr/bin:/bin",
					"XDG_CACHE_HOME=" + cache, "OS=linux", "ARCH=amd64", "TMPDIR=" + temp,
					"CALLS=" + filepath.Join(temp, "calls"),
				}, env...)
				output, err := cmd.CombinedOutput()
				return string(output), err
			}
			checkInstall := func(version, revision string) {
				t.Helper()
				bin := filepath.Join(prefix, "bin", "d2")
				for arg, want := range map[string]string{"--version": version, "--identify": revision} {
					output, err := exec.Command(bin, arg).CombinedOutput()
					if err != nil || strings.TrimSpace(string(output)) != want {
						t.Fatalf("d2 %s: %q, %v (want %s)", arg, output, err, want)
					}
				}
				tree := filepath.Join(prefix, "lib", "d2", "d2-"+version)
				for _, file := range []string{filepath.Join(prefix, "share/man/man1/d2.1"), filepath.Join(tree, "man/d2.1")} {
					data, err := os.ReadFile(file)
					if err != nil || string(data) != revision {
						t.Fatalf("%s: %q, %v (want %s)", file, data, err, revision)
					}
				}
			}

			if output, err := run(archiveEnv(oldVersion, "original"), "--version="+oldVersion); err != nil {
				t.Fatalf("initial install: %v\n%s", err, output)
			}
			env := archiveEnv(tc.version, "replacement")
			args := []string{"--version=" + tc.version}
			if tc.force {
				args = append(args, "--force")
			}
			marker := filepath.Join(temp, "failed-install")
			failureEnv := []string{"FAIL_MARKER=" + marker}
			if tc.failPublish {
				failureEnv = []string{"PUBLISH_DEST=" + filepath.Join(prefix, "bin/d2"), "PUBLISH_MARKER=" + marker}
			}
			output, err := run(append(env, failureEnv...), args...)
			if err == nil {
				t.Fatalf("expected failed install:\n%s", output)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("never reached the injected install failure: %v\n%s", err, output)
			}
			checkInstall(oldVersion, "original")
			if _, err := os.Stat(filepath.Join(prefix, "lib/d2/.install-lock")); !os.IsNotExist(err) {
				t.Fatalf("transaction left a lock: %v", err)
			}
			if tc.version != oldVersion {
				if _, err := os.Stat(filepath.Join(prefix, "lib/d2/d2-"+tc.version)); !os.IsNotExist(err) {
					t.Fatalf("failed upgrade left the incoming release tree: %v", err)
				}
			}

			if output, err := run(env, args...); err != nil {
				t.Fatalf("retry install: %v\n%s", err, output)
			}
			checkInstall(tc.version, "replacement")
			if output, err := run(nil, "--uninstall"); err != nil {
				t.Fatalf("uninstall replacement: %v\n%s", err, output)
			}
			for _, file := range []string{"bin/d2", "share/man/man1/d2.1", "lib/d2/d2-" + tc.version, "lib/d2/d2-" + oldVersion} {
				if _, err := os.Lstat(filepath.Join(prefix, file)); !os.IsNotExist(err) {
					t.Fatalf("%s still installed after uninstall: %v", file, err)
				}
			}
		})
	}
}
