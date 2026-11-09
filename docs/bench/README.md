# Driver performance

The driver comparison uses one deterministic synthetic catalog and identical query schedules through `search.Index`. The default baseline is **1,000,000 documents**. Larger runs accept up to 100,000,000 documents; measure those separately rather than mixing corpus sizes in one chart.

The [SQL benchmark audit](sql-audit.md) preserves the investigation of the original SQLite and PostgreSQL results, including verified arithmetic, replayed workloads, captured plans, and disposable index controls. The current comparison is a fresh complete run after the SQL access-path improvements.

## Reproduce

Run from the repository root with Go 1.25 or newer, a C compiler, and Docker:

```sh
# Measure all eight drivers sequentially. This can take many minutes.
make bench-drivers

# Validate and publish the completed run's measurements, graphs, and README section.
GOWORK=off go -C docs run ./bench/render -input "$PWD/.artifacts/performance/1000000"

# Redraw the checked-in snapshot without starting any engines.
make bench-render
```

The benchmark binary is built once, without the race detector or coverage instrumentation. Each driver runs in a separate process, gets a fresh index, and releases its files or disposable container before the next driver starts. Service startup and schema provisioning are excluded from load timing. The service fixtures use Testcontainers, verify actual engine versions, and share the [immutable image pins](../../integration/versions.json) used by the integration suite. Bleve uses its real disk index; SQLite uses its real FTS5 extension and WAL database. No hosted credentials or preexisting services are required.

The JSON reports and companion logs go into `.artifacts/performance/<document-count>/`. Existing reports are never overwritten. Set `SEARCH_BENCH_DIR` to a new directory for a repeat run, and keep other workloads idle while measuring.

```sh
# A larger scaling run on the same dataset generator.
SEARCH_BENCH_DOCUMENTS=10000000 make bench-drivers

# A short smoke run for one driver, kept separate from the published baseline.
SEARCH_BENCH_DOCUMENTS=10240 SEARCH_BENCH_DRIVERS=sqlite \
  SEARCH_BENCH_DIR="$PWD/.artifacts/performance/sqlite-smoke" make bench-drivers
```

`SEARCH_BENCH_DRIVERS` accepts space-separated driver names: `bleve meilisearch typesense opensearch elasticsearch postgres sqlite redis`. `SEARCH_BENCH_SAMPLES` defaults to 200 per workload and `SEARCH_BENCH_WARMUP` to 30. `SEARCH_BENCH_MACHINE` records a human-readable host description. The renderer requires reports for all eight drivers and rejects mixed corpus sizes, fingerprints, harness binaries, sample counts, or machine configurations. A failed driver remains visible with its failure and loaded-document count; it never receives a performance bar.

## Corpus and requests

The versioned `catalog-v1` generator lives in [the benchmark harness](../../integration/performance_test.go). It streams documents in ascending ordinal order without retaining the corpus in memory. Every document contains a title, a longer description, a partition keyword, one of eight categories, and an integer ordinal. Both text fields are indexed; title is the default searchable field. Partition and category are filterable, category is facetable, and ordinal is sortable.

Each partition contains at most 1,024 documents. The final partition may be shorter. The corpus SHA-256 hashes the ordered sequence of original document IDs and JSON bodies, each separated by a newline. Equal completed-run hashes prove the same source documents were generated for every driver. Root normalization and each driver's native encoding follow that shared input. The snapshot also records total source JSON body bytes. This is synthetic catalog data, not a claim to model every language, document length, or production distribution.

| Workload | Request | Validation |
| --- | --- | --- |
| Literal search | All terms `portable batchNNNNNNNN` in the title, limit 20 | Matching partition, unique IDs, correct bodies and total |
| Filter and sort | Exact partition filter, ordinal descending, limit 20 | Matching partition, exact ordinal order, IDs, bodies and total |
| Text and facets | Title term `portable`, exact partition filter, category facets, limit 20 | Matching hits, total, all category buckets and their counts |

The same fixed permutation visits partitions throughout the corpus. Warm-up uses the beginning of that sequence, followed by timed requests. Each sample times the complete public `Index.Search` call, including validation, transport when applicable, response parsing, and hit normalization. Correctness checks happen after the timer stops. Unexpected results fail the run rather than contributing samples. Total-count precision is retained as reported by the driver, including Meilisearch estimates.

