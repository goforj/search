package main

import (
	"strings"
	"testing"

	"github.com/goforj/search/docs/gen"
)

// TestQuickStartRequiresTestedSource prevents generation from silently dropping the opening workflow.
func TestQuickStartRequiresTestedSource(t *testing.T) {
	if _, err := quickStartMarkdown([]gen.Example{{Name: "filters"}}); err == nil {
		t.Fatal("missing quick start was accepted")
	}
	program := "// Code generated from examples/quickstart_test.go; DO NOT EDIT.\npackage main\n\n// main executes the documented workflow.\nfunc main() {}\n"
	markdown, err := quickStartMarkdown([]gen.Example{{Name: "quickstart", Program: []byte(program)}})
	if err != nil || !strings.Contains(markdown, "package main") || !strings.Contains(markdown, "func main() {}") {
		t.Fatal(markdown, err)
	}
	if strings.Contains(markdown, "DO NOT EDIT") || strings.Contains(markdown, "// main executes") {
		t.Fatal("generator metadata leaked into the opening example")
	}
}

// TestAPIIndexLinksToSourceAndInlineExamples keeps navigation useful before a module is published.
func TestAPIIndexLinksToSourceAndInlineExamples(t *testing.T) {
	symbols := []gen.Symbol{{Name: "search.Index.Search", Source: "index.go", Line: 79, Group: "Queries", Description: "Search matches text | filters."}}
	examples := []gen.Example{{Name: "search", Title: "Search documents", Source: "search_test.go", APIs: []string{"search.Index.Search"}, Snippet: "index.Search(ctx, query)\n"}}
	markdown, err := apiMarkdown(symbols, examples)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"[search.Index.Search](index.go#L79)",
		"[Search documents](#example-search)",
		`id="example-search"`,
		"```go\nindex.Search(ctx, query)\n```",
		"[Full program](examples/search/main.go)",
		"text \\| filters",
	} {
		if !strings.Contains(markdown, expected) {
			t.Fatal("missing API navigation or example", expected)
		}
	}
	if strings.Contains(markdown, "[`search.Index.Search`]") || strings.Contains(markdown, "pkg.go.dev") {
		t.Fatal("API links use code styling or unpublished documentation")
	}
	examples[0].APIs = []string{"search.Missing"}
	if _, err := apiMarkdown(symbols, examples); err == nil {
		t.Fatal("unknown API annotation was accepted")
	}
}
