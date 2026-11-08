#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
mkdir -p .artifacts
trap 'GOWORK=off go -C docs run ./report -showevents "$repo_root/.artifacts/unit.json"' ERR
GOWORK=off go test -json -count=1 -coverpkg='github.com/goforj/search/...' -coverprofile="$repo_root/.artifacts/root.cover" ./... > .artifacts/unit.json
scripts/local-modfile.sh driver/blevesearch test -json -count=1 -coverpkg='github.com/goforj/search/...' -coverprofile="$repo_root/.artifacts/bleve.cover" ./... >> .artifacts/unit.json
GOWORK=off go -C docs test -json -count=1 ./... >> .artifacts/unit.json
example_coverage="$(mktemp -d "$repo_root/.artifacts/example-coverage.XXXXXX")"
SEARCH_EXAMPLE_COVERAGE_DIR="$example_coverage" GOWORK=off go -C examples test -json -count=1 ./... >> .artifacts/unit.json
go tool covdata textfmt -i="$example_coverage" -o="$repo_root/.artifacts/examples.cover"
rm -rf "$example_coverage"
GOWORK=off go -C integration test -tags=integration,sqlite_fts5 -run '^$' ./...
GOWORK=off go -C docs run ./report -events "$repo_root/.artifacts/unit.json" -kind unit
GOWORK=off go -C docs run ./report -unit-coverage -coverage "$repo_root/.artifacts/root.cover,$repo_root/.artifacts/bleve.cover,$repo_root/.artifacts/examples.cover"

