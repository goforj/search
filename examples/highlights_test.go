package examples_test

import (
	"context"
	"fmt"
	"github.com/goforj/search"
	"github.com/goforj/search/driver/blevesearch"
	"html"
)

// Example_highlights demonstrates escape structured highlights.
// @readme Escape structured highlights
func Example_highlights() {

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
	// Output:
	// <mark>Blue</mark> mug
}
