// Command render projects persisted real-driver benchmark measurements into SVGs and the README.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/goforj/search/docs/bench"
	"github.com/goforj/search/docs/gen"
)

// main makes malformed or incomparable measurement evidence a generation failure.
func main() {
	input := flag.String("input", "", "import all eight driver reports from this directory")
	flag.Parse()
	root, err := gen.Root()
	if err == nil {
		if *input != "" {
			err = bench.Import(root, *input)
		} else {
			err = bench.Render(root)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
