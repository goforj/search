// Package bench renders recorded real-driver measurements in the GoForj benchmark style.
package bench

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/goforj/search/docs/gen"
)

// Report is the persisted search-performance-v1 interchange record emitted by integration/performance_test.go.
type Report struct {
	HarnessSHA256 string `json:"harness_sha256"`
	Schema        string `json:"schema"`
	Driver        string `json:"driver"`
	Version       struct {
		Version string `json:"version"`
		Image   string `json:"image"`
	} `json:"version"`
	Dataset     string  `json:"dataset"`
	Documents   int     `json:"documents"`
	Loaded      int     `json:"loaded"`
	SHA256      string  `json:"sha256"`
	BodyBytes   int64   `json:"body_bytes"`
	Batch       int     `json:"batch"`
	Warmup      int     `json:"warmup"`
	Samples     int     `json:"samples"`
	Started     string  `json:"started"`
	Revision    string  `json:"revision"`
	Go          string  `json:"go"`
	Platform    string  `json:"platform"`
	CPUs        int     `json:"cpus"`
	Machine     string  `json:"machine"`
	Docker      string  `json:"docker"`
	Settings    string  `json:"settings"`
	LoadSeconds float64 `json:"load_seconds"`
	Complete    bool    `json:"complete"`
	Failure     string  `json:"failure,omitempty"`
	Queries     []Query `json:"queries"`
}

// Query preserves the individual timed searches and the backend's total-count precision.
type Query struct {
	Name          string  `json:"name"`
	NS            []int64 `json:"ns"`
	TotalRelation string  `json:"total_relation"`
}

// driverOrder keeps missing or failed engines visible in the same order as the driver matrix.
var driverOrder = []string{"bleve", "meilisearch", "typesense", "opensearch", "elasticsearch", "postgres", "sqlite", "redis"}

// driverNames keeps chart labels readable without relying on externally loaded icons or fonts.
var driverNames = map[string]string{"bleve": "Bleve", "meilisearch": "Meilisearch", "typesense": "Typesense", "opensearch": "OpenSearch", "elasticsearch": "Elasticsearch", "postgres": "PostgreSQL FTS", "sqlite": "SQLite FTS5", "redis": "Redis Search"}

// Import validates a complete driver inventory before replacing the checked-in measurement snapshot.
func Import(root, directory string) error {
	var reports []Report
	for _, driver := range driverOrder {
		data, err := os.ReadFile(filepath.Join(directory, driver+".json"))
		if err != nil {
			return err
		}
		var report Report
		if err := json.Unmarshal(data, &report); err != nil {
			return err
		}
		if report.Driver != driver {
			return fmt.Errorf("%s: driver identity mismatch", driver)
		}
		reports = append(reports, report)
	}
	if err := Validate(reports); err != nil {
		return err
	}
	data, err := json.MarshalIndent(reports, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(root, "docs", "bench", "benchmarks_rows.json")
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return err
	}
	return Render(root)
}

