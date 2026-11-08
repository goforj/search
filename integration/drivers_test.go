//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/goforj/search"
	"github.com/goforj/search/driver/blevesearch"
	"github.com/goforj/search/driver/elasticsearch"
	"github.com/goforj/search/driver/meilisearch"
	"github.com/goforj/search/driver/opensearch"
	"github.com/goforj/search/driver/typesensesearch"
	"github.com/goforj/search/searchtest"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testAPIKey is a public, disposable fixture credential used only in isolated containers.
const testAPIKey = "search-integration-disposable-key"

// testOpenSearchPassword is a public, disposable credential for the isolated security-enabled node.
const testOpenSearchPassword = "Search-Fixture-Only-2026!"

// startServer provisions an isolated real server with pinned image, random port, and bounded readiness.
func startServer(t *testing.T, engine string) string {
	t.Helper()
	fixturePool.Lock()
	defer fixturePool.Unlock()
	if endpoint, ok := fixturePool.endpoints[engine]; ok {
		return endpoint
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var versions map[string]engineVersion
	if err := json.Unmarshal(pinnedVersions, &versions); err != nil {
		t.Fatal(err)
	}
	version, ok := versions[engine]
	if !ok || version.Image == "" || version.Version == "" {
		t.Fatal("missing pinned backend version")
	}
	request := testcontainers.ContainerRequest{Image: version.Image}
	port, health := "7700/tcp", "/health"
	switch engine {
	case "meilisearch":
		request.Env = map[string]string{"MEILI_MASTER_KEY": testAPIKey, "MEILI_NO_ANALYTICS": "true"}
	case "typesense":
		port = "8108/tcp"
		request.Cmd = []string{"--data-dir=/tmp", "--api-key=" + testAPIKey, "--enable-cors=false"}
	case "elasticsearch":
		port, health = "9200/tcp", "/"
		request.Env = map[string]string{"discovery.type": "single-node", "ELASTIC_PASSWORD": testOpenSearchPassword, "ES_JAVA_OPTS": "-Xms512m -Xmx512m", "xpack.security.enabled": "true", "xpack.security.http.ssl.enabled": "false"}
	case "opensearch":
		port, health = "9200/tcp", "/"
		request.Env = map[string]string{
			"discovery.type":                    "single-node",
			"OPENSEARCH_INITIAL_ADMIN_PASSWORD": testOpenSearchPassword,
			"OPENSEARCH_JAVA_OPTS":              "-Xms512m -Xmx512m",
			"plugins.security.ssl.http.enabled": "false",
			"bootstrap.memory_lock":             "false",
		}
	default:
		t.Fatalf("unknown engine %q", engine)
	}
	request.ExposedPorts = []string{port}
	readiness := wait.ForHTTP(health).WithPort(nat.Port(port)).WithStartupTimeout(60 * time.Second)
	if engine == "elasticsearch" {
		readiness.WithBasicAuth("elastic", testOpenSearchPassword)
	}
	if engine == "opensearch" {
		readiness.WithBasicAuth("admin", testOpenSearchPassword)
	}
	request.WaitingFor = readiness
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: request, Started: true})
	if container != nil {
		fixturePool.containers = append(fixturePool.containers, container)
		fixturePool.engines[engine] = container
	}
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := container.PortEndpoint(ctx, nat.Port(port), "http")
	if err != nil {
		t.Fatal(err)
	}
	verifyServerVersion(t, ctx, engine, endpoint, version.Version)
	fixturePool.endpoints[engine] = endpoint
	t.Logf("started shared %s container", engine)
	return endpoint
}

// TestMeilisearchContract proves the shared contract against the actual asynchronous engine.
func TestMeilisearchContract(t *testing.T) {
	endpoint := startServer(t, "meilisearch")
	searchtest.RunBackend(t, func(ctx context.Context, def search.Definition) (*search.Index, func(context.Context) error, error) {
		driver, err := meilisearch.New(meilisearch.Config{URL: endpoint, Prefix: fixturePrefix(t), APIKey: testAPIKey})
		if err != nil {
			return nil, nil, err
		}
		if err = driver.CreateIndex(ctx, def); err != nil {
			_ = driver.Close()
			return nil, nil, err
		}
		if err = driver.CreateIndex(ctx, def); err != nil {
			_ = driver.Close()
			return nil, nil, err
		}
		index, err := search.NewIndex(driver, def)
		return index, func(context.Context) error { return driver.Close() }, err
	})
}

