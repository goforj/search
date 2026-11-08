package examples_test

import (
	"context"
	"fmt"

	"github.com/goforj/search"
	"github.com/goforj/search/driver/blevesearch"
)

// Example_quickstart demonstrates the complete embedded indexing and typed-query lifecycle.
// @readme Index and search a document
func Example_quickstart() {
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

	// Output:
	// mug-1 Blue mug
}
