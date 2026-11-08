#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
mkdir -p .artifacts
backend="${1:-all}"
case "$backend" in
 all) pattern='.' ;;
 bleve) pattern='^TestBleve' ;;
 meilisearch) pattern='^TestMeilisearch|^TestService' ;;
 typesense) pattern='^TestTypesense|^TestService' ;;
 opensearch) pattern='^TestOpenSearch|^TestService' ;;
 *) echo 'usage: test-integration.sh [all|bleve|meilisearch|typesense|opensearch]' >&2; exit 1 ;;
esac
export INTEGRATION_DRIVER="$backend"
trap 'GOWORK=off go -C docs run ./report -showevents "$repo_root/.artifacts/integration.json"' ERR
GOWORK=off go -C integration test -json -count=1 -race -tags=integration -run "$pattern" \
  -coverpkg='github.com/goforj/search/...' -coverprofile="$repo_root/.artifacts/integration.cover" ./... > .artifacts/integration.json
if [[ "$backend" == all ]]; then
 GOWORK=off go -C docs run ./report -events "$repo_root/.artifacts/integration.json" -kind integration
fi
