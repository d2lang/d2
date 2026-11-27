package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStandaloneLatestVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the generated installer is a POSIX shell script")
	}
	const releases = "https://github.com/d2lang/d2/releases"
	const metadata = `{"id":123,"tag_name":"v0.9.0"}`
	for _, tc := range []struct {
		name, redirect, apiBody, tag, wantErr string
	}{
		{name: "redirect", redirect: releases + "/tag/v0.9.1-rc.1", tag: "v0.9.1-rc.1"},
		{name: "head_error", apiBody: metadata, tag: "v0.9.0"},
		{name: "no_redirect", redirect: releases + "/latest", apiBody: metadata, tag: "v0.9.0"},
		{name: "invalid_redirect_tag", redirect: releases + "/tag/unknown", apiBody: metadata, tag: "v0.9.0"},
		{name: "api_error", apiBody: "connection failed", wantErr: "could not fetch the latest release"},
		{name: "missing_api_tag", apiBody: `{"message":"no releases"}`, wantErr: "no valid release tag"},
		{name: "invalid_api_tag", apiBody: `{"tag_name":"latest"}`, wantErr: "no valid release tag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			const curl = `#!/bin/sh
for url do :; done
case "$url" in
  https://github.com/d2lang/d2/releases/latest)
    echo HEAD >> "$CALLS"
    [ -n "$REDIRECT" ] || exit 22
    printf '%s' "$REDIRECT" ;;
  https://api.github.com/repos/d2lang/d2/releases/latest)
    echo API >> "$CALLS"
    [ "$API_BODY" != 'connection failed' ] || exit 7
    printf '%s' "$API_BODY" ;;
  *) echo "unexpected request: $url" >> "$CALLS"; exit 7 ;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(curl), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("sh", "../../install.sh", "--dry-run", "--method=standalone", "--prefix="+filepath.Join(dir, "prefix"))
			calls := filepath.Join(dir, "calls")
			cmd.Env = []string{
				"PATH=" + dir + ":/usr/bin:/bin", "OS=linux", "ARCH=amd64",
				"TMPDIR=" + dir, "XDG_CACHE_HOME=" + dir, "CALLS=" + calls,
				"REDIRECT=" + tc.redirect,
				"API_BODY=" + tc.apiBody,
			}
			out, err := cmd.CombinedOutput()
			want := tc.wantErr
			if want == "" {
				want = "detected latest version: " + tc.tag
			}
			if (err == nil) != (tc.wantErr == "") || !strings.Contains(string(out), want) {
				t.Fatalf("installer error = %v, want %q in output:\n%s", err, want, out)
			}
			wantCalls := "HEAD\n"
			if tc.apiBody != "" {
				wantCalls += "API\n"
			}
			log, err := os.ReadFile(calls)
			if err != nil || string(log) != wantCalls {
				t.Fatalf("curl calls = %q, want %q (err = %v)", log, wantCalls, err)
			}
		})
	}
}

func TestHomebrewInstallShadowWarning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the generated installer is a POSIX shell script")
	}
	dir := t.TempDir()
	const brew = `#!/bin/sh
case "$*" in
  update|'install d2') ;;
  'info d2') echo '==> d2: stable 0.10.0 (bottled), HEAD' ;;
  *) exit 1 ;;
esac
`
	for name, script := range map[string]string{
		"brew": brew,
		"d2":   "#!/bin/sh\necho \"$PATH_VERSION\"\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, pathVersion := range []string{"v0.10.0", "v0.9.0"} {
		t.Run(pathVersion, func(t *testing.T) {
			cmd := exec.Command("sh", "../../install.sh", "--prefix="+dir)
			cmd.Env = []string{
				"PATH=" + dir + ":/usr/bin:/bin", "OS=macos", "ARCH=arm64",
				"XDG_CACHE_HOME=" + filepath.Join(dir, "cache"), "PATH_VERSION=" + pathVersion,
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("installer failed: %v\n%s", err, out)
			}
			if got, want := strings.Contains(string(out), "is shadowed by"), pathVersion == "v0.9.0"; got != want {
				t.Errorf("shadow warning = %v, want %v for %s:\n%s", got, want, pathVersion, out)
			}
		})
	}
}
