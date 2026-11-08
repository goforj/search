# Developing search

The public contract lives in the root module. Service drivers use the standard library and remain root-module packages. Bleve and its dependency tree live in a separately published module.

| Module | Purpose | Published |
| --- | --- | --- |
| `.` | Search contract, service drivers, `searchfake`, `searchtest` | Yes |
| `driver/blevesearch` | Embedded Bleve driver | Yes |
| `docs` | GoDoc indexing, example generation, test and coverage reports | No |
| `examples` | Executable example sources and generated standalone programs | No |
| `integration` | Testcontainers fixtures and real-driver acceptance tests | No |

The root supports Go 1.24. The embedded driver, examples, and integration harness require Go 1.25 because the selected Bleve release requires it. A service-only application does not import Bleve or Testcontainers.

## Source ownership

GoDoc owns API descriptions and `@group` classification. `examples/*_test.go` owns executable examples and expected output. `docs/examplegen` projects those sources into `examples/<name>/main.go`; `docs/readme` projects them into the README alongside its GoDoc-derived API index. Edit the sources and run `make generate`.

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
```

`integration/versions.json` owns service versions and digest-pinned images. The harness checks each running server's reported version against that file. OpenSearch uses its security plugin with disposable Basic credentials and runs the complete contract with both one and two primary shards. Native bulk rejection is tested against the real server.

A focused run does not refresh the all-driver support report. The complete run starts one Testcontainers container per engine for the entire package run. Tests use distinct physical index namespaces inside that container. Fixtures use disposable credentials, random host ports, bounded readiness, and package-level cleanup registered before assertions. The shared contract, authentication checks, schema drift, and native failure tests all reuse their engine container. Docker must be available; missing infrastructure fails the run instead of silently skipping it.

The checked-in `go.work` supports editor navigation. Tooling modules intentionally replace unpublished siblings with local source. Published modules have no local replacement. Validation scripts use `GOWORK=off`; the isolated driver receives a temporary modfile replacement only while testing unpublished root changes.

Run `scripts/tidy-modules.sh --check` to check every module without changing the published manifests. `make tidy` updates them deliberately.

## Release order

This checkout has not been released. The embedded driver currently requires the unpublished root at `v0.0.0`; release checks refuse that placeholder.

Before a first release:

1. Select a release version and replace the embedded driver's root requirement with that exact version. Update tooling requirements to match. Regenerate module sums and rerun all checks against staged siblings.
2. Run `make release-plan v0.1.0` with the chosen version after the pins are aligned. The plan reads `scripts/module-manifest.txt` and excludes tooling modules.
3. Commit the reviewed change. Tag the root as `v0.1.0` and the embedded module as `driver/blevesearch/v0.1.0` using `scripts/tag-all-modules.sh v0.1.0 --tag`.
4. Publish the root tag first. Confirm the root is resolvable through the Go module proxy, then publish the embedded driver's tag.
5. Run `make release-check v0.1.0`. It builds a separate consumer with `GOWORK=off`, rejects replacements, and tests independently downloaded root and embedded modules together.

Tagging, pushing, GitHub releases, documentation-site registration, framework generation, and external service setup remain explicit maintainer actions. Local passing tests do not prove that a module version is published.

## Design boundary

The implementation follows the portable lexical core from GoForj's search design: explicit definitions, complete replacement writes, typed filtering, stable pagination, term facets, structured highlights, receipt visibility, and honest bulk precision.

GoForj component wiring, named-engine registries, maker commands, reindex coordination, Lighthouse integration, vector search, and online promotion are separate work. Indexes remain derived data. Applications own authorization, tenant predicates, reconciliation, and recovery from their authoritative source.

## Driver roadmap

The implemented drivers cover embedded Bleve, Meilisearch, Typesense, and OpenSearch. [PostgreSQL full-text search](https://www.postgresql.org/docs/current/datatype-textsearch.html) and [SQLite FTS5](https://sqlite.org/fts5.html) are the next useful candidates for applications that want search in their existing database. [Redis Search](https://redis.io/docs/latest/develop/interact/search-and-query/) is another candidate for applications already operating Redis with its search capability. Elasticsearch should have an independent driver and version matrix rather than being treated as an OpenSearch alias. Algolia would add a hosted-service option. None of these candidates currently has a support claim or integration evidence.
