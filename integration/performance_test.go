//go:build integration && performance

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/goforj/search"
	"github.com/goforj/search/driver/blevesearch"
	"github.com/goforj/search/driver/elasticsearch"
	"github.com/goforj/search/driver/meilisearch"
	"github.com/goforj/search/driver/opensearch"
	"github.com/goforj/search/driver/postgressearch"
	"github.com/goforj/search/driver/redissearch"
	"github.com/goforj/search/driver/sqlitesearch"
	"github.com/goforj/search/driver/typesensesearch"
)

// performanceReport preserves provenance and partial progress instead of treating failed loads as measurements.
type performanceReport struct {
	HarnessSHA256 string             `json:"harness_sha256"`
	Schema        string             `json:"schema"`
	Driver        string             `json:"driver"`
	Version       engineVersion      `json:"version"`
	Dataset       string             `json:"dataset"`
	Documents     int                `json:"documents"`
	Loaded        int                `json:"loaded"`
	SHA256        string             `json:"sha256"`
	BodyBytes     int64              `json:"body_bytes"`
	Batch         int                `json:"batch"`
	Warmup        int                `json:"warmup"`
	Samples       int                `json:"samples"`
	Started       string             `json:"started"`
	Revision      string             `json:"revision"`
	Go            string             `json:"go"`
	Platform      string             `json:"platform"`
	CPUs          int                `json:"cpus"`
	Machine       string             `json:"machine"`
	Docker        string             `json:"docker"`
	Settings      string             `json:"settings"`
	LoadSeconds   float64            `json:"load_seconds"`
	Complete      bool               `json:"complete"`
	Failure       string             `json:"failure,omitempty"`
	Queries       []performanceQuery `json:"queries"`
}

// performanceQuery retains individual client-observed latencies for independently reproducible percentiles.
type performanceQuery struct {
	Name          string               `json:"name"`
	NS            []int64              `json:"ns"`
	TotalRelation search.TotalRelation `json:"total_relation"`
}

