#!/bin/sh
set -eu

deps=$(go list -deps .)
forbidden=$(printf '%s\n' "$deps" | grep -E '^(net/http(/|$)|crypto/tls$|golang\.org/x/crypto/ssh(/|$)|github\.com/war-and-code/dircue/third_party/go-git/plumbing/transport(/|$))' || true)
if [ -n "$forbidden" ]; then
	fprintf='forbidden network dependencies are linked into dircue:\n%s\n'
	printf "$fprintf" "$forbidden" >&2
	exit 1
fi
