# SQL benchmark audit, November 9, 2026

This report records the original pre-optimization measurements and investigation at [13afc50](https://github.com/goforj/search/tree/13afc50). Their calculations were correct, but the SQL results measured expensive query plans in those drivers. The original snapshot is preserved at that revision. The current README graphs use a fresh complete run after the SQL improvements. Neither run is a general ranking of SQLite or PostgreSQL against search services.

## Checks completed

- All eight original records contain 1,000,000 accepted documents, the same source corpus hash and benchmark executable hash, and 200 positive latency samples per workload.
- Independent recomputation of nearest-rank p95 and nanosecond-to-millisecond conversion matches every latency value in the original three SVG charts.
- Fresh million-document SQLite and PostgreSQL runs used the original compiled executable, without a tracing wrapper. Their corpus and executable hashes match the original run. Returned hits, counts, sort order and facet buckets passed the original correctness checks.
- Controlled 100,000-document runs captured the actual SQL and bound values emitted by the production drivers. SQLite plans came from `EXPLAIN QUERY PLAN`; PostgreSQL plans came from `EXPLAIN (ANALYZE, BUFFERS)`.

## What the plans show

The original SQLite schema had no persistent index on the encoded ID used to join FTS rows to stored documents. Its baseline plans built automatic covering indexes during searches, scanned the relational table for filters, and used temporary B-trees for ordering. These costs grew with the full corpus even when the query returns only 20 hits.

PostgreSQL's native full-text GIN index was used for selective literal searches. Filtered searches had a different problem: no secondary filter index was created, and the compiler wrapped equality in `coalesce(column = parameter, false)`. In the captured 100,000-document plan, filtering scanned 100,000 rows and removed 98,976 to retain 1,024. Adding a B-tree alone still left that expression as a filter over the index. The diagnostic equality control produced an actual `Index Cond` and avoided the full scan.

## Controlled comparisons

Both controls use the same `catalog-v1` source documents, field capabilities, and queries as the main benchmark, at 100,000 documents. Each phase executes three warm-up requests and ten timed requests per workload. Timed requests use the original partition-selection formula and timed-request offset, adjusted for the smaller corpus. Tables use nearest-rank p50 in milliseconds.

The phases are:

1. **Baseline:** pre-optimization production SQL and schema.
2. **Analyzed:** the same schema after explicit `ANALYZE` on the document and value tables.
3. **Indexed:** disposable B-trees added on document ID, partition, and `(field, owner, value)`, followed by `ANALYZE`.
4. **Equality control:** those indexes plus a diagnostic rewrite of the benchmark's positive partition equality in `WHERE`, removing its `coalesce` wrapper.

The equality control is restricted to this positive, non-null benchmark predicate. It is not a general replacement for the compiler's portable Boolean semantics. The subsequent production fix uses an explicit non-null guard and retains missing/null behavior, including negation, with native regression tests and the full conformance suite.

The SQLite report also contains a rowid-join control. It first checks that all document and FTS rowids match in this fresh disposable database. That assumption is not an established compatibility guarantee for existing indexes. This control is diagnostic and is not shipped as driver behavior.

<!-- audit-results:start -->
### Million-document confirmation

Both confirmations used the original executable, without SQL tracing. All response checks passed; source corpus and executable hashes match the original run. These shorter runs used three warm-ups and ten timed requests, so their timed window differs from the original 30-warm-up, 200-sample run. They confirm the scale of the issue, rather than reproducing the original p95 estimate.

| Driver | Workload | Original p50 (ms) | Confirmation p50 (ms) |
| --- | --- | ---: | ---: |
| sqlite | literal | 1061.935 | 4330.477 |
| sqlite | filter_sort | 303.523 | 328.707 |
| sqlite | facets | 1545.218 | 1579.806 |
| postgres | literal | 6.680 | 3.518 |
| postgres | filter_sort | 160.948 | 171.895 |
| postgres | facets | 998.463 | 937.507 |

### 100,000-document controls

These are diagnostic medians from the same disposable database within each engine. They are not replacement million-document benchmark rows.

| Driver | Workload | Baseline | Analyzed | Indexed | Equality control |
| --- | --- | ---: | ---: | ---: | ---: |
| sqlite | literal | 148.064 | 107.811 | 3.005 | 2.949 |
| sqlite | filter_sort | 31.294 | 40.238 | 19.681 | 0.659 |
| sqlite | facets | 159.858 | 167.636 | 185.951 | 186.877 |
| postgres | literal | 6.647 | 5.443 | 5.972 | 6.132 |
| postgres | filter_sort | 34.517 | 30.256 | 22.734 | 1.308 |
| postgres | facets | 78.090 | 79.663 | 78.556 | 5.011 |

Raw evidence: [SQLite million-document replay](sql-audit/2026-11-09-sqlite-stock-1m.json), [PostgreSQL million-document replay](sql-audit/2026-11-09-postgres-stock-1m.json), [SQLite SQL and plans](sql-audit/2026-11-09-sqlite-controls-100k.json), [PostgreSQL SQL and plans](sql-audit/2026-11-09-postgres-controls-100k.json).
<!-- audit-results:end -->

Explicit `ANALYZE` did not remove the structural access-path problems. Indexes alone substantially improved SQLite literal search, but did not fix its broad-term facet workload. PostgreSQL and SQLite filtering benefited most when the indexed equality became an index search condition. The raw reports include every measured sample, captured statement, bound argument and native plan.

## Repeatability limits

The original benchmark had one measured run per driver on a developer machine with background services. A repeat run can vary with filesystem caching, temporary-index I/O, concurrent activity and database statistics. The shorter confirmation runs do not provide a second 200-sample p95 estimate or confidence intervals.

A traced million-document SQLite diagnostic run timed out during filtering. It is excluded from the timing comparison. The original uninstrumented executable subsequently completed the million-document workload, which provides the repeat measurement reported here. The cause of the traced-run timeout has not been isolated, so its timings are excluded from both benchmark snapshots.

The original snapshot is preserved in Git at the revision linked above. The replacement comparison reruns all eight drivers, including index-build costs, rather than substituting diagnostic control timings for individual chart rows.

## Reproduce the original controls

Use an isolated checkout of `13afc50`, with Docker available, to reproduce these pre-optimization controls:

```sh
mkdir -p .artifacts/sql-audit
GOWORK=off go -C integration test -c -tags integration,performance,sqlite_fts5 \
  -o "$PWD/.artifacts/sql-audit/audit.test" .

for engine in sqlite postgres; do
  SEARCH_BENCH_DRIVER="$engine" \
  SEARCH_BENCH_DOCUMENTS=100000 \
  SEARCH_BENCH_SAMPLES=10 \
  SEARCH_BENCH_OUTPUT="$PWD/.artifacts/sql-audit/$engine.json" \
    .artifacts/sql-audit/audit.test \
      -test.run '^TestSQLPerformanceAudit$' -test.timeout=1h -test.v
done
```

Each process owns its disposable database or container. Statistics, experimental indexes and predicate rewrites apply only to that database. Queries still run through `search.Index`, and response verification occurs outside the timed region. The tracing connection forwards the native driver's transaction, value-conversion, session-reset and validation behavior. Plans are collected after the timed requests.

The audit implementation is [checked in](../../integration/sql_performance_audit_test.go). It adds no production driver behavior, schema migration or public API change.
