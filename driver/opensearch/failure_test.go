package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
	"github.com/goforj/search/searchtest"
)

// fixtureDriver isolates protocol failures after explicit construction, alongside the independent real-cluster suite.
func fixtureDriver(t *testing.T, handler http.HandlerFunc) (*Driver, search.Definition) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	driver, err := New(Config{URL: server.URL, RefreshInterval: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { driver.Close() })
	index, err := search.NewIndex(driver, searchtest.Definition())
	if err != nil {
		t.Fatal(err)
	}
	definition := index.Definition()
	driver.name = driver.target(definition)
	driver.fingerprint, err = definition.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	return driver, definition
}

// TestConstructionAndReadinessFailures rejects malformed policy before contacting a cluster.
func TestConstructionAndReadinessFailures(t *testing.T) {
	for _, config := range []Config{
		{URL: "file:///tmp"}, {URL: "http://localhost", Shards: -1}, {URL: "http://localhost", Shards: 17},
		{URL: "http://localhost", Replicas: -1}, {URL: "http://localhost", Replicas: 4},
		{URL: "http://localhost", RefreshInterval: time.Millisecond}, {URL: "http://localhost", RefreshInterval: 6 * time.Second},
		{URL: "http://localhost", Prefix: "_bad"}, {URL: "http://localhost", Prefix: "bad/prefix"},
		{URL: "http://localhost", Prefix: strings.Repeat("a", 65)},
		{URL: "http://localhost", Username: "only-user"}, {URL: "http://localhost", Password: "only-password"},
		{URL: "http://localhost", Username: "bad:user", Password: "password"},
	} {
		if _, err := New(config); !errors.Is(err, search.ErrInvalidDefinition) {
			t.Fatal(err)
		}
	}
	driver, err := New(Config{URL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if _, err := driver.Search(context.Background(), searchtest.Definition(), search.Query{}); !errors.Is(err, search.ErrIndexNotFound) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := driver.CreateIndex(ctx, searchtest.Definition()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := driver.Wait(ctx, search.Receipt{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	driver.Close()
	if err := driver.CreateIndex(context.Background(), searchtest.Definition()); !errors.Is(err, search.ErrClosed) {
		t.Fatal(err)
	}
	if err := driver.ValidateIndex(context.Background(), searchtest.Definition()); !errors.Is(err, search.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := driver.Wait(context.Background(), search.Receipt{}); !errors.Is(err, search.ErrClosed) {
		t.Fatal(err)
	}
}

// TestSearchCompleteness refuses timed-out, failed-shard, and structurally incomplete responses.
func TestSearchCompleteness(t *testing.T) {
	for name, response := range map[string]string{
		"timeout":                `{"timed_out":true,"_shards":{"total":1,"successful":1,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`,
		"failed-shard":           `{"timed_out":false,"_shards":{"total":2,"successful":1,"failed":1},"hits":{"total":{"value":1,"relation":"eq"},"hits":[]}}`,
		"missing-shards":         `{"timed_out":false,"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`,
		"missing-count-relation": `{"timed_out":false,"_shards":{"total":1,"successful":1,"failed":0},"hits":{"total":{"value":0},"hits":[]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			driver, definition := fixtureDriver(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, response) })
			index, err := search.NewIndex(driver, definition)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := index.Search(context.Background(), search.Query{}); !errors.Is(err, search.ErrUnavailable) {
				t.Fatal(err)
			}
		})
	}
}

// TestWriteResponseIntegrity prevents HTTP success from implying unproven refresh visibility.
func TestWriteResponseIntegrity(t *testing.T) {
	for _, failed := range []bool{false, true} {
		var driver *Driver
		var definition search.Definition
		driver, definition = fixtureDriver(t, func(w http.ResponseWriter, _ *http.Request) {
			if failed {
				fmt.Fprintf(w, `{"_id":"i6f6e65","_index":%q,"result":"created","_shards":{"total":1,"successful":0,"failed":1}}`, driver.name)
			} else {
				fmt.Fprintf(w, `{"_id":"wrong","_index":%q,"result":"created","_shards":{"total":1,"successful":1,"failed":0}}`, driver.name)
			}
		})
		if _, err := driver.Upsert(context.Background(), definition, search.Document{ID: "one", Body: []byte(`{"active":true,"rank":0,"code":"x"}`)}); !errors.Is(err, search.ErrOutcomeUnknown) {
			t.Fatal(err)
		}
	}
	driver, definition := fixtureDriver(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":"private-document-secret"}`)
	})
	if _, err := driver.Upsert(context.Background(), definition, search.Document{ID: "one", Body: []byte(`{"active":true,"rank":0,"code":"x"}`)}); !errors.Is(err, search.ErrInvalidDocument) || strings.Contains(fmt.Sprint(err), "private") {
		t.Fatal(err)
	}
}

// TestPhysicalKeywordBudget rejects overlong native terms before network submission.
func TestPhysicalKeywordBudget(t *testing.T) {
	driver, definition := fixtureDriver(t, func(http.ResponseWriter, *http.Request) { t.Error("overlong keyword reached transport") })
	body, err := json.Marshal(map[string]any{"active": true, "rank": 0, "code": strings.Repeat("a", MaxKeywordBytes+1)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := driver.Upsert(context.Background(), definition, search.Document{ID: "one", Body: body}); !errors.Is(err, search.ErrInvalidDocument) {
		t.Fatal(err)
	}
}

// TestBulkFailuresRemainData keeps partial rejection details without exposing native error reasons.
func TestBulkFailuresRemainData(t *testing.T) {
	var driver *Driver
	var definition search.Definition
	driver, definition = fixtureDriver(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"errors":true,"items":[{"index":{"_id":"i6f6e65","_index":%q,"status":201,"_shards":{"total":1,"successful":1,"failed":0}}},{"index":{"_id":"i74776f","_index":%q,"status":400,"error":{"type":"mapper_parsing_exception","reason":"private-document-secret"}}}]}`, driver.name, driver.name)
	})
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []search.Mutation{}
	for _, id := range []search.DocumentID{"one", "two"} {
		mutation, err := search.UpsertMutation(search.Document{ID: id, Body: []byte(`{"active":true,"rank":0,"code":"x"}`)})
		if err != nil {
			t.Fatal(err)
		}
		mutations = append(mutations, mutation)
	}
	batch, err := index.Bulk(context.Background(), mutations)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Items[0].Status != search.ItemAccepted || batch.Items[1].Status != search.ItemRejected || !errors.Is(batch.Items[1].Error, search.ErrInvalidDocument) {
		t.Fatal(batch.Items)
	}
	outcome, err := index.Wait(context.Background(), batch.Receipt)
	if err != nil || outcome.State != search.OutcomeFailed || outcome.Bulk.Items[0].Status != search.ItemSucceeded || outcome.Bulk.Items[1].Status != search.ItemFailed {
		t.Fatal(outcome, err)
	}
	if strings.Contains(fmt.Sprintf("%+v", outcome.Bulk.Items), "private") {
		t.Fatal("native reason leaked")
	}
}

// TestTargetEncodingAndStatusClassification keeps physical names bounded and error identities stable.
func TestTargetEncodingAndStatusClassification(t *testing.T) {
	driver, err := New(Config{URL: "http://localhost", Prefix: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	definition := searchtest.Definition()
	definition.Name = search.IndexName(strings.Repeat("é", 64))
	first := driver.target(definition)
	definition.Name = "other"
	if len(first) > 255 || first == driver.target(definition) {
		t.Fatal("unsafe physical identity")
	}
	for status, expected := range map[int]error{400: search.ErrInvalidDocument, 401: search.ErrUnauthorized, 403: search.ErrUnauthorized, 404: search.ErrIndexNotFound, 409: search.ErrConflict, 429: search.ErrRateLimited, 418: search.ErrMutationFailed} {
		if !errors.Is(classifyStatus(status), expected) {
			t.Fatal(status)
		}
	}
	if nativeValue(search.Boolean(true)) != "1" {
		t.Fatal("Boolean encoding changed")
	}
	integer, err := search.Integer(7)
	if err != nil {
		t.Fatal(err)
	}
	if nativeValue(integer) != int64(7) {
		t.Fatal("integer comparison is not numeric")
	}
	if _, err := driverutil.NewHTTP("http://user:pass@localhost", "", "", nil); !errors.Is(err, search.ErrInvalidDefinition) {
		t.Fatal(err)
	}
}

// TestDeleteRequiresShardEvidence prevents an incomplete acknowledgement from establishing visibility.
func TestDeleteRequiresShardEvidence(t *testing.T) {
	for name, shards := range map[string]shardResult{
		"missing":    {},
		"incomplete": {Total: 2, Successful: 1},
		"failed":     {Total: 2, Successful: 1, Failed: 1},
	} {
		t.Run(name, func(t *testing.T) {
			var driver *Driver
			var definition search.Definition
			driver, definition = fixtureDriver(t, func(w http.ResponseWriter, _ *http.Request) {
				json.NewEncoder(w).Encode(writeResult{ID: driverutil.IDKey("one"), Index: driver.name, Result: "deleted", Shards: shards})
			})
			if _, err := driver.Delete(context.Background(), definition, "one"); !errors.Is(err, search.ErrOutcomeUnknown) {
				t.Fatal(err)
			}
		})
	}
}

// TestValidationChecksMaterializedState rejects drift even when the stored fingerprint is unchanged.
func TestValidationChecksMaterializedState(t *testing.T) {
	for _, mode := range []string{"mapping", "shards", "replicas", "window", "refresh", "health", "health-timeout"} {
		t.Run(mode, func(t *testing.T) {
			var driver *Driver
			var definition search.Definition
			driver, definition = fixtureDriver(t, func(w http.ResponseWriter, r *http.Request) {
				var response any
				switch {
				case strings.HasSuffix(r.URL.Path, "/_mapping"):
					mapping := indexMapping(definition)
					if mode == "mapping" {
						mapping["dynamic"] = "true"
					}
					response = map[string]any{driver.name: map[string]any{"mappings": mapping}}
				case strings.HasSuffix(r.URL.Path, "/_settings"):
					settings := map[string]string{"number_of_shards": "1", "number_of_replicas": "0", "max_result_window": "16000", "refresh_interval": "100ms"}
					for key, branch := range map[string]string{"number_of_shards": "shards", "number_of_replicas": "replicas", "max_result_window": "window", "refresh_interval": "refresh"} {
						if mode == branch {
							settings[key] = "unexpected"
						}
					}
					response = map[string]any{driver.name: map[string]any{"settings": map[string]any{"index": settings}}}
				default:
					response = map[string]any{"status": "yellow", "timed_out": mode == "health-timeout"}
				}
				json.NewEncoder(w).Encode(response)
			})
			err := driver.ValidateIndex(context.Background(), definition)
			want := search.ErrSchemaDrift
			if strings.HasPrefix(mode, "health") {
				want = search.ErrUnavailable
			}
			if !errors.Is(err, want) {
				t.Fatal(err)
			}
		})
	}
}
