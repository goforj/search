// Command report generates test counts, driver evidence, and merged package coverage from executed tests.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/goforj/search/docs/gen"
	"github.com/goforj/search/searchtest"
)

// testEvent is the stable subset of go test -json consumed by documentation generation.
type testEvent struct {
	Action  string
	Package string
	Test    string
	Output  string
}

// testReport records executed tests without machine paths or nondeterministic elapsed times.
type testReport struct {
	Count int      `json:"executed_tests"`
	Tests []string `json:"tests"`
}

// driverEvidence requires every shared scenario to pass on the exact advertised engine.
type driverEvidence struct {
	Source  string         `json:"source_sha256"`
	Drivers []driverResult `json:"drivers"`
}

// driverResult retains one exact backend version and its successful scenario inventory.
type driverResult struct {
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Scenarios []string `json:"scenarios"`
}

// coverageReport retains measured statement coverage rather than guessed badge values.
type coverageReport struct {
	Total    float64            `json:"total_percent"`
	Packages map[string]float64 `json:"packages"`
}

// coverageBlock merges executions of the same source range across modules and test lanes.
type coverageBlock struct {
	statements int
	count      uint64
}

// main requires executed evidence before publishing support or coverage claims.
func main() {
	show := flag.String("showevents", "", "render go test output for a failed run")
	events := flag.String("events", "", "go test -json output")
	kind := flag.String("kind", "unit", "unit or integration")
	profiles := flag.String("coverage", "", "comma-separated coverage profiles")
	flag.Parse()
	if *show != "" {
		if err := showEvents(*show); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	root, err := gen.Root()
	if err == nil && *events != "" {
		err = recordTests(root, *events, *kind)
	}
	if err == nil && *profiles != "" {
		err = recordCoverage(root, strings.Split(*profiles, ","))
	}
	if err == nil && *events == "" && *profiles == "" {
		err = fmt.Errorf("provide -events or -coverage")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// readEvents rejects failed or skipped tests so stale or incomplete evidence cannot advertise a driver.
func readEvents(path string) (testReport, error) {
	file, err := os.Open(path)
	if err != nil {
		return testReport{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	unique := map[string]bool{}
	for decoder.More() {
		var event testEvent
		if err := decoder.Decode(&event); err != nil {
			return testReport{}, err
		}
		if event.Action == "fail" || event.Action == "skip" && event.Test != "" {
			return testReport{}, fmt.Errorf("incomplete test evidence: %s %s", event.Package, event.Test)
		}
		if event.Action == "pass" && event.Test != "" {
			unique[event.Package+"/"+event.Test] = true
		}
	}
	result := testReport{}
	for test := range unique {
		result.Tests = append(result.Tests, test)
	}
	sort.Strings(result.Tests)
	result.Count = len(result.Tests)
	if result.Count == 0 {
		return result, fmt.Errorf("test output contains no executed tests")
	}
	return result, nil
}

// recordTests writes only complete actual runs and checks every required shared scenario.
func recordTests(root, path, kind string) error {
	report, err := readEvents(path)
	if err != nil {
		return err
	}
	if kind != "unit" && kind != "integration" {
		return fmt.Errorf("unknown report kind %q", kind)
	}
	if kind == "integration" {
		source, err := gen.SourceFingerprint(root)
		if err != nil {
			return err
		}
		evidence := driverEvidence{Source: source}
		for _, engine := range []struct{ name, suite, version string }{
			{"bleve", "TestBleveContract", bleveVersion(root)},
			{"meilisearch", "TestMeilisearchContract", "v1.17.0"},
			{"typesense", "TestTypesenseContract", "30.0"},
		} {
			row := driverResult{Name: engine.name, Version: engine.version}
			for _, scenario := range searchtest.ScenarioNames() {
				found := false
				for _, executed := range report.Tests {
					if strings.HasSuffix(executed, "/"+engine.suite+"/"+scenario) {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("%s is missing executed scenario %s", engine.name, scenario)
				}
				row.Scenarios = append(row.Scenarios, scenario)
			}
			evidence.Drivers = append(evidence.Drivers, row)
		}
		if err := writeJSON(filepath.Join(root, "docs/driver-results.json"), evidence); err != nil {
			return err
		}
	}
	if err := writeJSON(filepath.Join(root, "docs/"+kind+"-results.json"), report); err != nil {
		return err
	}
	fmt.Printf("%s: %d executed tests, including subtests\n", kind, report.Count)
	return nil
}

// bleveVersion reads the exact dependency used in the isolated driver module.
func bleveVersion(root string) string {
	data, _ := os.ReadFile(filepath.Join(root, "driver/blevesearch/go.mod"))
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "github.com/blevesearch/bleve/v2" {
			return fields[1]
		}
	}
	return "unknown"
}

// recordCoverage combines block counts across root, examples, and real backend executions.
func recordCoverage(root string, profiles []string) error {
	blocks := map[string]coverageBlock{}
	for _, path := range profiles {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) != 3 || !strings.HasPrefix(fields[0], "github.com/goforj/search/") {
				continue
			}
			if strings.Contains(fields[0], "/docs/") || strings.Contains(fields[0], "/examples/") || strings.Contains(fields[0], "/integration/") {
				continue
			}
			statements, err := strconv.Atoi(fields[1])
			if err != nil {
				file.Close()
				return err
			}
			count, err := strconv.ParseUint(fields[2], 10, 64)
			if err != nil {
				file.Close()
				return err
			}
			block := blocks[fields[0]]
			block.statements = statements
			block.count += count
			blocks[fields[0]] = block
		}
		err = scanner.Err()
		file.Close()
		if err != nil {
			return err
		}
	}
	all, covered := 0, 0
	packageTotals, packageCovered := map[string]int{}, map[string]int{}
	for key, block := range blocks {
		path, _, _ := strings.Cut(key, ":")
		pkg := filepath.ToSlash(filepath.Dir(path))
		all += block.statements
		packageTotals[pkg] += block.statements
		if block.count > 0 {
			covered += block.statements
			packageCovered[pkg] += block.statements
		}
	}
	if all == 0 {
		return fmt.Errorf("coverage profiles contain no library statements")
	}
	report := coverageReport{Total: float64(covered) * 100 / float64(all), Packages: map[string]float64{}}
	for pkg, total := range packageTotals {
		report.Packages[pkg] = float64(packageCovered[pkg]) * 100 / float64(total)
	}
	keys := make([]string, 0, len(blocks))
	for key := range blocks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var merged strings.Builder
	merged.WriteString("mode: atomic\n")
	for _, key := range keys {
		block := blocks[key]
		fmt.Fprintf(&merged, "%s %d %d\n", key, block.statements, block.count)
	}
	if err := os.MkdirAll(filepath.Join(root, ".artifacts"), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, ".artifacts/coverage.out"), []byte(merged.String()), 0644); err != nil {
		return err
	}
	return writeJSON(filepath.Join(root, "docs/coverage.json"), report)
}

// writeJSON produces deterministic machine-readable evidence without run-specific timestamps.
func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

// showEvents renders the human-readable output retained in go test JSON artifacts.
func showEvents(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	for decoder.More() {
		var event testEvent
		if err := decoder.Decode(&event); err != nil {
			return err
		}
		fmt.Print(event.Output)
	}
	return nil
}
