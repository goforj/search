// Command quickstart demonstrates embedded indexing and search.
package main

import (
	"context"
	"fmt"

	"github.com/goforj/search"
	"github.com/goforj/search/driver/blevesearch"
)

// main demonstrates the complete embedded indexing and typed-query lifecycle.
// @readme Index and search a document
// @api blevesearch.Open blevesearch.Config blevesearch.Driver blevesearch.Driver.Close search.DocumentID search.Definition search.Field search.NewIndex search.Upsert search.Index.Wait search.Outcome search.OutcomeSucceeded
func main() {
	// snippet:start
	ctx := context.Background()

	// Use an in-memory index so this example needs no files or search service.
	driver, err := blevesearch.Open(ctx, blevesearch.Config{Memory: true})
	if err != nil {
		panic(err)
	}
	defer driver.Close()

	// Search descriptive text and expose catalog fields for filtering and sorting.
	definition := search.Definition{
		Name: "products",
		Fields: []search.Field{
			{Name: "title", Type: search.TextField, Searchable: true},
			{Name: "description", Type: search.TextField, Searchable: true},
			{Name: "category", Type: search.KeywordField, Filterable: true},
			{Name: "price_cents", Type: search.IntegerField, Required: true, Filterable: true, Sortable: true},
			{Name: "in_stock", Type: search.BooleanField, Filterable: true},
		},
	}
	// Provision the schema explicitly before creating an application handle.
	if err := driver.CreateIndex(ctx, definition); err != nil {
		panic(err)
	}
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		panic(err)
	}

	// product models a catalog item; its document ID is supplied separately to Upsert.
	type product struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Category    string `json:"category"`
		PriceCents  int64  `json:"price_cents"`
		InStock     bool   `json:"in_stock"`
	}
	receipt, err := search.Upsert(ctx, index, "mug-1", product{
		Title:       "Blue mug",
		Description: "Handcrafted ceramic mug with a matte glaze, 350 ml.",
		Category:    "kitchen",
		PriceCents:  2495,
		InStock:     true,
	})
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
	hit := page.Hits[0]
	fmt.Println(hit.ID, hit.Value.Title)
	// mug-1 Blue mug
	fmt.Printf("%s | $%.2f | in stock: %t\n", hit.Value.Category, float64(hit.Value.PriceCents)/100, hit.Value.InStock)
	// kitchen | $24.95 | in stock: true

	// snippet:end

	// Output:
	// mug-1 Blue mug
	// kitchen | $24.95 | in stock: true
}
