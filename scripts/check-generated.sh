#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
snapshot="$(mktemp -d "${TMPDIR:-/tmp}/search-generated.XXXXXX")"
trap 'rm -rf "$snapshot"' EXIT
cp README.md "$snapshot/README.md"
cp -R examples "$snapshot/examples"
cp -R docs/assets/badges "$snapshot/badges"
cp -R docs/bench "$snapshot/bench"
GOWORK=off go -C docs run ./readme
GOWORK=off go -C docs run ./bench/render
GOWORK=off go -C docs run ./readme
GOWORK=off go -C docs run ./bench/render
diff -u "$snapshot/README.md" README.md
diff -ru "$snapshot/examples" examples
diff -ru "$snapshot/badges" docs/assets/badges
diff -ru "$snapshot/bench" docs/bench
