#!/bin/sh
set -eu
cd "$(dirname "$0")/.."

dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
./ci/build-wasm.sh "$dir/d2.wasm"
wc -c < "$dir/d2.wasm" | awk '{printf "D2 WASM: %d bytes (%.2f MiB)\n", $1, $1 / 1048576}'