// TestTypesenseContract proves the shared contract against actual collection and search behavior.
func TestTypesenseContract(t *testing.T) {
	endpoint := startServer(t, "typesense")
	searchtest.RunBackend(t, func(ctx context.Context, def search.Definition) (*search.Index, func(context.Context) error, error) {
		driver, err := typesensesearch.New(typesensesearch.Config{URL: endpoint, Prefix: fixturePrefix(t), APIKey: testAPIKey})
		if err != nil {
			return nil, nil, err
		}
		if err = driver.CreateIndex(ctx, def); err != nil {
			_ = driver.Close()
			return nil, nil, err
		}
		if err = driver.CreateIndex(ctx, def); err != nil {
			_ = driver.Close()
			return nil, nil, err
		}
		index, err := search.NewIndex(driver, def)
		return index, func(context.Context) error { return driver.Close() }, err
	})
}

// TestServiceAuthentication ensures real backend errors preserve classification without server prose.
func TestServiceAuthentication(t *testing.T) {
	for _, engine := range selectedServices() {
		t.Run(engine, func(t *testing.T) {
			endpoint := startServer(t, engine)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var err error
			if engine == "meilisearch" {
				driver, e := meilisearch.New(meilisearch.Config{URL: endpoint, Prefix: fixturePrefix(t), APIKey: "wrong"})
				if e != nil {
					t.Fatal(e)
				}
				defer driver.Close()
				err = driver.ValidateIndex(ctx, searchtest.Definition())
			} else if engine == "elasticsearch" {
				driver, e := elasticsearch.New(elasticsearch.Config{URL: endpoint, Username: "elastic", Password: "wrong"})
				if e != nil {
					t.Fatal(e)
				}
				defer driver.Close()
				err = driver.ValidateIndex(ctx, searchtest.Definition())
			} else if engine == "opensearch" {
				driver, e := opensearch.New(opensearch.Config{URL: endpoint, Prefix: fixturePrefix(t), Username: "admin", Password: "wrong"})
				if e != nil {
					t.Fatal(e)
				}
				defer driver.Close()
				err = driver.ValidateIndex(ctx, searchtest.Definition())
			} else {
				driver, e := typesensesearch.New(typesensesearch.Config{URL: endpoint, Prefix: fixturePrefix(t), APIKey: "wrong"})
				if e != nil {
					t.Fatal(e)
				}
				defer driver.Close()
				err = driver.ValidateIndex(ctx, searchtest.Definition())
			}
			if !errors.Is(err, search.ErrUnauthorized) {
				t.Fatal(fmt.Sprintf("classification: %v", err))
			}
			if engine == "opensearch" || engine == "elasticsearch" {
				return
			}
			response, err := http.Get(endpoint + "/health")
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
		})
	}
}

// rejectingTransport injects an invalid physical ID below validation to exercise a real failed server task.
type rejectingTransport struct {
	base    http.RoundTripper
	enabled atomic.Bool
}

// RoundTrip changes only document submissions after provisioning and preserves normal server behavior.
func (r *rejectingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if r.enabled.Load() && request.Method == "POST" && strings.HasSuffix(request.URL.Path, "/documents") {
		data, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		request.Body.Close()
		var documents []map[string]any
		if err := json.Unmarshal(data, &documents); err != nil {
			return nil, err
		}
		for _, document := range documents {
			document["id"] = "invalid/physical/id"
		}
		data, err = json.Marshal(documents)
		if err != nil {
			return nil, err
		}
		request.Body = io.NopCloser(bytes.NewReader(data))
		request.ContentLength = int64(len(data))
	}
	return r.base.RoundTrip(request)
}

