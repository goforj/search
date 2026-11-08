package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
	"net/http"
	"strings"
	"testing"
)

// TestKeywordArrayBudgets validates every native term before any request can be submitted.
func TestKeywordArrayBudgets(t *testing.T) {
	d, def := fixtureDriver(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid projection reached transport") })
	for _, value := range []any{"wrong", []any{1}, []any{strings.Repeat("a", MaxKeywordBytes+1)}} {
		body, err := json.Marshal(map[string]any{"active": true, "rank": 1, "code": "a", "tags": value})
		if err != nil {
			t.Fatal(err)
		}
		doc := search.Document{ID: "one", Body: body}
		if _, err := physical(doc, def); !errors.Is(err, search.ErrInvalidDocument) {
			t.Fatal(err)
		}
		mutation, err := search.UpsertMutation(doc)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Bulk(context.Background(), def, []search.Mutation{mutation}); !errors.Is(err, search.ErrInvalidDocument) {
			t.Fatal(err)
		}
	}
	body := []byte(`{"active":true,"rank":1,"code":"a","tags":["one","two"]}`)
	if got, err := physical(search.Document{ID: "one", Body: body}, def); err != nil || len(got[driverutil.FieldKey("tags")].([]string)) != 2 {
		t.Fatal(got, err)
	}
}

// TestMutationTransportBoundaries checks deleted-index verification, malformed bulk acknowledgement, and lifecycle failures.
func TestMutationTransportBoundaries(t *testing.T) {
	for _, scenario := range []string{"missing-document", "missing-index", "delete-error", "bulk-error", "malformed-bulk"} {
		t.Run(scenario, func(t *testing.T) {
			d, def := fixtureDriver(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/_mapping") {
					if scenario == "missing-index" {
						w.WriteHeader(404)
						return
					}
					fmt.Fprint(w, `{}`)
					return
				}
				if scenario == "malformed-bulk" {
					fmt.Fprint(w, "invalid")
					return
				}
				if strings.HasPrefix(scenario, "missing-") {
					w.WriteHeader(404)
				} else {
					w.WriteHeader(503)
				}
			})
			var err error
			if strings.Contains(scenario, "bulk") {
				m, e := search.DeleteMutation("one")
				if e != nil {
					t.Fatal(e)
				}
				_, err = d.Bulk(context.Background(), def, []search.Mutation{m})
			} else {
				_, err = d.Delete(context.Background(), def, "one")
			}
			want := search.ErrOutcomeUnknown
			if scenario == "missing-document" {
				want = nil
			}
			if scenario == "missing-index" {
				want = search.ErrIndexNotFound
			}
			if !errors.Is(err, want) {
				t.Fatal(err, want)
			}
		})
	}
	d, def := fixtureDriver(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid lifecycle reached transport") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.ready(ctx, def); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	d.Close()
	if err := d.ready(context.Background(), def); !errors.Is(err, search.ErrClosed) {
		t.Fatal(err)
	}
	for _, call := range []func() error{func() error { _, err := d.Upsert(context.Background(), def, search.Document{}); return err }, func() error { _, err := d.Delete(context.Background(), def, "one"); return err }, func() error { _, err := d.Bulk(context.Background(), def, nil); return err }} {
		if err := call(); !errors.Is(err, search.ErrClosed) {
			t.Fatal(err)
		}
	}
	auth, err := New(Config{URL: "http://localhost", Username: "user", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	defer auth.Close()
	if auth.http.Authorization != "Basic dXNlcjpwYXNzd29yZA==" {
		t.Fatal("incorrect Basic authorization")
	}
}
