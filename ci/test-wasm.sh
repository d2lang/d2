#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
. ./ci/wasm-flags.sh

# The module must not have gained a go statement that it needs to run. Without
# the tag that makes them inert, TinyGo lists the reachable ones instead of
# building.
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
# shellcheck disable=SC2086 # wasm_flags is a list of flags.
tinygo build $(printf '%s' "$wasm_flags" | sed 's/ -tags=goroutines.inert//') -buildmode=c-shared \
  -o "$dir/d2.wasm" ./d2js >"$dir/out.txt" 2>&1 || true
# The go command reports module downloads on the same stream.
grep -v '^go: downloading ' "$dir/out.txt" >"$dir/build.txt" || true
message=': attempted to start a goroutine without a scheduler'
if grep -v "$message\$" "$dir/build.txt" >&2; then
  echo "ci/test-wasm.sh: listing the module's go statements failed" >&2
  exit 1
fi
sed -e "s|$message\$||" \
  -e 's|^.*/pkg/mod/golang.org/toolchain@[^/]*/src/||' \
  -e "s|^$(go env GOROOT)/src/||" \
  -e "s|^$(tinygo env TINYGOROOT)/src/||" \
  -e 's|^.*/pkg/mod/||' "$dir/build.txt" | sort >"$dir/found.txt"
grep -v '^#' ci/wasm-goroutines.txt | grep . | sort >"$dir/listed.txt"
if ! diff "$dir/listed.txt" "$dir/found.txt" >&2; then
  echo "ci/test-wasm.sh: the go statements in the module (>) are not the ones in ci/wasm-goroutines.txt (<)" >&2
  exit 1
fi

# TinyGo runs every test in a goroutine of its own, so the tests need the
# scheduler that the module is built without. What is particular to the
# module's build is covered by the go statements above and by d2.js's own
# tests, which run the module.
CI=1 tinygo test -target=wasm -stack-size=1MB -no-debug -timeout=30m \
  ./d2js/d2wasm ./d2layouts/d2elklayout "$@"
