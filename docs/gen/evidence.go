package gen

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
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
	out.WriteString("| Driver | Tested version | Mutation visibility | Bulk precision | Topology |\n| ---: | --- | --- | --- | --- |\n")
	for _, driver := range evidence.Drivers {
		visibility, precision, topology := "synchronous", "per item", "shared service"
		if driver.Name == "bleve" {
			topology = "single process; memory or disk"
		}
		if driver.Name == "opensearch" || driver.Name == "elasticsearch" {
			visibility = "refresh-waiting"
		}
		if driver.Name == "meilisearch" {
			visibility = "asynchronous receipt"
			precision = "aggregate task"
		}
		if driver.Name == "postgres" {
			topology = "application-owned SQL pool"
		}
		if driver.Name == "sqlite" {
			topology = "embedded SQL; application-owned pool"
		}
		if driver.Name == "redis" {
			topology = "standalone service; at most 4096 matches"
		}
		badge, err := driverBadge(root, driver.Name)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&out, "| %s | %s | %s | %s | %s |\n", badge, driver.Version, visibility, precision, topology)
	}
	out.WriteString("\nAll listed engines pass the same literal all-terms, typed-filter, stable-sort, pagination, facet, highlight, replacement, receipt, bulk, cancellation, and concurrent-call fixtures. See [executed driver evidence](docs/driver-results.json) for exact scenario names and the source fingerprint.\n")
	return out.String(), nil
}

// PackageCoverageBadges renders measured coverage for public packages and drivers, following the sibling mail library.
func PackageCoverageBadges(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "docs/coverage.json"))
	if err != nil {
		return "", err
	}
	var coverage struct {
		Packages map[string]*float64 `json:"packages"`
	}
	if err := json.Unmarshal(data, &coverage); err != nil {
		return "", err
	}
	const module = "github.com/goforj/search"
	var packages []string
	for name, percent := range coverage.Packages {
		if name != module && (!strings.HasPrefix(name, module+"/") || strings.HasPrefix(name, module+"/internal/")) {
			continue
		}
		if percent == nil || *percent < 0 || *percent > 100 {
			return "", fmt.Errorf("invalid coverage for %s", name)
		}
		packages = append(packages, name)
	}
	if len(packages) == 0 {
		return "", fmt.Errorf("coverage evidence contains no public search packages")
	}
	sort.Strings(packages)
	var out strings.Builder
	for _, name := range packages {
		label := filepath.Base(name)
		path, err := writeBadge(root, "coverage-"+label, label, fmt.Sprintf("%.1f%%", *coverage.Packages[name]))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&out, "  <a href=\"docs/coverage.json\"><img src=\"%s\" alt=\"%s statement coverage\"></a>\n", path, label)
	}
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
		path, err := writeBadge(root, kind+"-tests", kind+" tests", fmt.Sprint(result.Count))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&out, "  <img src=\"%s\" alt=\"Executed %s tests, including subtests\">\n", path, kind)
	}
	data, err := os.ReadFile(filepath.Join(root, "docs/coverage.json"))
	if err == nil {
		var coverage struct {
			Total float64 `json:"total_percent"`
		}
		if err := json.Unmarshal(data, &coverage); err != nil {
			return "", err
		}
		path, err := writeBadge(root, "coverage", "coverage", fmt.Sprintf("%.1f%%", coverage.Total))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&out, "  <a href=\"docs/coverage.json\"><img src=\"%s\" alt=\"Merged statement coverage\"></a>\n", path)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return out.String(), nil
}

// writeBadge keeps measured evidence readable without an external badge service or image proxy cache.
func writeBadge(root, name, label, value string) (string, error) {
	left, right := len(label)*7+14, len(value)*7+14
	path := "docs/assets/badges/" + name + ".svg"
	data := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="%s: %s">
  <title>%s: %s</title>
  <rect width="%d" height="20" rx="3" fill="#555"/>
  <path fill="#4c1" d="M%d 0h%dv20H%dz"/>
  <g fill="#fff" text-anchor="middle" font-family="Verdana,DejaVu Sans,sans-serif" font-size="11">
    <text x="%d" y="14">%s</text>
    <text x="%d" y="14">%s</text>
  </g>
</svg>
`, left+right, html.EscapeString(label), html.EscapeString(value), html.EscapeString(label), html.EscapeString(value), left+right, left, right, left, left/2, html.EscapeString(label), left+right/2, html.EscapeString(value))
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
		return "", err
	}
	return path, os.WriteFile(absolute, []byte(data), 0644)
}
