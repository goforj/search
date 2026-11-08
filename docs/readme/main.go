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
	examples, err := gen.Examples(root)
	if err != nil {
		return err
	}
	api, err := apiMarkdown(symbols, examples)
	if err != nil {
		return err
	}
	var usage strings.Builder
	usage.WriteString("## Runnable examples\n\nEach example is executed as a Go example test and generated into a standalone program under [`examples/`](examples). Run these commands from the repository root with Go 1.25 or newer.\n\n| Example | Run | Tested source |\n| --- | --- | --- |\n")
	for _, example := range examples {
		fmt.Fprintf(&usage, "| [%s](examples/%s/main.go) | `go run ./examples/%s` | [Example test](examples/%s) |\n", example.Title, example.Name, example.Name, example.Source)
	}
	quickstart, err := quickStartMarkdown(examples)
	if err != nil {
		return err
	}
	path := filepath.Join(root, "README.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	result, err := gen.Replace(string(data), "api", api)
	if err != nil {
		return err
	}
	result, err = gen.Replace(result, "examples", usage.String())
	if err != nil {
		return err
	}
	result, err = gen.Replace(result, "quickstart", quickstart)
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

// quickStartMarkdown keeps the opening workflow identical to its executable standalone program.
func quickStartMarkdown(examples []gen.Example) (string, error) {
	for _, example := range examples {
		if example.Name == "quickstart" {
			_, program, _ := strings.Cut(string(example.Program), "\n")
			program = strings.ReplaceAll(program, "// main executes the documented workflow.\n", "")
			return "## Quick Start\n\nGo 1.25+, with no external service required.\n\n```sh\ngit clone https://github.com/goforj/search.git\ncd search\ngo run ./examples/quickstart\n```\n\n<details>\n<summary>View the full Go example</summary>\n\n```go\n" + program + "```\n\n</details>\n", nil
		}
	}
	return "", fmt.Errorf("missing executable quickstart example")
}

// apiEntry selects operations around one application task rather than exported-symbol inventory.
type apiEntry struct {
	Task     string
	APIs     []string
	Examples []string
}

// applicationAPI keeps the README centered on the operations application services call.
var applicationAPI = []apiEntry{
	{"Define an index", []string{"search.Definition", "search.NewIndex"}, []string{"quickstart"}},
	{"Write and remove documents", []string{"search.Upsert", "search.Index.Delete"}, []string{"quickstart"}},
	{"Search documents", []string{"search.SearchAs", "search.Index.Search", "search.Query"}, []string{"search"}},
	{"Filter results", []string{"search.Keyword", "search.Integer", "search.And", "search.Equal"}, []string{"filters"}},
	{"Sort and paginate", []string{"search.Query", "search.Sort"}, []string{"filters"}},
	{"Count facets", []string{"search.TermFacet"}, []string{"facets"}},
	{"Highlight matches", []string{"search.Highlight"}, []string{"highlights"}},
	{"Write a batch", []string{"search.Index.Bulk", "search.UpsertMutation", "search.DeleteMutation"}, []string{"bulk"}},
	{"Wait for visibility", []string{"search.Index.Wait", "search.Outcome"}, []string{"quickstart", "bulk"}},
	{"Test application code", []string{"searchfake.New", "searchfake.Backend.QueueSearch", "searchfake.Backend.Calls"}, []string{"fake"}},
}

// apiMarkdown projects the curated application workflow while retaining tested example navigation.
func apiMarkdown(symbols []gen.Symbol, examples []gen.Example) (string, error) {
	return apiMarkdownFor(symbols, examples, applicationAPI)
}

// apiMarkdownFor resolves curated entries against actual declarations and executable examples.
func apiMarkdownFor(symbols []gen.Symbol, examples []gen.Example, entries []apiEntry) (string, error) {
	known := map[string]gen.Symbol{}
	for _, symbol := range symbols {
		known[symbol.Name] = symbol
	}
	byExample := map[string]gen.Example{}
	for _, example := range examples {
		byExample[example.Name] = example
		for _, name := range example.APIs {
			if _, ok := known[name]; !ok {
				return "", fmt.Errorf("example %s references unknown API %s", example.Name, name)
			}
		}
	}
	var out strings.Builder
	out.WriteString("## API Index\n\nChoose a backend in [Drivers](#drivers), then use these operations in your application. API names open their source definitions; examples jump to tested usage below.\n\n| Task | API | Example |\n| --- | --- | --- |\n")
	for _, entry := range entries {
		apis, links := []string{}, []string{}
		for _, name := range entry.APIs {
			symbol, ok := known[name]
			if !ok {
				return "", fmt.Errorf("API index references unknown API %s", name)
			}
			label := strings.TrimPrefix(symbol.Name, "search.")
			apis = append(apis, fmt.Sprintf("[%s](%s#L%d)", label, symbol.Source, symbol.Line))
		}
		for _, name := range entry.Examples {
			example, ok := byExample[name]
			if !ok {
				return "", fmt.Errorf("API index references missing example %s", name)
			}
			links = append(links, fmt.Sprintf("[%s](#example-%s)", example.Title, example.Name))
		}
		fmt.Fprintf(&out, "| %s | %s | %s |\n", entry.Task, strings.Join(apis, " · "), strings.Join(links, " · "))
	}
	out.WriteString("\n## API examples\n\nSelect an example title for its complete runnable program and setup.\n")
	for _, example := range examples {
		if example.Snippet == "" {
			continue
		}
		fmt.Fprintf(&out, "\n### <a id=\"example-%s\"></a>[%s](examples/%s/main.go)\n\n```go\n%s```\n", example.Name, example.Title, example.Name, example.Snippet)
	}
	return out.String(), nil
}
