#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
version="${1:-}"
mode="${2:---dry-run}"
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || [[ "$mode" != --dry-run && "$mode" != --tag ]]; then
  echo 'usage: tag-all-modules.sh vX.Y.Z [--dry-run|--tag]' >&2
  exit 1
fi
scripts/check-module-manifest.sh "$version"
if [[ "$mode" == --tag ]]; then
  test -z "$(git status --porcelain)" || { echo 'release requires a clean worktree' >&2; exit 1; }
  git rev-parse --verify HEAD >/dev/null
fi
while read -r classification module_directory extra; do
  [[ "$classification" == published ]] || continue
  tag="$version"
  if [[ "$module_directory" != . ]]; then tag="$module_directory/$version"; fi
  echo "$module_directory -> $tag"
  if [[ "$mode" == --tag ]]; then
    if git show-ref --verify --quiet "refs/tags/$tag"; then
      echo "tag already exists: $tag" >&2
      exit 1
    fi
  fi
done < scripts/module-manifest.txt
if [[ "$mode" == --tag ]]; then
  # Both tag names are preflighted before either mutation; pushing remains a separate operator action.
  git tag -a "$version" -m "chore: release $version"
  git tag -a "driver/blevesearch/$version" -m "chore: release blevesearch $version"
fi
