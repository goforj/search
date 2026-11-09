package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackageCoverageBadges preserves measured percentages and excludes implementation-only packages.
func TestPackageCoverageBadges(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	evidence := `{"packages":{"github.com/goforj/search/searchfake":97.25,"github.com/goforj/search/internal/driverutil":12,"unrelated/package":3,"github.com/goforj/search/driver/blevesearch":84.5,"github.com/goforj/search":100}}`
	if err := os.WriteFile(filepath.Join(root, "docs/coverage.json"), []byte(evidence), 0644); err != nil {
		t.Fatal(err)
	}
	markup, err := PackageCoverageBadges(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(markup, "<img ") != 3 || strings.Contains(markup, "driverutil") || strings.Contains(markup, "unrelated") {
		t.Fatal("incorrect public package inventory", markup)
	}
	for name, want := range map[string]string{"search": "100.0%", "blevesearch": "84.5%", "searchfake": "97.2%"} {
		data, err := os.ReadFile(filepath.Join(root, "docs/assets/badges/coverage-"+name+".svg"))
		if err != nil || !strings.Contains(string(data), name+": "+want) {
			t.Fatal("badge differs from recorded coverage", name, string(data), err)
		}
	}
	again, err := PackageCoverageBadges(root)
	if err != nil || again != markup {
		t.Fatal("coverage generation is not deterministic", err)
	}
}

// TestPackageCoverageBadgesRejectInvalidEvidence prevents incomplete or invalid measurements from becoming public claims.
func TestPackageCoverageBadgesRejectInvalidEvidence(t *testing.T) {
	for name, evidence := range map[string]string{
		"missing":       "",
		"malformed":     "{",
		"empty":         `{"packages":{}}`,
		"unknown":       `{"packages":{"github.com/goforj/search":null}}`,
		"internal only": `{"packages":{"github.com/goforj/search/internal/sqlsearch":100}}`,
		"negative":      `{"packages":{"github.com/goforj/search":-1}}`,
		"over maximum":  `{"packages":{"github.com/goforj/search":101}}`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "docs"), 0755); err != nil {
				t.Fatal(err)
			}
			if evidence != "" {
				if err := os.WriteFile(filepath.Join(root, "docs/coverage.json"), []byte(evidence), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := PackageCoverageBadges(root); err == nil {
				t.Fatal("invalid coverage evidence accepted")
			}
		})
	}
}
