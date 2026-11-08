#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
profile="${1:-$repo_root/.artifacts/unit-coverage.out}"
awk '
  NR > 1 { total += $2; if ($3 > 0) covered += $2 }
  END {
    if (total == 0 || covered != total) {
      printf "unit coverage: %d of %d statements covered; require 100%%\n", covered, total > "/dev/stderr"
      exit 1
    }
    printf "unit coverage: 100%% (%d statements)\n", total
  }
' "$profile"
