// Command search demonstrates typed document search.
package main

import (
	"context"
	"fmt"
	"github.com/goforj/search"
	"github.com/goforj/search/driver/blevesearch"
)

// main demonstrates search typed documents.
// @readme Search typed documents
// @api search.SearchAs search.Query search.Page search.HitOf
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

	// snippet:start
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

	// snippet:end

	// Output:
	// Blue mug
	// 1
}
