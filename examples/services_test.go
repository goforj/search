package examples_test

import (
	"fmt"

	"github.com/goforj/search/driver/meilisearch"
	"github.com/goforj/search/driver/opensearch"
	"github.com/goforj/search/driver/typesensesearch"
)

// Example_services demonstrates lazy client construction without opening a connection.
// @readme Construct service drivers
func Example_services() {
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
	fmt.Println("Clients constructed without backend I/O")
	// Clients constructed without backend I/O
	// Output:
	// Clients constructed without backend I/O
}