// Validate rejects mixed datasets, environments, harnesses, and incomplete successful records.
func Validate(reports []Report) error {
	if len(reports) != len(driverOrder) {
		return fmt.Errorf("expected all %d drivers", len(driverOrder))
	}
	baseline := reports[0]
	var corpus string
	var bodyBytes int64
	for n, report := range reports {
		if report.Driver != driverOrder[n] || report.Schema != "search-performance-v1" || report.Dataset != "catalog-v1" ||
			report.Documents < 1024 || report.Batch != 1000 || report.Samples < 10 || report.Warmup < 1 ||
			!validDigest(report.HarnessSHA256) || report.Version.Version == "" || report.Machine == "" ||
			report.Loaded < 0 || report.Loaded > report.Documents {
			return fmt.Errorf("%s: invalid benchmark identity or workload", report.Driver)
		}
		if report.Documents != baseline.Documents || report.Samples != baseline.Samples || report.Warmup != baseline.Warmup ||
			report.HarnessSHA256 != baseline.HarnessSHA256 || report.Platform != baseline.Platform || report.Machine != baseline.Machine ||
			report.Go != baseline.Go || report.Docker != baseline.Docker ||
			report.CPUs != baseline.CPUs || report.Settings != baseline.Settings {
			return fmt.Errorf("%s: incomparable benchmark conditions", report.Driver)
		}
		if !report.Complete {
			if report.Failure == "" {
				return fmt.Errorf("%s: unfinished run is not a measured failure", report.Driver)
			}
			continue
		}
		if report.Failure != "" || report.Loaded != report.Documents || report.LoadSeconds <= 0 ||
			report.BodyBytes <= 0 || !validDigest(report.SHA256) || len(report.Queries) != 3 {
			return fmt.Errorf("%s: incomplete benchmark evidence", report.Driver)
		}
		if corpus == "" {
			corpus = report.SHA256
			bodyBytes = report.BodyBytes
		}
		if report.SHA256 != corpus || report.BodyBytes != bodyBytes {
			return fmt.Errorf("%s: corpus fingerprint differs", report.Driver)
		}
		for i, name := range []string{"literal", "filter_sort", "facets"} {
			query := report.Queries[i]
			if query.Name != name || len(query.NS) != report.Samples ||
				(query.TotalRelation != "exact" && query.TotalRelation != "estimated" && query.TotalRelation != "lower_bound") {
				return fmt.Errorf("%s: incomplete %s samples", report.Driver, name)
			}
			for _, ns := range query.NS {
				if ns <= 0 {
					return fmt.Errorf("%s: invalid latency sample", report.Driver)
				}
			}
		}
	}
	if corpus == "" {
		return fmt.Errorf("no successful driver measurements")
	}
	return nil
}

// validDigest requires an actual SHA-256 encoding rather than a same-length placeholder.
func validDigest(value string) bool {
	_, err := hex.DecodeString(value)
	return len(value) == 64 && err == nil
}

// Percentile uses the nearest-rank definition and leaves the original measurement order intact.
func Percentile(samples []int64, percentile float64) float64 {
	if len(samples) == 0 || percentile <= 0 || percentile > 1 {
		return 0
	}
	ordered := slices.Clone(samples)
	slices.Sort(ordered)
	return float64(ordered[int(math.Ceil(float64(len(ordered))*percentile))-1]) / 1e6
}

// Render regenerates graphs and README content from the snapshot without executing a benchmark.
func Render(root string) error {
	data, err := os.ReadFile(filepath.Join(root, "docs", "bench", "benchmarks_rows.json"))
	if os.IsNotExist(err) {
		return fmt.Errorf("benchmark snapshot missing: restore docs/bench/benchmarks_rows.json or import a measured run")
	}
	if err != nil {
		return err
	}
	var reports []Report
	if err := json.Unmarshal(data, &reports); err != nil {
		return err
	}
	if err := Validate(reports); err != nil {
		return err
	}
	for i, name := range []string{"Literal search", "Filter and sort", "Text and facets", "Indexing throughput"} {
		if err := os.WriteFile(filepath.Join(root, "docs", "bench", chartFile(i)), chart(reports, i, name), 0644); err != nil {
			return err
		}
	}
	readmePath := filepath.Join(root, "README.md")
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		return err
	}
	result, err := gen.Replace(string(readme), "bench", markdown(reports))
	if err != nil {
		return err
	}
	return os.WriteFile(readmePath, []byte(result), 0644)
}

// chartFile binds the README links and generated filenames to one stable list.
func chartFile(metric int) string {
	return []string{"benchmarks_literal.svg", "benchmarks_sort.svg", "benchmarks_facets.svg", "benchmarks_ingest.svg"}[metric]
}

