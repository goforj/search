// Code generated from examples/services_test.go; DO NOT EDIT.
package main

import (
	"fmt"

	"github.com/goforj/search/driver/meilisearch"
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
	fmt.Println("Clients constructed without backend I/O")
	// Clients constructed without backend I/O

}
