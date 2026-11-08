// Command examplegen projects executable examples into standalone programs.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goforj/search/docs/gen"
)

// main fails generation visibly so stale artifacts cannot pass CI.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run writes deterministic standalone projections from tested source examples.
func run() error {
	root, err := gen.Root()
	if err != nil {
		return err
	}
	examples, err := gen.Examples(root)
	if err != nil {
		return err
	}
	for _, example := range examples {
		dir := filepath.Join(root, "examples", example.Name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "main.go"), example.Program, 0644); err != nil {
			return err
		}
	}
	return nil
}
