package examples_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/goforj/search"
	"github.com/goforj/search/driver/blevesearch"
)

// BenchmarkEmbeddedSearch measures the complete portable query path over a fixed 1000-document corpus.
func BenchmarkEmbeddedSearch(b *testing.B) {
	ctx := context.Background()
	driver, err := blevesearch.Open(ctx, blevesearch.Config{Memory: true})
	if err != nil {
		b.Fatal(err)
	}
	defer driver.Close()
	definition := search.Definition{Name: "products", Fields: []search.Field{
		{Name: "title", Type: search.TextField, Searchable: true},
		{Name: "category", Type: search.KeywordField, Filterable: true, Facetable: true},
	}}
	if err := driver.CreateIndex(ctx, definition); err != nil {
		b.Fatal(err)
	}
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		b.Fatal(err)
	}
	mutations := make([]search.Mutation, 0, 1000)
	for n := 0; n < 1000; n++ {
		document, err := search.Encode(search.DocumentID(fmt.Sprintf("product%04d", n)), map[string]string{"title": "blue mug", "category": "kitchen"})
		if err != nil {
			b.Fatal(err)
		}
		mutation, err := search.UpsertMutation(document)
		if err != nil {
			b.Fatal(err)
		}
		mutations = append(mutations, mutation)
	}
	batch, err := index.Bulk(ctx, mutations)
	if err != nil {
		b.Fatal(err)
	}
	if outcome, err := index.Wait(ctx, batch.Receipt); err != nil || outcome.State != search.OutcomeSucceeded {
		b.Fatal(outcome, err)
	}
	query := search.Query{Text: "blue mug", Limit: 20}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := index.Search(ctx, query); err != nil {
			b.Fatal(err)
		}
	}
}
