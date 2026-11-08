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
  <a href="https://github.com/goforj/search/tags"><img src="https://img.shields.io/github/v/tag/goforj/search?label=version&sort=semver&filter=%21driver%2A" alt="Latest tag"></a>
<!-- test-count:embed:start -->
<img src="https://img.shields.io/badge/unit_tests-139-brightgreen" alt="Executed unit tests, including subtests">
  <img src="https://img.shields.io/badge/integration_tests-113-brightgreen" alt="Executed integration tests, including subtests">
  <a href="docs/coverage.json"><img src="https://img.shields.io/badge/coverage-85.7%25-brightgreen" alt="Merged statement coverage"></a>
<!-- test-count:embed:end -->
</p>

`search` gives application services one explicit contract for text queries, typed filters, sorting, pagination, term facets, and structured highlights. Applications own their definitions and source records. Drivers own backend translation and ranking.

The root package has no third-party dependencies. Bleve is isolated in its own module so service clients do not inherit an embedded search engine.

## Status

This is an unreleased implementation. Local examples and development use the checked-in workspace. Installation commands below describe the module layout; they become usable after the first release is published.

## Installation

The root and service drivers require Go 1.24 or newer:

```sh
go get github.com/goforj/search
```

The embedded driver requires Go 1.25 because Bleve v2.6.3 requires it:

```sh
go get github.com/goforj/search/driver/blevesearch
```

Service drivers are packages in the root module:

```go
import "github.com/goforj/search/driver/meilisearch"
import "github.com/goforj/search/driver/typesensesearch"
```

## Contract

- Declare an explicit `Definition`, provision it with a concrete driver's `CreateIndex`, then construct a portable `*search.Index`.
- Construction performs no backend I/O. Runtime validation never alters an existing schema. Definition changes require an explicit rebuild from authoritative records.
- Upserts replace complete documents. Deletes are idempotent. Call `Wait` to establish visibility after accepted writes; cancellation of a wait does not cancel accepted work.
- Text is literal, with every analyzed term required. Ranking and tokenization vary across engines. Driver changes preserve the application workflow, not identical results.
- Sorts are field-primary, followed by an ascending document-ID tie-break. Sortable fields must be required. Stable pages assume the index has not changed between queries.
- Integers use the exact common range from -9007199254740991 to 9007199254740991. Keywords retain UTF-8 byte order. Missing and null values are absent to filters; negation uses ordinary two-valued Boolean logic.
- Facets count matching documents before hit pagination. Repeated array values count once per document. Highlights contain untrusted text segments, never trusted HTML.
- Backend errors and observer events omit query text, documents, credentials, and server prose.

Keep search data rebuildable. Search is not the authoritative database or an authorization boundary. Apply trusted tenant predicates before querying so hits, totals, and facets all respect access policy.

## Drivers

<!-- drivers:embed:start -->
| Driver | Tested engine | Mutation visibility | Bulk precision | Topology |
| --- | --- | --- | --- | --- |
| bleve | v2.6.3 | synchronous | per item | single process; memory or disk |
| meilisearch | v1.17.0 | asynchronous receipt | aggregate task | shared service |
| typesense | 30.0 | synchronous | per item | shared service |

All listed engines pass the same literal all-terms, typed-filter, stable-sort, pagination, facet, highlight, replacement, receipt, bulk, cancellation, and concurrent-call fixtures. See [executed driver evidence](docs/driver-results.json) for exact scenario names and the source fingerprint.
<!-- drivers:embed:end -->

Meilisearch facets support at most 4096 distinct terms per requested field after filtering. Higher cardinality returns `ErrUnsupported`; it does not return an incorrectly chosen top bucket. The driver collects a bounded complete distribution because the engine does not preserve portable lexical ties at its native count cutoff.

Meilisearch accepts at most ten backend words. Queries in scripts with finer native segmentation use a conservative rune bound to prevent silent truncation. Its validated server search cutoff exceeds the enforced 30-second maximum HTTP deadline, so the client fails before the engine can return a silently timed-out response. As with schema validation, this assumes administrators do not change settings behind a running client.

Bleve owns a single index path in one process. Its ownership lock refuses a second writer. Use a shared service when querying and indexing from separate processes. Close concrete drivers at the application lifecycle boundary.

Service credentials stay on the server. Use provisioning credentials only for explicit administration, then validate and serve with appropriately scoped runtime credentials. HTTP clients do not follow redirects, bound response bodies, and preserve TLS verification by default.

Receipts are process-local and bounded. Drivers retain the latest 4096 submissions. A retained receipt caches its first terminal result after `Wait`; an expired unobserved receipt reports `ErrReceiptExpired` as an unknown outcome. Recover after process loss by reconciling or replaying idempotent work from the source.

## Development

```sh
make test
make test-race
make vet
make test-integration
make generate
make check-generated
```

Integration tests use Testcontainers with pinned Meilisearch and Typesense images and real Bleve indexes. See [development and release notes](docs/development.md) for module ownership and release order.

<!-- examples:embed:start -->
## Examples

These examples run as Go example tests. Standalone programs live under `examples/`.

### Inspect bulk visibility

```go
// Code generated from examples/bulk_test.go; DO NOT EDIT.
package main

import (
	"context"
	"fmt"
	"github.com/goforj/search"
	"github.com/goforj/search/driver/blevesearch"
)

// main executes the documented workflow.
func main() {

	ctx := context.Background()
	driver, err := blevesearch.Open(ctx, blevesearch.Config{Memory: true})
	if err != nil {
		panic(err)
	}
	defer driver.Close()
	definition := search.Definition{
		Name: "products",
		Fields: []search.Field{
			{Name: "title", Type: search.TextField, Searchable: true},
			{Name: "category", Type: search.KeywordField, Filterable: true, Facetable: true},
			{Name: "price", Type: search.IntegerField, Required: true, Sortable: true, Filterable: true},
		},
	}
	if err := driver.CreateIndex(ctx, definition); err != nil {
		panic(err)
	}
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		panic(err)
	}
	for _, product := range []struct {
		ID, Title, Category string
		Price               int
	}{
		{"one", "Blue mug", "kitchen", 1200},
		{"two", "Red mug", "kitchen", 900},
		{"three", "Go guide", "books", 2500},
	} {
		value := struct {
			Title    string `json:"title"`
			Category string `json:"category"`
			Price    int    `json:"price"`
		}{product.Title, product.Category, product.Price}
		receipt, err := search.Upsert(ctx, index, search.DocumentID(product.ID), value)
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
	}

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

}
```

