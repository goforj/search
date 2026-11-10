// Command readme generates the README API index and tested examples from authoritative Go source.
package main

import (
	"fmt"
	"go/format"
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
	usage.WriteString("## Runnable examples\n\nEach program under [`examples/`](examples) is executed by a [shared test runner](examples/example_test.go) that checks its expected output. Run these commands from the repository root with Go 1.25 or newer.\n\n| Example | Run |\n| --- | --- |\n")
	for _, example := range examples {
		fmt.Fprintf(&usage, "| [%s](examples/%s/main.go) | `go run ./examples/%s` |\n", example.Title, example.Name, example.Name)
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
			var lines []string
			output := false
			for _, line := range strings.Split(string(example.Program), "\n") {
				trimmed := strings.TrimSpace(line)
				if trimmed == "// Output:" {
					for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
						lines = lines[:len(lines)-1]
					}
					output = true
				}
				if strings.HasPrefix(trimmed, "// @") || strings.HasPrefix(trimmed, "// snippet:") || (output && (strings.HasPrefix(trimmed, "//") || trimmed == "")) {
					continue
				}
				lines = append(lines, line)
			}
			formatted, err := format.Source([]byte(strings.Join(lines, "\n")))
			if err != nil {
				return "", fmt.Errorf("format quickstart: %w", err)
			}
			program := string(formatted)
			return "## Quick Start\n\nGo 1.25+, with no external service required.\n\n```sh\ngit clone https://github.com/goforj/search.git\ncd search\ngo run ./examples/quickstart\n```\n\n```go\n" + program + "```\n", nil
		}
	}
	return "", fmt.Errorf("missing executable quickstart example")
}

// apiEntry groups application operations without exposing the full exported-symbol inventory.
type apiEntry struct {
	Group    string
	APIs     []string
	Examples []string
}

// applicationAPI keeps the README centered on the operations application services call.
var applicationAPI = []apiEntry{
	{"Construction", []string{"search.Definition", "search.NewIndex"}, []string{"quickstart"}},
	{"Facets", []string{"search.TermFacet"}, []string{"facets"}},
	{"Filters", []string{"search.And", "search.Equal", "search.Integer", "search.Keyword"}, []string{"filters"}},
	{"Highlights", []string{"search.Highlight"}, []string{"highlights"}},
	{"Mutations", []string{"search.DeleteMutation", "search.Index.Bulk", "search.Upsert"}, []string{"bulk", "quickstart"}},
	{"Queries", []string{"search.Index.Search", "search.Query", "search.SearchAs"}, []string{"search", "filters"}},
	{"Sorting", []string{"search.Sort"}, []string{"filters"}},
	{"Testing", []string{"searchfake.Backend.Calls", "searchfake.Backend.QueueSearch", "searchfake.New"}, []string{"fake"}},
	{"Visibility", []string{"search.Index.Wait", "search.Outcome"}, []string{"quickstart", "bulk"}},
}

// apiMarkdown projects the curated application categories while retaining tested example navigation.
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
	var out, usage strings.Builder
	anchored := map[string]bool{}
	out.WriteString("## API Index\n\n| Group | Functions |\n| ---: | --- |\n")
	for _, entry := range entries {
		apis := []string{}
		byUsage := map[string][]string{}
		if len(entry.Examples) == 0 {
			return "", fmt.Errorf("API index group %s has no example", entry.Group)
		}
		for _, name := range entry.Examples {
			example, ok := byExample[name]
			if !ok {
				return "", fmt.Errorf("API index references missing example %s", name)
			}
			if example.Snippet == "" {
				return "", fmt.Errorf("API index references example %s without an inline snippet", name)
			}
		}
		for _, name := range entry.APIs {
			symbol, ok := known[name]
			if !ok {
				return "", fmt.Errorf("API index references unknown API %s", name)
			}
			target := ""
			for _, exampleName := range entry.Examples {
				for _, api := range byExample[exampleName].APIs {
					if api == name {
						target = exampleName
						break
					}
				}
				if target != "" {
					break
				}
			}
			if target == "" {
				return "", fmt.Errorf("API index references API %s without a matching example", name)
			}
			label := strings.TrimPrefix(symbol.Name, "search.")
			anchor := "api-" + strings.ToLower(strings.ReplaceAll(label, ".", "-"))
			apis = append(apis, fmt.Sprintf("[%s](#%s)", label, anchor))
			byUsage[target] = append(byUsage[target], fmt.Sprintf(`<a id="%s"></a>%s`, anchor, label))
		}
		fmt.Fprintf(&out, "| **%s** | %s |\n", entry.Group, strings.Join(apis, " · "))
		fmt.Fprintf(&usage, "\n## %s\n", entry.Group)
		for _, exampleName := range entry.Examples {
			headings := byUsage[exampleName]
			if len(headings) == 0 {
				continue
			}
			example := byExample[exampleName]
			if !anchored[exampleName] {
				fmt.Fprintf(&usage, "\n<a id=\"example-%s\"></a>\n", exampleName)
				anchored[exampleName] = true
			}
			fmt.Fprintf(&usage, "\n### %s\n\n%s.\n\n```go\n%s```\n", strings.Join(headings, " · "), example.Title, example.Snippet)
		}
	}
	out.WriteString("\nFind complete programs and setup in [Runnable examples](#runnable-examples).\n")
	out.WriteString(usage.String())
	return out.String(), nil
}
