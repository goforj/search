#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
mkdir -p .artifacts
trap 'GOWORK=off go -C docs run ./report -showevents "$repo_root/.artifacts/unit.json"' ERR
GOWORK=off go test -json -count=1 -coverpkg='github.com/goforj/search/...' -coverprofile="$repo_root/.artifacts/root.cover" ./... > .artifacts/unit.json
scripts/local-modfile.sh driver/blevesearch test -json -count=1 -coverpkg='github.com/goforj/search/...' -coverprofile="$repo_root/.artifacts/bleve.cover" ./... >> .artifacts/unit.json
GOWORK=off go -C docs test -json -count=1 ./... >> .artifacts/unit.json
GOWORK=off go -C examples test -json -count=1 -coverpkg='github.com/goforj/search/...' -coverprofile="$repo_root/.artifacts/examples.cover" ./... >> .artifacts/unit.json
GOWORK=off go -C integration test -tags=integration -run '^$' ./...
GOWORK=off go -C docs run ./report -events "$repo_root/.artifacts/unit.json" -kind unit

