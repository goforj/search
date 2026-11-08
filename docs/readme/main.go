// Command readme generates the README API index and tested examples from authoritative Go source.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goforj/search/docs/gen"
)

// main reports a failed projection instead of leaving apparently successful stale documentation.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run computes every replacement before writing the completed README.
func run() error {
	root, err := gen.Root()
	if err != nil {
		return err
	}
	symbols, err := gen.Symbols(root)
	if err != nil {
		return err
	}
	var api strings.Builder
	api.WriteString("## API Index\n\nGenerated from GoDoc. Edit the owning Go source and run `make generate`.\n")
	group := ""
	for _, symbol := range symbols {
		if group != symbol.Group {
			group = symbol.Group
			fmt.Fprintf(&api, "\n### %s\n\n| API | Description |\n| --- | --- |\n", group)
		}
		fmt.Fprintf(&api, "| [`%s`](https://pkg.go.dev/%s#%s) | %s |\n", symbol.Name, symbol.Package, symbol.Anchor, strings.ReplaceAll(symbol.Description, "|", "\\|"))
	}
	examples, err := gen.Examples(root)
	if err != nil {
		return err
	}
	var usage strings.Builder
	usage.WriteString("## Examples\n\nThese examples run as Go example tests. Standalone programs live under `examples/`.\n")
	for _, example := range examples {
		fmt.Fprintf(&usage, "\n### %s\n\n```go\n%s```\n", example.Title, example.Program)
	}
	path := filepath.Join(root, "README.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	result, err := gen.Replace(string(data), "api", api.String())
	if err != nil {
		return err
	}
	result, err = gen.Replace(result, "examples", usage.String())
	if err != nil {
		return err
	}
	drivers, err := gen.EvidenceMarkdown(root)
	if err != nil {
		return err
	}
	result, err = gen.Replace(result, "drivers", drivers)
	if err != nil {
		return err
	}
	features, err := gen.FeaturesMarkdown(root)
	if err != nil {
		return err
	}
	result, err = gen.Replace(result, "features", features)
	if err != nil {
		return err
	}
	badges, err := gen.TestBadges(root)
	if err != nil {
		return err
	}
	result, err = gen.Replace(result, "test-count", badges)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(result), 0644)
}
