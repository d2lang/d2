#!/bin/sh
set -eu
cd "$(dirname "$0")/.."

CI=1 tinygo test -target=wasm -stack-size=1MB -no-debug -timeout=30m \
  ./d2js/d2wasm ./d2layouts/d2elklayout "$@"
