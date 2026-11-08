package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCoverageLanesRemainIndependent prevents integration coverage from masking unit gaps.
func TestCoverageLanesRemainIndependent(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	unit, integration := filepath.Join(root, "unit.cover"), filepath.Join(root, "integration.cover")
	for path, content := range map[string]string{
		unit:        "mode: atomic\ngithub.com/goforj/search/index.go:1.1,2.1 1 1\ngithub.com/goforj/search/index.go:3.1,4.1 1 0\n",
		integration: "mode: atomic\ngithub.com/goforj/search/index.go:1.1,2.1 1 0\ngithub.com/goforj/search/index.go:3.1,4.1 1 1\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := recordCoverage(root, []string{unit, integration}, false); err != nil {
		t.Fatal(err)
	}
	combinedPath := filepath.Join(root, "docs/coverage.json")
	combined, err := os.ReadFile(combinedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := recordCoverage(root, []string{unit}, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".artifacts/unit-coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report coverageReport
	if err := json.Unmarshal(data, &report); err != nil || report.Total != 50 {
		t.Fatal(report, err)
	}
	profile, err := os.ReadFile(filepath.Join(root, ".artifacts/unit-coverage.out"))
	if err != nil || !strings.Contains(string(profile), "index.go:3.1,4.1 1 0") {
		t.Fatal(string(profile), err)
	}
	after, err := os.ReadFile(combinedPath)
	if err != nil || string(after) != string(combined) {
		t.Fatal("unit coverage replaced combined evidence", err)
	}
	if err := json.Unmarshal(after, &report); err != nil || report.Total != 100 {
		t.Fatal(report, err)
	}
}
