#!/bin/sh

# Print a verifier executable, using the caller's private temporary directory
# for a one-install copy when a compatible GitHub CLI is unavailable.
installer_gh() (
  # gh 2.102.0 fixed exact workflow and case-sensitive ref matching
  # (GHSA-wjmr-j3rp-mh2g and GHSA-4mq3-hpgx-9cx8).
  if gh --version 2>/dev/null | awk '
    NR == 1 && $1 == "gh" && $2 == "version" && $3 ~ /^[0-9]+\.[0-9]+\.[0-9]+$/ {
      split($3, v, ".")
      supported = v[1] > 2 || (v[1] == 2 && v[2] >= 102)
    }
    END { exit !supported }
  ' && gh attestation verify --bundle /dev/null --source-ref test --signer-workflow test --help >/dev/null 2>&1; then
    command -v gh
    return
  fi

  version=2.105.0
  # The verifier must run on this host even when OS/ARCH select a different D2 archive.
  host_os=$(uname -s) || return 1
  host_arch=$(uname -m) || return 1
  case "$host_os" in
    Linux) host_os=linux ;;
    Darwin) host_os=macos ;;
    MSYS*|MINGW*|CYGWIN*) host_os=windows ;;
  esac
  case "$host_arch" in
    x86_64|amd64) host_arch=amd64 ;;
    aarch64|arm64) host_arch=arm64 ;;
  esac
  # From cli/cli's immutable release and gh_2.105.0_checksums.txt. Pin the
  # archive here so it is authenticated before any of its code can run.
  case "$host_os-$host_arch" in
    linux-amd64)   digest=79e7ab5db738c48777b696e374f8b428f0647debfb424f44d2f426370407fe2f ;;
    linux-arm64)   digest=b1278c75107423d818f1937fd92cafc9e5e9b606631568f68d7fb816f5885a02 ;;
    macos-amd64)   digest=23602cbccbbdc6285a8520745b714cb927ce2ca621779373c2a20cd8e33fda66 ;;
    macos-arm64)   digest=2a5d03103ae444927383b93d9ffd65579a26e2eb900ce1cf11f55258d5319ded ;;
    windows-amd64) digest=8813a72c899900afaa10e3fec8fbc64b908c89dbfe837e95ed2fa500fc140627 ;;
    windows-arm64) digest=5282a09c7757210c6e2dfb11a56b11e14e646df9b3b6a49f155465f5f881c3b7 ;;
    *) echo >&2 "No pinned GitHub CLI for $host_os-$host_arch. Install gh 2.102.0 or later."; return 1 ;;
  esac

  executable=gh
  case "$host_os" in
    linux)   archive="gh_${version}_linux_${host_arch}.tar.gz"; member="${archive%.tar.gz}/bin/gh" ;;
    macos)   archive="gh_${version}_macOS_${host_arch}.zip"; member="${archive%.zip}/bin/gh" ;;
    windows) archive="gh_${version}_windows_${host_arch}.zip"; member=bin/gh.exe; executable=gh.exe ;;
  esac

  echo >&2 "Downloading GitHub CLI $version to verify D2 release provenance"
  curl -qfsSL --proto '=https' --proto-redir '=https' -o "$1/$archive" \
    "https://github.com/cli/cli/releases/download/v$version/$archive" || return 1
  verify_sha256 "$1/$archive" "$digest" || return 1

  if [ "$host_os" = linux ]; then
    tar -xzOf "$1/$archive" "$member"
  elif command -v unzip >/dev/null 2>&1; then
    unzip -p "$1/$archive" "$member"
  else
    bsdtar -xOf "$1/$archive" "$member"
  fi >"$1/$executable" || return 1
  chmod 700 "$1/$executable" || return 1
  printf '%s\n' "$1/$executable"
)
