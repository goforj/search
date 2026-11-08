package examples_test

import (
	"context"
	"fmt"

	"github.com/goforj/search"
	"github.com/goforj/search/searchfake"
)

// Example_fake demonstrates application testing with an explicitly scripted backend.
// @readme Test an application service
func Example_fake() {
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
	// Output:
	// 3 search
}