### Count facets before pagination

```go
// Code generated from examples/facets_test.go; DO NOT EDIT.
package main

import (
	"context"
	"fmt"
	"github.com/goforj/search"
	"github.com/goforj/search/driver/blevesearch"
)

// main executes the documented workflow.
func main() {

	ctx := context.Background()
	driver, err := blevesearch.Open(ctx, blevesearch.Config{Memory: true})
	if err != nil {
		panic(err)
	}
	defer driver.Close()
	definition := search.Definition{
		Name: "products",
		Fields: []search.Field{
			{Name: "title", Type: search.TextField, Searchable: true},
			{Name: "category", Type: search.KeywordField, Filterable: true, Facetable: true},
			{Name: "price", Type: search.IntegerField, Required: true, Sortable: true, Filterable: true},
		},
	}
	if err := driver.CreateIndex(ctx, definition); err != nil {
		panic(err)
	}
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		panic(err)
	}
	for _, product := range []struct {
		ID, Title, Category string
		Price               int
	}{
		{"one", "Blue mug", "kitchen", 1200},
		{"two", "Red mug", "kitchen", 900},
		{"three", "Go guide", "books", 2500},
	} {
		value := struct {
			Title    string `json:"title"`
			Category string `json:"category"`
			Price    int    `json:"price"`
		}{product.Title, product.Category, product.Price}
		receipt, err := search.Upsert(ctx, index, search.DocumentID(product.ID), value)
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
	}

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

}
```

### Test an application service

```go
// Code generated from examples/fake_test.go; DO NOT EDIT.
package main

import (
	"context"
	"fmt"

	"github.com/goforj/search"
	"github.com/goforj/search/searchfake"
)

// main executes the documented workflow.
func main() {
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

}
```

### Filter and sort results

```go
// Code generated from examples/filters_test.go; DO NOT EDIT.
package main

import (
	"context"
	"fmt"
	"github.com/goforj/search"
	"github.com/goforj/search/driver/blevesearch"
)

// main executes the documented workflow.
func main() {

	ctx := context.Background()
	driver, err := blevesearch.Open(ctx, blevesearch.Config{Memory: true})
	if err != nil {
		panic(err)
	}
	defer driver.Close()
	definition := search.Definition{
		Name: "products",
		Fields: []search.Field{
			{Name: "title", Type: search.TextField, Searchable: true},
			{Name: "category", Type: search.KeywordField, Filterable: true, Facetable: true},
			{Name: "price", Type: search.IntegerField, Required: true, Sortable: true, Filterable: true},
		},
	}
	if err := driver.CreateIndex(ctx, definition); err != nil {
		panic(err)
	}
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		panic(err)
	}
	for _, product := range []struct {
		ID, Title, Category string
		Price               int
	}{
		{"one", "Blue mug", "kitchen", 1200},
		{"two", "Red mug", "kitchen", 900},
		{"three", "Go guide", "books", 2500},
	} {
		value := struct {
			Title    string `json:"title"`
			Category string `json:"category"`
			Price    int    `json:"price"`
		}{product.Title, product.Category, product.Price}
		receipt, err := search.Upsert(ctx, index, search.DocumentID(product.ID), value)
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
	}

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

}
```

### Escape structured highlights

```go
// Code generated from examples/highlights_test.go; DO NOT EDIT.
package main

import (
	"context"
	"fmt"
	"github.com/goforj/search"
	"github.com/goforj/search/driver/blevesearch"
	"html"
)

// main executes the documented workflow.
func main() {

	ctx := context.Background()
	driver, err := blevesearch.Open(ctx, blevesearch.Config{Memory: true})
	if err != nil {
		panic(err)
	}
	defer driver.Close()
	definition := search.Definition{
		Name: "products",
		Fields: []search.Field{
			{Name: "title", Type: search.TextField, Searchable: true},
			{Name: "category", Type: search.KeywordField, Filterable: true, Facetable: true},
			{Name: "price", Type: search.IntegerField, Required: true, Sortable: true, Filterable: true},
		},
	}
	if err := driver.CreateIndex(ctx, definition); err != nil {
		panic(err)
	}
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		panic(err)
	}
	for _, product := range []struct {
		ID, Title, Category string
		Price               int
	}{
		{"one", "Blue mug", "kitchen", 1200},
		{"two", "Red mug", "kitchen", 900},
		{"three", "Go guide", "books", 2500},
	} {
		value := struct {
			Title    string `json:"title"`
			Category string `json:"category"`
			Price    int    `json:"price"`
		}{product.Title, product.Category, product.Price}
		receipt, err := search.Upsert(ctx, index, search.DocumentID(product.ID), value)
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
	}

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

}
```

### Search typed documents

```go
// Code generated from examples/search_test.go; DO NOT EDIT.
package main

import (
	"context"
	"fmt"
	"github.com/goforj/search"
	"github.com/goforj/search/driver/blevesearch"
)

// main executes the documented workflow.
func main() {

	ctx := context.Background()
	driver, err := blevesearch.Open(ctx, blevesearch.Config{Memory: true})
	if err != nil {
		panic(err)
	}
	defer driver.Close()
	definition := search.Definition{
		Name: "products",
		Fields: []search.Field{
			{Name: "title", Type: search.TextField, Searchable: true},
			{Name: "category", Type: search.KeywordField, Filterable: true, Facetable: true},
			{Name: "price", Type: search.IntegerField, Required: true, Sortable: true, Filterable: true},
		},
	}
	if err := driver.CreateIndex(ctx, definition); err != nil {
		panic(err)
	}
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		panic(err)
	}
	for _, product := range []struct {
		ID, Title, Category string
		Price               int
	}{
		{"one", "Blue mug", "kitchen", 1200},
		{"two", "Red mug", "kitchen", 900},
		{"three", "Go guide", "books", 2500},
	} {
		value := struct {
			Title    string `json:"title"`
			Category string `json:"category"`
			Price    int    `json:"price"`
		}{product.Title, product.Category, product.Price}
		receipt, err := search.Upsert(ctx, index, search.DocumentID(product.ID), value)
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
	}

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

}
```

