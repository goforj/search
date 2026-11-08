package typesensesearch

import (
	"context"
	"errors"
	"fmt"
	"github.com/goforj/search"
	"net/http"
	"strings"
	"testing"
)

// TestMutationProtocol verifies synchronous receipts, import item failures, and adjacent operation groups.
func TestMutationProtocol(t *testing.T) {
	for _, operation := range []string{"upsert", "delete", "bulk"} {
		for _, failure := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/failure-%v", operation, failure), func(t *testing.T) {
				d, def := fixtureDriver(t, func(w http.ResponseWriter, r *http.Request) {
					if failure && operation != "bulk" {
						w.WriteHeader(503)
						return
					}
					if strings.Contains(r.URL.Path, "/import") {
						fmt.Fprintf(w, "{\"success\":true,\"id\":\"i6f6e65\"}\n{\"success\":%v,\"id\":\"i74776f\"}\n", !failure)
						return
					}
					fmt.Fprint(w, `{"id":"i6f6e65"}`)
				})
				doc := search.Document{ID: "one", Body: []byte(`{"title":"red","active":true,"rank":1,"code":"a"}`)}
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
					del, e := search.DeleteMutation("one")
					if e != nil {
						t.Fatal(e)
					}
					batch, e := d.Bulk(context.Background(), def, []search.Mutation{first, second, del})
					receipt, err = batch.Receipt, e
					if e == nil && len(batch.Items) != 3 {
						t.Fatal(batch)
					}
				}
				if failure && operation != "bulk" {
					if !errors.Is(err, search.ErrOutcomeUnknown) {
						t.Fatal(err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				o, err := d.Wait(context.Background(), receipt)
				want := search.OutcomeSucceeded
				if failure {
					want = search.OutcomeFailed
				}
				if err != nil || o.State != want {
					t.Fatal(o, err)
				}
			})
		}
	}
	d, def := fixtureDriver(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid lifecycle used transport") })
	if _, err := d.Search(context.Background(), def, search.Query{Limit: 251}); !errors.Is(err, search.ErrInvalidQuery) {
		t.Fatal(err)
	}
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
}
