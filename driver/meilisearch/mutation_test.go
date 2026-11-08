package meilisearch

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/goforj/search"
	"net/http"
	"strings"
	"testing"
)

// TestMutationProtocol verifies submission acknowledgement and aggregate task evidence for every operation.
func TestMutationProtocol(t *testing.T) {
	for _, operation := range []string{"upsert", "delete", "bulk"} {
		for _, scenario := range []string{"success", "missing-task", "failure"} {
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				d, def := fixtureDriver(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "GET" {
						json.NewEncoder(w).Encode(map[string]any{"status": "succeeded"})
						return
					}
					if scenario == "failure" {
						w.WriteHeader(503)
						return
					}
					response := map[string]any{"taskUid": 7}
					if scenario == "missing-task" {
						delete(response, "taskUid")
					}
					json.NewEncoder(w).Encode(response)
				})
				doc := search.Document{ID: "one", Body: []byte(`{"active":true,"rank":1,"code":"a"}`)}
				var receipt search.Receipt
				var err error
				switch operation {
				case "upsert":
					receipt, err = d.Upsert(context.Background(), def, doc)
				case "delete":
					receipt, err = d.Delete(context.Background(), def, "one")
				case "bulk":
					first, e := search.UpsertMutation(doc)
					if e != nil {
						t.Fatal(e)
					}
					doc.ID = "two"
					second, e := search.UpsertMutation(doc)
					if e != nil {
						t.Fatal(e)
					}
					del, e := search.DeleteMutation("three")
					if e != nil {
						t.Fatal(e)
					}
					batch, e := d.Bulk(context.Background(), def, []search.Mutation{first, second, del})
					err, receipt = e, batch.Receipt
					if e == nil && (batch.Precision != search.BatchOnly || len(batch.Items) != 3) {
						t.Fatal(batch)
					}
				}
				if scenario != "success" {
					if !errors.Is(err, search.ErrOutcomeUnknown) {
						t.Fatal(err)
					}
					return
				}
				outcome, err := d.Wait(context.Background(), receipt)
				if err != nil || outcome.State != search.OutcomeSucceeded {
					t.Fatal(outcome, err)
				}
			})
		}
	}
	d, def := fixtureDriver(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid lifecycle used transport") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.ready(ctx, def); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := d.Wait(ctx, search.Receipt{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := d.Search(context.Background(), def, search.Query{Text: strings.Repeat("word ", 11)}); !errors.Is(err, search.ErrInvalidQuery) {
		t.Fatal(err)
	}
	d.Close()
	if err := d.ready(context.Background(), def); !errors.Is(err, search.ErrClosed) {
		t.Fatal(err)
	}
	for _, call := range []func() error{
		func() error { _, err := d.Upsert(context.Background(), def, search.Document{}); return err },
		func() error { _, err := d.Delete(context.Background(), def, "one"); return err },
		func() error { _, err := d.Bulk(context.Background(), def, nil); return err },
	} {
		if err := call(); !errors.Is(err, search.ErrClosed) {
			t.Fatal(err)
		}
	}
}

// TestTaskReceiptEviction bounds retention independently of the server's task history.
func TestTaskReceiptEviction(t *testing.T) {
	d, _ := fixtureDriver(t, func(http.ResponseWriter, *http.Request) { t.Error("expired receipt used network") })
	id := uint64(1)
	first, err := d.remember(&id, nil)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 4096; n++ {
		if _, err := d.remember(&id, nil); err != nil {
			t.Fatal(err)
		}
	}
	o, err := d.Wait(context.Background(), first)
	if err != nil || !errors.Is(o.Error, search.ErrReceiptExpired) || len(d.pending) != 4096 {
		t.Fatal(o, err)
	}
}
