<p align="center">
  <img src="https://raw.githubusercontent.com/goforj/search/main/docs/assets/logo.png" width="300" alt="search logo">
</p>

<p align="center">
  Portable lexical search for Go, from an embedded index to a search service.
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/goforj/search"><img src="https://pkg.go.dev/badge/github.com/goforj/search.svg" alt="Go Reference"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License: MIT"></a>
  <a href="https://github.com/goforj/search/actions/workflows/ci.yml"><img src="https://github.com/goforj/search/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://go.dev"><img src="https://img.shields.io/badge/go-1.24%2B-blue?logo=go" alt="Go 1.24 or newer"></a>
  <a href="https://codecov.io/gh/goforj/search"><img src="https://codecov.io/gh/goforj/search/graph/badge.svg" alt="Coverage"></a>
  <a href="https://github.com/goforj/search/tags"><img src="docs/assets/status.svg" alt="Unreleased"></a>
<!-- test-count:embed:start -->
<img src="docs/assets/badges/unit-tests.svg" alt="Executed unit tests, including subtests">
  <img src="docs/assets/badges/integration-tests.svg" alt="Executed integration tests, including subtests">
  <a href="docs/coverage.json"><img src="docs/assets/badges/coverage.svg" alt="Merged statement coverage"></a>
<!-- test-count:embed:end -->
</p>

<p align="center">
  <a href="#features">Features</a> · <a href="#quick-start">Quick Start</a> · <a href="#drivers">Drivers</a> · <a href="#installation">Installation</a> · <a href="#runnable-examples">Examples</a> · <a href="#api-index">API</a>
</p>

## Features

<!-- features:embed:start -->
| Feature | What you get | Example or API |
| --- | --- | --- |
| ![Literal search](docs/assets/badges/feature-search.svg) | Require every analyzed term, with explicit searchable fields. | [Typed search example](examples/search/main.go) |
| ![Typed filters](docs/assets/badges/feature-filters.svg) | Combine Boolean logic, exact values, integer ranges, and array membership. | [Filter example](examples/filters/main.go) |
| ![Stable sorting](docs/assets/badges/feature-sorting.svg) | Order by up to two declared fields, followed by the document ID. | [Sort](definition.go) |
| ![Pagination](docs/assets/badges/feature-pagination.svg) | Use bounded offsets and limits with explicit total-count precision. | [Query](query.go) |
| ![Term facets](docs/assets/badges/feature-facets.svg) | Count matching documents before pagination, deduplicating repeated array terms. | [Facet example](examples/facets/main.go) |
| ![Structured highlights](docs/assets/badges/feature-highlights.svg) | Render untrusted text segments with explicit match markers. | [Highlight example](examples/highlights/main.go) |
| ![Bulk operations](docs/assets/badges/feature-bulk.svg) | Retain input positions and honest per-item or aggregate outcomes. | [Bulk example](examples/bulk/main.go) |
| ![Visibility receipts](docs/assets/badges/feature-receipts.svg) | Wait for accepted writes to become searchable and retain recovery evidence. | [`Receipt` and `Outcome`](mutation.go) |
| ![Typed documents](docs/assets/badges/feature-documents.svg) | Use typed Go values while keeping document IDs separate from JSON bodies. | [Typed search example](examples/search/main.go) |
| ![Explicit schemas](docs/assets/badges/feature-schemas.svg) | Provision deliberately and detect schema drift without runtime migration. | [Definition](definition.go) |
| ![Safe observability](docs/assets/badges/feature-observability.svg) | Observe classified events with panic isolation and optional bounded async delivery. | [Observer](observer.go) · [AsyncObserver](observer_async.go) |
| ![Isolated testing](docs/assets/badges/feature-testing.svg) | Script results and inspect recorded calls without running a search engine. | [Fake backend example](examples/fake/main.go) |
<!-- features:embed:end -->

<!-- quickstart:embed:start -->
## Quick Start

Go 1.25+, with no external service required.

```sh
git clone https://github.com/goforj/search.git
cd search
go run ./examples/quickstart
```

<details>
<summary>View the full Go example</summary>

```go
package main

import (
	"context"
	"fmt"

	"github.com/goforj/search"
	"github.com/goforj/search/driver/blevesearch"
)

func main() {
	ctx := context.Background()
	driver, err := blevesearch.Open(ctx, blevesearch.Config{Memory: true})
	if err != nil {
		panic(err)
	}
	defer driver.Close()

	definition := search.Definition{
		Name:   "products",
		Fields: []search.Field{{Name: "title", Type: search.TextField, Searchable: true}},
	}
	if err := driver.CreateIndex(ctx, definition); err != nil {
		panic(err)
	}
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		panic(err)
	}

	type product struct {
		Title string `json:"title"`
	}
	receipt, err := search.Upsert(ctx, index, "mug-1", product{Title: "Blue mug"})
	if err != nil {
		panic(err)
	}
	outcome, err := index.Wait(ctx, receipt)
	if err != nil {
		panic(err)
	}
	if outcome.State != search.OutcomeSucceeded {
		panic(outcome.Error)
	}

	page, err := search.SearchAs[product](ctx, index, search.Query{Text: "blue mug"})
	if err != nil {
		panic(err)
	}
	fmt.Println(page.Hits[0].ID, page.Hits[0].Value.Title)
	// mug-1 Blue mug
}
```

</details>
<!-- quickstart:embed:end -->

## Drivers

Choose an embedded index, an existing SQL pool, or a shared search service.

<!-- drivers:embed:start -->
| Driver | Tested version | Mutation visibility | Bulk precision | Topology |
| ---: | --- | --- | --- | --- |
| [![Bleve](docs/assets/badges/driver-bleve.svg)](driver/blevesearch) | `v2.6.3` | synchronous | per item | single process; memory or disk |
| [![Meilisearch](docs/assets/badges/driver-meilisearch.svg)](driver/meilisearch) | `v1.17.0` | asynchronous receipt | aggregate task | shared service |
| [![Typesense](docs/assets/badges/driver-typesense.svg)](driver/typesensesearch) | `30.0` | synchronous | per item | shared service |
| [![OpenSearch](docs/assets/badges/driver-opensearch.svg)](driver/opensearch) | `3.9.0` | refresh-waiting | per item | shared service |
| [![Elasticsearch](docs/assets/badges/driver-elasticsearch.svg)](driver/elasticsearch) | `9.5.6` | refresh-waiting | per item | shared service |
| [![PostgreSQL FTS](docs/assets/badges/driver-postgres.svg)](driver/postgressearch) | `18.6` | synchronous | per item | application-owned SQL pool |
| [![SQLite FTS5](docs/assets/badges/driver-sqlite.svg)](driver/sqlitesearch) | `3.54.1` | synchronous | per item | embedded SQL; application-owned pool |
| [![Redis Search](docs/assets/badges/driver-redis.svg)](driver/redissearch) | `8.10.3` | synchronous | per item | standalone service; at most 4096 matches |

All listed engines pass the same literal all-terms, typed-filter, stable-sort, pagination, facet, highlight, replacement, receipt, bulk, cancellation, and concurrent-call fixtures. See [executed driver evidence](docs/driver-results.json) for exact scenario names and the source fingerprint.
<!-- drivers:embed:end -->

