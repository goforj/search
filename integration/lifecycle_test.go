//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/goforj/search"
	"github.com/goforj/search/driver/meilisearch"
	"github.com/goforj/search/driver/opensearch"
	"github.com/goforj/search/driver/typesensesearch"
	"github.com/goforj/search/internal/driverutil"
	"github.com/goforj/search/searchtest"
)

// administeredBackend is the consumer-owned lifecycle contract for isolated integration fixtures.
type administeredBackend interface {
	search.Backend
	CreateIndex(context.Context, search.Definition) error
	ValidateIndex(context.Context, search.Definition) error
	Close() error
}

// serviceDriver constructs one concrete backend without coupling the shared corpus to administration.
func serviceDriver(t *testing.T, engine, endpoint, prefix string) administeredBackend {
	t.Helper()
	if engine == "meilisearch" {
		driver, err := meilisearch.New(meilisearch.Config{URL: endpoint, APIKey: testAPIKey, Prefix: prefix, HTTPClient: &http.Client{Transport: &classificationTransport{t: t, base: http.DefaultTransport.(*http.Transport).Clone()}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := driver.Close(); err != nil {
				t.Error(err)
			}
		})
		return driver
	}
	if engine == "opensearch" {
		driver, err := opensearch.New(opensearch.Config{URL: endpoint, Username: "admin", Password: testOpenSearchPassword, Prefix: prefix, RefreshInterval: 100 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := driver.Close(); err != nil {
				t.Error(err)
			}
		})
		return driver
	}
	driver, err := typesensesearch.New(typesensesearch.Config{URL: endpoint, APIKey: testAPIKey, Prefix: prefix, HTTPClient: &http.Client{Transport: &classificationTransport{t: t, base: http.DefaultTransport.(*http.Transport).Clone()}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := driver.Close(); err != nil {
			t.Error(err)
		}
	})
	return driver
}

// TestServiceOwnershipAndActualDrift proves physical isolation and actual-schema validation on every service engine.
func TestServiceOwnershipAndActualDrift(t *testing.T) {
	for _, engine := range selectedServices() {
		t.Run(engine, func(t *testing.T) {
			endpoint := startServer(t, engine)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			definition := searchtest.Definition()
			first := serviceDriver(t, engine, endpoint, "first_")
			second := serviceDriver(t, engine, endpoint, "second_")
			if err := first.ValidateIndex(ctx, definition); !errors.Is(err, search.ErrIndexNotFound) {
				t.Fatal(err)
			}
			for _, driver := range []administeredBackend{first, second} {
				if err := driver.CreateIndex(ctx, definition); err != nil {
					t.Fatal(err)
				}
			}
			index, err := search.NewIndex(first, definition)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := index.Upsert(ctx, search.Document{ID: "one", Body: []byte(`{"active":true,"rank":1,"code":"x"}`)})
			if err != nil {
				t.Fatal(err)
			}
			if outcome, err := index.Wait(ctx, receipt); err != nil || outcome.State != search.OutcomeSucceeded {
				t.Fatal(outcome, err)
			}
			other, err := search.NewIndex(second, definition)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := other.Search(ctx, search.Query{}); err != nil || result.Total.Value != 0 {
				t.Fatal(result, err)
			}
			changed := definition
			changed.Fields = append(changed.Fields, search.Field{Name: "extra", Type: search.KeywordField})
			if err := first.CreateIndex(ctx, changed); !errors.Is(err, search.ErrSchemaDrift) {
				t.Fatal(err)
			}
			header := "X-TYPESENSE-API-KEY"
			if engine == "meilisearch" {
				header = "Authorization"
			}
			admin, err := driverutil.NewHTTP(endpoint, testAPIKey, header, nil)
			if engine == "opensearch" {
				admin, err = driverutil.NewHTTP(endpoint, "", "", nil)
				if err == nil {
					admin.Authorization = "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:"+testOpenSearchPassword))
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			physical := "first_s_" + driverutil.FieldKey(search.FieldName(definition.Name))
			if engine == "opensearch" {
				var observed map[string]any
				if err := admin.Do(ctx, "GET", "/first_*/_mapping", nil, &observed, false); err != nil {
					t.Fatal(err)
				}
				if len(observed) != 1 {
					t.Fatal("ambiguous owned index target")
				}
				for name := range observed {
					physical = name
				}
				var acknowledgement map[string]any
				if err := admin.Do(ctx, "PUT", "/"+physical+"/_settings", map[string]any{"index": map[string]any{"max_result_window": 20000}}, &acknowledgement, true); err != nil {
					t.Fatal(err)
				}
			} else if engine == "meilisearch" {
				var task struct {
					TaskUID uint64 `json:"taskUid"`
				}
				if err := admin.Do(ctx, "PATCH", "/indexes/"+physical+"/settings", map[string]any{"sortableAttributes": []string{}}, &task, true); err != nil {
					t.Fatal(err)
				}
				for {
					var status struct{ Status string }
					if err := admin.Do(ctx, "GET", fmt.Sprintf("/tasks/%d", task.TaskUID), nil, &status, false); err != nil {
						t.Fatal(err)
					}
					if status.Status == "succeeded" {
						break
					}
					if status.Status == "failed" {
						t.Fatal("drift fixture was rejected")
					}
					timer := time.NewTimer(10 * time.Millisecond)
					select {
					case <-ctx.Done():
						timer.Stop()
						t.Fatal(ctx.Err())
					case <-timer.C:
					}
				}
			} else {
				var result map[string]any
				if err := admin.Do(ctx, "PATCH", "/collections/"+physical, map[string]any{"fields": []map[string]any{{"name": driverutil.FieldKey("title"), "drop": true}}}, &result, true); err != nil {
					t.Fatal(err)
				}
			}
			if err := first.ValidateIndex(ctx, definition); !errors.Is(err, search.ErrSchemaDrift) {
				t.Fatal("actual drift was not detected", err)
			}
			if err := second.ValidateIndex(ctx, definition); err != nil {
				t.Fatal("drift crossed physical namespace", err)
			}
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := index.Search(ctx, search.Query{}); !errors.Is(err, search.ErrClosed) {
				t.Fatal(err)
			}
		})
	}
}

// classificationTransport reports only a server error code from disposable fixtures when a test fails.
type classificationTransport struct {
	t    *testing.T
	base http.RoundTripper
}

// RoundTrip retains the response body while recording a bounded error-code classification.
func (c *classificationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := c.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		response.Body = io.NopCloser(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		var code struct{ Code string }
		_ = json.Unmarshal(body, &code)
		c.t.Logf("disposable backend status=%d code=%s", response.StatusCode, code.Code)
	}
	return response, nil
}

// TestMeilisearchFacetCardinalityLimit refuses an incomplete native facet dictionary instead of inventing portable top buckets.
func TestMeilisearchFacetCardinalityLimit(t *testing.T) {
	endpoint := startServer(t, "meilisearch")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	driver := serviceDriver(t, "meilisearch", endpoint, "")
	definition := searchtest.Definition()
	if err := driver.CreateIndex(ctx, definition); err != nil {
		t.Fatal(err)
	}
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		t.Fatal(err)
	}
	for start := 0; start < 4100; start += 1000 {
		mutations := []search.Mutation{}
		for n := start; n < min(start+1000, 4100); n++ {
			body := fmt.Sprintf(`{"active":true,"rank":0,"code":"x","category":"tag%04d"}`, n)
			mutation, err := search.UpsertMutation(search.Document{ID: search.DocumentID(fmt.Sprintf("item%04d", n)), Body: []byte(body)})
			if err != nil {
				t.Fatal(err)
			}
			mutations = append(mutations, mutation)
		}
		batch, err := index.Bulk(ctx, mutations)
		if err != nil {
			t.Fatal(err)
		}
		if outcome, err := index.Wait(ctx, batch.Receipt); err != nil || outcome.State != search.OutcomeSucceeded {
			t.Fatal(outcome, err)
		}
	}
	if _, err := index.Search(ctx, search.Query{Facets: []search.TermFacet{{Name: "categories", Field: "category", Limit: 2}}}); !errors.Is(err, search.ErrUnsupported) {
		t.Fatal(err)
	}
}
