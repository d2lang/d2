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

func TestStandaloneInstallProvenance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the generated installer is a POSIX shell script")
	}
	// Give the fixture archive its own trusted pin in a private copy of install.sh.
	// This exercises the production lookup, hashing, staging and extraction paths.
	pins := map[string]string{
		"v0.8.2": "0ea49e35f17c3ac00ffa5d0a1fa571f5c344cd38127dbaa4bf6158fcd0c7bfb0",
		"v0.9.0": "5669ddc46b99e942cc96078f4a4e36d5e62103348f4c05179ede27802fdd87a9",
	}
	installer, err := os.ReadFile("../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	const uncatalogued = "v0.0.0-installer-test.1"
	for _, tc := range []struct {
		name    string
		version string
		outcome string
		cached  bool
		latest  bool
		strict  bool
		method  string
	}{
		{name: "verified_upgrade", version: uncatalogued, outcome: "verified"},
		{name: "pinned_first_attested_release", version: "v0.8.2", outcome: "pinned"},
		{name: "pinned_current_release", version: "v0.9.0", outcome: "pinned"},
		{name: "latest_resolves_to_pinned_archive", version: "v0.9.0", outcome: "pinned", latest: true},
		{name: "forced_pinned_install_from_cache", version: "v0.9.0", outcome: "pinned", cached: true},
		{name: "corrupt_pinned_cache_downloads_replacement", version: "v0.9.0", outcome: "corrupt_cache", cached: true},
		{name: "reject_substituted_pinned_download", version: "v0.9.0", outcome: "substituted"},
		{name: "pinned_install_already_current", version: "v0.9.0", outcome: "already_installed", cached: true},
		{name: "strict_first_attested_release", version: "v0.8.2", outcome: "verified", strict: true},
		{name: "strict_historical_bundle", version: "v0.9.0", outcome: "verified", strict: true},
		{name: "strict_rejects_invalid_signature_even_with_pin", version: "v0.9.0", outcome: "invalid", strict: true},
		{name: "strict_detect_uses_standalone_with_brew_available", version: uncatalogued, outcome: "verified", strict: true, method: "detect"},
		{name: "strict_explicit_homebrew_rejected", version: uncatalogued, outcome: "incompatible", strict: true, method: "homebrew"},
		{name: "explicit_legacy_release", version: "v0.7.1", outcome: "legacy"},
		{name: "strict_rejects_unattested_legacy_release", version: "v0.7.1", outcome: "unattested", strict: true},
		{name: "invalid_signature_for_cached_forced_install", version: uncatalogued, outcome: "invalid", cached: true},
		{name: "different_signed_platform", version: uncatalogued, outcome: "wrong_subject"},
		{name: "cache_changes_after_verification", version: uncatalogued, outcome: "cache_changes", cached: true},
		{name: "different_signed_digest", version: uncatalogued, outcome: "wrong_digest"},
		{name: "missing_bundle", version: uncatalogued, outcome: "missing"},
		{name: "latest_resolves_to_unattested_release", version: "v0.7.1", outcome: "unattested", latest: true},
		{name: "dry_run", version: uncatalogued, outcome: "dry_run"},
		{name: "pinned_dry_run", version: "v0.9.0", outcome: "dry_run"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			temp := t.TempDir()
			bin := filepath.Join(temp, "bin")
			prefix := filepath.Join(temp, "prefix")
			cache := filepath.Join(temp, "cache")
			staged := filepath.Join(temp, "fixtures")
			oldVersion := "v0.7.0"
			if tc.cached {
				oldVersion = tc.version
			}
			old := filepath.Join(prefix, "lib", "d2", "d2-"+oldVersion)
			newDir := "d2-" + tc.version
			for _, dir := range []string{bin, old, filepath.Join(staged, newDir), filepath.Join(cache, "d2", "release")} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path, contents string, perm os.FileMode) {
				t.Helper()
				if err := os.WriteFile(path, []byte(contents), perm); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(old, "old-install"), "working", 0o644)
			write(filepath.Join(staged, newDir, "new-install"), "verified", 0o644)
			platform := "linux"
			if tc.method != "" {
				platform = "macos"
			}
			archive := newDir + "-" + platform + "-amd64.tar.gz"
			asset := filepath.Join(staged, archive)
			if out, err := exec.Command("tar", "-czf", asset, "-C", staged, newDir).CombinedOutput(); err != nil {
				t.Fatalf("create fixture: %v\n%s", err, out)
			}
			payload, err := os.ReadFile(asset)
			if err != nil {
				t.Fatal(err)
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(payload))
			script := "../../install.sh"
			pin := pins[tc.version]
			if pin != "" {
				if strings.Count(string(installer), pin) != 1 {
					t.Fatalf("generated installer must contain the linux-amd64 pin for %s exactly once", tc.version)
				}
				script = filepath.Join(temp, "install.sh")
				write(script, strings.Replace(string(installer), pin, digest, 1), 0o755)
			}
			if tc.cached {
				if tc.outcome == "corrupt_cache" {
					payload = []byte("corrupted cache")
				}
				if err := os.WriteFile(filepath.Join(cache, "d2", "release", archive), payload, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(bin, "d2"), "#!/bin/sh\necho \"$OLD_VERSION\"\n", 0o755)
			write(filepath.Join(bin, "make"), `#!/bin/sh
set -eu
echo "$1" >> "$EVENTS"
if [ "$1" = install ]; then
  prefix=${2#PREFIX=}
  mkdir -p "$prefix/bin" "$prefix/share/man/man1"
  cp new-install "$prefix/bin/d2"
  cp new-install "$prefix/share/man/man1/d2.1"
fi
`, 0o755)
			write(filepath.Join(bin, "brew"), "#!/bin/sh\necho brew >> \"$EVENTS\"\nexit 1\n", 0o755)
			write(filepath.Join(bin, "curl"), fakeReleaseCurl, 0o755)
			write(filepath.Join(bin, "gh"), fakeAttestationVerifier, 0o755)
			events := filepath.Join(temp, "events")
			write(events, "", 0o644)
			calls := filepath.Join(temp, "calls")
			write(calls, "", 0o644)
			version := tc.version
			if tc.latest {
				version = "latest"
			}
			method := tc.method
			if method == "" {
				method = "standalone"
			}
			args := []string{script, "--method=" + method, "--version=" + version, "--prefix=" + prefix}
			if tc.cached && tc.outcome != "already_installed" {
				args = append(args, "--force")
			}
			if tc.strict {
				args = append(args, "--verify-provenance")
			}
			if tc.outcome == "dry_run" {
				args = append(args, "--dry-run")
			}
			bundleURL := "https://github.com/d2lang/d2/releases/download/" + tc.version + "/d2-" + tc.version + "-provenance.sigstore.json"
			switch tc.version {
			case "v0.8.2":
				bundleURL = "https://github.com/d2lang/d2/attestations/42650843/download"
			case "v0.9.0":
				bundleURL = "https://github.com/d2lang/d2/attestations/45830930/download"
			}
			cmd := exec.Command("sh", args...)
			cmd.Env = []string{
				"PATH=" + bin + ":/usr/bin:/bin", "OS=" + platform, "ARCH=amd64", "TMPDIR=" + temp,
				"XDG_CACHE_HOME=" + cache, "OLD_VERSION=" + oldVersion, "TAG=" + tc.version,
				"ARCHIVE=" + archive, "ASSET=" + asset, "DIGEST=" + digest, "BUNDLE_URL=" + bundleURL, "EVENTS=" + events, "CALLS=" + calls, "RESULT=" + tc.outcome,
			}
			output, err := cmd.CombinedOutput()
			installed := tc.outcome == "verified" || tc.outcome == "cache_changes" || tc.outcome == "legacy" || tc.outcome == "pinned" || tc.outcome == "corrupt_cache"
			wantSuccess := installed || tc.outcome == "dry_run" || tc.outcome == "already_installed"
			if (err == nil) != wantSuccess {
				t.Fatalf("installer error = %v, want success = %t\n%s", err, wantSuccess, output)
			}
			log, err := os.ReadFile(events)
			if err != nil {
				t.Fatal(err)
			}
			trace := string(log)
			if tc.outcome == "incompatible" || tc.outcome == "unattested" {
				if trace != "" {
					t.Fatalf("installer acted on an unsupported verification request: %q", trace)
				}
			}
			if pin != "" && !tc.strict {
				var wantCalls string
				if tc.latest {
					wantCalls += "latest\n"
				}
				if tc.outcome != "dry_run" && (!tc.cached || tc.outcome == "corrupt_cache") {
					wantCalls += "archive\n"
				}
				got, err := os.ReadFile(calls)
				if err != nil || string(got) != wantCalls {
					t.Fatalf("pinned install network/verifier calls = %q, want %q (err = %v)", got, wantCalls, err)
				}
			}
			if tc.cached && tc.outcome != "corrupt_cache" && strings.Contains(trace, "download") {
				t.Fatal("installer redownloaded a cache entry that passed its checksum")
			}
			if installed {
				wantOrder := "verify\ninstall\n"
				if tc.outcome == "legacy" || tc.outcome == "pinned" || tc.outcome == "corrupt_cache" {
					wantOrder = "install\n"
				}
				if strings.Contains(trace, "uninstall\n") {
					t.Fatalf("installer ran the previous release's uninstall recipe: %q", trace)
				}
				if !strings.HasSuffix(trace, wantOrder) {
					t.Fatalf("unexpected upgrade order: %q", trace)
				}
				if _, err := os.Stat(filepath.Join(prefix, "lib", "d2", newDir, "new-install")); err != nil {
					t.Fatalf("verified archive was not extracted: %v", err)
				}
				return
			}
			if wantSuccess && trace != "" {
				t.Fatalf("installer performed unexpected work: %q", trace)
			}
			requiresVerifier := tc.outcome == "invalid" || tc.outcome == "wrong_digest" || tc.outcome == "wrong_subject"
			if requiresVerifier && !strings.Contains(trace, "verify\n") {
				t.Fatalf("installer failed before testing the attestation: %q\n%s", trace, output)
			}
			if strings.Contains(trace, "install") {
				t.Fatalf("installer modified the installation: %q", trace)
			}
			if _, err := os.Stat(filepath.Join(old, "old-install")); err != nil {
				t.Fatalf("existing install lost: %v", err)
			}
			if _, err := os.Stat(filepath.Join(prefix, "lib", "d2", newDir, "new-install")); !os.IsNotExist(err) {
				t.Fatalf("unexpected extraction: %v", err)
			}
		})
	}
}

