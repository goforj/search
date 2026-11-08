#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
snapshot="$(mktemp -d "${TMPDIR:-/tmp}/search-generated.XXXXXX")"
trap 'rm -rf "$snapshot"' EXIT
cp README.md "$snapshot/README.md"
cp -R examples "$snapshot/examples"
GOWORK=off go -C docs run ./examplegen
GOWORK=off go -C docs run ./readme
GOWORK=off go -C docs run ./examplegen
GOWORK=off go -C docs run ./readme
diff -u "$snapshot/README.md" README.md
diff -ru "$snapshot/examples" examples
