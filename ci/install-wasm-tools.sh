#!/bin/sh
# Install the WASM toolchain for GitHub's Linux x86-64 runners.
set -eu
cd "${RUNNER_TEMP:?}"

tinygo=tinygo0.44.0.linux-amd64.tar.gz
binaryen=binaryen-version_133-x86_64-linux.tar.gz
curl --retry 5 --retry-all-errors -fsSL "https://github.com/tinygo-org/tinygo/releases/download/v0.44.0/$tinygo" -o "$tinygo"
curl --retry 5 --retry-all-errors -fsSL "https://github.com/WebAssembly/binaryen/releases/download/version_133/$binaryen" -o "$binaryen"
sha256sum --check --strict <<EOF
ed0f45543a49ff2a99120a5a1b8814a0750e1fda1a66f8e2a0a172576134aaf9  $tinygo
2dc9c7813f5375db93d96ead4b78222fcc3e2677bbb832297af4797782a37489  $binaryen
EOF
tar -xzf "$tinygo"
tar -xzf "$binaryen"
printf '%s\n' "$RUNNER_TEMP/tinygo/bin" "$RUNNER_TEMP/binaryen-version_133/bin" >> "$GITHUB_PATH"
