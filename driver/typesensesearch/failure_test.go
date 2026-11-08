package typesensesearch

import (
	"context"
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
