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
  <a href="https://github.com/goforj/search/tags"><img src="docs/assets/status.svg" alt="Version v0.1.0"></a>
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
<table>
<thead><tr><th align="left" width="250">Feature</th><th align="left">What you get</th><th align="left">Example or API</th></tr></thead>
<tbody>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-search.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Literal search</strong></td><td>Require every analyzed term, with explicit searchable fields.</td><td><a href="examples/search/main.go">Typed search example</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-filters.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Typed filters</strong></td><td>Combine Boolean logic, exact values, integer ranges, and array membership.</td><td><a href="examples/filters/main.go">Filter example</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-sorting.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Stable sorting</strong></td><td>Order by up to two declared fields, followed by the document ID.</td><td><a href="definition.go">Sort</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-pagination.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Pagination</strong></td><td>Use bounded offsets and limits with explicit total-count precision.</td><td><a href="query.go">Query</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-facets.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Term facets</strong></td><td>Count matching documents before pagination, deduplicating repeated array terms.</td><td><a href="examples/facets/main.go">Facet example</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-highlights.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Structured highlights</strong></td><td>Render untrusted text segments with explicit match markers.</td><td><a href="examples/highlights/main.go">Highlight example</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-bulk.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Bulk operations</strong></td><td>Retain input positions and honest per-item or aggregate outcomes.</td><td><a href="examples/bulk/main.go">Bulk example</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-receipts.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Visibility receipts</strong></td><td>Wait for accepted writes to become searchable and retain recovery evidence.</td><td><a href="mutation.go">Receipt and Outcome</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-documents.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Typed documents</strong></td><td>Use typed Go values while keeping document IDs separate from JSON bodies.</td><td><a href="examples/search/main.go">Typed search example</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-schemas.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Explicit schemas</strong></td><td>Provision deliberately and detect schema drift without runtime migration.</td><td><a href="definition.go">Definition</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-observability.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Safe observability</strong></td><td>Observe classified events with panic isolation and optional bounded async delivery.</td><td><a href="observer.go">Observer</a> · <a href="observer_async.go">AsyncObserver</a></td></tr>
<tr><td width="250" nowrap><img src="docs/assets/badges/feature-testing.svg" width="22" height="22" alt="" align="absmiddle"> <strong>Isolated testing</strong></td><td>Script results and inspect recorded calls without running a search engine.</td><td><a href="examples/fake/main.go">Fake backend example</a></td></tr>
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

## Testing

Use [`searchfake`](searchfake) to queue search results and inspect recorded calls in application tests. It implements `search.Backend` without a running search engine. The [fake backend example](examples/fake/main.go) shows the complete workflow.

Driver authors can reuse [`searchtest`](searchtest) to verify the portable behavior contract. Integration tests run against real Bleve indexes and SQLite FTS5 databases, plus Testcontainers with pinned Meilisearch, Typesense, OpenSearch, Elasticsearch, PostgreSQL, and Redis images. Each service container starts once per run; tests use separate index namespaces. OpenSearch and Elasticsearch run with authentication enabled and execute the shared contract with one and two primary shards.

See the [executed driver evidence](docs/driver-results.json), [unit test inventory](docs/unit-results.json), and [integration test inventory](docs/integration-results.json) for the scenarios that actually ran.

Run `make bench` for allocation-aware [document validation benchmarks](validation_test.go) and [embedded search over 1,000 documents](examples/benchmark_test.go).

<!-- examples:embed:start -->
## Runnable examples

Each program under [`examples/`](examples) is executed by a [shared test runner](examples/example_test.go) that checks its expected output. Run these commands from the repository root with Go 1.25 or newer.

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

Choose a backend in [Drivers](#drivers), then use these operations in your application. API names and example titles jump to tested usage below.

| Task | API | Example |
| --- | --- | --- |
| Define an index | [Definition](#example-quickstart) · [NewIndex](#example-quickstart) | [Index and search a document](#example-quickstart) |
| Write documents | [Upsert](#example-quickstart) | [Index and search a document](#example-quickstart) |
| Search documents | [SearchAs](#example-search) · [Query](#example-search) | [Search typed documents](#example-search) |
| Filter results | [Index.Search](#example-filters) · [Keyword](#example-filters) · [Integer](#example-filters) · [And](#example-filters) · [Equal](#example-filters) | [Filter and sort results](#example-filters) |
| Sort and paginate | [Query](#example-filters) · [Sort](#example-filters) | [Filter and sort results](#example-filters) |
| Count facets | [TermFacet](#example-facets) | [Count facets before pagination](#example-facets) |
| Highlight matches | [Highlight](#example-highlights) | [Escape structured highlights](#example-highlights) |
| Write a batch | [Index.Bulk](#example-bulk) · [DeleteMutation](#example-bulk) | [Inspect bulk visibility](#example-bulk) |
| Wait for visibility | [Index.Wait](#example-quickstart) · [Outcome](#example-quickstart) | [Index and search a document](#example-quickstart) · [Inspect bulk visibility](#example-bulk) |
| Test application code | [searchfake.New](#example-fake) · [searchfake.Backend.QueueSearch](#example-fake) · [searchfake.Backend.Calls](#example-fake) | [Test an application service](#example-fake) |

## API examples

Find complete programs and setup in [Runnable examples](#runnable-examples).

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

### <a id="example-quickstart"></a>Index and search a document

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
<!-- api:embed:end -->
