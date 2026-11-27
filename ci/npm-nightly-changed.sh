#!/usr/bin/env bash
# Run in a full checkout of master. Print true to publish or false to skip.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

tags=$(curl --fail --silent --show-error --retry 3 --max-time 30 \
  'https://registry.npmjs.org/-/package/@d2lang%2fd2/dist-tags')
version=$(jq -er '
  if type != "object" or (.latest | type) != "string" then error("invalid npm dist-tags")
  elif has("nightly") then .nightly | select(type == "string" and length > 0)
  else "" end' <<<"$tags")
if [[ -z $version ]]; then
  echo true
  exit 0
fi

# The previous manual packer used a bare SHA. New nightlies prefix it with g.
if [[ ! $version =~ ^[0-9]+\.[0-9]+\.[0-9]+-nightly\.[0-9]{8}\.g?([0-9a-f]{7,40})$ ]]; then
  echo "Unrecognized npm nightly version: $version" >&2
  exit 1
fi
base=$(git rev-parse --verify "${BASH_REMATCH[1]}^{commit}")
common=$(git merge-base "$base" HEAD)
if [[ $common == "$(git rev-parse HEAD)" ]]; then
  # Already published, or a rerun of an older master commit.
  echo false
  exit 0
fi
if [[ $common != "$base" ]]; then
  echo "Published nightly $version is not on this master's history" >&2
  exit 1
fi

publish_if_changed() {
  local status=0
  git diff --quiet --no-renames "$base" HEAD -- "$@" || status=$?
  case $status in
    0) ;;
    1) echo true; exit 0 ;;
    *) exit "$status" ;;
  esac
}

publish_if_changed d2js
# The WASM engine consumes shared Go packages and their embedded fonts, JS and SVG.
publish_if_changed 'd2*' internal lib go.mod go.sum LICENSE.txt THIRD_PARTY_NOTICES.txt \
  ci/build-wasm.sh ci/install-wasm-tools.sh ci/sub .gitmodules .github/workflows/npm-nightly.yml \
  ':(exclude)d2cli' ':(exclude)d2chaos' ':(glob,exclude)**/*_test.go' ':(glob,exclude)**/testdata/**'
echo false
