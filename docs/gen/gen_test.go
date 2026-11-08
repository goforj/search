package gen

import (
	"bytes"
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
		found[symbol.Name] = true
	}
	for _, name := range []string{"search.Value.Keyword", "search.Keyword", "blevesearch.Driver.Search", "meilisearch.Driver.Search", "typesensesearch.Driver.Search", "searchfake.New"} {
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
