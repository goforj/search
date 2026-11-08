#!/usr/bin/env bash
set -euo pipefail

# Run a Go command against unpublished root changes without committing a driver replacement.
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
module_directory="${1:?usage: local-modfile.sh module-directory go-command arguments...}"
shift
module_temp="$(mktemp -d "${TMPDIR:-/tmp}/search-modfile.XXXXXX")"
trap 'rm -rf "$module_temp"' EXIT
cp "$repo_root/$module_directory/go.mod" "$module_temp/module.mod"
if [[ -f "$repo_root/$module_directory/go.sum" ]]; then
  cp "$repo_root/$module_directory/go.sum" "$module_temp/module.sum"
fi
cd "$repo_root/$module_directory"
GOWORK=off go mod edit -modfile="$module_temp/module.mod" -replace="github.com/goforj/search=$repo_root"
GOWORK=off go "$1" -modfile="$module_temp/module.mod" "${@:2}"
