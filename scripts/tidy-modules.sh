#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
check="${1:-}"
if [[ -n "$check" && "$check" != --check ]]; then echo 'usage: tidy-modules.sh [--check]' >&2; exit 1; fi
for module_directory in . docs examples integration; do
  if [[ "$check" == --check ]]; then
    GOWORK=off go -C "$module_directory" mod tidy -diff
  else
    GOWORK=off go -C "$module_directory" mod tidy
  fi
done
module_temp="$(mktemp -d "${TMPDIR:-/tmp}/search-tidy.XXXXXX")"
trap 'rm -rf "$module_temp"' EXIT
cp driver/blevesearch/go.mod "$module_temp/module.mod"
if [[ -f driver/blevesearch/go.sum ]]; then cp driver/blevesearch/go.sum "$module_temp/module.sum"; fi
GOWORK=off go -C driver/blevesearch mod edit -modfile="$module_temp/module.mod" -replace="github.com/goforj/search=$repo_root"
GOWORK=off go -C driver/blevesearch mod tidy -modfile="$module_temp/module.mod"
GOWORK=off go -C driver/blevesearch mod edit -modfile="$module_temp/module.mod" -dropreplace=github.com/goforj/search
if [[ "$check" == --check ]]; then
  diff -u driver/blevesearch/go.mod "$module_temp/module.mod"
  diff -u driver/blevesearch/go.sum "$module_temp/module.sum"
else
  cp "$module_temp/module.mod" driver/blevesearch/go.mod
  cp "$module_temp/module.sum" driver/blevesearch/go.sum
fi