The latency charts show p95 in milliseconds on an explicitly labeled logarithmic axis. Each tick is ten times the previous tick, and the three latency charts share the same bounds. This keeps all drivers visible when latencies span several orders of magnitude. Labels show milliseconds rounded to three decimals; the table also shows p50. Indexing throughput uses a linear axis starting at zero. Percentiles use the nearest-rank definition; all raw nanosecond samples remain in the snapshot. These are observations from one run, not confidence intervals. Repeat runs are needed before making close comparisons. A single client issues requests sequentially; reciprocal latency is not a measurement of concurrent throughput.

## Indexing and resource limits

Load time includes deterministic document generation, corpus hashing, root validation, batches of 1,000 `Bulk` mutations, and `Wait` until each batch is searchable. It includes native commit or refresh costs. It excludes container startup and schema provisioning. Indexing throughput is document count divided by this complete load time, not a native bulk-import speed claim. Background merging may continue after load completion; query warm-up begins immediately afterward.

Each load inserts new IDs into a fresh index. It does not measure replacing or deleting existing documents, which can have different costs from initial indexing.

The snapshot records the Go version, platform, CPU count, machine description, Docker CPU and memory allocation, backend versions and image digests, timestamps, Git revision, benchmark-binary SHA-256, load time, and latency samples. The benchmark binary checksum binds the driver runs to one compiled harness, including any source changes after the recorded Git revision. Reports from an interrupted load remain incomplete and cannot be imported until the failure is recorded or that driver is rerun in a fresh output directory.

The current million-document run was compiled from [33b0485](https://github.com/goforj/search/tree/33b0485), after adding SQL secondary indexes and indexable scalar predicates. Every driver used the same executable, corpus, warm-up schedule, and sample count. The snapshot retains the exact source revision and executable checksum. The original pre-optimization snapshot remains available at [13afc50](https://github.com/goforj/search/blob/13afc50/docs/bench/benchmarks_rows.json). These are separate runs on a shared developer machine; differences include native query plans, caching, background activity, and the patched Go toolchain. They are not a controlled measurement of the SQL changes alone.

Services run with the integration fixture settings, without per-engine CPU or RAM limits. Elasticsearch and OpenSearch use one primary shard, zero replicas, a 100 ms refresh interval, and a 512 MiB JVM heap. PostgreSQL uses its stock container configuration and an application-owned four-connection pool. Redis persistence is disabled. SQLite uses WAL and a four-connection pool; Bleve writes to disk. The snapshot records these settings. This measures the library's portable path with explicit development configurations, rather than tuning each engine for its best production result.

The [SQL driver schema](../../internal/sqlsearch/provision.go) creates primary keys and native full-text indexes, plus secondary indexes for declared scalar capabilities. SQLite indexes the encoded document-ID join and `(scalar, id)` for filterable or sortable scalar fields. PostgreSQL uses bounded digest indexes for scalar equality filters, with the complete original comparison retained to reject digest collisions, and direct B-trees for bounded integer and Boolean range or sort keys. This preserves support for keyword values and IDs wider than PostgreSQL's B-tree tuple limit. Keyword ranges and ordering can still require scans or sorts.

These indexes apply to newly provisioned SQL storage. Existing storage remains supported without automatic migration; gaining the new physical indexes requires an explicit rebuild into a fresh namespace, as described in [SQL physical indexes](../development.md#sql-physical-indexes). SQLite's broad-term text-and-facets query still traverses the FTS match set even when the scalar predicate selects a small partition. The measurements describe this library's portable implementations and configurations, rather than the best possible performance of separately tuned native schemas.

Embedded engines execute on the host, while service engines include Docker networking and virtualization. The baseline was measured on a developer machine with other background services, rather than a dedicated benchmark host. Persistence, durability, ranking, merge behavior, native caching, and memory policies differ. The bars distinguish embedded and service engines using the sibling libraries' orange and gold palette. The graphs do not measure backend RSS, index size, cold-cache latency, concurrent saturation, replicas, sustained concurrent writes, or operational availability.

All searches deliberately stay within the portable Redis Search limit of 4,096 matches. The corpus still contains the full million documents, but each request matches at most 1,024. These selective workloads do not establish performance for broad queries matching hundreds of thousands of documents. In particular, the bounded Redis driver cannot execute those broad searches. Add a separately labeled workload and report unsupported drivers explicitly when exploring that behavior.

## Generation

The authoritative measurements are `benchmarks_rows.json`. `render.go` owns chart styling and README content. `make generate` and `make check-generated` regenerate both, and never launch a large benchmark. The SVG palette, typography, horizontal bars, and embedded/service legend follow the sibling queue and cache renderers. Raw benchmark artifacts and indexes stay ignored; only the validated snapshot and its generated SVG projections belong in Git.
