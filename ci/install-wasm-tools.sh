#!/bin/sh
# Install the WASM toolchain for GitHub's Linux x86-64 runners.
set -eu
cd "${RUNNER_TEMP:?}"

tinygo_version=0.44.0
binaryen_version=version_133
tinygo=tinygo${tinygo_version}.linux-amd64.tar.gz
binaryen=binaryen-${binaryen_version}-x86_64-linux.tar.gz
curl --retry 5 --retry-all-errors -fsSL "https://github.com/tinygo-org/tinygo/releases/download/v$tinygo_version/$tinygo" -o "$tinygo"
curl --retry 5 --retry-all-errors -fsSL "https://github.com/WebAssembly/binaryen/releases/download/$binaryen_version/$binaryen" -o "$binaryen"
sha256sum --check --strict <<EOF
ed0f45543a49ff2a99120a5a1b8814a0750e1fda1a66f8e2a0a172576134aaf9  $tinygo
2dc9c7813f5375db93d96ead4b78222fcc3e2677bbb832297af4797782a37489  $binaryen
EOF
tar -xzf "$tinygo"
tar -xzf "$binaryen"
printf '%s\n' "$RUNNER_TEMP/tinygo/bin" "$RUNNER_TEMP/binaryen-$binaryen_version/bin" >> "$GITHUB_PATH"