// TestDriverPerformance streams an identical corpus through the public API into one real engine per process.
func TestDriverPerformance(t *testing.T) {
	engine := os.Getenv("SEARCH_BENCH_DRIVER")
	count := performanceInt(t, "SEARCH_BENCH_DOCUMENTS", 1_000_000, 1024, 100_000_000)
	samples := performanceInt(t, "SEARCH_BENCH_SAMPLES", 200, 10, 100_000)
	warmup := performanceInt(t, "SEARCH_BENCH_WARMUP", 30, 1, 10_000)
	output := os.Getenv("SEARCH_BENCH_OUTPUT")
	if output == "" {
		t.Fatal("SEARCH_BENCH_OUTPUT is required")
	}
	var versions map[string]engineVersion
	if err := json.Unmarshal(pinnedVersions, &versions); err != nil {
		t.Fatal(err)
	}
	versions["bleve"] = engineVersion{Version: "v2.6.3"}
	report := performanceReport{Schema: "search-performance-v1", Driver: engine, Version: versions[engine], Dataset: "catalog-v1", Documents: count, Batch: 1000, Samples: samples, Warmup: warmup, Started: time.Now().UTC().Format(time.RFC3339), Revision: performanceCommand("git", "rev-parse", "HEAD"), Go: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH, CPUs: runtime.NumCPU(), Machine: os.Getenv("SEARCH_BENCH_MACHINE"), Docker: performanceCommand("docker", "info", "--format", "{{.OperatingSystem}}; server {{.ServerVersion}}; {{.NCPU}} CPUs; {{.MemTotal}} bytes RAM"), Settings: "single client; one engine at a time; default integration fixture; SQLite WAL; Bleve disk; Elasticsearch/OpenSearch 1 shard, 0 replicas, 100ms refresh, 512MiB JVM; Redis persistence disabled"}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	binaryHash := sha256.New()
	_, err = io.Copy(binaryHash, binary)
	binary.Close()
	if err != nil {
		t.Fatal(err)
	}
	report.HarnessSHA256 = hex.EncodeToString(binaryHash.Sum(nil))
	defer func() {
		if t.Failed() && report.Failure == "" {
			report.Failure = "benchmark failed; see companion log"
		}
		performanceSave(t, output, report)
	}()
	definition := performanceDefinition()
	driver := performanceDriver(t, engine)
	t.Cleanup(func() {
		if err := driver.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx := context.Background()
	if err := driver.CreateIndex(ctx, definition); err != nil {
		t.Fatal(err)
	}
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	started := time.Now()
	for offset := 0; offset < count; offset += report.Batch {
		batch := make([]search.Mutation, 0, min(report.Batch, count-offset))
		for ordinal := offset; ordinal < min(offset+report.Batch, count); ordinal++ {
			document := performanceDocument(ordinal)
			fmt.Fprintf(hash, "%s\n%s\n", document.ID, document.Body)
			report.BodyBytes += int64(len(document.Body))
			mutation, err := search.UpsertMutation(document)
			if err != nil {
				t.Fatal(err)
			}
			batch = append(batch, mutation)
		}
		batchCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		result, err := index.Bulk(batchCtx, batch)
		if err == nil {
			var outcome search.Outcome
			outcome, err = index.Wait(batchCtx, result.Receipt)
			if err == nil && outcome.State != search.OutcomeSucceeded {
				err = fmt.Errorf("visibility outcome %s", outcome.State)
			}
		}
		cancel()
		if err != nil {
			report.Failure = fmt.Sprintf("load at %d: %v", offset, err)
			t.Fatal(report.Failure)
		}
		report.Loaded += len(batch)
		report.LoadSeconds = time.Since(started).Seconds()
		if report.Loaded%100_000 == 0 || report.Loaded == count {
			report.SHA256 = hex.EncodeToString(hash.Sum(nil))
			performanceSave(t, output, report)
			t.Logf("%s: %d/%d documents, %.0f docs/s", engine, report.Loaded, count, float64(report.Loaded)/report.LoadSeconds)
		}
	}
	for _, name := range []string{"literal", "filter_sort", "facets"} {
		measurement := performanceQuery{Name: name}
		for sample := -warmup; sample < samples; sample++ {
			// The fixed permutation distributes requests across the entire corpus without retaining it in memory.
			partition := ((sample+warmup)*7919 + 17) % ((count + 1023) / 1024)
			query := performanceRequest(name, partition)
			queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			start := time.Now()
			result, err := index.Search(queryCtx, query)
			elapsed := time.Since(start).Nanoseconds()
			cancel()
			if err != nil {
				report.Failure = fmt.Sprintf("%s: %v", name, err)
				t.Fatal(report.Failure)
			}
			performanceVerify(t, name, partition, count, result)
			if sample >= 0 {
				measurement.NS = append(measurement.NS, elapsed)
			}
			measurement.TotalRelation = result.Total.Relation
		}
		report.Queries = append(report.Queries, measurement)
		t.Logf("%s %s: %d checked searches", engine, name, len(measurement.NS))
	}
	report.Complete = true
}

// performanceInt rejects accidental unbounded workloads before provisioning resources.
func performanceInt(t *testing.T, name string, fallback, low, high int) int {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < low || n > high {
		t.Fatalf("%s must be between %d and %d", name, low, high)
	}
	return n
}

// performanceDefinition applies identical field capabilities to every physical index.
func performanceDefinition() search.Definition {
	return search.Definition{Name: "catalog", DefaultSearchable: []search.FieldName{"title"}, Fields: []search.Field{
		{Name: "title", Type: search.TextField, Required: true, Searchable: true},
		{Name: "description", Type: search.TextField, Required: true, Searchable: true},
		{Name: "partition", Type: search.KeywordField, Required: true, Filterable: true},
		{Name: "category", Type: search.KeywordField, Required: true, Filterable: true, Facetable: true},
		{Name: "ordinal", Type: search.IntegerField, Required: true, Sortable: true},
	}}
}

// performanceDocument is the versioned, deterministic source of every driver's corpus.
func performanceDocument(ordinal int) search.Document {
	colors := [...]string{"blue", "green", "red", "silver", "black", "white", "orange", "purple"}
	category := ordinal % 8
	body := fmt.Sprintf(`{"title":"portable search batch%08d %s catalog item","description":"Durable everyday equipment for home office workshop and travel. Indexed with the same public search API and deterministic catalog data.","partition":"batch%08d","category":"category%d","ordinal":%d}`, ordinal/1024, colors[category], ordinal/1024, category, ordinal)
	return search.Document{ID: search.DocumentID(fmt.Sprintf("doc%012d", ordinal)), Body: []byte(body)}
}

// performanceRequest varies the partition while retaining a bounded, identical match set on every engine.
func performanceRequest(name string, partition int) search.Query {
	key := fmt.Sprintf("batch%08d", partition)
	value, _ := search.Keyword(key)
	query := search.Query{Limit: 20}
	switch name {
	case "literal":
		query.Text = "portable " + key
	case "filter_sort":
		query.Filter = search.Equal("partition", value)
		query.Sort = []search.Sort{{Field: "ordinal", Direction: search.Descending}}
	case "facets":
		query.Text = "portable"
		query.Filter = search.Equal("partition", value)
		query.Facets = []search.TermFacet{{Name: "categories", Field: "category", Limit: 8}}
	default:
		panic("unknown performance workload")
	}
	return query
}

// performanceVerify excludes incorrect or incomplete responses from the published timing samples.
func performanceVerify(t *testing.T, name string, partition, count int, result search.Result) {
	t.Helper()
	want := min(1024, count-partition*1024)
	if result.Total.Value != uint64(want) || len(result.Hits) != min(20, want) {
		t.Fatalf("%s: unexpected count/hits: %+v / %d, want %d", name, result.Total, len(result.Hits), want)
	}
	seen := map[search.DocumentID]bool{}
	for position, hit := range result.Hits {
		var body struct {
			Ordinal   int    `json:"ordinal"`
			Partition string `json:"partition"`
		}
		if err := json.Unmarshal(hit.Document.Body, &body); err != nil {
			t.Fatal(err)
		}
		if body.Ordinal < partition*1024 || body.Ordinal >= partition*1024+want || body.Partition != fmt.Sprintf("batch%08d", partition) || hit.Document.ID != search.DocumentID(fmt.Sprintf("doc%012d", body.Ordinal)) || seen[hit.Document.ID] {
			t.Fatal("incorrect document in benchmark result")
		}
		seen[hit.Document.ID] = true
		if name == "filter_sort" && body.Ordinal != partition*1024+want-1-position {
			t.Fatal("incorrect benchmark sort")
		}
	}
	if name == "facets" {
		if len(result.Facets) != 1 || len(result.Facets[0].Buckets) != min(8, want) || result.Facets[0].Truncated {
			t.Fatal("incomplete benchmark facets")
		}
		counts := make([]int, 0, 8)
		for _, bucket := range result.Facets[0].Buckets {
			value, ok := bucket.Value.Keyword()
			var category int
			if _, err := fmt.Sscanf(value, "category%d", &category); !ok || err != nil || category < 0 || category >= 8 {
				t.Fatal("incorrect benchmark facet value")
			}
			expected := (want + 7 - category) / 8
			if bucket.Count != uint64(expected) {
				t.Fatal("incorrect benchmark facet count")
			}
			counts = append(counts, category)
		}
		sort.Ints(counts)
		for n, category := range counts {
			if n != category {
				t.Fatal("duplicate benchmark facet")
			}
		}
	}
}

// performanceDriver reuses pinned fixture lifecycle and authentication without timing container startup.
func performanceDriver(t *testing.T, engine string) administeredBackend {
	t.Helper()
	var driver administeredBackend
	var err error
	switch engine {
	case "bleve":
		driver, err = blevesearch.Open(context.Background(), blevesearch.Config{Path: filepath.Join(t.TempDir(), "catalog.bleve")})
	case "sqlite":
		driver, err = sqlitesearch.New(sqlitesearch.Config{DB: sqlDatabase(t, "sqlite")})
	case "postgres":
		driver, err = postgressearch.New(postgressearch.Config{DB: sqlDatabase(t, "postgres")})
	case "redis":
		driver, err = redissearch.New(redissearch.Config{Addr: startTCPServer(t, "redis"), Password: testAPIKey})
	case "meilisearch":
		driver, err = meilisearch.New(meilisearch.Config{URL: startServer(t, engine), APIKey: testAPIKey})
	case "typesense":
		driver, err = typesensesearch.New(typesensesearch.Config{URL: startServer(t, engine), APIKey: testAPIKey})
	case "opensearch":
		driver, err = opensearch.New(opensearch.Config{URL: startServer(t, engine), Username: "admin", Password: testOpenSearchPassword, RefreshInterval: 100 * time.Millisecond})
	case "elasticsearch":
		driver, err = elasticsearch.New(elasticsearch.Config{URL: startServer(t, engine), Username: "elastic", Password: testOpenSearchPassword, RefreshInterval: 100 * time.Millisecond})
	default:
		t.Fatalf("unknown SEARCH_BENCH_DRIVER %q", engine)
	}
	if err != nil {
		t.Fatal(err)
	}
	return driver
}

// performanceCommand records public host metadata without including environment values or credentials.
func performanceCommand(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "unavailable"
	}
	return string(data)
}

// performanceSave atomically publishes progress so interruptions cannot leave a valid-looking truncated report.
func performanceSave(t *testing.T, path string, report performanceReport) {
	t.Helper()
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".tmp", append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		t.Fatal(err)
	}
}
