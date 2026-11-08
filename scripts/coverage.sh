#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
scripts/test-unit.sh
scripts/test-integration.sh
GOWORK=off go -C docs run ./report -coverage "$repo_root/.artifacts/root.cover,$repo_root/.artifacts/bleve.cover,$repo_root/.artifacts/examples.cover,$repo_root/.artifacts/integration.cover"