// chart follows the sibling queue and cache palette, typography, bar geometry, and embedded SVG format.
func chart(reports []Report, metric int, title string) []byte {
	const width, left, right, top, row, gap, bottom = 1200, 230, 150, 120, 34, 16, 50
	height := top + len(reports)*(row+gap) + bottom
	plot := width - left - right
	values := make([]float64, len(reports))
	maximum := 1.0
	for n, report := range reports {
		if !report.Complete {
			continue
		}
		if metric == 3 {
			values[n] = float64(report.Documents) / report.LoadSeconds
		} else {
			values[n] = Percentile(report.Queries[metric].NS, .95)
		}
		maximum = max(maximum, values[n])
	}
	maximum *= 1.15
	lower, steps, scale := 0.0, 5, "linear scale"
	unit, direction := "p95 latency (ms, log scale)", "lower is better"
	if metric == 3 {
		unit, direction = "documents per second", "higher is better"
	} else {
		lower, maximum = latencyBounds(reports)
		steps = int(math.Round(math.Log10(maximum / lower)))
		scale = "logarithmic scale, each decade is 10x"
	}
	var svg bytes.Buffer
	fmt.Fprintf(&svg, `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-labelledby="title description">
<title id="title">%s</title>
<desc id="description">%s; %d documents; %s; single client; %s. Orange: embedded. Gold: service.</desc>
<rect width="100%%" height="100%%" rx="8" fill="#1A1620"/>
<text x="600" y="38" text-anchor="middle" fill="#FFFFFF" font-size="26" font-weight="700" letter-spacing="-0.8" font-family="Space Grotesk, Inter, system-ui, sans-serif">%s</text>
<text x="600" y="66" text-anchor="middle" fill="#A9A1B3" font-size="16" font-family="Inter, system-ui, sans-serif">%s | %s | %s documents</text>
`, width, height, width, height, html.EscapeString(title), html.EscapeString(unit), reports[0].Documents, direction, scale, html.EscapeString(title), html.EscapeString(unit), direction, documentCount(reports[0].Documents))
	for n := 0; n <= steps; n++ {
		x := left + n*plot/steps
		value := maximum * float64(n) / float64(steps)
		if metric < 3 {
			value = lower * math.Pow(10, float64(n))
		}
		fmt.Fprintf(&svg, `<line x1="%d" y1="110" x2="%d" y2="%d" stroke="#2A2333"/><text x="%d" y="98" text-anchor="middle" fill="#A9A1B3" font-size="13" font-family="JetBrains Mono, ui-monospace, monospace">%s</text>
`, x, x, height-bottom, x, number(value))
	}
	for n, report := range reports {
		y := top + n*(row+gap)
		position := values[n] / maximum
		if metric < 3 && values[n] > 0 {
			position = math.Log10(values[n]/lower) / math.Log10(maximum/lower)
		}
		barWidth := int(position * float64(plot))
		if values[n] > 0 {
			barWidth = max(1, barWidth)
		}
		color := "#FFC24D"
		if report.Driver == "bleve" || report.Driver == "sqlite" {
			color = "#FF5E3A"
		}
		label := number(values[n])
		if metric < 3 {
			label = fmt.Sprintf("%.3f", values[n])
		}
		if !report.Complete {
			label = "failed"
		}
		fmt.Fprintf(&svg, `<text x="%d" y="%d" text-anchor="end" fill="#A9A1B3" font-size="15" font-family="Inter, system-ui, sans-serif">%s</text>
<rect x="%d" y="%d" width="%d" height="34" rx="6" fill="#2C2734" stroke="#3D3349"/>
<rect x="%d" y="%d" width="%d" height="34" rx="6" fill="%s"/>
<text x="%d" y="%d" fill="#FFFFFF" font-size="14" font-family="JetBrains Mono, ui-monospace, monospace">%s</text>
`, left-14, y+22, driverNames[report.Driver], left, y, plot, left, y, barWidth, color, left+barWidth+9, y+22, label)
	}
	fmt.Fprintf(&svg, `<rect x="230" y="%d" width="12" height="12" rx="3" fill="#FF5E3A"/><text x="248" y="%d" fill="#A9A1B3" font-size="12" font-family="Inter, system-ui, sans-serif">Embedded</text><rect x="340" y="%d" width="12" height="12" rx="3" fill="#FFC24D"/><text x="358" y="%d" fill="#A9A1B3" font-size="12" font-family="Inter, system-ui, sans-serif">Service</text>
</svg>
`, height-27, height-16, height-27, height-16)
	return svg.Bytes()
}

