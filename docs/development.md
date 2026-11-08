# Developing search

The public contract lives in the root module. Service drivers use the standard library and remain root-module packages. Bleve and its dependency tree live in a separately published module.

| Module | Purpose | Published |
| --- | --- | --- |
| `.` | Search contract, service drivers, `searchfake`, `searchtest` | Yes |
| `driver/blevesearch` | Embedded Bleve driver | Yes |
| `docs` | GoDoc indexing, README generation, test and coverage reports | No |
| `examples` | Runnable example programs and their shared test runner | No |
| `integration` | Testcontainers fixtures and real-driver acceptance tests | No |

The root supports Go 1.24. The embedded driver, examples, and integration harness require Go 1.25 because the selected Bleve release requires it. A service-only application does not import Bleve or Testcontainers.

## Source ownership

GoDoc owns API descriptions and `@group` classification. `examples/<name>/main.go` owns each runnable example and its expected output. A shared test runner executes every program and checks its `Output:` comment. Unit coverage instruments these subprocesses and merges their Go coverage data; race test runs also enable race detection in the programs. `docs/readme` projects the quick start and a linked example index into the README alongside a curated index of application-facing operations. Example `@api` annotations associate symbols with code between `// snippet:start` and `// snippet:end`; the curated API index links API names to the relevant tested snippets within the README. Edit the sources and run `make generate`.

`searchtest.ScenarioNames` inventories mandatory conformance fixtures. The shared runner verifies that inventory against executed subtests. Every real driver must execute the same fixtures without skips before `docs/report` can generate a support claim. Driver evidence includes a hash of library sources, tests, integration fixtures, and module pins. Editing those files makes existing evidence stale until the full integration suite passes again.

`docs/unit-results.json` and `docs/integration-results.json` list actual executed tests, including subtests. `docs/coverage.json` combines statement blocks from unit tests, embedded tests, executable examples, and real service tests. Generated badges use these files. They do not count test declarations or substitute mocked HTTP tests for real-backend evidence.

## Local checks

```sh
make test
make test-race
make vet
make modules-check
make test-integration
make test-coverage
make generate
make check-generated
```

Focused backend checks are available:

```sh
make test-integration bleve
make test-integration meilisearch
make test-integration typesense
make test-integration opensearch
make test-integration elasticsearch
make test-integration postgres
make test-integration sqlite
make test-integration redis
```

`integration/versions.json` owns service versions and digest-pinned images. The harness checks each running server's reported version against that file. OpenSearch and Elasticsearch use authenticated nodes and run the complete contract with both one and two primary shards. Native bulk rejection is tested against both real servers. PostgreSQL and Redis also require disposable credentials. SQLite runs against a real local database with FTS5 enabled. The integration module registers pgx and go-sqlite3; library packages use only database/sql. The integration scripts enable the sqlite_fts5 build tag and require a C compiler for that selected test provider. Applications may select another FTS5-enabled SQLite provider without importing this test dependency.

A focused run does not refresh the all-driver support report. The complete run starts one Testcontainers container per engine for the entire package run. Tests use distinct physical index namespaces inside that container. Fixtures use disposable credentials, random host ports, bounded readiness, and package-level cleanup registered before assertions. The shared contract, authentication checks, schema drift, and native failure tests all reuse their engine container. Docker must be available; missing infrastructure fails the run instead of silently skipping it.

The checked-in `go.work` supports editor navigation. Tooling modules intentionally replace unpublished siblings with local source. Published modules have no local replacement. Validation scripts use `GOWORK=off`; the isolated driver receives a temporary modfile replacement only while testing unpublished root changes.

Run `scripts/tidy-modules.sh --check` to check every module without changing the published manifests. `make tidy` updates them deliberately.

## Release order

This checkout is prepared for the first `v0.1.0` release. The root and embedded-driver requirements are aligned to that candidate version. No tags have been published yet.

See the [first release review](release-readiness.md) for candidate verification and the remaining publication handoff.