<details>
<summary>Driver behavior and limits</summary>

### Meilisearch

Meilisearch facets support at most 4096 distinct terms per requested field after filtering. Higher cardinality returns `ErrUnsupported`; it does not return an incorrectly chosen top bucket. The driver collects a bounded complete distribution because the engine does not preserve portable lexical ties at its native count cutoff.

Meilisearch accepts at most ten backend words. Queries in scripts with finer native segmentation use a conservative rune bound to prevent silent truncation. Its validated server search cutoff exceeds the enforced 30-second maximum HTTP deadline, so the client fails before the engine can return a silently timed-out response. As with schema validation, this assumes administrators do not change settings behind a running client.

### Elasticsearch and OpenSearch

Elasticsearch and OpenSearch each have an independent driver and tested version matrix. Their writes wait for refresh and active shard copies before reporting visibility. Native bulk responses retain per-item failures even when HTTP succeeds. Queries reject timeouts and partial shard responses. Multi-shard facet counts retain the engine's estimated precision when native error bounds are nonzero. Keyword values are limited to 16382 UTF-8 bytes because their ordered encoding must fit Lucene's term limit. Provisioning defaults to one primary shard and no replicas; configure replicas explicitly for a production cluster.

### PostgreSQL FTS and SQLite FTS5

PostgreSQL FTS and SQLite FTS5 accept an application-owned `*sql.DB`. The application selects and registers its SQL client, configures the pool, and closes it. The search driver creates dedicated tables, native full-text projections, and schema ownership metadata. Writes and their projections commit in one transaction. Queries compute hits, totals, and facets from one database snapshot. PostgreSQL uses the `simple` text-search configuration and byte-order collation. SQLite requires an FTS5-enabled provider. Driver `Close` preserves the injected pool.

### Redis Search

Redis Search targets standalone Redis with its native Search module. It uses native literal matching and typed filtering, then collects at most 4096 matching documents to provide deterministic multiple sorts and exact array facets. More matches return `ErrUnsupported`. Mutations execute as one Lua command, and interrupted or failed scripts report uncertainty rather than implying rollback. Native query timeout exceeds the enforced maximum client deadline to avoid accepting partial timed-out results. Cluster routing is not supported by this driver.

### Bleve

Bleve owns a single index path in one process. Its ownership lock refuses a second writer. Use a shared service when querying and indexing from separate processes. Close concrete drivers at the application lifecycle boundary.

</details>

## Installation

The root package and service drivers require **Go 1.24+** and have no third-party dependencies. The optional Bleve module requires **Go 1.25+**.

