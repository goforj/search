//go:build integration

package integration_test

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
	"github.com/goforj/search/driver/meilisearch"
	"github.com/goforj/search/driver/typesensesearch"
	"github.com/goforj/search/searchtest"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testAPIKey is a public, disposable fixture credential used only in isolated containers.
const testAPIKey = "search-integration-disposable-key"

// startServer provisions an isolated real server with pinned image, random port, and bounded readiness.
func startServer(t *testing.T, engine string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	request := testcontainers.ContainerRequest{}
	port, health := "7700/tcp", "/health"
	switch engine {
	case "meilisearch":
		request.Image = "getmeili/meilisearch:v1.17.0"
		request.Env = map[string]string{"MEILI_MASTER_KEY": testAPIKey, "MEILI_NO_ANALYTICS": "true"}
	case "typesense":
		port = "8108/tcp"
		request.Image = "typesense/typesense:30.0"
		request.Cmd = []string{"--data-dir=/tmp", "--api-key=" + testAPIKey, "--enable-cors=false"}
	default:
		t.Fatalf("unknown engine %q", engine)
	}
	request.ExposedPorts = []string{port}
	request.WaitingFor = wait.ForHTTP(health).WithPort(nat.Port(port)).WithStartupTimeout(60 * time.Second)
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: request, Started: true})
	if container != nil {
		t.Cleanup(func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := container.Terminate(cleanup); err != nil {
				t.Error(err)
			}
		})
	}
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := container.PortEndpoint(ctx, nat.Port(port), "http")
	if err != nil {
		t.Fatal(err)
	}
	return endpoint
}

// TestMeilisearchContract proves the shared contract against the actual asynchronous engine.
func TestMeilisearchContract(t *testing.T) {
	endpoint := startServer(t, "meilisearch")
	searchtest.RunBackend(t, func(ctx context.Context, def search.Definition) (*search.Index, func(context.Context) error, error) {
		driver, err := meilisearch.New(meilisearch.Config{URL: endpoint, APIKey: testAPIKey})
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
		driver, err := typesensesearch.New(typesensesearch.Config{URL: endpoint, APIKey: testAPIKey})
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
				driver, e := meilisearch.New(meilisearch.Config{URL: endpoint, APIKey: "wrong"})
				if e != nil {
					t.Fatal(e)
				}
				defer driver.Close()
				err = driver.ValidateIndex(ctx, searchtest.Definition())
			} else {
				driver, e := typesensesearch.New(typesensesearch.Config{URL: endpoint, APIKey: "wrong"})
				if e != nil {
					t.Fatal(e)
				}
				defer driver.Close()
				err = driver.ValidateIndex(ctx, searchtest.Definition())
			}
			if !errors.Is(err, search.ErrUnauthorized) {
				t.Fatal(fmt.Sprintf("classification: %v", err))
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
	driver, err := meilisearch.New(meilisearch.Config{URL: endpoint, APIKey: testAPIKey, HTTPClient: &http.Client{Transport: transport}})
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
	driver, err := typesensesearch.New(typesensesearch.Config{URL: endpoint, APIKey: testAPIKey})
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
	default:
		return []string{"meilisearch", "typesense"}
	}
}