### Construct service drivers

```go
// Code generated from examples/services_test.go; DO NOT EDIT.
package main

import (
	"fmt"

	"github.com/goforj/search/driver/meilisearch"
	"github.com/goforj/search/driver/typesensesearch"
)

// main executes the documented workflow.
func main() {
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
	fmt.Println("Clients constructed without backend I/O")
	// Clients constructed without backend I/O

}
```
<!-- examples:embed:end -->

<!-- api:embed:start -->
## API Index

Generated from GoDoc. Edit the owning Go source and run `make generate`.

### Backend

| API | Description |
| --- | --- |
| [`blevesearch.Config`](https://pkg.go.dev/github.com/goforj/search/driver/blevesearch#Config) | Config selects a single-index persistent path or an explicit in-memory index. |
| [`blevesearch.Driver`](https://pkg.go.dev/github.com/goforj/search/driver/blevesearch#Driver) | Driver owns one embedded index and its process lock. Close releases both. |
| [`blevesearch.Driver.Close`](https://pkg.go.dev/github.com/goforj/search/driver/blevesearch#Driver.Close) | Close waits for admitted calls before releasing the embedded index and ownership lock. |
| [`meilisearch.Config`](https://pkg.go.dev/github.com/goforj/search/driver/meilisearch#Config) | Config owns service credentials, physical namespace, and optional transport policy. |
| [`meilisearch.Driver`](https://pkg.go.dev/github.com/goforj/search/driver/meilisearch#Driver) | Driver is a concurrent client for one provisioned logical index. |
| [`meilisearch.Driver.Close`](https://pkg.go.dev/github.com/goforj/search/driver/meilisearch#Driver.Close) | Close prevents new calls without closing application-owned HTTP transports. |
| [`search.ErrClosed`](https://pkg.go.dev/github.com/goforj/search#ErrClosed) | ErrClosed identifies a closed driver. |
| [`search.ErrConflict`](https://pkg.go.dev/github.com/goforj/search#ErrConflict) | ErrConflict identifies a competing owner or conflicting request. |
| [`search.ErrIndexNotFound`](https://pkg.go.dev/github.com/goforj/search#ErrIndexNotFound) | ErrIndexNotFound identifies an unprovisioned index. |
| [`search.ErrInvalidDefinition`](https://pkg.go.dev/github.com/goforj/search#ErrInvalidDefinition) | ErrInvalidDefinition identifies an invalid schema. |
| [`search.ErrInvalidDocument`](https://pkg.go.dev/github.com/goforj/search#ErrInvalidDocument) | ErrInvalidDocument identifies invalid document input. |
| [`search.ErrInvalidQuery`](https://pkg.go.dev/github.com/goforj/search#ErrInvalidQuery) | ErrInvalidQuery identifies invalid query input. |
| [`search.ErrInvalidReceipt`](https://pkg.go.dev/github.com/goforj/search#ErrInvalidReceipt) | ErrInvalidReceipt identifies a forged, foreign, or empty receipt. |
| [`search.ErrMutationFailed`](https://pkg.go.dev/github.com/goforj/search#ErrMutationFailed) | ErrMutationFailed identifies accepted work that failed. |
| [`search.ErrNotFound`](https://pkg.go.dev/github.com/goforj/search#ErrNotFound) | ErrNotFound identifies a missing document. |
| [`search.ErrOutcomeUnknown`](https://pkg.go.dev/github.com/goforj/search#ErrOutcomeUnknown) | ErrOutcomeUnknown means work may have been accepted and must be reconciled. |
| [`search.ErrRateLimited`](https://pkg.go.dev/github.com/goforj/search#ErrRateLimited) | ErrRateLimited identifies throttling. |
| [`search.ErrReceiptExpired`](https://pkg.go.dev/github.com/goforj/search#ErrReceiptExpired) | ErrReceiptExpired identifies accepted work whose result is no longer retained. |
| [`search.ErrSchemaDrift`](https://pkg.go.dev/github.com/goforj/search#ErrSchemaDrift) | ErrSchemaDrift identifies a difference from the provisioned schema. |
| [`search.ErrUnauthorized`](https://pkg.go.dev/github.com/goforj/search#ErrUnauthorized) | ErrUnauthorized identifies rejected credentials or permissions. |
| [`search.ErrUnavailable`](https://pkg.go.dev/github.com/goforj/search#ErrUnavailable) | ErrUnavailable identifies a failed connection or incomplete backend response. |
| [`search.ErrUnsupported`](https://pkg.go.dev/github.com/goforj/search#ErrUnsupported) | ErrUnsupported identifies an unavailable backend capability. |
| [`search.SafeError`](https://pkg.go.dev/github.com/goforj/search#SafeError) | SafeError removes backend prose and unwrap chains while retaining known classifications. |
| [`search.UnknownOutcomeError`](https://pkg.go.dev/github.com/goforj/search#UnknownOutcomeError) | UnknownOutcomeError retains recoverable accepted-work evidence without exposing backend prose. |
| [`typesensesearch.Config`](https://pkg.go.dev/github.com/goforj/search/driver/typesensesearch#Config) | Config supplies server-owned credentials and an optional physical namespace. |
| [`typesensesearch.Driver`](https://pkg.go.dev/github.com/goforj/search/driver/typesensesearch#Driver) | Driver owns one validated collection and bounded process-local mutation evidence. |
| [`typesensesearch.Driver.Close`](https://pkg.go.dev/github.com/goforj/search/driver/typesensesearch#Driver.Close) | Close prevents new operations after admitted calls have completed. |

### Construction

| API | Description |
| --- | --- |
| [`blevesearch.Open`](https://pkg.go.dev/github.com/goforj/search/driver/blevesearch#Open) | Open acquires ownership without creating or modifying an index schema. |
| [`meilisearch.New`](https://pkg.go.dev/github.com/goforj/search/driver/meilisearch#New) | New validates transport configuration without contacting the server. |
| [`search.Backend`](https://pkg.go.dev/github.com/goforj/search#Backend) | Backend is the consumer-owned request-path contract implemented by concrete drivers. |
| [`search.DefaultLimits`](https://pkg.go.dev/github.com/goforj/search#DefaultLimits) | DefaultLimits returns conservative allocation and backend-work budgets. |
| [`search.Index`](https://pkg.go.dev/github.com/goforj/search#Index) | Index validates calls and owns its immutable definition. It is safe for concurrent use. |
| [`search.Limits`](https://pkg.go.dev/github.com/goforj/search#Limits) | Limits bounds work before it reaches a backend. Zero option fields inherit defaults. |
| [`search.NewIndex`](https://pkg.go.dev/github.com/goforj/search#NewIndex) | NewIndex validates a definition without performing backend I/O or schema mutation. |
| [`search.Option`](https://pkg.go.dev/github.com/goforj/search#Option) | Option configures domain limits or safe observation before the handle is published. |
| [`search.WithLimits`](https://pkg.go.dev/github.com/goforj/search#WithLimits) | WithLimits replaces nonzero limits, up to sixteen times each default ceiling. |
| [`search.WithObserver`](https://pkg.go.dev/github.com/goforj/search#WithObserver) | WithObserver attaches synchronous, panic-isolated observation without retaining contexts. |
| [`typesensesearch.New`](https://pkg.go.dev/github.com/goforj/search/driver/typesensesearch#New) | New validates configuration without provisioning or contacting Typesense. |

### Definitions

| API | Description |
| --- | --- |
| [`blevesearch.Driver.CreateIndex`](https://pkg.go.dev/github.com/goforj/search/driver/blevesearch#Driver.CreateIndex) | CreateIndex creates only an absent index; existing schema must match exactly. |
| [`blevesearch.Driver.ValidateIndex`](https://pkg.go.dev/github.com/goforj/search/driver/blevesearch#Driver.ValidateIndex) | ValidateIndex compares both portable metadata and the actual materialized mapping. |
| [`meilisearch.Driver.CreateIndex`](https://pkg.go.dev/github.com/goforj/search/driver/meilisearch#Driver.CreateIndex) | CreateIndex provisions only missing state and never applies changes to an existing schema. |
| [`meilisearch.Driver.ValidateIndex`](https://pkg.go.dev/github.com/goforj/search/driver/meilisearch#Driver.ValidateIndex) | ValidateIndex compares portable identity and every driver-owned server setting. |
| [`search.Ascending`](https://pkg.go.dev/github.com/goforj/search#Ascending) | Ascending orders strings by UTF-8 bytes, Booleans false first, and integers numerically. |
| [`search.BooleanField`](https://pkg.go.dev/github.com/goforj/search#BooleanField) | BooleanField enables Boolean filtering, ordering, and facets. |
| [`search.Definition`](https://pkg.go.dev/github.com/goforj/search#Definition) | Definition describes rebuildable index data and default query behavior. |
| [`search.Definition.Fingerprint`](https://pkg.go.dev/github.com/goforj/search#Definition.Fingerprint) | Fingerprint returns a versioned canonical hash of the normalized search contract. |
| [`search.Definition.Validate`](https://pkg.go.dev/github.com/goforj/search#Definition.Validate) | Validate checks a definition with conservative default limits. |
| [`search.Descending`](https://pkg.go.dev/github.com/goforj/search#Descending) | Descending reverses the portable scalar order. |
| [`search.Direction`](https://pkg.go.dev/github.com/goforj/search#Direction) | Direction selects portable scalar order. |
| [`search.Field`](https://pkg.go.dev/github.com/goforj/search#Field) | Field maps one declared name to an explicit document path. |
| [`search.FieldType`](https://pkg.go.dev/github.com/goforj/search#FieldType) | FieldType defines the portable mapping shapes. |
| [`search.Index.Definition`](https://pkg.go.dev/github.com/goforj/search#Index.Definition) | Definition returns an owned copy of the normalized definition. |
| [`search.IntegerField`](https://pkg.go.dev/github.com/goforj/search#IntegerField) | IntegerField enables exact integer filtering and ordering. |
| [`search.KeywordField`](https://pkg.go.dev/github.com/goforj/search#KeywordField) | KeywordField enables exact scalar operations. |
| [`search.KeywordsField`](https://pkg.go.dev/github.com/goforj/search#KeywordsField) | KeywordsField enables array membership and term facets. |
| [`search.Sort`](https://pkg.go.dev/github.com/goforj/search#Sort) | Sort selects a required, sortable field. Drivers append an ascending ID tie-break. |
| [`search.TextField`](https://pkg.go.dev/github.com/goforj/search#TextField) | TextField enables analyzed full-text matching only. |
| [`typesensesearch.Driver.CreateIndex`](https://pkg.go.dev/github.com/goforj/search/driver/typesensesearch#Driver.CreateIndex) | CreateIndex creates an absent collection and refuses to mutate an existing schema. |
| [`typesensesearch.Driver.ValidateIndex`](https://pkg.go.dev/github.com/goforj/search/driver/typesensesearch#Driver.ValidateIndex) | ValidateIndex compares actual field settings and persisted ownership metadata. |

### Documents

| API | Description |
| --- | --- |
| [`search.Decode`](https://pkg.go.dev/github.com/goforj/search#Decode) | Decode reads a document without requiring an ID field in the application type. |
| [`search.DecodeError`](https://pkg.go.dev/github.com/goforj/search#DecodeError) | DecodeError identifies a malformed typed hit without disclosing its contents. |
| [`search.Document`](https://pkg.go.dev/github.com/goforj/search#Document) | Document retains explicit identity separately from application-owned JSON. |
| [`search.Encode`](https://pkg.go.dev/github.com/goforj/search#Encode) | Encode marshals a typed value into a validated JSON object. |
| [`search.Upsert`](https://pkg.go.dev/github.com/goforj/search#Upsert) | Upsert encodes a typed value before replacing the complete document. |

### Filters

| API | Description |
| --- | --- |
| [`search.And`](https://pkg.go.dev/github.com/goforj/search#And) | And combines predicates without allowing caller mutation of the child slice. |
| [`search.Between`](https://pkg.go.dev/github.com/goforj/search#Between) | Between tests an inclusive, ordered integer range. |
| [`search.ContainsAll`](https://pkg.go.dev/github.com/goforj/search#ContainsAll) | ContainsAll tests whether an array contains a nonempty set. |
| [`search.ContainsAny`](https://pkg.go.dev/github.com/goforj/search#ContainsAny) | ContainsAny tests array intersection with a nonempty set. |
| [`search.Equal`](https://pkg.go.dev/github.com/goforj/search#Equal) | Equal tests scalar equality; absent and null values compare false. |
| [`search.Exists`](https://pkg.go.dev/github.com/goforj/search#Exists) | Exists tests non-null presence, independently of array membership. |
| [`search.Filter`](https://pkg.go.dev/github.com/goforj/search#Filter) | Filter is an immutable expression. A zero filter means no predicate on Query only. |
| [`search.Filter.View`](https://pkg.go.dev/github.com/goforj/search#Filter.View) | View returns cloned collections so compilers cannot alter the expression. |
| [`search.FilterAnd`](https://pkg.go.dev/github.com/goforj/search#FilterAnd) | FilterAnd applies conjunction, with an empty conjunction matching all. |
| [`search.FilterBetween`](https://pkg.go.dev/github.com/goforj/search#FilterBetween) | FilterBetween tests an inclusive integer interval. |
| [`search.FilterContainsAll`](https://pkg.go.dev/github.com/goforj/search#FilterContainsAll) | FilterContainsAll tests every requested array member. |
| [`search.FilterContainsAny`](https://pkg.go.dev/github.com/goforj/search#FilterContainsAny) | FilterContainsAny tests at least one array member. |
| [`search.FilterEqual`](https://pkg.go.dev/github.com/goforj/search#FilterEqual) | FilterEqual compares an exact scalar. |
| [`search.FilterExists`](https://pkg.go.dev/github.com/goforj/search#FilterExists) | FilterExists tests presence of a non-null value, including an empty array. |
| [`search.FilterGreaterThan`](https://pkg.go.dev/github.com/goforj/search#FilterGreaterThan) | FilterGreaterThan compares integers exclusively. |
| [`search.FilterGreaterThanOrEqual`](https://pkg.go.dev/github.com/goforj/search#FilterGreaterThanOrEqual) | FilterGreaterThanOrEqual compares integers inclusively. |
| [`search.FilterIn`](https://pkg.go.dev/github.com/goforj/search#FilterIn) | FilterIn tests membership in a scalar set. |
| [`search.FilterKind`](https://pkg.go.dev/github.com/goforj/search#FilterKind) | FilterKind identifies an immutable expression node. |
| [`search.FilterLessThan`](https://pkg.go.dev/github.com/goforj/search#FilterLessThan) | FilterLessThan compares integers exclusively. |
| [`search.FilterLessThanOrEqual`](https://pkg.go.dev/github.com/goforj/search#FilterLessThanOrEqual) | FilterLessThanOrEqual compares integers inclusively. |
| [`search.FilterNot`](https://pkg.go.dev/github.com/goforj/search#FilterNot) | FilterNot complements its child's two-valued result. |
| [`search.FilterOr`](https://pkg.go.dev/github.com/goforj/search#FilterOr) | FilterOr applies disjunction, with an empty disjunction matching none. |
| [`search.FilterView`](https://pkg.go.dev/github.com/goforj/search#FilterView) | FilterView is a detached driver-facing expression view. |
| [`search.GreaterThan`](https://pkg.go.dev/github.com/goforj/search#GreaterThan) | GreaterThan compares an integer against an exclusive lower bound. |
| [`search.GreaterThanOrEqual`](https://pkg.go.dev/github.com/goforj/search#GreaterThanOrEqual) | GreaterThanOrEqual compares an integer against an inclusive lower bound. |
| [`search.In`](https://pkg.go.dev/github.com/goforj/search#In) | In tests whether a scalar equals one of a nonempty set. |
| [`search.LessThan`](https://pkg.go.dev/github.com/goforj/search#LessThan) | LessThan compares an integer against an exclusive upper bound. |
| [`search.LessThanOrEqual`](https://pkg.go.dev/github.com/goforj/search#LessThanOrEqual) | LessThanOrEqual compares an integer against an inclusive upper bound. |
| [`search.Not`](https://pkg.go.dev/github.com/goforj/search#Not) | Not complements a predicate; a zero child fails query validation. |
| [`search.Or`](https://pkg.go.dev/github.com/goforj/search#Or) | Or combines alternatives, with zero alternatives matching nothing. |

### Mutations

| API | Description |
| --- | --- |
| [`blevesearch.Driver.Bulk`](https://pkg.go.dev/github.com/goforj/search/driver/blevesearch#Driver.Bulk) | Bulk commits one atomic Bleve batch and retains ordered per-item outcomes. |
| [`blevesearch.Driver.Delete`](https://pkg.go.dev/github.com/goforj/search/driver/blevesearch#Driver.Delete) | Delete commits idempotent removal synchronously. |
| [`blevesearch.Driver.Upsert`](https://pkg.go.dev/github.com/goforj/search/driver/blevesearch#Driver.Upsert) | Upsert commits complete replacement data synchronously. |
| [`meilisearch.Driver.Bulk`](https://pkg.go.dev/github.com/goforj/search/driver/meilisearch#Driver.Bulk) | Bulk submits contiguous operation groups and retains every accepted task. |
| [`meilisearch.Driver.Delete`](https://pkg.go.dev/github.com/goforj/search/driver/meilisearch#Driver.Delete) | Delete submits an idempotent document removal. |
| [`meilisearch.Driver.Upsert`](https://pkg.go.dev/github.com/goforj/search/driver/meilisearch#Driver.Upsert) | Upsert submits a complete replacement rather than a partial document merge. |
| [`search.DeleteMutation`](https://pkg.go.dev/github.com/goforj/search#DeleteMutation) | DeleteMutation validates an ID for idempotent deletion. |
| [`search.Index.Bulk`](https://pkg.go.dev/github.com/goforj/search#Index.Bulk) | Bulk rejects duplicate IDs and validates the entire bounded batch before submission. |
| [`search.Index.Delete`](https://pkg.go.dev/github.com/goforj/search#Index.Delete) | Delete removes an ID idempotently; visibility is established through Wait. |
| [`search.Index.Upsert`](https://pkg.go.dev/github.com/goforj/search#Index.Upsert) | Upsert replaces the complete document and returns accepted-work evidence. |
| [`search.Mutation.Document`](https://pkg.go.dev/github.com/goforj/search#Mutation.Document) | Document returns owned data only for upserts. |
| [`search.Mutation.ID`](https://pkg.go.dev/github.com/goforj/search#Mutation.ID) | ID returns the explicit document identity. |
| [`search.Mutation.Kind`](https://pkg.go.dev/github.com/goforj/search#Mutation.Kind) | Kind identifies which operation this mutation contains. |
| [`search.UpsertMutation`](https://pkg.go.dev/github.com/goforj/search#UpsertMutation) | UpsertMutation takes ownership of document bytes after structural validation. |
| [`typesensesearch.Driver.Bulk`](https://pkg.go.dev/github.com/goforj/search/driver/typesensesearch#Driver.Bulk) | Bulk parses every native import response line, retaining known per-item failures. |
| [`typesensesearch.Driver.Delete`](https://pkg.go.dev/github.com/goforj/search/driver/typesensesearch#Driver.Delete) | Delete removes an ID idempotently and proves synchronous visibility. |
| [`typesensesearch.Driver.Upsert`](https://pkg.go.dev/github.com/goforj/search/driver/typesensesearch#Driver.Upsert) | Upsert replaces a complete document, including removal of previously indexed fields. |

### Observability

| API | Description |
| --- | --- |
| [`search.AsyncObserver`](https://pkg.go.dev/github.com/goforj/search#AsyncObserver) | AsyncObserver delivers retained events in FIFO order from one bounded worker. |
| [`search.AsyncObserver.Dropped`](https://pkg.go.dev/github.com/goforj/search#AsyncObserver.Dropped) | Dropped reports the cumulative number of events discarded by bounded delivery. |
| [`search.AsyncObserver.Observe`](https://pkg.go.dev/github.com/goforj/search#AsyncObserver.Observe) | Observe enqueues without blocking and counts full-queue or post-shutdown drops. |
| [`search.AsyncObserver.Shutdown`](https://pkg.go.dev/github.com/goforj/search#AsyncObserver.Shutdown) | Shutdown closes admission once and waits for retained callbacks within the caller's deadline. |
| [`search.Event`](https://pkg.go.dev/github.com/goforj/search#Event) | Event contains bounded metadata without document, query, or backend prose. |
| [`search.EventPhase`](https://pkg.go.dev/github.com/goforj/search#EventPhase) | EventPhase distinguishes submission latency from visibility and search latency. |
| [`search.MultiObserver`](https://pkg.go.dev/github.com/goforj/search#MultiObserver) | MultiObserver delivers events in registration order with independent panic isolation. |
| [`search.MultiObserver.Observe`](https://pkg.go.dev/github.com/goforj/search#MultiObserver.Observe) | Observe ensures a failed observer does not suppress later observers. |
| [`search.NewAsyncObserver`](https://pkg.go.dev/github.com/goforj/search#NewAsyncObserver) | NewAsyncObserver starts a single delivery worker with capacity from 1 to 65536. |
| [`search.Observer`](https://pkg.go.dev/github.com/goforj/search#Observer) | Observer receives synchronous events after driver locks have been released. |
| [`search.ObserverFunc`](https://pkg.go.dev/github.com/goforj/search#ObserverFunc) | ObserverFunc adapts an application callback to an Observer. |
| [`search.ObserverFunc.Observe`](https://pkg.go.dev/github.com/goforj/search#ObserverFunc.Observe) | Observe invokes the application callback. |
| [`search.PhaseSearchComplete`](https://pkg.go.dev/github.com/goforj/search#PhaseSearchComplete) | PhaseSearchComplete identifies a complete search call. |
| [`search.PhaseSubmission`](https://pkg.go.dev/github.com/goforj/search#PhaseSubmission) | PhaseSubmission identifies accepted-work submission. |
| [`search.PhaseTerminal`](https://pkg.go.dev/github.com/goforj/search#PhaseTerminal) | PhaseTerminal identifies terminal mutation observation. |

### Queries

| API | Description |
| --- | --- |
| [`blevesearch.Driver.Search`](https://pkg.go.dev/github.com/goforj/search/driver/blevesearch#Driver.Search) | Search compiles literal queries and typed filters without a backend query-string parser. |
| [`meilisearch.Driver.Search`](https://pkg.go.dev/github.com/goforj/search/driver/meilisearch#Driver.Search) | Search disables progressive term dropping and makes declared field sorts primary. |
| [`search.Highlight`](https://pkg.go.dev/github.com/goforj/search#Highlight) | Highlight requests untrusted text segments for one searchable field. |
| [`search.Index.Search`](https://pkg.go.dev/github.com/goforj/search#Index.Search) | Search validates literal text, filters, limits, and result documents before exposure. |
| [`search.Query`](https://pkg.go.dev/github.com/goforj/search#Query) | Query specifies literal all-terms text, typed filtering, and bounded pagination. |
| [`search.TermFacet`](https://pkg.go.dev/github.com/goforj/search#TermFacet) | TermFacet counts distinct per-document terms before hit pagination. |
| [`typesensesearch.Driver.Search`](https://pkg.go.dev/github.com/goforj/search/driver/typesensesearch#Driver.Search) | Search disables implicit token dropping, fuzzy expansion, and prefix matching. |

### Receipts

| API | Description |
| --- | --- |
| [`blevesearch.Driver.Wait`](https://pkg.go.dev/github.com/goforj/search/driver/blevesearch#Driver.Wait) | Wait reports synchronous commit evidence, including bounded receipt expiration. |
| [`meilisearch.Driver.Wait`](https://pkg.go.dev/github.com/goforj/search/driver/meilisearch#Driver.Wait) | Wait reports aggregate task precision without fabricating per-document success. |
| [`search.BatchOnly`](https://pkg.go.dev/github.com/goforj/search#BatchOnly) | BatchOnly means only the aggregate outcome is known. |
| [`search.BulkOutcome`](https://pkg.go.dev/github.com/goforj/search#BulkOutcome) | BulkOutcome retains terminal per-item evidence without inventing precision. |
| [`search.BulkResult`](https://pkg.go.dev/github.com/goforj/search#BulkResult) | BulkResult retains submission precision and accepted-work receipt. |
| [`search.Index.Wait`](https://pkg.go.dev/github.com/goforj/search#Index.Wait) | Wait returns terminal failure as Outcome data; method errors mean no trustworthy outcome yet. |
| [`search.ItemAccepted`](https://pkg.go.dev/github.com/goforj/search#ItemAccepted) | ItemAccepted reports proven submission only. |
| [`search.ItemFailed`](https://pkg.go.dev/github.com/goforj/search#ItemFailed) | ItemFailed reports proven per-item failure. |
| [`search.ItemOutcomePrecision`](https://pkg.go.dev/github.com/goforj/search#ItemOutcomePrecision) | ItemOutcomePrecision prevents aggregate success from implying individual success. |
| [`search.ItemRejected`](https://pkg.go.dev/github.com/goforj/search#ItemRejected) | ItemRejected reports a submission rejection. |
| [`search.ItemResult`](https://pkg.go.dev/github.com/goforj/search#ItemResult) | ItemResult preserves the input position even when the backend reorders work. |
| [`search.ItemStatus`](https://pkg.go.dev/github.com/goforj/search#ItemStatus) | ItemStatus retains submission and terminal states separately. |
| [`search.ItemSucceeded`](https://pkg.go.dev/github.com/goforj/search#ItemSucceeded) | ItemSucceeded reports proven per-item visibility. |
| [`search.ItemUnknown`](https://pkg.go.dev/github.com/goforj/search#ItemUnknown) | ItemUnknown reports an unprovable individual outcome. |
| [`search.Mutation`](https://pkg.go.dev/github.com/goforj/search#Mutation) | Mutation is an immutable tagged operation whose zero value is invalid. |
| [`search.MutationDelete`](https://pkg.go.dev/github.com/goforj/search#MutationDelete) | MutationDelete removes an ID if present. |
| [`search.MutationKind`](https://pkg.go.dev/github.com/goforj/search#MutationKind) | MutationKind distinguishes complete replacement from idempotent deletion. |
| [`search.MutationUpsert`](https://pkg.go.dev/github.com/goforj/search#MutationUpsert) | MutationUpsert replaces a complete document. |
| [`search.NewReceipt`](https://pkg.go.dev/github.com/goforj/search#NewReceipt) | NewReceipt creates a bounded driver token without attaching it to an Index. |
| [`search.NewUnknownOutcomeError`](https://pkg.go.dev/github.com/goforj/search#NewUnknownOutcomeError) | NewUnknownOutcomeError lets a driver return recoverable evidence after uncertain submission. |
| [`search.Outcome`](https://pkg.go.dev/github.com/goforj/search#Outcome) | Outcome returns terminal failure as data, keeping any bulk details available. |
| [`search.OutcomeFailed`](https://pkg.go.dev/github.com/goforj/search#OutcomeFailed) | OutcomeFailed identifies terminal failure. |
| [`search.OutcomeState`](https://pkg.go.dev/github.com/goforj/search#OutcomeState) | OutcomeState describes a trustworthy terminal mutation outcome. |
| [`search.OutcomeSucceeded`](https://pkg.go.dev/github.com/goforj/search#OutcomeSucceeded) | OutcomeSucceeded proves visibility to subsequent queries on this index. |
| [`search.OutcomeUnknown`](https://pkg.go.dev/github.com/goforj/search#OutcomeUnknown) | OutcomeUnknown identifies accepted work whose terminal result cannot be proved. |
| [`search.PerItem`](https://pkg.go.dev/github.com/goforj/search#PerItem) | PerItem means every item has an individually proven outcome. |
| [`search.Receipt`](https://pkg.go.dev/github.com/goforj/search#Receipt) | Receipt is an opaque, immutable process-local accepted-work token. |
| [`search.Receipt.Token`](https://pkg.go.dev/github.com/goforj/search#Receipt.Token) | Token returns an owned copy for the issuing backend. |
| [`search.UnknownOutcomeError.Error`](https://pkg.go.dev/github.com/goforj/search#UnknownOutcomeError.Error) | Error reports uncertainty without serializing the opaque token. |
| [`search.UnknownOutcomeError.Receipt`](https://pkg.go.dev/github.com/goforj/search#UnknownOutcomeError.Receipt) | Receipt returns immutable recovery evidence when the driver obtained a trustworthy token. |
| [`search.UnknownOutcomeError.Unwrap`](https://pkg.go.dev/github.com/goforj/search#UnknownOutcomeError.Unwrap) | Unwrap preserves the stable uncertainty classification. |
| [`typesensesearch.Driver.Wait`](https://pkg.go.dev/github.com/goforj/search/driver/typesensesearch#Driver.Wait) | Wait reports retained per-item evidence or explicit receipt expiration. |

### Results

| API | Description |
| --- | --- |
| [`search.Bucket`](https://pkg.go.dev/github.com/goforj/search#Bucket) | Bucket counts documents, deduplicating repeated array terms within each document. |
| [`search.CountEstimated`](https://pkg.go.dev/github.com/goforj/search#CountEstimated) | CountEstimated reports approximate term counts. |
| [`search.CountExact`](https://pkg.go.dev/github.com/goforj/search#CountExact) | CountExact reports complete term counts. |
| [`search.CountPrecision`](https://pkg.go.dev/github.com/goforj/search#CountPrecision) | CountPrecision describes term-bucket count accuracy. |
| [`search.DecodeError.Error`](https://pkg.go.dev/github.com/goforj/search#DecodeError.Error) | Error reports only the classification; callers may inspect ID under their disclosure policy. |
| [`search.DecodeError.Unwrap`](https://pkg.go.dev/github.com/goforj/search#DecodeError.Unwrap) | Unwrap preserves invalid-document classification. |
| [`search.FacetResult`](https://pkg.go.dev/github.com/goforj/search#FacetResult) | FacetResult retains truncation separately from count precision. |
| [`search.Fragment`](https://pkg.go.dev/github.com/goforj/search#Fragment) | Fragment groups adjacent highlight segments. |
| [`search.Hit`](https://pkg.go.dev/github.com/goforj/search#Hit) | Hit is one document and its response-local ranking metadata. |
| [`search.HitOf`](https://pkg.go.dev/github.com/goforj/search#HitOf) | HitOf is a typed hit whose document need not contain its identity. |
| [`search.Page`](https://pkg.go.dev/github.com/goforj/search#Page) | Page is a typed complete search response. |
| [`search.Result`](https://pkg.go.dev/github.com/goforj/search#Result) | Result contains one complete backend search response. |
| [`search.SearchAs`](https://pkg.go.dev/github.com/goforj/search#SearchAs) | SearchAs decodes every hit and preserves its separate identity and metadata. |
| [`search.Segment`](https://pkg.go.dev/github.com/goforj/search#Segment) | Segment is untrusted text; UIs escape Text even when Matched is true. |
| [`search.Total`](https://pkg.go.dev/github.com/goforj/search#Total) | Total retains count precision independently of pagination. |
| [`search.TotalEstimated`](https://pkg.go.dev/github.com/goforj/search#TotalEstimated) | TotalEstimated reports an approximate count. |
| [`search.TotalExact`](https://pkg.go.dev/github.com/goforj/search#TotalExact) | TotalExact reports a complete count. |
| [`search.TotalLowerBound`](https://pkg.go.dev/github.com/goforj/search#TotalLowerBound) | TotalLowerBound reports a proven minimum. |
| [`search.TotalRelation`](https://pkg.go.dev/github.com/goforj/search#TotalRelation) | TotalRelation describes the accuracy of a result count. |

### Testing

| API | Description |
| --- | --- |
| [`searchfake.Backend`](https://pkg.go.dev/github.com/goforj/search/searchfake#Backend) | Backend is a concurrent, explicitly scripted test double. |
| [`searchfake.Backend.Bulk`](https://pkg.go.dev/github.com/goforj/search/searchfake#Backend.Bulk) | Bulk records input order and returns individually proven success for every item. |
| [`searchfake.Backend.Calls`](https://pkg.go.dev/github.com/goforj/search/searchfake#Backend.Calls) | Calls returns a detached snapshot so assertions cannot alter recorded evidence. |
| [`searchfake.Backend.Delete`](https://pkg.go.dev/github.com/goforj/search/searchfake#Backend.Delete) | Delete records an idempotent removal and issues synchronous success evidence. |
| [`searchfake.Backend.QueueSearch`](https://pkg.go.dev/github.com/goforj/search/searchfake#Backend.QueueSearch) | QueueSearch appends a detached response for the next Search call. |
| [`searchfake.Backend.Search`](https://pkg.go.dev/github.com/goforj/search/searchfake#Backend.Search) | Search consumes the next scripted result, failing explicitly when no response is queued. |
| [`searchfake.Backend.Upsert`](https://pkg.go.dev/github.com/goforj/search/searchfake#Backend.Upsert) | Upsert records a replacement and issues synchronous success evidence. |
| [`searchfake.Backend.Wait`](https://pkg.go.dev/github.com/goforj/search/searchfake#Backend.Wait) | Wait respects cancellation and returns retained terminal evidence. |
| [`searchfake.Call`](https://pkg.go.dev/github.com/goforj/search/searchfake#Call) | Call records the operation and owned inputs received after root validation. |
| [`searchfake.New`](https://pkg.go.dev/github.com/goforj/search/searchfake#New) | New creates a fake whose writes succeed and whose reads require queued responses. |
| [`searchtest.Definition`](https://pkg.go.dev/github.com/goforj/search/searchtest#Definition) | Definition returns the shared corpus schema with nullable, array, and required sort fields. |
| [`searchtest.Provision`](https://pkg.go.dev/github.com/goforj/search/searchtest#Provision) | Provision creates an isolated, provisioned index and an idempotent cleanup callback. |
| [`searchtest.RunBackend`](https://pkg.go.dev/github.com/goforj/search/searchtest#RunBackend) | RunBackend proves one portable corpus against an actual provisioned driver. |
| [`searchtest.ScenarioNames`](https://pkg.go.dev/github.com/goforj/search/searchtest#ScenarioNames) | ScenarioNames inventories required fixtures for generated driver evidence. A skipped or missing scenario is not support. |

### Values

| API | Description |
| --- | --- |
| [`search.Boolean`](https://pkg.go.dev/github.com/goforj/search#Boolean) | Boolean constructs a Boolean scalar. |
| [`search.BooleanValue`](https://pkg.go.dev/github.com/goforj/search#BooleanValue) | BooleanValue represents a Boolean. |
| [`search.DocumentID`](https://pkg.go.dev/github.com/goforj/search#DocumentID) | DocumentID identifies a complete replacement document. |
| [`search.FieldName`](https://pkg.go.dev/github.com/goforj/search#FieldName) | FieldName identifies a declared mapping, independently of its JSON path. |
| [`search.IndexName`](https://pkg.go.dev/github.com/goforj/search#IndexName) | IndexName identifies an application-owned logical index. |
| [`search.Integer`](https://pkg.go.dev/github.com/goforj/search#Integer) | Integer rejects values outside the exact common integer range. |
| [`search.IntegerValue`](https://pkg.go.dev/github.com/goforj/search#IntegerValue) | IntegerValue represents an exactly portable signed integer. |
| [`search.JSONPointer`](https://pkg.go.dev/github.com/goforj/search#JSONPointer) | JSONPointer addresses a value using RFC 6901. |
| [`search.Keyword`](https://pkg.go.dev/github.com/goforj/search#Keyword) | Keyword constructs a literal string without normalization. |
| [`search.KeywordValue`](https://pkg.go.dev/github.com/goforj/search#KeywordValue) | KeywordValue represents an exact UTF-8 string. |
| [`search.MaxInteger`](https://pkg.go.dev/github.com/goforj/search#MaxInteger) | MaxInteger is the largest exactly portable integer. |
| [`search.Value`](https://pkg.go.dev/github.com/goforj/search#Value) | Value is an immutable typed filter, facet, or sort scalar. Its zero value is invalid. |
| [`search.Value.Boolean`](https://pkg.go.dev/github.com/goforj/search#Value.Boolean) | Boolean returns the Boolean only for Boolean values. |
| [`search.Value.Integer`](https://pkg.go.dev/github.com/goforj/search#Value.Integer) | Integer returns the integer only for integer values. |
| [`search.Value.Keyword`](https://pkg.go.dev/github.com/goforj/search#Value.Keyword) | Keyword returns the string only for keyword values. |
| [`search.Value.Kind`](https://pkg.go.dev/github.com/goforj/search#Value.Kind) | Kind reports the scalar's discriminator. |
| [`search.ValueKind`](https://pkg.go.dev/github.com/goforj/search#ValueKind) | ValueKind distinguishes portable scalar types. |
<!-- api:embed:end -->
