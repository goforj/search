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

	// snippet:end

	// Output:
	// mug-1 Blue mug
}
