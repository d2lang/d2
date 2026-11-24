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
	for _, tc := range []struct {
		name    string
		version string
		outcome string
		cached  bool
		latest  bool
	}{
		{"verified_upgrade", "v0.9.1", "verified", false, false},
		{"verified_first_attested_release", "v0.8.2", "verified", false, false},
		{"verified_historical_bundle", "v0.9.0", "verified", false, false},
		{"explicit_legacy_release", "v0.7.1", "legacy", false, false},
		{"invalid_signature_for_cached_forced_install", "v0.9.1", "invalid", true, false},
		{"different_signed_platform", "v0.9.1", "wrong_subject", false, false},
		{"cache_changes_after_verification", "v0.9.1", "cache_changes", true, false},
		{"different_signed_digest", "v0.9.1", "wrong_digest", false, false},
		{"missing_bundle", "v0.9.1", "missing", false, false},
		{"latest_resolves_to_unattested_release", "v0.7.1", "missing", false, true},
		{"dry_run", "v0.9.1", "dry_run", false, false},
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
			archive := newDir + "-linux-amd64.tar.gz"
			asset := filepath.Join(staged, archive)
			if out, err := exec.Command("tar", "-czf", asset, "-C", staged, newDir).CombinedOutput(); err != nil {
				t.Fatalf("create fixture: %v\n%s", err, out)
			}
			payload, err := os.ReadFile(asset)
			if err != nil {
				t.Fatal(err)
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(payload))
			if tc.cached {
				if err := os.WriteFile(filepath.Join(cache, "d2", "release", archive), payload, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(bin, "d2"), "#!/bin/sh\necho \"$OLD_VERSION\"\n", 0o755)
			write(filepath.Join(bin, "make"), "#!/bin/sh\necho \"$1\" >> \"$EVENTS\"\n", 0o755)
			write(filepath.Join(bin, "curl"), fakeReleaseCurl, 0o755)
			write(filepath.Join(bin, "gh"), fakeAttestationVerifier, 0o755)
			events := filepath.Join(temp, "events")
			write(events, "", 0o644)
			version := tc.version
			if tc.latest {
				version = "latest"
			}
			args := []string{"../../install.sh", "--method=standalone", "--version=" + version, "--prefix=" + prefix}
			if tc.cached {
				args = append(args, "--force")
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
				"PATH=" + bin + ":/usr/bin:/bin", "OS=linux", "ARCH=amd64", "TMPDIR=" + temp,
				"XDG_CACHE_HOME=" + cache, "OLD_VERSION=" + oldVersion, "TAG=" + tc.version,
				"ARCHIVE=" + archive, "ASSET=" + asset, "DIGEST=" + digest, "BUNDLE_URL=" + bundleURL, "EVENTS=" + events, "RESULT=" + tc.outcome,
			}
			output, err := cmd.CombinedOutput()
			wantSuccess := tc.outcome == "verified" || tc.outcome == "cache_changes" || tc.outcome == "legacy" || tc.outcome == "dry_run"
			if (err == nil) != wantSuccess {
				t.Fatalf("installer error = %v, want success = %t\n%s", err, wantSuccess, output)
			}
			log, err := os.ReadFile(events)
			if err != nil {
				t.Fatal(err)
			}
			trace := string(log)
			if tc.cached && strings.Contains(trace, "download") {
				t.Fatal("installer redownloaded a cache entry that passed its checksum")
			}
			if wantSuccess && tc.outcome != "dry_run" {
				wantOrder := "verify\nuninstall\ninstall\n"
				if tc.outcome == "legacy" {
					wantOrder = "download\nuninstall\ninstall\n"
				}
				if !strings.HasSuffix(trace, wantOrder) {
					t.Fatalf("unexpected upgrade order: %q", trace)
				}
				if _, err := os.Stat(filepath.Join(prefix, "lib", "d2", newDir, "new-install")); err != nil {
					t.Fatalf("verified archive was not extracted: %v", err)
				}
				return
			}
			if tc.outcome == "dry_run" && trace != "" {
				t.Fatalf("dry run performed work: %q", trace)
			}
			if tc.outcome != "dry_run" && tc.outcome != "missing" && !strings.Contains(trace, "verify\n") {
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
    echo "https://github.com/d2lang/d2/releases/tag/$TAG"
    exit ;;
  https://api.github.com/repos/d2lang/d2/releases/tags/"$TAG")
    printf '{\n  "assets": [\n    {\n      "name": "%s",\n      "digest": "sha256:%s"\n    }\n  ]\n}\n' "$ARCHIVE" "$DIGEST" > "$out" ;;
  https://github.com/d2lang/d2/releases/download/"$TAG"/"$ARCHIVE")
    echo download >> "$EVENTS"
    cp "$ASSET" "$out" ;;
  "$BUNDLE_URL")
    [ "$RESULT" != missing ] || exit 1
    if [ -n "$out" ]; then echo bundle > "$out"; else echo bundle; fi ;;
  *) echo "unexpected curl request: $url" >&2; exit 2 ;;
esac
`
const fakeAttestationVerifier = `#!/bin/sh
set -eu
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
