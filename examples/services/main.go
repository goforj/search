// Code generated from examples/services_test.go; DO NOT EDIT.
package main

import (
	"fmt"

	"github.com/goforj/search/driver/elasticsearch"
	"github.com/goforj/search/driver/meilisearch"
	"github.com/goforj/search/driver/opensearch"
	"github.com/goforj/search/driver/redissearch"
	"github.com/goforj/search/driver/typesensesearch"
)

// main executes the documented workflow.
func main() {
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

}
