package examples_test

import (
	"fmt"

	"github.com/goforj/search/driver/elasticsearch"
	"github.com/goforj/search/driver/meilisearch"
	"github.com/goforj/search/driver/opensearch"
	"github.com/goforj/search/driver/redissearch"
	"github.com/goforj/search/driver/typesensesearch"
)

// Example_services demonstrates lazy client construction without opening a connection.
// @readme Construct service drivers
// @api meilisearch.Driver meilisearch.Driver.Close typesensesearch.Driver typesensesearch.Driver.Close opensearch.Driver opensearch.Driver.Close elasticsearch.Driver elasticsearch.Driver.Close redissearch.Driver redissearch.Driver.Close meilisearch.New meilisearch.Config typesensesearch.New typesensesearch.Config opensearch.New opensearch.Config elasticsearch.New elasticsearch.Config redissearch.New redissearch.Config
func Example_services() {
	// snippet:start
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
	elastic, err := elasticsearch.New(elasticsearch.Config{URL: "http://127.0.0.1:9200"})
	if err != nil {
		panic(err)
	}
	defer elastic.Close()
	redis, err := redissearch.New(redissearch.Config{Addr: "127.0.0.1:6379"})
	if err != nil {
		panic(err)
	}
	defer redis.Close()
	fmt.Println("Clients constructed without backend I/O")
	// Clients constructed without backend I/O

	// snippet:end

	// Output:
	// Clients constructed without backend I/O
}
