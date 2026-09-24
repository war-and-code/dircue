#!/bin/sh
set -eu

repo=$(git rev-parse --show-toplevel)
tmp=$(mktemp -d)
# The module cache is read-only, so make it writable before removing it.
trap 'chmod -R u+w "$tmp" 2>/dev/null; rm -rf "$tmp"' EXIT INT TERM
mkdir -p "$tmp/source" "$tmp/gopath" "$tmp/gomodcache" "$tmp/bin"
git -C "$repo" archive HEAD | tar -x -C "$tmp/source"
go run "$tmp/source/scripts/modulezip/main.go" "$tmp/source" "$tmp/proxy"

GOWORK=off \
GOPROXY="file://$tmp/proxy,https://proxy.golang.org" \
GONOSUMDB=github.com/war-and-code/dircue \
GOPATH="$tmp/gopath" \
GOMODCACHE="$tmp/gomodcache" \
GOBIN="$tmp/bin" \
	go install github.com/war-and-code/dircue@v1.0.0

test -x "$tmp/bin/dircue"
test "$("$tmp/bin/dircue" --version)" = "dircue 1.0.0"
