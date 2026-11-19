#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
. ./ci/sub/lib.sh

# D2 and its npm package have independent versions.
d2_version=$(git_describe_ref)
js_version=${NPM_VERSION:-$(awk -F'"' '/"version"/ {print $4}' d2js/js/package.json)}
# The default 64 KiB goroutine stack overflows during D2 initialization.
exec tinygo build -target=wasm -stack-size=1MB -no-debug \
  -ldflags="-X github.com/d2lang/d2/lib/version.Version=$d2_version -X github.com/d2lang/d2/d2js/d2wasm.jsVersion=$js_version" \
  -o "${1:?expected output .wasm path}" ./d2js
