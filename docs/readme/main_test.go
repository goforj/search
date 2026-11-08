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
	program := "package main\n\n// main demonstrates a workflow.\n// @readme Quickstart\n// @api search.NewIndex\nfunc main() {\n\t// snippet:start\n\tfmt.Println(1)\n\t// 1\n\t// snippet:end\n\n\t// Output:\n\t// 1\n}\n"
	markdown, err := quickStartMarkdown([]gen.Example{{Name: "quickstart", Program: []byte(program)}})
	if err != nil || !strings.Contains(markdown, "package main") || !strings.Contains(markdown, "fmt.Println(1)") {
		t.Fatal(markdown, err)
	}
	if strings.Contains(markdown, "<details>") {
		t.Fatal("quickstart must be visible without expanding a disclosure")
	}
	if !strings.Contains(markdown, "}\n```\n") {
		t.Fatal("quickstart code fence must start on a new line")
	}
	if strings.Contains(markdown, "@readme") || strings.Contains(markdown, "@api") || strings.Contains(markdown, "snippet:") || strings.Contains(markdown, "Output:") {
		t.Fatal("generator metadata leaked into the opening example")
	}
	if _, err := quickStartMarkdown([]gen.Example{{Name: "quickstart", Program: []byte("invalid Go source")}}); err == nil {
		t.Fatal("malformed quickstart source was accepted")
	}
}

// TestAPIIndexLinksToInlineExamples keeps API navigation inside the README.
func TestAPIIndexLinksToInlineExamples(t *testing.T) {
	symbols := []gen.Symbol{{Name: "search.Index.Search", Source: "index.go", Line: 79, Group: "Queries", Description: "Search matches text | filters."}}
	examples := []gen.Example{{Name: "search", Title: "Search documents", Source: "search/main.go", APIs: []string{"search.Index.Search"}, Snippet: "index.Search(ctx, query)\n"}}
	entries := []apiEntry{{Task: "Search documents", APIs: []string{"search.Index.Search"}, Examples: []string{"search"}}}
	markdown, err := apiMarkdownFor(symbols, examples, entries)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"[Index.Search](#example-search)",
		"[Search documents](#example-search)",
		`id="example-search"`,
		"```go\nindex.Search(ctx, query)\n```",
		`### <a id="example-search"></a>Search documents`,
	} {
		if !strings.Contains(markdown, expected) {
			t.Fatal("missing API navigation or example", expected)
		}
	}
	index, _, _ := strings.Cut(markdown, "## API examples")
	if strings.Contains(index, "#L") || strings.Contains(index, "](index.go") {
		t.Fatal("API index links to implementation source")
	}
	if strings.Contains(markdown, "[search.Index.Search]") || strings.Contains(markdown, "[`Index.Search`]") || strings.Contains(markdown, "pkg.go.dev") {
		t.Fatal("API links use code styling or unpublished documentation")
	}
	if strings.Contains(markdown, "></a>[") {
		t.Fatal("example headings must be plain text")
	}
	if strings.Contains(markdown, "[Full program]") || strings.Contains(markdown, "[Executable test]") {
		t.Fatal("repeated example footers returned")
	}
	examples[0].APIs = []string{"search.Missing"}
	if _, err := apiMarkdownFor(symbols, examples, entries); err == nil {
		t.Fatal("unknown API annotation was accepted")
	}
}

// TestApplicationAPIStaysFocused prevents driver plumbing and supporting DTOs from returning to the primary index.
func TestApplicationAPIStaysFocused(t *testing.T) {
	root, err := gen.Root()
	if err != nil {
		t.Fatal(err)
	}
	symbols, err := gen.Symbols(root)
	if err != nil {
		t.Fatal(err)
	}
	examples, err := gen.Examples(root)
	if err != nil {
		t.Fatal(err)
	}
	markdown, err := apiMarkdown(symbols, examples)
	if err != nil {
		t.Fatal(err)
	}
	index, _, _ := strings.Cut(markdown, "## API examples")
	for _, name := range []string{"NewIndex", "Upsert", "SearchAs", "Index.Bulk", "Index.Wait", "searchfake.New"} {
		if !strings.Contains(index, "["+name+"]") {
			t.Fatal("missing application operation", name)
		}
	}
	for _, name := range []string{"FilterView", "Receipt.Token", "Value.MarshalJSON", "meilisearch.Driver.Search", "ItemOutcomePrecision", "ErrClosed"} {
		if strings.Contains(index, "["+name+"]") {
			t.Fatal("supporting API returned to application index", name)
		}
	}
	if strings.Contains(index, "| Description |") {
		t.Fatal("exhaustive symbol table returned")
	}
}

// TestCuratedEntriesRejectStaleReferences catches renamed APIs and examples at generation time.
func TestCuratedEntriesRejectStaleReferences(t *testing.T) {
	for _, entry := range []apiEntry{
		{Task: "Missing API", APIs: []string{"search.Missing"}, Examples: []string{"example"}},
		{Task: "Missing example", Examples: []string{"missing"}},
		{Task: "No example", APIs: []string{"search.Index.Search"}},
		{Task: "No snippet", Examples: []string{"empty"}},
	} {
		if _, err := apiMarkdownFor(nil, []gen.Example{{Name: "example", Snippet: "code"}, {Name: "empty"}}, []apiEntry{entry}); err == nil {
			t.Fatal("stale curated entry accepted", entry.Task)
		}
	}
}

// TestAPIIndexRequiresMatchingExamples prevents valid anchors from hiding unrelated usage.
func TestAPIIndexRequiresMatchingExamples(t *testing.T) {
	symbols := []gen.Symbol{{Name: "search.Upsert"}, {Name: "search.Index.Wait"}}
	examples := []gen.Example{
		{Name: "write", APIs: []string{"search.Upsert"}, Snippet: "search.Upsert(ctx, index, id, value)"},
		{Name: "wait", APIs: []string{"search.Index.Wait"}, Snippet: "index.Wait(ctx, receipt)"},
	}
	entries := []apiEntry{{Task: "Write and wait", APIs: []string{"search.Upsert", "search.Index.Wait"}, Examples: []string{"write", "wait"}}}
	markdown, err := apiMarkdownFor(symbols, examples, entries)
	if err != nil || !strings.Contains(markdown, "[Index.Wait](#example-wait)") {
		t.Fatal(markdown, err)
	}
	entries[0].Examples = []string{"write"}
	if _, err := apiMarkdownFor(symbols, examples, entries); err == nil {
		t.Fatal("unrelated inline example accepted")
	}
}