// TestMeilisearchAsyncFailure proves an accepted real task can fail without losing its terminal evidence.
func TestMeilisearchAsyncFailure(t *testing.T) {
	endpoint := startServer(t, "meilisearch")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	transport := &rejectingTransport{base: http.DefaultTransport.(*http.Transport).Clone()}
	driver, err := meilisearch.New(meilisearch.Config{URL: endpoint, Prefix: fixturePrefix(t), APIKey: testAPIKey, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	definition := searchtest.Definition()
	if err := driver.CreateIndex(ctx, definition); err != nil {
		t.Fatal(err)
	}
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		t.Fatal(err)
	}
	transport.enabled.Store(true)
	receipt, err := index.Upsert(ctx, search.Document{ID: "bad", Body: []byte(`{"active":true,"rank":1,"code":"x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := index.Wait(ctx, receipt)
	if err != nil || outcome.State != search.OutcomeFailed || !errors.Is(outcome.Error, search.ErrMutationFailed) {
		t.Fatal(outcome, err)
	}
}

// TestTypesensePartialImport proves HTTP success cannot conceal a real per-document import rejection.
func TestTypesensePartialImport(t *testing.T) {
	endpoint := startServer(t, "typesense")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	driver, err := typesensesearch.New(typesensesearch.Config{URL: endpoint, Prefix: fixturePrefix(t), APIKey: testAPIKey})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	definition := searchtest.Definition()
	if err := driver.CreateIndex(ctx, definition); err != nil {
		t.Fatal(err)
	}
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		t.Fatal(err)
	}
	good, err := search.UpsertMutation(search.Document{ID: "good", Body: []byte(`{"active":true,"rank":1,"code":"x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	bad, err := search.UpsertMutation(search.Document{ID: "bad", Body: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	// Bypass root validation intentionally to prove native import parsing against a real rejection.
	batch, err := driver.Bulk(ctx, index.Definition(), []search.Mutation{good, bad})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Items[0].Status != search.ItemAccepted || batch.Items[1].Status != search.ItemRejected {
		t.Fatal(batch.Items)
	}
	outcome, err := driver.Wait(ctx, batch.Receipt)
	if err != nil || outcome.State != search.OutcomeFailed || outcome.Bulk.Items[0].Status != search.ItemSucceeded || outcome.Bulk.Items[1].Status != search.ItemFailed {
		t.Fatal(outcome, err)
	}
}

// TestBleveContract retains embedded evidence in the same real-driver report as the service engines.
func TestBleveContract(t *testing.T) {
	searchtest.RunBackend(t, func(ctx context.Context, definition search.Definition) (*search.Index, func(context.Context) error, error) {
		driver, err := blevesearch.Open(ctx, blevesearch.Config{Path: filepath.Join(t.TempDir(), "index")})
		if err != nil {
			return nil, nil, err
		}
		if err := driver.CreateIndex(ctx, definition); err != nil {
			_ = driver.Close()
			return nil, nil, err
		}
		index, err := search.NewIndex(driver, definition)
		return index, func(context.Context) error { return driver.Close() }, err
	})
}

// selectedServices keeps focused local runs from starting an unrelated service engine.
func selectedServices() []string {
	switch os.Getenv("INTEGRATION_DRIVER") {
	case "meilisearch":
		return []string{"meilisearch"}
	case "typesense":
		return []string{"typesense"}
	case "opensearch":
		return []string{"opensearch"}
	case "elasticsearch":
		return []string{"elasticsearch"}
	case "postgres", "sqlite", "redis":
		return nil
	default:
		return []string{"meilisearch", "typesense", "opensearch", "elasticsearch"}
	}
}

// verifyServerVersion prevents a support table from advertising a version that the actual fixture did not run.
func verifyServerVersion(t *testing.T, ctx context.Context, engine, endpoint, expected string) {
	t.Helper()
	path, header, value := "/version", "Authorization", "Bearer "+testAPIKey
	if engine == "typesense" {
		path, header, value = "/debug", "X-TYPESENSE-API-KEY", testAPIKey
	}
	if engine == "opensearch" || engine == "elasticsearch" {
		path = "/"
	}
	request, err := http.NewRequestWithContext(ctx, "GET", endpoint+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(header, value)
	if engine == "elasticsearch" {
		request.SetBasicAuth("elastic", testOpenSearchPassword)
	}
	if engine == "opensearch" {
		request.SetBasicAuth("admin", testOpenSearchPassword)
	}
	client := http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("cannot inspect disposable backend version")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("backend version inspection was rejected")
	}
	var version struct {
		Version    json.RawMessage
		PkgVersion string `json:"pkgVersion"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&version); err != nil {
		t.Fatal("invalid backend version response")
	}
	var actual string
	if engine == "opensearch" || engine == "elasticsearch" {
		var native struct{ Number string }
		if err := json.Unmarshal(version.Version, &native); err != nil {
			t.Fatal(err)
		}
		actual = native.Number
	} else if len(version.Version) != 0 {
		if err := json.Unmarshal(version.Version, &actual); err != nil {
			t.Fatal(err)
		}
	}
	if engine == "meilisearch" {
		actual = version.PkgVersion
	}
	if strings.TrimPrefix(actual, "v") != strings.TrimPrefix(expected, "v") {
		t.Fatalf("actual backend version %q differs from pinned version %q", actual, expected)
	}
}

// TestOpenSearchContract proves the complete portable suite against a security-enabled, pinned real cluster.
func TestOpenSearchContract(t *testing.T) {
	endpoint := startServer(t, "opensearch")
	searchtest.RunBackend(t, func(ctx context.Context, definition search.Definition) (*search.Index, func(context.Context) error, error) {
		driver, err := opensearch.New(opensearch.Config{URL: endpoint, Prefix: fixturePrefix(t), Username: "admin", Password: testOpenSearchPassword, RefreshInterval: 100 * time.Millisecond})
		if err != nil {
			return nil, nil, err
		}
		if err := driver.CreateIndex(ctx, definition); err != nil {
			driver.Close()
			return nil, nil, err
		}
		index, err := search.NewIndex(driver, definition)
		return index, func(context.Context) error { return driver.Close() }, err
	})
}

// TestOpenSearchMultiShardContract proves the portable contract across native shard routing and coordinator merges.
func TestOpenSearchMultiShardContract(t *testing.T) {
	endpoint := startServer(t, "opensearch")
	searchtest.RunBackend(t, func(ctx context.Context, definition search.Definition) (*search.Index, func(context.Context) error, error) {
		driver, err := opensearch.New(opensearch.Config{URL: endpoint, Prefix: fixturePrefix(t), Username: "admin", Password: testOpenSearchPassword, Shards: 2, RefreshInterval: 100 * time.Millisecond})
		if err != nil {
			return nil, nil, err
		}
		if err := driver.CreateIndex(ctx, definition); err != nil {
			driver.Close()
			return nil, nil, err
		}
		index, err := search.NewIndex(driver, definition)
		return index, func(context.Context) error { return driver.Close() }, err
	})
}

// TestOpenSearchPartialBulk proves a real mapping rejection remains visible beside a successful native item.
func TestOpenSearchPartialBulk(t *testing.T) {
	endpoint := startServer(t, "opensearch")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	driver, err := opensearch.New(opensearch.Config{URL: endpoint, Prefix: fixturePrefix(t), Username: "admin", Password: testOpenSearchPassword, RefreshInterval: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	definition := searchtest.Definition()
	if err := driver.CreateIndex(ctx, definition); err != nil {
		t.Fatal(err)
	}
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		t.Fatal(err)
	}
	good, err := search.UpsertMutation(search.Document{ID: "good", Body: []byte(`{"active":true,"rank":1,"code":"x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	bad, err := search.UpsertMutation(search.Document{ID: "bad", Body: []byte(`{"active":true,"rank":"private-invalid-number","code":"x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	// Intentionally bypass root validation so the real server must produce a per-item mapping failure.
	batch, err := driver.Bulk(ctx, index.Definition(), []search.Mutation{good, bad})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Items[0].Status != search.ItemAccepted || batch.Items[1].Status != search.ItemRejected || !errors.Is(batch.Items[1].Error, search.ErrInvalidDocument) {
		t.Fatal(batch.Items)
	}
	outcome, err := driver.Wait(ctx, batch.Receipt)
	if err != nil || outcome.State != search.OutcomeFailed || outcome.Bulk.Items[0].Status != search.ItemSucceeded || outcome.Bulk.Items[1].Status != search.ItemFailed {
		t.Fatal(outcome, err)
	}
	if strings.Contains(fmt.Sprintf("%+v", outcome.Bulk.Items), "private-invalid") {
		t.Fatal("native mapping reason leaked")
	}
}
