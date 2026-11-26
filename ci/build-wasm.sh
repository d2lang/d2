#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
. ./ci/sub/lib.sh
. ./ci/wasm-flags.sh

# D2 and its npm package have independent versions.
d2_version=$(git_describe_ref)
js_version=${NPM_VERSION:-$(awk -F'"' '/"version"/ {print $4}' d2js/js/package.json)}
out=${1:?expected output .wasm path}
# -buildmode=c-shared keeps the module loaded after its initialization, which
# is when d2js registers the API.
# shellcheck disable=SC2086 # wasm_flags is a list of flags.
tinygo build $wasm_flags -buildmode=c-shared \
  -ldflags="-X github.com/d2lang/d2/lib/version.Version=$d2_version -X github.com/d2lang/d2/d2js/d2wasm.jsVersion=$js_version" \
  -o "$out" ./d2js
# -no-debug leaves the function names in the module.
go run ./ci/wasm-strip "$out"
