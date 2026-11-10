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
  <a href="https://github.com/goforj/search/actions/workflows/security.yml"><img src="https://github.com/goforj/search/actions/workflows/security.yml/badge.svg" alt="Security"></a>
  <a href="https://go.dev"><img src="https://img.shields.io/badge/go-1.24%2B-blue?logo=go" alt="Go 1.24 or newer"></a>
  <a href="https://codecov.io/gh/goforj/search"><img src="https://codecov.io/gh/goforj/search/graph/badge.svg" alt="Coverage"></a>
  <a href="https://github.com/goforj/search/tags"><img src="docs/assets/status.svg" alt="Version v0.1.1"></a>
<!-- test-count:embed:start -->
<img src="docs/assets/badges/unit-tests.svg" alt="Executed unit tests, including subtests">
  <img src="docs/assets/badges/integration-tests.svg" alt="Executed integration tests, including subtests">
  <a href="docs/coverage.json"><img src="docs/assets/badges/coverage.svg" alt="Merged statement coverage"></a>
<!-- test-count:embed:end -->
</p>

<p align="center">
  <a href="#features">Features</a> · <a href="#quick-start">Quick Start</a> · <a href="#drivers">Drivers</a> · <a href="#installation">Installation</a> · <a href="#performance">Performance</a> · <a href="#runnable-examples">Examples</a> · <a href="#api-index">API</a>
</p>

## Features

<!-- features:embed:start -->
<table>
<thead><tr><th align="left" width="250">Feature</th><th align="left">What you get</th><th align="left">Example or API</th></tr></thead>
<tbody>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-icon-search.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Literal search</strong></td><td>Require every analyzed term, with explicit searchable fields.</td><td><a href="examples/search/main.go">Typed search example</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-icon-filters.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Typed filters</strong></td><td>Combine Boolean logic, exact values, integer ranges, and array membership.</td><td><a href="examples/filters/main.go">Filter example</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-icon-sorting.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Stable sorting</strong></td><td>Order by up to two declared fields, followed by the document ID.</td><td><a href="definition.go">Sort</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-icon-pagination.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Pagination</strong></td><td>Use bounded offsets and limits with explicit total-count precision.</td><td><a href="query.go">Query</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-icon-facets.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Term facets</strong></td><td>Count matching documents before pagination, deduplicating repeated array terms.</td><td><a href="examples/facets/main.go">Facet example</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-icon-highlights.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Structured highlights</strong></td><td>Render untrusted text segments with explicit match markers.</td><td><a href="examples/highlights/main.go">Highlight example</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-icon-bulk.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Bulk operations</strong></td><td>Retain input positions and honest per-item or aggregate outcomes.</td><td><a href="examples/bulk/main.go">Bulk example</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-icon-receipts.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Visibility receipts</strong></td><td>Wait for accepted writes to become searchable and retain recovery evidence.</td><td><a href="mutation.go">Receipt and Outcome</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-icon-documents.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Typed documents</strong></td><td>Use typed Go values while keeping document IDs separate from JSON bodies.</td><td><a href="examples/search/main.go">Typed search example</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-icon-schemas.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Explicit schemas</strong></td><td>Provision deliberately and detect schema drift without runtime migration.</td><td><a href="definition.go">Definition</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-icon-observability.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Safe observability</strong></td><td>Observe classified events with panic isolation and optional bounded async delivery.</td><td><a href="observer.go">Observer</a> · <a href="observer_async.go">AsyncObserver</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-icon-testing.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Isolated testing</strong></td><td>Script results and inspect recorded calls without running a search engine.</td><td><a href="examples/fake/main.go">Fake backend example</a></td></tr>
</tbody>
</table>
<!-- features:embed:end -->

<!-- quickstart:embed:start -->
## Quick Start

Go 1.25+, with no external service required.

```sh
git clone https://github.com/goforj/search.git
cd search
go run ./examples/quickstart
```