Before a first release:

Replace the pre-release README notice and its authoritative `docs/assets/status.svg` badge with the published version information when tagging the first release.

1. Confirm the candidate version. If it changes, update the embedded driver's root requirement, tooling requirements, and version-specific workspace replacements together. Regenerate module sums and rerun all checks against staged siblings.
2. Run `make release-plan v0.1.0` with the chosen version after the pins are aligned. The plan reads `scripts/module-manifest.txt` and excludes tooling modules.
3. Commit the reviewed change. Tag the root as `v0.1.0` and the embedded module as `driver/blevesearch/v0.1.0` using `scripts/tag-all-modules.sh v0.1.0 --tag`.
4. Publish the root tag first. Confirm the root is resolvable through the Go module proxy, then publish the embedded driver's tag.
5. Run `make release-check v0.1.0`. It builds a separate consumer with `GOWORK=off`, rejects replacements, and tests independently downloaded root and embedded modules together.

Tagging, pushing, GitHub releases, documentation-site registration, framework generation, and external service setup remain explicit maintainer actions. Local passing tests do not prove that a module version is published.

## Design boundary

The implementation follows the portable lexical core from GoForj's search design: explicit definitions, complete replacement writes, typed filtering, stable pagination, term facets, structured highlights, receipt visibility, and honest bulk precision.

GoForj component wiring, named-engine registries, maker commands, reindex coordination, Lighthouse integration, vector search, and online promotion are separate work. Indexes remain derived data. Applications own authorization, tenant predicates, reconciliation, and recovery from their authoritative source.

## Driver support

Implemented drivers cover Bleve, Meilisearch, Typesense, OpenSearch, Elasticsearch, PostgreSQL FTS, SQLite FTS5, and standalone Redis Search. Every supported driver must execute the same complete scenario inventory. Algolia is outside the current scope.

SQL tests additionally prove actual schema drift detection, caller-owned pool lifetime, wide exact IDs and keywords, and rollback after a native constraint rejection. Redis tests prove its 4096-match boundary, authentication failures, native schema alteration, and removed-index detection. Lua commands isolate concurrent visibility but do not provide transactional rollback after a script error; such unproven writes retain unknown outcomes.

## Search behavior

- Declare an explicit `Definition`, provision it with a concrete driver's `CreateIndex`, then construct a portable `*search.Index`.
- Construction performs no backend I/O. Runtime validation never alters an existing schema. Definition changes require an explicit rebuild from authoritative records.
- Upserts replace complete documents. Deletes are idempotent. Call `Wait` to establish visibility after accepted writes; cancellation of a wait does not cancel accepted work.
- Text is literal, with every analyzed term required. Ranking and tokenization vary across engines. Driver changes preserve the application workflow, not identical results.
- Sorts are field-primary, followed by an ascending document-ID tie-break. Sortable fields must be required. Stable pages assume the index has not changed between queries.
- Integers use the exact common range from -9007199254740991 to 9007199254740991. Keywords retain UTF-8 byte order. Missing and null values are absent to filters; negation uses ordinary two-valued Boolean logic.
- Facets count matching documents before hit pagination. Repeated array values count once per document. Highlights contain untrusted text segments, never trusted HTML.
- Backend errors and observer events omit query text, documents, credentials, and server prose.

Keep search data rebuildable. Search is not the authoritative database or an authorization boundary. Apply trusted tenant predicates before querying so hits, totals, and facets all respect access policy.

### Lifecycle and credentials

Service credentials stay on the server. Use provisioning credentials only for explicit administration, then validate and serve with appropriately scoped runtime credentials. HTTP clients do not follow redirects, bound response bodies, and preserve TLS verification by default.

Receipts are process-local and bounded. Drivers retain the latest 4096 submissions. A retained receipt caches its first terminal result after `Wait`; an expired unobserved receipt reports `ErrReceiptExpired` as an unknown outcome. Recover after process loss by reconciling or replaying idempotent work from the source.
