package bench

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// testReports constructs a complete transport fixture without passing invented measurements to production generation.
func testReports() []Report {
	reports := make([]Report, len(driverOrder))
	for n, driver := range driverOrder {
		reports[n] = Report{Schema: "search-performance-v1", Driver: driver, Dataset: "catalog-v1", Documents: 1_000_000, Loaded: 1_000_000, SHA256: strings.Repeat("a", 64), HarnessSHA256: strings.Repeat("b", 64), BodyBytes: 123_000_000, Batch: 1000, Warmup: 30, Samples: 20, Machine: "test fixture", Go: "test", Platform: "test/test", CPUs: 4, LoadSeconds: 20, Complete: true}
		reports[n].Version.Version = "test"
		for _, name := range []string{"literal", "filter_sort", "facets"} {
			query := Query{Name: name, TotalRelation: "exact"}
			for sample := 1; sample <= 20; sample++ {
				query.NS = append(query.NS, int64(sample)*1_000_000)
			}
			reports[n].Queries = append(reports[n].Queries, query)
		}
	}
	return reports
}

// TestPercentile checks nearest-rank boundary behavior without mutating raw sample order.
func TestPercentile(t *testing.T) {
	input := []int64{10_000_000, 1_000_000, 3_000_000, 2_000_000}
	before := slices.Clone(input)
	for _, test := range []struct{ percentile, want float64 }{{.5, 2}, {.95, 10}, {1, 10}, {0, 0}, {1.1, 0}} {
		if got := Percentile(input, test.percentile); got != test.want {
			t.Errorf("percentile %v: got %v, want %v", test.percentile, got, test.want)
		}
	}
	if !slices.Equal(input, before) || Percentile(nil, .95) != 0 {
		t.Fatal("raw samples changed or empty input accepted")
	}
}

// TestValidate rejects comparisons whose apparent differences come from changed input or missing evidence.
func TestValidate(t *testing.T) {
	if err := Validate(testReports()); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func([]Report){
		"driver":          func(r []Report) { r[1].Driver = r[0].Driver },
		"version":         func(r []Report) { r[1].Version.Version = "" },
		"invalid digest":  func(r []Report) { r[1].SHA256 = strings.Repeat("z", 64) },
		"short digest":    func(r []Report) { r[1].SHA256 = "abc" },
		"missing machine": func(r []Report) { r[1].Machine = "" },
		"no warmup":       func(r []Report) { r[1].Warmup = 0 },
		"few samples":     func(r []Report) { r[1].Samples = 1 },
		"negative load":   func(r []Report) { r[1].Loaded = -1 },
		"excess load":     func(r []Report) { r[1].Loaded = r[1].Documents + 1 },
		"schema":          func(r []Report) { r[1].Schema = "future" },
		"dataset":         func(r []Report) { r[1].Dataset = "other" },
		"count":           func(r []Report) { r[1].Documents++ },
		"hash":            func(r []Report) { r[1].SHA256 = strings.Repeat("c", 64) },
		"harness":         func(r []Report) { r[1].HarnessSHA256 = strings.Repeat("c", 64) },
		"machine":         func(r []Report) { r[1].Machine = "other" },
		"platform":        func(r []Report) { r[1].Platform = "other" },
		"go":              func(r []Report) { r[1].Go = "other" },
		"docker":          func(r []Report) { r[1].Docker = "other" },
		"cpus":            func(r []Report) { r[1].CPUs++ },
		"settings":        func(r []Report) { r[1].Settings = "other" },
		"warmup":          func(r []Report) { r[1].Warmup++ },
		"samples":         func(r []Report) { r[1].Samples++ },
		"batch":           func(r []Report) { r[1].Batch++ },
		"partial":         func(r []Report) { r[1].Loaded-- },
		"failure":         func(r []Report) { r[1].Failure = "failed" },
		"interrupted":     func(r []Report) { r[1].Complete = false },
		"load":            func(r []Report) { r[1].LoadSeconds = 0 },
		"bytes":           func(r []Report) { r[1].BodyBytes = 0 },
		"different bytes": func(r []Report) { r[1].BodyBytes++ },
		"queries":         func(r []Report) { r[1].Queries = r[1].Queries[:2] },
		"query identity":  func(r []Report) { r[1].Queries[1].Name = "other" },
		"precision":       func(r []Report) { r[1].Queries[1].TotalRelation = "" },
		"missing samples": func(r []Report) { r[1].Queries[1].NS = nil },
		"latency":         func(r []Report) { r[1].Queries[1].NS[2] = 0 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			reports := testReports()
			change(reports)
			if Validate(reports) == nil {
				t.Fatal("incomparable data accepted")
			}
		})
	}
	if Validate(testReports()[:7]) == nil {
		t.Fatal("missing driver accepted")
	}
	reports := testReports()
	reports[1].Complete = false
	reports[1].Failure = "load failed"
	reports[1].Loaded = 1000
	reports[1].Queries = nil
	if err := Validate(reports); err != nil {
		t.Fatal("honest failed driver rejected", err)
	}
	for n := range reports {
		reports[n].Complete = false
		reports[n].Failure = "failed"
	}
	if Validate(reports) == nil {
		t.Fatal("entirely failed run accepted")
	}
}

// TestRenderSnapshot proves charts contain measured values, valid XML, visible failures, and deterministic output.
func TestRenderSnapshot(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "docs", "bench")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("before\n<!-- bench:embed:start -->\nold\n<!-- bench:embed:end -->\nafter"), 0644); err != nil {
		t.Fatal(err)
	}
	input := t.TempDir()
	reports := testReports()
	reports[7].Complete = false
	reports[7].Failure = "resource limit"
	reports[7].Loaded = 8000
	reports[7].Queries = nil
	for _, report := range reports {
		data, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(input, report.Driver+".json"), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := Import(root, input); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(before, []byte("Failed at 8000 documents")) || !bytes.Contains(before, []byte("10.000 / 19.000")) || !bytes.HasSuffix(before, []byte("after")) {
		t.Fatal("README omitted measured evidence or damaged surrounding content")
	}
	for n := 0; n < 4; n++ {
		data, err := os.ReadFile(filepath.Join(directory, chartFile(n)))
		if err != nil {
			t.Fatal(err)
		}
		var document any
		if err := xml.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte("#1A1620")) || !bytes.Contains(data, []byte(">failed</text>")) {
			t.Fatal("chart styling or failed row missing")
		}
		if n < 3 && !bytes.Contains(data, []byte(">19.000</text>")) {
			t.Fatal("measured p95 missing")
		}
		if n < 3 && !bytes.Contains(data, []byte("logarithmic scale, each decade is 10x")) {
			t.Fatal("logarithmic latency axis is not labeled")
		}
		if n == 3 && !bytes.Contains(data, []byte("linear scale")) {
			t.Fatal("throughput axis should remain linear")
		}
		if err := Render(root); err != nil {
			t.Fatal(err)
		}
		again, err := os.ReadFile(filepath.Join(directory, chartFile(n)))
		if err != nil || !bytes.Equal(data, again) {
			t.Fatal("chart generation is not deterministic", err)
		}
	}
	after, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("README generation is not deterministic", err)
	}
	if err := Render(t.TempDir()); err == nil {
		t.Fatal("missing source measurements should fail generation")
	}
	if err := os.WriteFile(filepath.Join(input, "bleve.json"), []byte(fmt.Sprintf(`{"driver":%q}`, "wrong")), 0644); err != nil {
		t.Fatal(err)
	}
	if Import(root, input) == nil {
		t.Fatal("mislabeled driver report accepted")
	}
}