// documentCount groups large corpus sizes for readable chart subtitles and README prose.
func documentCount(count int) string {
	value := strconv.Itoa(count)
	for position := len(value) - 3; position > 0; position -= 3 {
		value = value[:position] + "," + value[position:]
	}
	return value
}

// latencyBounds gives all three latency charts the same decade ticks so their bars remain comparable.
func latencyBounds(reports []Report) (float64, float64) {
	minimum, maximum := math.MaxFloat64, 0.0
	for _, report := range reports {
		if !report.Complete {
			continue
		}
		for _, query := range report.Queries {
			value := Percentile(query.NS, .95)
			minimum = min(minimum, value)
			maximum = max(maximum, value)
		}
	}
	return math.Pow(10, math.Floor(math.Log10(minimum))-1), math.Pow(10, math.Ceil(math.Log10(maximum)))
}

// number keeps graph labels legible across latency and throughput units without hiding small measurements.
func number(value float64) string {
	if value >= 1000 {
		return fmt.Sprintf("%.0f", value)
	}
	if value >= 10 {
		return fmt.Sprintf("%.1f", value)
	}
	return fmt.Sprintf("%.3f", value)
}

// markdown links measurements to their dataset, methodology, and complete raw samples.
func markdown(reports []Report) string {
	var out strings.Builder
	fmt.Fprintf(&out, "## Performance\n\nThe same **%s-document synthetic catalog** and query sequence across all eight drivers, using the public `search.Index` API. Latency charts show client-observed p95 from %d searches after %d warm-up requests per workload. Latency axes are logarithmic; indexing throughput is linear.\n\n", documentCount(reports[0].Documents), reports[0].Samples, reports[0].Warmup)
	for i, name := range []string{"Literal search", "Filter and sort", "Text and facets", "Indexing throughput"} {
		fmt.Fprintf(&out, "### %s\n\n![%s benchmark](docs/bench/%s)\n\n", name, name, chartFile(i))
	}
	out.WriteString("| Driver | Load (s) | Literal p50 / p95 (ms) | Filter + sort p50 / p95 (ms) | Facets p50 / p95 (ms) |\n| ---: | ---: | ---: | ---: | ---: |\n")
	for _, report := range reports {
		fmt.Fprintf(&out, "| <img src=\"docs/assets/badges/driver-%s.svg\" alt=\"%s\" height=\"22\"> |", report.Driver, driverNames[report.Driver])
		if !report.Complete {
			fmt.Fprintf(&out, " Failed at %d documents | Failed | Failed | Failed |\n", report.Loaded)
			continue
		}
		fmt.Fprintf(&out, " %.1f |", report.LoadSeconds)
		for _, query := range report.Queries {
			fmt.Fprintf(&out, " %.3f / %.3f |", Percentile(query.NS, .5), Percentile(query.NS, .95))
		}
		out.WriteString("\n")
	}
	out.WriteString("\nThe SQL drivers currently have query-plan limitations that dominate several results. See the [SQL benchmark audit](docs/bench/sql-audit.md) for reproduced timings, captured plans, and controlled comparisons.\n")
	fmt.Fprintf(&out, "\nMeasured on %s. Embedded engines run on the host; service engines run in Docker. These are warmed, single-client, selective searches returning at most 1,024 matches and 20 hits, without concurrent writes. Indexing includes document generation, validation, and waiting for visibility in batches of 1,000.\n\n", reports[0].Machine)
	out.WriteString("See [methodology and reproduction](docs/bench/README.md) for workload details, pinned versions, persistence settings, and limitations. The [measurement snapshot](docs/bench/benchmarks_rows.json) contains corpus fingerprints, environment metadata, count precision, and every latency sample. Run `make bench-drivers` to measure or `make bench-render` to redraw the recorded results.\n")
	return out.String()
}
