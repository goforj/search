package gen

import (
	"bytes"
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReplace rejects ambiguous ownership boundaries before changing a document.
func TestReplace(t *testing.T) {
	source := "before\n<!-- api:embed:start -->\nold\n<!-- api:embed:end -->\nafter"
	out, err := Replace(source, "api", "new")
	if err != nil || !strings.Contains(out, "\nnew\n") || !strings.HasSuffix(out, "after") {
		t.Fatal(out, err)
	}
	for _, source := range []string{"missing", "<!-- api:embed:end --><!-- api:embed:start -->", "<!-- api:embed:start --><!-- api:embed:start --><!-- api:embed:end -->"} {
		if _, err := Replace(source, "api", "new"); err == nil {
			t.Fatal("ambiguous marker accepted")
		}
	}
}

// TestExamplesDeclareExecutableOutput catches examples that go test would otherwise compile without executing.
func TestExamplesDeclareExecutableOutput(t *testing.T) {
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	examples, err := Examples(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range examples {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "examples", example.Source), nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, executable := range doc.Examples(file) {
			if executable.Name == "_"+example.Name {
				found = true
				if executable.Output == "" && !executable.EmptyOutput {
					t.Errorf("%s has no executable Output: comment", example.Source)
				}
			}
		}
		if !found {
			t.Errorf("%s is not discoverable as a Go example", example.Source)
		}
	}
}

// TestQualifiedAPIIndex preserves receiver and package ownership for identically named APIs.
func TestQualifiedAPIIndex(t *testing.T) {
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	symbols, err := Symbols(root)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, symbol := range symbols {
		if symbol.Group == "" || symbol.Description == "" {
			t.Fatal("undocumented symbol", symbol)
		}
		if found[symbol.Name] {
			t.Fatal("colliding qualified symbol", symbol.Name)
		}
		data, err := os.ReadFile(filepath.Join(root, symbol.Source))
		if err != nil {
			t.Fatal("missing source link", symbol.Name, err)
		}
		lines := strings.Split(string(data), "\n")
		name := symbol.Name[strings.LastIndex(symbol.Name, ".")+1:]
		if symbol.Line < 1 || symbol.Line > len(lines) || !strings.Contains(lines[symbol.Line-1], name) {
			t.Fatal("source link misses declaration", symbol.Name, symbol.Source, symbol.Line)
		}
		found[symbol.Name] = true
	}
	for _, name := range []string{"search.Value.Keyword", "search.Keyword", "blevesearch.Driver.Search", "meilisearch.Driver.Search", "typesensesearch.Driver.Search", "opensearch.Driver.Search", "elasticsearch.Driver.Search", "postgressearch.Driver.Search", "sqlitesearch.Driver.Search", "redissearch.Driver.Search", "searchfake.New"} {
		if !found[name] {
			t.Fatal("missing API", name)
		}
	}
}

// TestExamplesMatchStandalonePrograms catches stale mirrors without rewriting authoritative sources.
func TestExamplesMatchStandalonePrograms(t *testing.T) {
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	examples, err := Examples(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(examples) < 7 {
		t.Fatal("missing executable examples")
	}
	for _, example := range examples {
		if len(example.APIs) == 0 || example.Snippet == "" {
			t.Fatal("missing documented API example", example.Name)
		}
		if strings.Contains(example.Snippet, "snippet:") || strings.Contains(string(example.Program), "snippet:") {
			t.Fatal("snippet markers leaked into generated documentation", example.Name)
		}
		program := strings.ReplaceAll(string(example.Program), "\n\t", "\n")
		if !strings.Contains(program, strings.TrimSpace(example.Snippet)) {
			t.Fatal("API snippet differs from its executable program", example.Name)
		}
		data, err := os.ReadFile(filepath.Join(root, "examples", example.Name, "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, example.Program) {
			t.Fatal("stale example", example.Name)
		}
		if strings.Contains(string(data), "// Output:") {
			t.Fatal("test-only output marker leaked into standalone program")
		}
	}
}

// TestExampleSnippetBoundaries rejects malformed annotations before publishing incomplete API examples.
func TestExampleSnippetBoundaries(t *testing.T) {
	for _, body := range []string{
		"unmarked code",
		"// snippet:start\ncode",
		"// snippet:end\n// snippet:start\ncode",
		"// snippet:start\n\t// snippet:end",
		"// snippet:start\ncode\n// snippet:end\n// snippet:end",
	} {
		if _, err := exampleSnippet(body, true); err == nil {
			t.Fatalf("accepted malformed snippet %q", body)
		}
	}
	if snippet, err := exampleSnippet("unmarked code", false); err != nil || snippet != "" {
		t.Fatal(snippet, err)
	}
	input := "\t// snippet:start\n\tif ok {\n\t\tfmt.Println(1)\n\t}\n\t// snippet:end\n"
	want := "if ok {\n\tfmt.Println(1)\n}\n"
	if snippet, err := exampleSnippet(input, true); err != nil || snippet != want {
		t.Fatal(snippet, err)
	}
}

// TestSourceFingerprintTracksExecutableOwnership excludes generated prose while tracking behavior and pins.
func TestSourceFingerprintTracksExecutableOwnership(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	first, err := SourceFingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("generated prose"), 0644); err != nil {
		t.Fatal(err)
	}
	second, err := SourceFingerprint(root)
	if err != nil || first != second {
		t.Fatal("prose changed behavior fingerprint", err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.go"), []byte("package example\n"), 0644); err != nil {
		t.Fatal(err)
	}
	third, err := SourceFingerprint(root)
	if err != nil || third == second {
		t.Fatal("behavior did not change fingerprint", err)
	}
}
