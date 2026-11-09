#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
documents="${SEARCH_BENCH_DOCUMENTS:-1000000}"
drivers="${SEARCH_BENCH_DRIVERS:-bleve meilisearch typesense opensearch elasticsearch postgres sqlite redis}"
output="${SEARCH_BENCH_DIR:-$repo_root/.artifacts/performance/$documents}"
mkdir -p "$output"
output="$(cd "$output" && pwd)"
export GOCACHE="${GOCACHE:-/tmp/gocache}"
export GOMODCACHE="${GOMODCACHE:-/tmp/gomodcache}"
export SEARCH_BENCH_DOCUMENTS="$documents"
export SEARCH_BENCH_MACHINE="${SEARCH_BENCH_MACHINE:-$(uname -m)}"
GOWORK=off go -C integration test -c -tags integration,performance,sqlite_fts5 -o "$output/benchmark.test" .
failed=0
for driver in $drivers; do
  case "$driver" in
    bleve|meilisearch|typesense|opensearch|elasticsearch|postgres|sqlite|redis) ;;
    *) echo "Unknown driver: $driver" >&2; exit 1 ;;
  esac
  if [[ -e "$output/$driver.json" ]]; then
    echo "Refusing to overwrite $output/$driver.json. Use a new SEARCH_BENCH_DIR." >&2
    exit 1
  fi
  echo "Benchmarking $driver with $documents documents"
  export SEARCH_BENCH_DRIVER="$driver"
  export SEARCH_BENCH_OUTPUT="$output/$driver.json"
  if ! (cd "$repo_root/integration" && "$output/benchmark.test" -test.run '^TestDriverPerformance$' -test.count=1 -test.timeout=24h -test.v) 2>&1 | tee "$output/$driver.log"; then
    failed=1
  fi
done
exit "$failed"