```go
// Command quickstart demonstrates embedded indexing and search.
package main

import (
	"context"
	"fmt"

	"github.com/goforj/search"
	"github.com/goforj/search/driver/blevesearch"
)

// main demonstrates the complete embedded indexing and typed-query lifecycle.
func main() {
	ctx := context.Background()

	// Use an in-memory index so this example needs no files or search service.
	driver, err := blevesearch.Open(ctx, blevesearch.Config{Memory: true})
	if err != nil {
		panic(err)
	}
	defer driver.Close()

	// Declare which document fields participate in full-text search.
	definition := search.Definition{
		Name:   "products",
		Fields: []search.Field{{Name: "title", Type: search.TextField, Searchable: true}},
	}
	// Provision the schema explicitly before creating an application handle.
	if err := driver.CreateIndex(ctx, definition); err != nil {
		panic(err)
	}
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		panic(err)
	}

	// Keep the document ID separate from the application JSON.
	type product struct {
		Title string `json:"title"`
	}
	receipt, err := search.Upsert(ctx, index, "mug-1", product{Title: "Blue mug"})
	if err != nil {
		panic(err)
	}
	// A receipt acknowledges the write; Wait establishes search visibility.
	outcome, err := index.Wait(ctx, receipt)
	if err != nil {
		panic(err)
	}
	if outcome.State != search.OutcomeSucceeded {
		panic(outcome.Error)
	}

	// Require both search terms and decode hits directly into the application type.
	page, err := search.SearchAs[product](ctx, index, search.Query{Text: "blue mug"})
	if err != nil {
		panic(err)
	}
	fmt.Println(page.Hits[0].ID, page.Hits[0].Value.Title)
	// mug-1 Blue mug
}
```
<!-- quickstart:embed:end -->

## Drivers

Choose an embedded index, an existing SQL pool, or a shared search service.

<!-- drivers:embed:start -->
| Driver | Tested version | Mutation visibility | Bulk precision | Topology |
| ---: | --- | --- | --- | --- |
| [![Bleve](docs/assets/badges/driver-bleve.svg)](driver/blevesearch) | v2.6.3 | synchronous | per item | single process; memory or disk |
| [![Meilisearch](docs/assets/badges/driver-meilisearch.svg)](driver/meilisearch) | v1.17.0 | asynchronous receipt | aggregate task | shared service |
| [![Typesense](docs/assets/badges/driver-typesense.svg)](driver/typesensesearch) | 30.0 | synchronous | per item | shared service |
| [![OpenSearch](docs/assets/badges/driver-opensearch.svg)](driver/opensearch) | 3.9.0 | refresh-waiting | per item | shared service |
| [![Elasticsearch](docs/assets/badges/driver-elasticsearch.svg)](driver/elasticsearch) | 9.5.6 | refresh-waiting | per item | shared service |
| [![PostgreSQL FTS](docs/assets/badges/driver-postgres.svg)](driver/postgressearch) | 18.6 | synchronous | per item | application-owned SQL pool |
| [![SQLite FTS5](docs/assets/badges/driver-sqlite.svg)](driver/sqlitesearch) | 3.54.1 | synchronous | per item | embedded SQL; application-owned pool |
| [![Redis Search](docs/assets/badges/driver-redis.svg)](driver/redissearch) | 8.10.3 | synchronous | per item | standalone service; at most 4096 matches |

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

Install the root package and the drivers you need:

```sh
go get github.com/goforj/search

# Bleve (embedded, Go 1.25+)
go get github.com/goforj/search/driver/blevesearch

# Meilisearch
go get github.com/goforj/search/driver/meilisearch

# Typesense
go get github.com/goforj/search/driver/typesensesearch

# OpenSearch
go get github.com/goforj/search/driver/opensearch

# Elasticsearch
go get github.com/goforj/search/driver/elasticsearch

# PostgreSQL FTS
go get github.com/goforj/search/driver/postgressearch

# SQLite FTS5
go get github.com/goforj/search/driver/sqlitesearch

# Redis Search
go get github.com/goforj/search/driver/redissearch
```

Install the drivers you need. All drivers except Bleve are packages in the root module and are included when you install `search`; their commands above are provided for convenient copying. Bleve is a separate optional module. See [module ownership and release order](docs/development.md).

