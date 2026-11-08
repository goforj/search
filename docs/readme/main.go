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

// apiMarkdown links declarations to checked-in source and associates APIs with their tested inline examples.
func apiMarkdown(symbols []gen.Symbol, examples []gen.Example) (string, error) {
	known := map[string]bool{}
	for _, symbol := range symbols {
		known[symbol.Name] = true
	}
	related := map[string][]gen.Example{}
	for _, example := range examples {
		for _, name := range example.APIs {
			if !known[name] {
				return "", fmt.Errorf("example %s references unknown API %s", example.Name, name)
			}
			related[name] = append(related[name], example)
		}
	}
	var out strings.Builder
	out.WriteString("## API Index\n\nAPI names open their source definitions. Example links jump to tested usage below.\n")
	group := ""
	for _, symbol := range symbols {
		if group != symbol.Group {
			group = symbol.Group
			fmt.Fprintf(&out, "\n### %s\n\n| API | Description | Example |\n| --- | --- | --- |\n", group)
		}
		links := []string{}
		for _, example := range related[symbol.Name] {
			links = append(links, fmt.Sprintf("[%s](#example-%s)", example.Title, example.Name))
		}
		fmt.Fprintf(&out, "| [%s](%s#L%d) | %s | %s |\n", symbol.Name, symbol.Source, symbol.Line, strings.ReplaceAll(symbol.Description, "|", "\\|"), strings.Join(links, " · "))
	}
	out.WriteString("\n## API examples\n\nEach snippet comes from its linked executable test. Open the full program for its index definition and setup.\n")
	for _, example := range examples {
		if example.Snippet == "" {
			continue
		}
		fmt.Fprintf(&out, "\n### <a id=\"example-%s\"></a>%s\n\n```go\n%s```\n\n[Full program](examples/%s/main.go) · [Executable test](examples/%s)\n", example.Name, example.Title, example.Snippet, example.Name, example.Source)
	}
	return out.String(), nil
}
