#!/bin/sh
# Install the WASM toolchain for GitHub's Linux x86-64 runners.
set -eu
cd "${RUNNER_TEMP:?}"

# TinyGo comes from https://github.com/d2lang/tinygo: 0.44.0 with what the
# build in ci/wasm-flags.sh needs.
tinygo_version=0.44.0-d2.2
binaryen_version=version_133
tinygo=tinygo${tinygo_version}.linux-amd64.tar.gz
binaryen=binaryen-${binaryen_version}-x86_64-linux.tar.gz
curl --retry 5 --retry-all-errors -fsSL "https://github.com/d2lang/tinygo/releases/download/v$tinygo_version/$tinygo" -o "$tinygo"
curl --retry 5 --retry-all-errors -fsSL "https://github.com/WebAssembly/binaryen/releases/download/$binaryen_version/$binaryen" -o "$binaryen"
sha256sum --check --strict <<EOF
6521b0bfdef55e979e49d978c61569cec3be333ffc64e26eaa186a9781d426d0  $tinygo
2dc9c7813f5375db93d96ead4b78222fcc3e2677bbb832297af4797782a37489  $binaryen
EOF
tar -xzf "$tinygo"
tar -xzf "$binaryen"
printf '%s\n' "$RUNNER_TEMP/tinygo/bin" "$RUNNER_TEMP/binaryen-$binaryen_version/bin" >> "$GITHUB_PATH"
