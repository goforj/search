// Command fake demonstrates testing application search.
package main

import (
	"context"
	"fmt"

	"github.com/goforj/search"
	"github.com/goforj/search/searchfake"
)

// main demonstrates application testing with an explicitly scripted backend.
// @readme Test an application service
// @api searchfake.New searchfake.Backend.QueueSearch searchfake.Backend.Calls search.Result search.Total search.TotalExact
func main() {
	// snippet:start
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

	// snippet:end

	// Output:
	// 3 search
}
