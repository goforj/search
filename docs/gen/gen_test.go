package gen

import (
	"bytes"
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

// TestExamplesDeclareExecutableOutput ensures the runner can verify each canonical program.
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
		outputs := 0
		for _, group := range file.Comments {
			if _, ok := strings.CutPrefix(group.Text(), "Output:\n"); ok {
				outputs++
			}
		}
		if outputs != 1 {
			t.Errorf("%s must have exactly one executable Output: comment", example.Source)
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

// TestExamplesReadCanonicalPrograms keeps generated snippets tied to runnable source.
func TestExamplesReadCanonicalPrograms(t *testing.T) {
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
		if strings.Contains(example.Snippet, "snippet:") {
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

// TestExamplesRejectInvalidSources prevents malformed canonical programs from becoming documentation.
func TestExamplesRejectInvalidSources(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"syntax", "package main\nfunc main( {", "expected"},
		{"documentation", "package main\nfunc main() {}", "missing main documentation"},
		{"snippet", "package main\n// main demonstrates indexing.\n// @api search.NewIndex\nfunc main() {}", "expected one snippet marker pair"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "examples", "sample")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(tc.source), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := Examples(root); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}