See [search behavior and recovery](docs/development.md#search-behavior) for write visibility, query semantics, and receipt lifetime.

<!-- bench:embed:start -->
## Performance

The same **1,000,000-document synthetic catalog** and query sequence across all eight drivers, using the public `search.Index` API. Latency charts show client-observed p95 from 200 searches after 30 warm-up requests per workload. Latency axes are logarithmic; indexing throughput is linear.

### Literal search

![Literal search benchmark](docs/bench/benchmarks_literal.svg)

### Filter and sort

![Filter and sort benchmark](docs/bench/benchmarks_sort.svg)

### Text and facets

![Text and facets benchmark](docs/bench/benchmarks_facets.svg)

### Indexing throughput

![Indexing throughput benchmark](docs/bench/benchmarks_ingest.svg)

| Driver | Load (s) | Literal p50 / p95 (ms) | Filter + sort p50 / p95 (ms) | Facets p50 / p95 (ms) |
| ---: | ---: | ---: | ---: | ---: |
| <img src="docs/assets/badges/driver-bleve.svg" alt="Bleve" height="22"> | 45.1 | 7.984 / 14.845 | 0.868 / 2.105 | 0.874 / 1.331 |
| <img src="docs/assets/badges/driver-meilisearch.svg" alt="Meilisearch" height="22"> | 274.9 | 1.350 / 1.829 | 1.896 / 2.494 | 1.941 / 2.710 |
| <img src="docs/assets/badges/driver-typesense.svg" alt="Typesense" height="22"> | 67.5 | 5.973 / 6.675 | 1.387 / 1.573 | 1.770 / 3.031 |
| <img src="docs/assets/badges/driver-opensearch.svg" alt="OpenSearch" height="22"> | 116.9 | 2.161 / 3.526 | 2.076 / 3.299 | 2.445 / 4.101 |
| <img src="docs/assets/badges/driver-elasticsearch.svg" alt="Elasticsearch" height="22"> | 110.4 | 2.179 / 5.343 | 1.886 / 4.407 | 1.899 / 5.216 |
| <img src="docs/assets/badges/driver-postgres.svg" alt="PostgreSQL FTS" height="22"> | 697.1 | 4.587 / 6.903 | 2.180 / 3.798 | 86.901 / 97.977 |
| <img src="docs/assets/badges/driver-sqlite.svg" alt="SQLite FTS5" height="22"> | 204.3 | 17.569 / 22.071 | 0.686 / 5.012 | 922.885 / 1496.732 |
| <img src="docs/assets/badges/driver-redis.svg" alt="Redis Search" height="22"> | 30.1 | 6.256 / 7.844 | 5.970 / 7.508 | 8.029 / 10.369 |

SQL measurements include generated secondary indexes; broad-term facets remain comparatively expensive. The [SQL benchmark audit](docs/bench/sql-audit.md) records the original bottlenecks and subsequent improvements.

Measured on Apple M4 Max, 16 CPU cores, 48 GiB RAM; OrbStack 16 CPUs, 19.57 GiB reported RAM. Embedded engines run on the host; service engines run in Docker. These are warmed, single-client, selective searches returning at most 1,024 matches and 20 hits, without concurrent writes. Indexing includes document generation, validation, and waiting for visibility in batches of 1,000.

See [methodology and reproduction](docs/bench/README.md) for workload details, pinned versions, persistence settings, and limitations. The [measurement snapshot](docs/bench/benchmarks_rows.json) contains corpus fingerprints, environment metadata, count precision, and every latency sample. Run `make bench-drivers` to measure or `make bench-render` to redraw the recorded results.
<!-- bench:embed:end -->

## Testing

Use [`searchfake`](searchfake) to queue search results and inspect recorded calls in application tests. It implements `search.Backend` without a running search engine. The [fake backend example](examples/fake/main.go) shows the complete workflow.

Driver authors can reuse [`searchtest`](searchtest) to verify the portable behavior contract. Integration tests run against real Bleve indexes and SQLite FTS5 databases, plus Testcontainers with pinned Meilisearch, Typesense, OpenSearch, Elasticsearch, PostgreSQL, and Redis images. Each service container starts once per run; tests use separate index namespaces. OpenSearch and Elasticsearch run with authentication enabled and execute the shared contract with one and two primary shards.

See the [executed driver evidence](docs/driver-results.json), [unit test inventory](docs/unit-results.json), and [integration test inventory](docs/integration-results.json) for the scenarios that actually ran.

Run `make bench` for allocation-aware [document validation benchmarks](validation_test.go) and [embedded search over 1,000 documents](examples/benchmark_test.go).

<!-- examples:embed:start -->
## Runnable examples

Run these examples from the repository root with **Go 1.25+**. Each program includes expected output, verified by [automated tests](examples/example_test.go).

| Example | Run |
| --- | --- |
| [Inspect bulk visibility](examples/bulk/main.go) | `go run ./examples/bulk` |
| [Count facets before pagination](examples/facets/main.go) | `go run ./examples/facets` |
| [Test an application service](examples/fake/main.go) | `go run ./examples/fake` |
| [Filter and sort results](examples/filters/main.go) | `go run ./examples/filters` |
| [Escape structured highlights](examples/highlights/main.go) | `go run ./examples/highlights` |
| [Index and search a document](examples/quickstart/main.go) | `go run ./examples/quickstart` |
| [Search typed documents](examples/search/main.go) | `go run ./examples/search` |
| [Construct service drivers](examples/services/main.go) | `go run ./examples/services` |
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

API descriptions come from GoDoc. Runnable examples and their expected output live in `examples/<name>/main.go`. A shared test runner executes those programs. Edit those sources and run `make generate`; generation refreshes the quick start, example index, feature badges, and API index. CI checks that regeneration produces no diff.

See [development and release notes](docs/development.md) for focused driver tests, module boundaries, and publishing. Report problems or request features in [GitHub issues](https://github.com/goforj/search/issues).

<!-- api:embed:start -->
## API Index

| Group | Functions |
| ---: | --- |
| **Construction** | [Definition](#api-definition) · [NewIndex](#api-newindex) |
| **Facets** | [TermFacet](#api-termfacet) |
| **Filters** | [And](#api-and) · [Equal](#api-equal) · [Integer](#api-integer) · [Keyword](#api-keyword) |
| **Highlights** | [Highlight](#api-highlight) |
| **Mutations** | [DeleteMutation](#api-deletemutation) · [Index.Bulk](#api-index-bulk) · [Upsert](#api-upsert) |
| **Queries** | [Index.Search](#api-index-search) · [Query](#api-query) · [SearchAs](#api-searchas) |
| **Sorting** | [Sort](#api-sort) |
| **Testing** | [searchfake.Backend.Calls](#api-searchfake-backend-calls) · [searchfake.Backend.QueueSearch](#api-searchfake-backend-queuesearch) · [searchfake.New](#api-searchfake-new) |
| **Visibility** | [Index.Wait](#api-index-wait) · [Outcome](#api-outcome) |

Find complete programs and setup in [Runnable examples](#runnable-examples).

## Construction

<a id="example-quickstart"></a>

### <a id="api-definition"></a>Definition · <a id="api-newindex"></a>NewIndex

Index and search a document.

```go
ctx := context.Background()

// Use an in-memory index so this example needs no files or search service.
driver, err := blevesearch.Open(ctx, blevesearch.Config{Memory: true})
if err != nil {
	panic(err)
}
defer driver.Close()

// Declare which document fields participate in full-text search.
definition := search.Definition{
	Name:   "products",
	Fields: []search.Field{{Name: "title", Type: search.TextField, Searchable: true}},
}
// Provision the schema explicitly before creating an application handle.
if err := driver.CreateIndex(ctx, definition); err != nil {
	panic(err)
}
index, err := search.NewIndex(driver, definition)
if err != nil {
	panic(err)
}

// Keep the document ID separate from the application JSON.
type product struct {
	Title string `json:"title"`
}
receipt, err := search.Upsert(ctx, index, "mug-1", product{Title: "Blue mug"})
if err != nil {
	panic(err)
}
// A receipt acknowledges the write; Wait establishes search visibility.
outcome, err := index.Wait(ctx, receipt)
if err != nil {
	panic(err)
}
if outcome.State != search.OutcomeSucceeded {
	panic(outcome.Error)
}

// Require both search terms and decode hits directly into the application type.
page, err := search.SearchAs[product](ctx, index, search.Query{Text: "blue mug"})
if err != nil {
	panic(err)
}
fmt.Println(page.Hits[0].ID, page.Hits[0].Value.Title)
// mug-1 Blue mug
```

## Facets

<a id="example-facets"></a>

### <a id="api-termfacet"></a>TermFacet

Count facets before pagination.

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

## Filters

<a id="example-filters"></a>

### <a id="api-and"></a>And · <a id="api-equal"></a>Equal · <a id="api-integer"></a>Integer · <a id="api-keyword"></a>Keyword

Filter and sort results.

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

## Highlights

<a id="example-highlights"></a>

### <a id="api-highlight"></a>Highlight

Escape structured highlights.

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

## Mutations

<a id="example-bulk"></a>

### <a id="api-deletemutation"></a>DeleteMutation · <a id="api-index-bulk"></a>Index.Bulk

Inspect bulk visibility.

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

### <a id="api-upsert"></a>Upsert

Index and search a document.

```go
ctx := context.Background()

// Use an in-memory index so this example needs no files or search service.
driver, err := blevesearch.Open(ctx, blevesearch.Config{Memory: true})
if err != nil {
	panic(err)
}
defer driver.Close()

// Declare which document fields participate in full-text search.
definition := search.Definition{
	Name:   "products",
	Fields: []search.Field{{Name: "title", Type: search.TextField, Searchable: true}},
}
// Provision the schema explicitly before creating an application handle.
if err := driver.CreateIndex(ctx, definition); err != nil {
	panic(err)
}
index, err := search.NewIndex(driver, definition)
if err != nil {
	panic(err)
}

// Keep the document ID separate from the application JSON.
type product struct {
	Title string `json:"title"`
}
receipt, err := search.Upsert(ctx, index, "mug-1", product{Title: "Blue mug"})
if err != nil {
	panic(err)
}
// A receipt acknowledges the write; Wait establishes search visibility.
outcome, err := index.Wait(ctx, receipt)
if err != nil {
	panic(err)
}
if outcome.State != search.OutcomeSucceeded {
	panic(outcome.Error)
}

// Require both search terms and decode hits directly into the application type.
page, err := search.SearchAs[product](ctx, index, search.Query{Text: "blue mug"})
if err != nil {
	panic(err)
}
fmt.Println(page.Hits[0].ID, page.Hits[0].Value.Title)
// mug-1 Blue mug
```

## Queries

<a id="example-search"></a>

### <a id="api-query"></a>Query · <a id="api-searchas"></a>SearchAs

Search typed documents.

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

### <a id="api-index-search"></a>Index.Search

Filter and sort results.

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

## Sorting

### <a id="api-sort"></a>Sort

Filter and sort results.

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

## Testing

<a id="example-fake"></a>

### <a id="api-searchfake-backend-calls"></a>searchfake.Backend.Calls · <a id="api-searchfake-backend-queuesearch"></a>searchfake.Backend.QueueSearch · <a id="api-searchfake-new"></a>searchfake.New

Test an application service.

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

## Visibility

### <a id="api-index-wait"></a>Index.Wait · <a id="api-outcome"></a>Outcome

Index and search a document.

```go
ctx := context.Background()

// Use an in-memory index so this example needs no files or search service.
driver, err := blevesearch.Open(ctx, blevesearch.Config{Memory: true})
if err != nil {
	panic(err)
}
defer driver.Close()

// Declare which document fields participate in full-text search.
definition := search.Definition{
	Name:   "products",
	Fields: []search.Field{{Name: "title", Type: search.TextField, Searchable: true}},
}
// Provision the schema explicitly before creating an application handle.
if err := driver.CreateIndex(ctx, definition); err != nil {
	panic(err)
}
index, err := search.NewIndex(driver, definition)
if err != nil {
	panic(err)
}

// Keep the document ID separate from the application JSON.
type product struct {
	Title string `json:"title"`
}
receipt, err := search.Upsert(ctx, index, "mug-1", product{Title: "Blue mug"})
if err != nil {
	panic(err)
}
// A receipt acknowledges the write; Wait establishes search visibility.
outcome, err := index.Wait(ctx, receipt)
if err != nil {
	panic(err)
}
if outcome.State != search.OutcomeSucceeded {
	panic(outcome.Error)
}

// Require both search terms and decode hits directly into the application type.
page, err := search.SearchAs[product](ctx, index, search.Query{Text: "blue mug"})
if err != nil {
	panic(err)
}
fmt.Println(page.Hits[0].ID, page.Hits[0].Value.Title)
// mug-1 Blue mug
```
<!-- api:embed:end -->
