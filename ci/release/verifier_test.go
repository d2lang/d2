package release_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerGHReusesCompatibleCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	installed := filepath.Join(dir, "gh")
	if err := os.WriteFile(installed, []byte("#!/bin/sh\necho 'gh version 2.102.0 (fixture)'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-eu", "-c", `
		. ./verifier.sh
		PATH="$1:$PATH"
		curl() { exit 1; }
		installer_gh "$1"
	`, "sh", dir)
	output, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != installed {
		t.Fatalf("expected installed verifier %s, got %q (%v)", installed, output, err)
	}
}

func TestInstallerGHRejectsCorruptDownloadBeforeExtraction(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cmd := exec.Command("sh", "-eu", "-c", `
		. ./checksum.sh
		. ./verifier.sh
		# This version recognizes the options but does not enforce them securely.
		gh() { echo 'gh version 2.101.0 (fixture)'; }
		uname() { case "$1" in -s) echo Darwin;; -m) echo arm64;; esac; }
		curl() {
		  while [ "$1" != -o ]; do shift; done
		  printf corrupt >"$2"
		}
		OS=linux ARCH=amd64 installer_gh "$1"
	`, "sh", dir)
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "macOS_arm64.zip has SHA-256") {
		t.Fatalf("expected verifier checksum rejection, got %q (%v)", output, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gh")); !os.IsNotExist(err) {
		t.Fatalf("verifier extraction started before checksum validation: %v", err)
	}
}

func TestInstallerGHBootstrapsOnMSYSWithoutUnzip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cmd := exec.Command("sh", "-eu", "-c", `
		. ./verifier.sh
		# Model a minimal MSYS2 PATH with bsdtar and no unzip utility.
		real_awk=$(command -v awk)
		real_chmod=$(command -v chmod)
		PATH=$1
		awk() { "$real_awk" "$@"; }
		chmod() { "$real_chmod" "$@"; }
		uname() { case "$1" in -s) echo MSYS_NT-10.0;; -m) echo aarch64;; esac; }
		curl() { :; }
		verify_sha256() { [ "${1##*/}" = gh_2.105.0_windows_arm64.zip ]; }
		bsdtar() {
		  [ "$1" = -xOf ] && [ "$3" = bin/gh.exe ] || return 1
		  printf payload
		}
		installer_gh "$1"
	`, "sh", dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("MSYS2 bootstrap failed: %s (%v)", output, err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "gh.exe")); err != nil || string(got) != "payload" {
		t.Fatalf("bootstrapped verifier = %q (%v)", got, err)
	}
}