const fakeReleaseCurl = `#!/bin/sh
set -eu
out=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    *o|-o|--output) shift; out=$1 ;;
    https://*) url=$1 ;;
  esac
  shift
done
case "$url" in
  https://github.com/d2lang/d2/releases/latest)
    echo latest >> "$CALLS"
    echo "https://github.com/d2lang/d2/releases/tag/$TAG"
    exit ;;
  https://api.github.com/repos/d2lang/d2/releases/tags/"$TAG")
    echo metadata >> "$CALLS"
    printf '{\n  "assets": [\n    {\n      "name": "%s",\n      "digest": "sha256:%s"\n    }\n  ]\n}\n' "$ARCHIVE" "$DIGEST" > "$out" ;;
  https://github.com/d2lang/d2/releases/download/"$TAG"/"$ARCHIVE")
    echo archive >> "$CALLS"
    echo download >> "$EVENTS"
    if [ "$RESULT" = substituted ]; then
      echo substituted > "$out"
    else
      cp "$ASSET" "$out"
    fi ;;
  "$BUNDLE_URL")
    echo bundle >> "$CALLS"
    [ "$RESULT" != missing ] || exit 1
    if [ -n "$out" ]; then echo bundle > "$out"; else echo bundle; fi ;;
  *) echo "unexpected curl request: $url" >&2; exit 2 ;;
esac
`
const fakeAttestationVerifier = `#!/bin/sh
set -eu
echo gh >> "$CALLS"
case "$*" in
  --version) echo 'gh version 2.105.0 (fixture)'; exit ;;
  "attestation verify "*--help) echo '--bundle --repo --signer-workflow --source-ref --format --jq' ; exit ;;
esac
[ "$1 $2" = "attestation verify" ]
shift 2
archive=$1
shift
[ "${archive##*/}" = "$ARCHIVE" ]
cmp "$archive" "$ASSET"
repo= workflow= ref= bundle= format= query= predicate=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --repo|-R) repo=$2 ;;
    --signer-workflow) workflow=$2 ;;
    --source-ref) ref=$2 ;;
    --bundle|-b) bundle=$2 ;;
    --format) format=$2 ;;
    --predicate-type) predicate=$2 ;;
    --jq|-q) query=$2 ;;
    *) echo "unexpected verifier option $1" >&2; exit 2 ;;
  esac
  shift 2
done
[ "$repo" = "d2lang/d2" ]
[ "$workflow" = "d2lang/d2/.github/workflows/release-archives.yml" ]
[ "$ref" = "refs/tags/$TAG" ]
[ "$(cat "$bundle")" = "bundle" ]
[ "$predicate" = https://slsa.dev/provenance/v1 ]
[ "$format" = json ]
[ -n "$query" ]
echo verify >> "$EVENTS"
case "$RESULT" in
  invalid) exit 1 ;;
  wrong_subject)
    printf '%s\t%s\n' "$DIGEST" "d2-$TAG-linux-arm64.tar.gz"
    printf '%064d\t%s\n' 0 "$ARCHIVE" ;;
  wrong_digest) printf '%064d\t%s\n' 0 "$ARCHIVE" ;;
  cache_changes)
    # A concurrent replacement of the shared cache must not change what gets unpacked.
    echo replaced > "$XDG_CACHE_HOME/d2/release/$ARCHIVE"
    printf '%s\t%s\n' "$DIGEST" "$ARCHIVE" ;;
  verified) printf '%s\t%s\n' "$DIGEST" "$ARCHIVE" ;;
  *) exit 2 ;;
esac
`
