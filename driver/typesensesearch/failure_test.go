package typesensesearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goforj/search"
	"github.com/goforj/search/searchtest"
)

// fixtureDriver isolates protocol faults after explicit construction rather than replacing real-server coverage.
func fixtureDriver(t *testing.T, handler http.HandlerFunc) (*Driver, search.Definition) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	driver, err := New(Config{URL: server.URL})
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

// TestImportResponseIntegrity refuses missing lines, malformed results, and mismatched physical identities.
func TestImportResponseIntegrity(t *testing.T) {
	for name, response := range map[string]string{
		"malformed":      "not-json-secret",
		"missing-status": `{"id":"i6f6e65"}`,
		"wrong-identity": `{"success":true,"id":"wrong"}`,
		"extra-line":     "{\"success\":true,\"id\":\"i6f6e65\"}\n{\"success\":true,\"id\":\"extra\"}",
	} {
		t.Run(name, func(t *testing.T) {
			driver, definition := fixtureDriver(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, response) })
			mutation, err := search.UpsertMutation(search.Document{ID: "one", Body: []byte(`{"active":true,"rank":1,"code":"x"}`)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := driver.Bulk(context.Background(), definition, []search.Mutation{mutation}); !errors.Is(err, search.ErrOutcomeUnknown) {
				t.Fatal(err)
			}
		})
	}
}

// TestSingleMutationResponseIntegrity prevents a body-less success or a removed collection from proving visibility.
func TestSingleMutationResponseIntegrity(t *testing.T) {
	driver, definition := fixtureDriver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" || r.Method == "GET" {
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, `{}`)
	})
	if _, err := driver.Upsert(context.Background(), definition, search.Document{ID: "one", Body: []byte(`{"active":true,"rank":1,"code":"x"}`)}); !errors.Is(err, search.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	if _, err := driver.Delete(context.Background(), definition, "one"); !errors.Is(err, search.ErrIndexNotFound) {
		t.Fatal(err)
	}
}

// TestConstructionAndReadinessFailures keeps invalid configuration, cancellation, and closure observable.
func TestConstructionAndReadinessFailures(t *testing.T) {
	for _, config := range []Config{{URL: "file:///tmp"}, {URL: "http://localhost", Prefix: "bad/prefix"}, {URL: "http://localhost", Prefix: strings.Repeat("a", 65)}} {
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

// TestInterruptedMixedBatchRetainsKnownItems preserves earlier synchronous success through later transport uncertainty.
func TestInterruptedMixedBatchRetainsKnownItems(t *testing.T) {
	driver, definition := fixtureDriver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			fmt.Fprint(w, `{"id":"i6f6e65"}`)
			return
		}
		w.WriteHeader(503)
	})
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		t.Fatal(err)
	}
	first, err := search.DeleteMutation("one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := search.UpsertMutation(search.Document{ID: "two", Body: []byte(`{"active":true,"rank":0,"code":"x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = index.Bulk(context.Background(), []search.Mutation{first, second})
	var unknown *search.UnknownOutcomeError
	if !errors.As(err, &unknown) {
		t.Fatal(err)
	}
	receipt, ok := unknown.Receipt()
	if !ok {
		t.Fatal("recoverable item outcomes lost")
	}
	outcome, err := index.Wait(context.Background(), receipt)
	if err != nil || outcome.State != search.OutcomeUnknown || outcome.Bulk == nil || outcome.Bulk.Items[0].Status != search.ItemSucceeded || outcome.Bulk.Items[1].Status != search.ItemUnknown {
		t.Fatal(outcome, err)
	}
}

// TestDeleteAcknowledgementIdentity prevents malformed successful deletes from proving visibility.
func TestDeleteAcknowledgementIdentity(t *testing.T) {
	for _, body := range []string{`{}`, `{"id":"other"}`} {
		driver, definition := fixtureDriver(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) })
		if _, err := driver.Delete(context.Background(), definition, "one"); !errors.Is(err, search.ErrOutcomeUnknown) {
			t.Fatal(err)
		}
		deletion, _ := search.DeleteMutation("one")
		if _, err := driver.Bulk(context.Background(), definition, []search.Mutation{deletion}); !errors.Is(err, search.ErrOutcomeUnknown) {
			t.Fatal(err)
		}
	}
}

// TestSearchRequiresCountEvidence rejects incomplete successful HTTP responses instead of reporting an empty index.
func TestSearchRequiresCountEvidence(t *testing.T) {
	driver, definition := fixtureDriver(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"hits":[]}`) })
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.Search(context.Background(), search.Query{}); !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
}

// TestSchemaNamesRejectMalformedKeys handles invalid native name types without using them as Go map keys.
func TestSchemaNamesRejectMalformedKeys(t *testing.T) {
	for name, badName := range map[string]any{"array": []string{"invalid"}, "object": map[string]string{"invalid": "name"}, "missing": nil, "duplicate": "duplicate"} {
		t.Run(name, func(t *testing.T) {
			var driver *Driver
			var definition search.Definition
			driver, definition = fixtureDriver(t, func(w http.ResponseWriter, _ *http.Request) {
				actual := schema(driver.name, definition)
				fields := actual["fields"].([]map[string]any)
				if name == "duplicate" {
					fields[1]["name"] = fields[0]["name"]
				} else {
					fields[0]["name"] = badName
				}
				json.NewEncoder(w).Encode(actual)
			})
			if err := driver.ValidateIndex(context.Background(), definition); !errors.Is(err, search.ErrSchemaDrift) {
				t.Fatal(err)
			}
		})
	}
}