**Pre-release:** there are no published tags yet. Use the [workspace quick start](#quick-start) until the first release.

After the first release:

```sh
go get github.com/goforj/search
# Optional embedded driver:
go get github.com/goforj/search/driver/blevesearch
```

Service drivers are packages in the root module. See [module ownership and release order](docs/development.md).

See [search behavior and recovery](docs/development.md#search-behavior) for write visibility, query semantics, and receipt lifetime.

## Testing

Use [`searchfake`](searchfake) to queue search results and inspect recorded calls in application tests. It implements `search.Backend` without a running search engine. The [fake backend example](examples/fake/main.go) shows the complete workflow.

Driver authors can reuse [`searchtest`](searchtest) to verify the portable behavior contract. Integration tests run against real Bleve indexes and SQLite FTS5 databases, plus Testcontainers with pinned Meilisearch, Typesense, OpenSearch, Elasticsearch, PostgreSQL, and Redis images. Each service container starts once per run; tests use separate index namespaces. OpenSearch and Elasticsearch run with authentication enabled and execute the shared contract with one and two primary shards.

See the [executed driver evidence](docs/driver-results.json), [unit test inventory](docs/unit-results.json), and [integration test inventory](docs/integration-results.json) for the scenarios that actually ran.

Run `make bench` for allocation-aware [document validation benchmarks](validation_test.go) and [embedded search over 1,000 documents](examples/benchmark_test.go).

<!-- examples:embed:start -->
## Runnable examples

Each example is executed as a Go example test and generated into a standalone program under [`examples/`](examples). Run these commands from the repository root with Go 1.25 or newer.

| Example | Run | Tested source |
| --- | --- | --- |
| [Inspect bulk visibility](examples/bulk/main.go) | `go run ./examples/bulk` | [Example test](examples/bulk_test.go) |
| [Count facets before pagination](examples/facets/main.go) | `go run ./examples/facets` | [Example test](examples/facets_test.go) |
| [Test an application service](examples/fake/main.go) | `go run ./examples/fake` | [Example test](examples/fake_test.go) |
| [Filter and sort results](examples/filters/main.go) | `go run ./examples/filters` | [Example test](examples/filters_test.go) |
| [Escape structured highlights](examples/highlights/main.go) | `go run ./examples/highlights` | [Example test](examples/highlights_test.go) |
| [Index and search a document](examples/quickstart/main.go) | `go run ./examples/quickstart` | [Example test](examples/quickstart_test.go) |
| [Search typed documents](examples/search/main.go) | `go run ./examples/search` | [Example test](examples/search_test.go) |
| [Construct service drivers](examples/services/main.go) | `go run ./examples/services` | [Example test](examples/services_test.go) |
<!-- examples:embed:end -->

## Development

```sh
make test
make test-race
make vet
make test-integration
make test-coverage
make generate
make check-generated
```

API descriptions come from GoDoc. Runnable examples and their expected output come from `examples/*_test.go`. Edit those sources and run `make generate`; generation refreshes the standalone programs, quick start, example index, feature badges, and API index. CI checks that regeneration produces no diff.

See [development and release notes](docs/development.md) for focused driver tests, module boundaries, and publishing. Report problems or request features in [GitHub issues](https://github.com/goforj/search/issues).

<!-- api:embed:start -->
## API Index

API names open their source definitions. Example links jump to tested usage below.

### Backend

| API | Description | Example |
| --- | --- | --- |
| [blevesearch.Config](driver/blevesearch/driver.go#L19) | Config selects a single-index persistent path or an explicit in-memory index. | [Index and search a document](#example-quickstart) |
| [blevesearch.Driver](driver/blevesearch/driver.go#L25) | Driver owns one embedded index and its process lock. Close releases both. | [Index and search a document](#example-quickstart) |
| [blevesearch.Driver.Close](driver/blevesearch/driver.go#L109) | Close waits for admitted calls before releasing the embedded index and ownership lock. | [Index and search a document](#example-quickstart) |
| [elasticsearch.Config](driver/elasticsearch/driver.go#L25) | Config owns connection policy and materialized index settings. | [Construct service drivers](#example-services) |
| [elasticsearch.Driver](driver/elasticsearch/driver.go#L37) | Driver owns a concurrent client for one explicitly provisioned logical index. | [Construct service drivers](#example-services) |
| [elasticsearch.Driver.Close](driver/elasticsearch/driver.go#L106) | Close drains admitted calls and releases only connection pools owned by this driver. | [Construct service drivers](#example-services) |
| [elasticsearch.MaxKeywordBytes](driver/elasticsearch/driver.go#L19) | MaxKeywordBytes keeps the physical hexadecimal term below Lucene's term-byte limit. |  |
| [meilisearch.Config](driver/meilisearch/driver.go#L18) | Config owns service credentials, physical namespace, and optional transport policy. | [Construct service drivers](#example-services) |
| [meilisearch.Driver](driver/meilisearch/driver.go#L26) | Driver is a concurrent client for one provisioned logical index. | [Construct service drivers](#example-services) |
| [meilisearch.Driver.Close](driver/meilisearch/driver.go#L95) | Close prevents new calls without closing application-owned HTTP transports. | [Construct service drivers](#example-services) |
| [meilisearch.MaxFacetValues](driver/meilisearch/driver.go#L15) | MaxFacetValues bounds the distinct terms collected per requested facet before portable count-and-value ordering. |  |
| [opensearch.Config](driver/opensearch/driver.go#L25) | Config owns connection policy and materialized index settings. | [Construct service drivers](#example-services) |
| [opensearch.Driver](driver/opensearch/driver.go#L37) | Driver owns a concurrent client for one explicitly provisioned logical index. | [Construct service drivers](#example-services) |
| [opensearch.Driver.Close](driver/opensearch/driver.go#L106) | Close drains admitted calls and releases only connection pools owned by this driver. | [Construct service drivers](#example-services) |
| [opensearch.MaxKeywordBytes](driver/opensearch/driver.go#L19) | MaxKeywordBytes keeps the physical hexadecimal term below Lucene's term-byte limit. |  |
| [postgressearch.Config](driver/postgressearch/driver.go#L13) | Config uses an application-owned pool and an optional lowercase ASCII table namespace. |  |
| [postgressearch.Driver](driver/postgressearch/driver.go#L19) | Driver owns one logical index without taking ownership of the injected database pool. |  |
| [postgressearch.Driver.Close](driver/postgressearch/driver.go#L75) | Close drains admitted operations and preserves the application-owned database pool. |  |
| [redissearch.Config](driver/redissearch/driver.go#L25) | Config owns standalone Redis connection policy without changing persistent client authentication. | [Construct service drivers](#example-services) |
| [redissearch.Driver](driver/redissearch/driver.go#L36) | Driver owns one explicitly provisioned Redis Search index and process-local receipt state. | [Construct service drivers](#example-services) |
| [redissearch.Driver.Close](driver/redissearch/driver.go#L100) | Close drains admitted calls; every connection is already scoped to a single operation. | [Construct service drivers](#example-services) |
| [redissearch.MaxMatches](driver/redissearch/driver.go#L20) | MaxMatches bounds complete match collection needed for portable multi-sort and exact array facets. |  |
| [search.ErrClosed](errors.go#L54) | ErrClosed identifies a closed driver. |  |
| [search.ErrConflict](errors.go#L33) | ErrConflict identifies a competing owner or conflicting request. |  |
| [search.ErrIndexNotFound](errors.go#L12) | ErrIndexNotFound identifies an unprovisioned index. |  |
| [search.ErrInvalidDefinition](errors.go#L18) | ErrInvalidDefinition identifies an invalid schema. |  |
| [search.ErrInvalidDocument](errors.go#L21) | ErrInvalidDocument identifies invalid document input. |  |
| [search.ErrInvalidQuery](errors.go#L24) | ErrInvalidQuery identifies invalid query input. |  |
| [search.ErrInvalidReceipt](errors.go#L27) | ErrInvalidReceipt identifies a forged, foreign, or empty receipt. |  |
| [search.ErrMutationFailed](errors.go#L48) | ErrMutationFailed identifies accepted work that failed. |  |
| [search.ErrNotFound](errors.go#L9) | ErrNotFound identifies a missing document. |  |
| [search.ErrOutcomeUnknown](errors.go#L36) | ErrOutcomeUnknown means work may have been accepted and must be reconciled. |  |
| [search.ErrRateLimited](errors.go#L45) | ErrRateLimited identifies throttling. |  |
| [search.ErrReceiptExpired](errors.go#L51) | ErrReceiptExpired identifies accepted work whose result is no longer retained. |  |
| [search.ErrSchemaDrift](errors.go#L30) | ErrSchemaDrift identifies a difference from the provisioned schema. |  |
| [search.ErrUnauthorized](errors.go#L42) | ErrUnauthorized identifies rejected credentials or permissions. |  |
| [search.ErrUnavailable](errors.go#L39) | ErrUnavailable identifies a failed connection or incomplete backend response. |  |
| [search.ErrUnsupported](errors.go#L15) | ErrUnsupported identifies an unavailable backend capability. |  |
| [search.SafeError](errors.go#L58) | SafeError removes backend prose and unwrap chains while retaining known classifications. |  |
| [search.UnknownOutcomeError](errors.go#L78) | UnknownOutcomeError retains recoverable accepted-work evidence without exposing backend prose. |  |
| [sqlitesearch.Config](driver/sqlitesearch/driver.go#L13) | Config uses an application-owned pool and an optional lowercase ASCII table namespace. |  |
| [sqlitesearch.Driver](driver/sqlitesearch/driver.go#L19) | Driver owns one logical index without taking ownership of the injected database pool. |  |
| [sqlitesearch.Driver.Close](driver/sqlitesearch/driver.go#L75) | Close drains admitted operations and preserves the application-owned database pool. |  |
| [typesensesearch.Config](driver/typesensesearch/driver.go#L14) | Config supplies server-owned credentials and an optional physical namespace. | [Construct service drivers](#example-services) |
| [typesensesearch.Driver](driver/typesensesearch/driver.go#L22) | Driver owns one validated collection and bounded process-local mutation evidence. | [Construct service drivers](#example-services) |
| [typesensesearch.Driver.Close](driver/typesensesearch/driver.go#L78) | Close prevents new operations after admitted calls have completed. | [Construct service drivers](#example-services) |

### Construction

| API | Description | Example |
| --- | --- | --- |
| [blevesearch.Open](driver/blevesearch/driver.go#L37) | Open acquires ownership without creating or modifying an index schema. | [Index and search a document](#example-quickstart) |
| [elasticsearch.New](driver/elasticsearch/driver.go#L49) | New validates configuration without contacting the cluster or changing index state. | [Construct service drivers](#example-services) |
| [meilisearch.New](driver/meilisearch/driver.go#L46) | New validates transport configuration without contacting the server. | [Construct service drivers](#example-services) |
| [opensearch.New](driver/opensearch/driver.go#L49) | New validates configuration without contacting the cluster or changing index state. | [Construct service drivers](#example-services) |
| [postgressearch.New](driver/postgressearch/driver.go#L23) | New validates namespace policy without making connections or provisioning tables. |  |
| [redissearch.New](driver/redissearch/driver.go#L47) | New validates configuration without connecting or provisioning native state. | [Construct service drivers](#example-services) |
| [search.Backend](index.go#L14) | Backend is the consumer-owned request-path contract implemented by concrete drivers. |  |
| [search.DefaultLimits](limits.go#L32) | DefaultLimits returns conservative allocation and backend-work budgets. |  |
| [search.Index](index.go#L26) | Index validates calls and owns its immutable definition. It is safe for concurrent use. |  |
| [search.Limits](limits.go#L4) | Limits bounds work before it reaches a backend. Zero option fields inherit defaults. |  |
| [search.NewIndex](index.go#L54) | NewIndex validates a definition without performing backend I/O or schema mutation. | [Index and search a document](#example-quickstart) |
| [search.Option](index.go#L23) | Option configures domain limits or safe observation before the handle is published. |  |
| [search.WithLimits](index.go#L35) | WithLimits replaces nonzero limits, up to sixteen times each default ceiling. |  |
| [search.WithObserver](index.go#L45) | WithObserver attaches synchronous, panic-isolated observation without retaining contexts. |  |
| [sqlitesearch.New](driver/sqlitesearch/driver.go#L23) | New validates namespace policy without making connections or provisioning tables. |  |
| [typesensesearch.New](driver/typesensesearch/driver.go#L34) | New validates configuration without provisioning or contacting Typesense. | [Construct service drivers](#example-services) |

### Definitions

| API | Description | Example |
| --- | --- | --- |
| [blevesearch.Driver.CreateIndex](driver/blevesearch/provision.go#L16) | CreateIndex creates only an absent index; existing schema must match exactly. |  |
| [blevesearch.Driver.ValidateIndex](driver/blevesearch/provision.go#L56) | ValidateIndex compares both portable metadata and the actual materialized mapping. |  |
| [elasticsearch.Driver.CreateIndex](driver/elasticsearch/provision.go#L53) | CreateIndex creates only absent state and validates existing mappings without applying changes. |  |
| [elasticsearch.Driver.ValidateIndex](driver/elasticsearch/provision.go#L89) | ValidateIndex compares actual mappings and every owned index setting before marking the handle ready. |  |
| [meilisearch.Driver.CreateIndex](driver/meilisearch/provision.go#L64) | CreateIndex provisions only missing state and never applies changes to an existing schema. |  |
| [meilisearch.Driver.ValidateIndex](driver/meilisearch/provision.go#L116) | ValidateIndex compares portable identity and every driver-owned server setting. |  |
| [opensearch.Driver.CreateIndex](driver/opensearch/provision.go#L53) | CreateIndex creates only absent state and validates existing mappings without applying changes. |  |
| [opensearch.Driver.ValidateIndex](driver/opensearch/provision.go#L89) | ValidateIndex compares actual mappings and every owned index setting before marking the handle ready. |  |
| [postgressearch.Driver.CreateIndex](driver/postgressearch/driver.go#L33) | CreateIndex atomically provisions absent storage and validates existing schemas without patching them. |  |
| [postgressearch.Driver.ValidateIndex](driver/postgressearch/driver.go#L39) | ValidateIndex checks the portable fingerprint and actual materialized native schema. |  |
| [redissearch.Driver.CreateIndex](driver/redissearch/provision.go#L35) | CreateIndex provisions absent native schema with no stemming or stop-word dropping. |  |
| [redissearch.Driver.ValidateIndex](driver/redissearch/provision.go#L89) | ValidateIndex checks both portable ownership and actual native attributes without changing either. |  |
| [search.Ascending](definition.go#L34) | Ascending orders strings by UTF-8 bytes, Booleans false first, and integers numerically. | [Filter and sort results](#example-filters) |
| [search.BooleanField](definition.go#L22) | BooleanField enables Boolean filtering, ordering, and facets. |  |
| [search.Definition](definition.go#L58) | Definition describes rebuildable index data and default query behavior. | [Index and search a document](#example-quickstart) |
| [search.Definition.Fingerprint](definition.go#L74) | Fingerprint returns a versioned canonical hash of the normalized search contract. |  |
| [search.Definition.Validate](definition.go#L67) | Validate checks a definition with conservative default limits. |  |
| [search.Descending](definition.go#L36) | Descending reverses the portable scalar order. |  |
| [search.Direction](definition.go#L30) | Direction selects portable scalar order. |  |
| [search.Field](definition.go#L46) | Field maps one declared name to an explicit document path. | [Index and search a document](#example-quickstart) |
| [search.FieldType](definition.go#L14) | FieldType defines the portable mapping shapes. |  |
| [search.Index.Definition](index.go#L71) | Definition returns an owned copy of the normalized definition. |  |
| [search.IntegerField](definition.go#L24) | IntegerField enables exact integer filtering and ordering. |  |
| [search.KeywordField](definition.go#L20) | KeywordField enables exact scalar operations. |  |
| [search.KeywordsField](definition.go#L26) | KeywordsField enables array membership and term facets. |  |
| [search.Sort](definition.go#L40) | Sort selects a required, sortable field. Drivers append an ascending ID tie-break. | [Filter and sort results](#example-filters) |
| [search.TextField](definition.go#L18) | TextField enables analyzed full-text matching only. |  |
| [sqlitesearch.Driver.CreateIndex](driver/sqlitesearch/driver.go#L33) | CreateIndex atomically provisions absent storage and validates existing schemas without patching them. |  |
| [sqlitesearch.Driver.ValidateIndex](driver/sqlitesearch/driver.go#L39) | ValidateIndex checks the portable fingerprint and actual materialized native schema. |  |
| [typesensesearch.Driver.CreateIndex](driver/typesensesearch/provision.go#L48) | CreateIndex creates an absent collection and refuses to mutate an existing schema. |  |
| [typesensesearch.Driver.ValidateIndex](driver/typesensesearch/provision.go#L76) | ValidateIndex compares actual field settings and persisted ownership metadata. |  |

### Documents

| API | Description | Example |
| --- | --- | --- |
| [search.Decode](document.go#L33) | Decode reads a document without requiring an ID field in the application type. |  |
| [search.DecodeError](document.go#L78) | DecodeError identifies a malformed typed hit without disclosing its contents. |  |
| [search.Document](document.go#L14) | Document retains explicit identity separately from application-owned JSON. |  |
| [search.Encode](document.go#L21) | Encode marshals a typed value into a validated JSON object. |  |
| [search.Upsert](document.go#L47) | Upsert encodes a typed value before replacing the complete document. | [Index and search a document](#example-quickstart) |

### Filters

| API | Description | Example |
| --- | --- | --- |
| [search.And](filter.go#L74) | And combines predicates without allowing caller mutation of the child slice. | [Filter and sort results](#example-filters) |
| [search.Between](filter.go#L146) | Between tests an inclusive, ordered integer range. |  |
| [search.ContainsAll](filter.go#L110) | ContainsAll tests whether an array contains a nonempty set. |  |
| [search.ContainsAny](filter.go#L104) | ContainsAny tests array intersection with a nonempty set. |  |
| [search.Equal](filter.go#L92) | Equal tests scalar equality; absent and null values compare false. | [Filter and sort results](#example-filters) |
| [search.Exists](filter.go#L116) | Exists tests non-null presence, independently of array membership. |  |
| [search.Filter](filter.go#L38) | Filter is an immutable expression. A zero filter means no predicate on Query only. |  |
| [search.Filter.View](filter.go#L60) | View returns cloned collections so compilers cannot alter the expression. |  |
| [search.FilterAnd](filter.go#L10) | FilterAnd applies conjunction, with an empty conjunction matching all. |  |
| [search.FilterBetween](filter.go#L32) | FilterBetween tests an inclusive integer interval. |  |
| [search.FilterContainsAll](filter.go#L22) | FilterContainsAll tests every requested array member. |  |
| [search.FilterContainsAny](filter.go#L20) | FilterContainsAny tests at least one array member. |  |
| [search.FilterEqual](filter.go#L16) | FilterEqual compares an exact scalar. |  |
| [search.FilterExists](filter.go#L34) | FilterExists tests presence of a non-null value, including an empty array. |  |
| [search.FilterGreaterThan](filter.go#L28) | FilterGreaterThan compares integers exclusively. |  |
| [search.FilterGreaterThanOrEqual](filter.go#L30) | FilterGreaterThanOrEqual compares integers inclusively. |  |
| [search.FilterIn](filter.go#L18) | FilterIn tests membership in a scalar set. |  |
| [search.FilterKind](filter.go#L6) | FilterKind identifies an immutable expression node. |  |
| [search.FilterLessThan](filter.go#L24) | FilterLessThan compares integers exclusively. |  |
| [search.FilterLessThanOrEqual](filter.go#L26) | FilterLessThanOrEqual compares integers inclusively. |  |
| [search.FilterNot](filter.go#L14) | FilterNot complements its child's two-valued result. |  |
| [search.FilterOr](filter.go#L12) | FilterOr applies disjunction, with an empty disjunction matching none. |  |
| [search.FilterView](filter.go#L51) | FilterView is a detached driver-facing expression view. |  |
| [search.GreaterThan](filter.go#L134) | GreaterThan compares an integer against an exclusive lower bound. |  |
| [search.GreaterThanOrEqual](filter.go#L140) | GreaterThanOrEqual compares an integer against an inclusive lower bound. |  |
| [search.In](filter.go#L98) | In tests whether a scalar equals one of a nonempty set. |  |
| [search.LessThan](filter.go#L122) | LessThan compares an integer against an exclusive upper bound. | [Filter and sort results](#example-filters) |
| [search.LessThanOrEqual](filter.go#L128) | LessThanOrEqual compares an integer against an inclusive upper bound. |  |
| [search.Not](filter.go#L86) | Not complements a predicate; a zero child fails query validation. |  |
| [search.Or](filter.go#L80) | Or combines alternatives, with zero alternatives matching nothing. |  |

### Mutations

| API | Description | Example |
| --- | --- | --- |
| [blevesearch.Driver.Bulk](driver/blevesearch/mutation.go#L40) | Bulk commits one atomic Bleve batch and retains ordered per-item outcomes. |  |
| [blevesearch.Driver.Delete](driver/blevesearch/mutation.go#L26) | Delete commits idempotent removal synchronously. |  |
| [blevesearch.Driver.Upsert](driver/blevesearch/mutation.go#L12) | Upsert commits complete replacement data synchronously. |  |
| [elasticsearch.Driver.Bulk](driver/elasticsearch/mutation.go#L117) | Bulk submits native NDJSON and checks every item even when the HTTP request succeeds. |  |
| [elasticsearch.Driver.Delete](driver/elasticsearch/mutation.go#L93) | Delete treats missing documents idempotently while refusing to hide a removed index. |  |
| [elasticsearch.Driver.Upsert](driver/elasticsearch/mutation.go#L70) | Upsert replaces the complete document and waits for refresh on every participating shard. |  |
| [meilisearch.Driver.Bulk](driver/meilisearch/mutation.go#L91) | Bulk submits contiguous operation groups and retains every accepted task. |  |
| [meilisearch.Driver.Delete](driver/meilisearch/mutation.go#L76) | Delete submits an idempotent document removal. |  |
| [meilisearch.Driver.Upsert](driver/meilisearch/mutation.go#L61) | Upsert submits a complete replacement rather than a partial document merge. |  |
| [opensearch.Driver.Bulk](driver/opensearch/mutation.go#L117) | Bulk submits native NDJSON and checks every item even when the HTTP request succeeds. |  |
| [opensearch.Driver.Delete](driver/opensearch/mutation.go#L93) | Delete treats missing documents idempotently while refusing to hide a removed index. |  |
| [opensearch.Driver.Upsert](driver/opensearch/mutation.go#L70) | Upsert replaces the complete document and waits for refresh on every participating shard. |  |
| [postgressearch.Driver.Bulk](driver/postgressearch/driver.go#L63) | Bulk commits ordered mutations as one transaction with per-item terminal success evidence. |  |
| [postgressearch.Driver.Delete](driver/postgressearch/driver.go#L57) | Delete atomically removes the body and search projections, including absent IDs idempotently. |  |
| [postgressearch.Driver.Upsert](driver/postgressearch/driver.go#L51) | Upsert commits replacement and native search visibility in one transaction. |  |
| [redissearch.Driver.Bulk](driver/redissearch/mutation.go#L134) | Bulk atomically applies ordered mutations and retains exact per-item terminal success evidence. |  |
| [redissearch.Driver.Delete](driver/redissearch/mutation.go#L123) | Delete removes the complete native hash idempotently while checking index existence. |  |
| [redissearch.Driver.Upsert](driver/redissearch/mutation.go#L112) | Upsert replaces the entire native hash atomically before issuing a visibility receipt. |  |
| [search.DeleteMutation](mutation.go#L37) | DeleteMutation validates an ID for idempotent deletion. | [Inspect bulk visibility](#example-bulk) |
| [search.Index.Bulk](index.go#L153) | Bulk rejects duplicate IDs and validates the entire bounded batch before submission. | [Inspect bulk visibility](#example-bulk) |
| [search.Index.Delete](index.go#L132) | Delete removes an ID idempotently; visibility is established through Wait. |  |
| [search.Index.Upsert](index.go#L110) | Upsert replaces the complete document and returns accepted-work evidence. |  |
| [search.Mutation.Document](mutation.go#L58) | Document returns owned data only for upserts. |  |
| [search.Mutation.ID](mutation.go#L52) | ID returns the explicit document identity. |  |
| [search.Mutation.Kind](mutation.go#L46) | Kind identifies which operation this mutation contains. |  |
| [search.UpsertMutation](mutation.go#L27) | UpsertMutation takes ownership of document bytes after structural validation. |  |
| [sqlitesearch.Driver.Bulk](driver/sqlitesearch/driver.go#L63) | Bulk commits ordered mutations as one transaction with per-item terminal success evidence. |  |
| [sqlitesearch.Driver.Delete](driver/sqlitesearch/driver.go#L57) | Delete atomically removes the body and search projections, including absent IDs idempotently. |  |
| [sqlitesearch.Driver.Upsert](driver/sqlitesearch/driver.go#L51) | Upsert commits replacement and native search visibility in one transaction. |  |
| [typesensesearch.Driver.Bulk](driver/typesensesearch/mutation.go#L68) | Bulk parses every native import response line, retaining known per-item failures. |  |
| [typesensesearch.Driver.Delete](driver/typesensesearch/mutation.go#L54) | Delete removes an ID idempotently and proves synchronous visibility. |  |
| [typesensesearch.Driver.Upsert](driver/typesensesearch/mutation.go#L36) | Upsert replaces a complete document, including removal of previously indexed fields. |  |

### Observability

| API | Description | Example |
| --- | --- | --- |
| [search.AsyncObserver](observer_async.go#L17) | AsyncObserver delivers retained events in FIFO order from one bounded worker. |  |
| [search.AsyncObserver.Dropped](observer_async.go#L55) | Dropped reports the cumulative number of events discarded by bounded delivery. |  |
| [search.AsyncObserver.Observe](observer_async.go#L39) | Observe enqueues without blocking and counts full-queue or post-shutdown drops. |  |
| [search.AsyncObserver.Shutdown](observer_async.go#L61) | Shutdown closes admission once and waits for retained callbacks within the caller's deadline. |  |
| [search.Event](observer.go#L21) | Event contains bounded metadata without document, query, or backend prose. |  |
| [search.EventPhase](observer.go#L9) | EventPhase distinguishes submission latency from visibility and search latency. |  |
| [search.MultiObserver](observer.go#L47) | MultiObserver delivers events in registration order with independent panic isolation. |  |
| [search.MultiObserver.Observe](observer.go#L51) | Observe ensures a failed observer does not suppress later observers. |  |
| [search.NewAsyncObserver](observer_async.go#L28) | NewAsyncObserver starts a single delivery worker with capacity from 1 to 65536. |  |
| [search.Observer](observer.go#L35) | Observer receives synchronous events after driver locks have been released. |  |
| [search.ObserverFunc](observer.go#L38) | ObserverFunc adapts an application callback to an Observer. |  |
| [search.ObserverFunc.Observe](observer.go#L42) | Observe invokes the application callback. |  |
| [search.PhaseSearchComplete](observer.go#L17) | PhaseSearchComplete identifies a complete search call. |  |
| [search.PhaseSubmission](observer.go#L13) | PhaseSubmission identifies accepted-work submission. |  |
| [search.PhaseTerminal](observer.go#L15) | PhaseTerminal identifies terminal mutation observation. |  |

### Queries

| API | Description | Example |
| --- | --- | --- |
| [blevesearch.Driver.Search](driver/blevesearch/search.go#L14) | Search compiles literal queries and typed filters without a backend query-string parser. |  |
| [elasticsearch.Driver.Search](driver/elasticsearch/search.go#L15) | Search rejects timed-out or failed-shard responses and retains native count precision. |  |
| [meilisearch.Driver.Search](driver/meilisearch/search.go#L15) | Search disables progressive term dropping and makes declared field sorts primary. |  |
| [opensearch.Driver.Search](driver/opensearch/search.go#L15) | Search rejects timed-out or failed-shard responses and retains native count precision. |  |
| [postgressearch.Driver.Search](driver/postgressearch/driver.go#L45) | Search uses native full-text matching and exact snapshot-consistent totals and facets. |  |
| [redissearch.Driver.Search](driver/redissearch/search.go#L16) | Search collects a bounded complete native match set before portable sorting and exact pre-page facets. |  |
| [search.Highlight](query.go#L24) | Highlight requests untrusted text segments for one searchable field. | [Escape structured highlights](#example-highlights) |
| [search.Index.Search](index.go#L77) | Search validates literal text, filters, limits, and result documents before exposure. | [Filter and sort results](#example-filters) |
| [search.Query](query.go#L12) | Query specifies literal all-terms text, typed filtering, and bounded pagination. | [Filter and sort results](#example-filters) |
| [search.TermFacet](query.go#L29) | TermFacet counts distinct per-document terms before hit pagination. | [Count facets before pagination](#example-facets) |
| [sqlitesearch.Driver.Search](driver/sqlitesearch/driver.go#L45) | Search uses native full-text matching and exact snapshot-consistent totals and facets. |  |
| [typesensesearch.Driver.Search](driver/typesensesearch/search.go#L16) | Search disables implicit token dropping, fuzzy expansion, and prefix matching. |  |

### Receipts

| API | Description | Example |
| --- | --- | --- |
| [blevesearch.Driver.Wait](driver/blevesearch/mutation.go#L92) | Wait reports synchronous commit evidence, including bounded receipt expiration. |  |
| [elasticsearch.Driver.Wait](driver/elasticsearch/mutation.go#L227) | Wait returns visibility evidence established by the refresh-waiting write, including exact bulk outcomes. |  |
| [meilisearch.Driver.Wait](driver/meilisearch/mutation.go#L135) | Wait reports aggregate task precision without fabricating per-document success. |  |
| [opensearch.Driver.Wait](driver/opensearch/mutation.go#L227) | Wait returns visibility evidence established by the refresh-waiting write, including exact bulk outcomes. |  |
| [postgressearch.Driver.Wait](driver/postgressearch/driver.go#L69) | Wait reports visibility established by transaction commit without issuing another database query. |  |
| [redissearch.Driver.Wait](driver/redissearch/mutation.go#L160) | Wait returns only visibility established by an acknowledged atomic mutation command. |  |
| [search.BatchOnly](mutation.go#L119) | BatchOnly means only the aggregate outcome is known. |  |
| [search.BulkOutcome](mutation.go#L138) | BulkOutcome retains terminal per-item evidence without inventing precision. | [Inspect bulk visibility](#example-bulk) |
| [search.BulkResult](mutation.go#L131) | BulkResult retains submission precision and accepted-work receipt. | [Inspect bulk visibility](#example-bulk) |
| [search.Index.Wait](index.go#L216) | Wait returns terminal failure as Outcome data; method errors mean no trustworthy outcome yet. | [Index and search a document](#example-quickstart) |
| [search.ItemAccepted](mutation.go#L101) | ItemAccepted reports proven submission only. |  |
| [search.ItemFailed](mutation.go#L109) | ItemFailed reports proven per-item failure. |  |
| [search.ItemOutcomePrecision](mutation.go#L113) | ItemOutcomePrecision prevents aggregate success from implying individual success. |  |
| [search.ItemRejected](mutation.go#L103) | ItemRejected reports a submission rejection. |  |
| [search.ItemResult](mutation.go#L123) | ItemResult preserves the input position even when the backend reorders work. | [Inspect bulk visibility](#example-bulk) |
| [search.ItemStatus](mutation.go#L97) | ItemStatus retains submission and terminal states separately. |  |
| [search.ItemSucceeded](mutation.go#L107) | ItemSucceeded reports proven per-item visibility. |  |
| [search.ItemUnknown](mutation.go#L105) | ItemUnknown reports an unprovable individual outcome. |  |
| [search.Mutation](mutation.go#L20) | Mutation is an immutable tagged operation whose zero value is invalid. |  |
| [search.MutationDelete](mutation.go#L16) | MutationDelete removes an ID if present. |  |
| [search.MutationKind](mutation.go#L10) | MutationKind distinguishes complete replacement from idempotent deletion. |  |
| [search.MutationUpsert](mutation.go#L14) | MutationUpsert replaces a complete document. |  |
| [search.NewReceipt](mutation.go#L71) | NewReceipt creates a bounded driver token without attaching it to an Index. |  |
| [search.NewUnknownOutcomeError](errors.go#L84) | NewUnknownOutcomeError lets a driver return recoverable evidence after uncertain submission. |  |
| [search.Outcome](mutation.go#L144) | Outcome returns terminal failure as data, keeping any bulk details available. | [Index and search a document](#example-quickstart) |
| [search.OutcomeFailed](mutation.go#L91) | OutcomeFailed identifies terminal failure. |  |
| [search.OutcomeState](mutation.go#L85) | OutcomeState describes a trustworthy terminal mutation outcome. |  |
| [search.OutcomeSucceeded](mutation.go#L89) | OutcomeSucceeded proves visibility to subsequent queries on this index. | [Index and search a document](#example-quickstart) |
| [search.OutcomeUnknown](mutation.go#L93) | OutcomeUnknown identifies accepted work whose terminal result cannot be proved. |  |
| [search.PerItem](mutation.go#L117) | PerItem means every item has an individually proven outcome. |  |
| [search.Receipt](mutation.go#L63) | Receipt is an opaque, immutable process-local accepted-work token. |  |
| [search.Receipt.Token](mutation.go#L80) | Token returns an owned copy for the issuing backend. |  |
| [search.UnknownOutcomeError.Error](errors.go#L90) | Error reports uncertainty without serializing the opaque token. |  |
| [search.UnknownOutcomeError.Receipt](errors.go#L102) | Receipt returns immutable recovery evidence when the driver obtained a trustworthy token. |  |
| [search.UnknownOutcomeError.Unwrap](errors.go#L96) | Unwrap preserves the stable uncertainty classification. |  |
| [sqlitesearch.Driver.Wait](driver/sqlitesearch/driver.go#L69) | Wait reports visibility established by transaction commit without issuing another database query. |  |
| [typesensesearch.Driver.Wait](driver/typesensesearch/mutation.go#L143) | Wait reports retained per-item evidence or explicit receipt expiration. |  |

### Results

| API | Description | Example |
| --- | --- | --- |
| [search.Bucket](result.go#L45) | Bucket counts documents, deduplicating repeated array terms within each document. | [Count facets before pagination](#example-facets) |
| [search.CountEstimated](result.go#L30) | CountEstimated reports approximate term counts. |  |
| [search.CountExact](result.go#L28) | CountExact reports complete term counts. |  |
| [search.CountPrecision](result.go#L24) | CountPrecision describes term-bucket count accuracy. |  |
| [search.DecodeError.Error](document.go#L84) | Error reports only the classification; callers may inspect ID under their disclosure policy. |  |
| [search.DecodeError.Unwrap](document.go#L90) | Unwrap preserves invalid-document classification. |  |
| [search.FacetResult](result.go#L51) | FacetResult retains truncation separately from count precision. | [Count facets before pagination](#example-facets) |
| [search.Fragment](result.go#L40) | Fragment groups adjacent highlight segments. | [Escape structured highlights](#example-highlights) |
| [search.Hit](result.go#L60) | Hit is one document and its response-local ranking metadata. |  |
| [search.HitOf](result.go#L76) | HitOf is a typed hit whose document need not contain its identity. | [Search typed documents](#example-search) |
| [search.Page](result.go#L85) | Page is a typed complete search response. | [Search typed documents](#example-search) |
| [search.Result](result.go#L68) | Result contains one complete backend search response. | [Test an application service](#example-fake) |
| [search.SearchAs](document.go#L60) | SearchAs decodes every hit and preserves its separate identity and metadata. | [Search typed documents](#example-search) |
| [search.Segment](result.go#L34) | Segment is untrusted text; UIs escape Text even when Matched is true. | [Escape structured highlights](#example-highlights) |
| [search.Total](result.go#L18) | Total retains count precision independently of pagination. | [Test an application service](#example-fake) |
| [search.TotalEstimated](result.go#L12) | TotalEstimated reports an approximate count. |  |
| [search.TotalExact](result.go#L10) | TotalExact reports a complete count. | [Test an application service](#example-fake) |
| [search.TotalLowerBound](result.go#L14) | TotalLowerBound reports a proven minimum. |  |
| [search.TotalRelation](result.go#L6) | TotalRelation describes the accuracy of a result count. |  |

### Testing

| API | Description | Example |
| --- | --- | --- |
| [searchfake.Backend](searchfake/fake.go#L31) | Backend is a concurrent, explicitly scripted test double. |  |
| [searchfake.Backend.Bulk](searchfake/fake.go#L107) | Bulk records input order and returns individually proven success for every item. |  |
| [searchfake.Backend.Calls](searchfake/fake.go#L54) | Calls returns a detached snapshot so assertions cannot alter recorded evidence. | [Test an application service](#example-fake) |
| [searchfake.Backend.Delete](searchfake/fake.go#L95) | Delete records an idempotent removal and issues synchronous success evidence. |  |
| [searchfake.Backend.QueueSearch](searchfake/fake.go#L46) | QueueSearch appends a detached response for the next Search call. | [Test an application service](#example-fake) |
| [searchfake.Backend.Search](searchfake/fake.go#L66) | Search consumes the next scripted result, failing explicitly when no response is queued. |  |
| [searchfake.Backend.Upsert](searchfake/fake.go#L83) | Upsert records a replacement and issues synchronous success evidence. |  |
| [searchfake.Backend.Wait](searchfake/fake.go#L125) | Wait respects cancellation and returns retained terminal evidence. |  |
| [searchfake.Call](searchfake/fake.go#L15) | Call records the operation and owned inputs received after root validation. |  |
| [searchfake.New](searchfake/fake.go#L40) | New creates a fake whose writes succeed and whose reads require queued responses. | [Test an application service](#example-fake) |
| [searchtest.Definition](searchtest/searchtest.go#L21) | Definition returns the shared corpus schema with nullable, array, and required sort fields. |  |
| [searchtest.Provision](searchtest/searchtest.go#L17) | Provision creates an isolated, provisioned index and an idempotent cleanup callback. |  |
| [searchtest.RunBackend](searchtest/searchtest.go#L34) | RunBackend proves one portable corpus against an actual provisioned driver. |  |
| [searchtest.ScenarioNames](searchtest/searchtest.go#L429) | ScenarioNames inventories required fixtures for generated driver evidence. A skipped or missing scenario is not support. |  |

### Values

| API | Description | Example |
| --- | --- | --- |
| [search.Boolean](value.go#L53) | Boolean constructs a Boolean scalar. |  |
| [search.BooleanValue](value.go#L27) | BooleanValue represents a Boolean. |  |
| [search.DocumentID](value.go#L15) | DocumentID identifies a complete replacement document. | [Index and search a document](#example-quickstart) |
| [search.FieldName](value.go#L12) | FieldName identifies a declared mapping, independently of its JSON path. |  |
| [search.IndexName](value.go#L9) | IndexName identifies an application-owned logical index. |  |
| [search.Integer](value.go#L59) | Integer rejects values outside the exact common integer range. | [Filter and sort results](#example-filters) |
| [search.IntegerValue](value.go#L29) | IntegerValue represents an exactly portable signed integer. |  |
| [search.JSONPointer](value.go#L18) | JSONPointer addresses a value using RFC 6901. |  |
| [search.Keyword](value.go#L44) | Keyword constructs a literal string without normalization. | [Filter and sort results](#example-filters) |
| [search.KeywordValue](value.go#L25) | KeywordValue represents an exact UTF-8 string. |  |
| [search.MaxInteger](value.go#L31) | MaxInteger is the largest exactly portable integer. |  |
| [search.Value](value.go#L35) | Value is an immutable typed filter, facet, or sort scalar. Its zero value is invalid. |  |
| [search.Value.Boolean](value.go#L80) | Boolean returns the Boolean only for Boolean values. |  |
| [search.Value.Integer](value.go#L86) | Integer returns the integer only for integer values. |  |
| [search.Value.Keyword](value.go#L74) | Keyword returns the string only for keyword values. | [Count facets before pagination](#example-facets) |
| [search.Value.Kind](value.go#L68) | Kind reports the scalar's discriminator. |  |
| [search.Value.MarshalJSON](value.go#L92) | MarshalJSON preserves a Value's scalar JSON shape for typed result DTOs and rejects its invalid zero value. |  |
| [search.ValueKind](value.go#L21) | ValueKind distinguishes portable scalar types. |  |

## API examples

Each snippet comes from its linked executable test. Open the full program for its index definition and setup.

### <a id="example-bulk"></a>Inspect bulk visibility

```go
first, err := search.DeleteMutation("one")
if err != nil {
	panic(err)
}
second, err := search.DeleteMutation("two")
if err != nil {
	panic(err)
}
batch, err := index.Bulk(ctx, []search.Mutation{first, second})
if err != nil {
	panic(err)
}
outcome, err := index.Wait(ctx, batch.Receipt)
if err != nil {
	panic(err)
}
for _, item := range outcome.Bulk.Items {
	fmt.Println(item.ID, item.Status)
	// one succeeded
	// two succeeded
}
```

[Full program](examples/bulk/main.go) · [Executable test](examples/bulk_test.go)

### <a id="example-facets"></a>Count facets before pagination

```go
result, err := index.Search(ctx, search.Query{Limit: 1, Facets: []search.TermFacet{{Name: "categories", Field: "category"}}})
if err != nil {
	panic(err)
}
for _, bucket := range result.Facets[0].Buckets {
	value, _ := bucket.Value.Keyword()
	fmt.Println(value, bucket.Count)
	// kitchen 2
	// books 1
}
```

[Full program](examples/facets/main.go) · [Executable test](examples/facets_test.go)

### <a id="example-fake"></a>Test an application service

```go
backend := searchfake.New()
backend.QueueSearch(search.Result{Total: search.Total{Value: 3, Relation: search.TotalExact}}, nil)
index, err := search.NewIndex(backend, search.Definition{
	Name:   "products",
	Fields: []search.Field{{Name: "title", Type: search.TextField, Searchable: true}},
})
if err != nil {
	panic(err)
}
result, err := index.Search(context.Background(), search.Query{Text: "mug"})
if err != nil {
	panic(err)
}
fmt.Println(result.Total.Value, backend.Calls()[0].Kind)
// 3 search
```

[Full program](examples/fake/main.go) · [Executable test](examples/fake_test.go)

### <a id="example-filters"></a>Filter and sort results

```go
category, err := search.Keyword("kitchen")
if err != nil {
	panic(err)
}
maximum, err := search.Integer(1500)
if err != nil {
	panic(err)
}
result, err := index.Search(ctx, search.Query{
	Filter: search.And(search.Equal("category", category), search.LessThan("price", maximum)),
	Sort:   []search.Sort{{Field: "price", Direction: search.Ascending}},
	Limit:  1,
})
if err != nil {
	panic(err)
}
fmt.Println(result.Hits[0].Document.ID)
// two
fmt.Println(result.Total.Value)
// 2
```

[Full program](examples/filters/main.go) · [Executable test](examples/filters_test.go)

### <a id="example-highlights"></a>Escape structured highlights

```go
result, err := index.Search(ctx, search.Query{Text: "blue", Highlight: []search.Highlight{{Field: "title"}}})
if err != nil {
	panic(err)
}
for _, segment := range result.Hits[0].Highlights["title"][0].Segments {
	text := html.EscapeString(segment.Text)
	if segment.Matched {
		fmt.Print("<mark>" + text + "</mark>")
	} else {
		fmt.Print(text)
	}
}
fmt.Println()
// <mark>Blue</mark> mug
```

[Full program](examples/highlights/main.go) · [Executable test](examples/highlights_test.go)

### <a id="example-quickstart"></a>Index and search a document

```go
ctx := context.Background()
driver, err := blevesearch.Open(ctx, blevesearch.Config{Memory: true})
if err != nil {
	panic(err)
}
defer driver.Close()

definition := search.Definition{
	Name:   "products",
	Fields: []search.Field{{Name: "title", Type: search.TextField, Searchable: true}},
}
if err := driver.CreateIndex(ctx, definition); err != nil {
	panic(err)
}
index, err := search.NewIndex(driver, definition)
if err != nil {
	panic(err)
}

type product struct {
	Title string `json:"title"`
}
receipt, err := search.Upsert(ctx, index, "mug-1", product{Title: "Blue mug"})
if err != nil {
	panic(err)
}
outcome, err := index.Wait(ctx, receipt)
if err != nil {
	panic(err)
}
if outcome.State != search.OutcomeSucceeded {
	panic(outcome.Error)
}

page, err := search.SearchAs[product](ctx, index, search.Query{Text: "blue mug"})
if err != nil {
	panic(err)
}
fmt.Println(page.Hits[0].ID, page.Hits[0].Value.Title)
// mug-1 Blue mug
```

[Full program](examples/quickstart/main.go) · [Executable test](examples/quickstart_test.go)

### <a id="example-search"></a>Search typed documents

```go
page, err := search.SearchAs[struct {
	Title string `json:"title"`
}](ctx, index, search.Query{Text: "blue mug"})
if err != nil {
	panic(err)
}
fmt.Println(page.Hits[0].Value.Title)
// Blue mug
fmt.Println(page.Total.Value)
// 1
```

[Full program](examples/search/main.go) · [Executable test](examples/search_test.go)

### <a id="example-services"></a>Construct service drivers

```go
meili, err := meilisearch.New(meilisearch.Config{URL: "http://127.0.0.1:7700"})
if err != nil {
	panic(err)
}
defer meili.Close()
typesense, err := typesensesearch.New(typesensesearch.Config{URL: "http://127.0.0.1:8108"})
if err != nil {
	panic(err)
}
defer typesense.Close()
openSearch, err := opensearch.New(opensearch.Config{URL: "http://127.0.0.1:9200"})
if err != nil {
	panic(err)
}
defer openSearch.Close()
elastic, err := elasticsearch.New(elasticsearch.Config{URL: "http://127.0.0.1:9200"})
if err != nil {
	panic(err)
}
defer elastic.Close()
redis, err := redissearch.New(redissearch.Config{Addr: "127.0.0.1:6379"})
if err != nil {
	panic(err)
}
defer redis.Close()
fmt.Println("Clients constructed without backend I/O")
// Clients constructed without backend I/O
```

[Full program](examples/services/main.go) · [Executable test](examples/services_test.go)
<!-- api:embed:end -->
