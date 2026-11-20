#!/bin/sh
set -eu
styles=$(go list -f '{{.Dir}}' github.com/alecthomas/chroma/v2/styles)
cp "$styles/github.xml" "$styles/catppuccin-mocha.xml" .
