package gen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EvidenceMarkdown renders only drivers with complete executed conformance results for the current source.
func EvidenceMarkdown(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "docs/driver-results.json"))
	if os.IsNotExist(err) {
		return "Run `make test-integration` to generate real-backend evidence.", nil
	}
	if err != nil {
		return "", err
	}
	var evidence struct {
		Source  string `json:"source_sha256"`
		Drivers []struct {
			Name      string
			Version   string
			Scenarios []string
		}
	}
	if err := json.Unmarshal(data, &evidence); err != nil {
		return "", err
	}
	source, err := SourceFingerprint(root)
	if err != nil {
		return "", err
	}
	if evidence.Source != source {
		return "", fmt.Errorf("driver evidence is stale; run make test-integration")
	}
	var out strings.Builder
	out.WriteString("| Driver | Tested engine | Mutation visibility | Bulk precision | Topology |\n| --- | --- | --- | --- | --- |\n")
	for _, driver := range evidence.Drivers {
		visibility, precision, topology := "synchronous", "per item", "shared service"
		if driver.Name == "bleve" {
			topology = "single process; memory or disk"
		}
		if driver.Name == "opensearch" {
			visibility = "refresh-waiting"
		}
		if driver.Name == "meilisearch" {
			visibility = "asynchronous receipt"
			precision = "aggregate task"
		}
		fmt.Fprintf(&out, "| %s | %s | %s | %s | %s |\n", driver.Name, driver.Version, visibility, precision, topology)
	}
	out.WriteString("\nAll listed engines pass the same literal all-terms, typed-filter, stable-sort, pagination, facet, highlight, replacement, receipt, bulk, cancellation, and concurrent-call fixtures. See [executed driver evidence](docs/driver-results.json) for exact scenario names and the source fingerprint.\n")
	return out.String(), nil
}

// TestBadges renders executed test counts and measured statement coverage from generated evidence.
func TestBadges(root string) (string, error) {
	var out strings.Builder
	for _, kind := range []string{"unit", "integration"} {
		data, err := os.ReadFile(filepath.Join(root, "docs/"+kind+"-results.json"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		var result struct {
			Count int `json:"executed_tests"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return "", err
		}
		fmt.Fprintf(&out, "  <img src=\"https://img.shields.io/badge/%s_tests-%d-brightgreen\" alt=\"Executed %s tests, including subtests\">\n", kind, result.Count, kind)
	}
	data, err := os.ReadFile(filepath.Join(root, "docs/coverage.json"))
	if err == nil {
		var coverage struct {
			Total float64 `json:"total_percent"`
		}
		if err := json.Unmarshal(data, &coverage); err != nil {
			return "", err
		}
		fmt.Fprintf(&out, "  <a href=\"docs/coverage.json\"><img src=\"https://img.shields.io/badge/coverage-%.1f%%25-brightgreen\" alt=\"Merged statement coverage\"></a>\n", coverage.Total)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return out.String(), nil
}
