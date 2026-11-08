package typesensesearch

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
	"net/http"
	"strings"
	"testing"
)

// TestSearchProtocol verifies successful native responses, literal request policy, and corrupted stored data.
func TestSearchProtocol(t *testing.T) {
	for _, scenario := range []string{"text", "browse", "wrong-id", "corrupt-body", "corrupt-facet", "transport"} {
		t.Run(scenario, func(t *testing.T) {
			d, def := fixtureDriver(t, func(w http.ResponseWriter, r *http.Request) {
				if scenario == "transport" {
					w.WriteHeader(503)
					return
				}

				params := r.URL.Query()
				if params.Get("prefix") != "false" || params.Get("num_typos") != "0" || params.Get("facet_strategy") != "exhaustive" {
					t.Error(params)
				}
				if scenario == "browse" {
					if params.Get("q") != "*" || !strings.HasPrefix(params.Get("sort_by"), driverutil.FieldKey("rank")+":desc") {
						t.Error(params)
					}
				} else if params.Get("q") != "red cup" {
					t.Error(params)
				}
				hit := map[string]any{"id": "i6f6e65", "_forjoriginal": "one", "_forjbody": `{"title":"red cup","active":true,"rank":1,"code":"a","category":"cup"}`}
				if scenario == "wrong-id" {
					hit["id"] = "foreign"
				}
				if scenario == "corrupt-body" {
					hit["_forjbody"] = "[]"
				}
				key := "v637570"
				if scenario == "corrupt-facet" {
					key = "invalid"
				}
				response := map[string]any{"found": 1, "hits": []any{map[string]any{"document": hit, "text_match": 1}}, "facet_counts": []any{map[string]any{"field_name": driverutil.FieldKey("category"), "counts": []any{map[string]any{"value": key, "count": 1}}}}}
				json.NewEncoder(w).Encode(response)

			})
			q := search.Query{Text: "red cup", Fields: []search.FieldName{"title"}, Limit: 2, Facets: []search.TermFacet{{Name: "categories", Field: "category", Limit: 1}}}
			if scenario == "browse" {
				q.Text = ""
				q.Sort = []search.Sort{{Field: "rank", Direction: search.Descending}, {Field: "code", Direction: search.Ascending}}
			}
			r, err := d.Search(context.Background(), def, q)
			want := error(nil)
			switch scenario {
			case "wrong-id", "corrupt-body", "corrupt-facet", "transport":
				want = search.ErrUnavailable
			case "too-many-facets":
				want = search.ErrUnsupported
			}
			if !errors.Is(err, want) {
				t.Fatal(err, want)
			}
			if err == nil && (len(r.Hits) != 1 || r.Hits[0].Document.ID != "one" || (r.Hits[0].Score != nil) != (q.Text != "") || len(r.Facets) != 1 || r.Facets[0].Buckets[0].Count != 1) {
				t.Fatal(r)
			}
		})
	}
	d, def := fixtureDriver(t, func(http.ResponseWriter, *http.Request) { t.Error("punctuation-only query used transport") })
	if r, err := d.Search(context.Background(), def, search.Query{Text: "!!!"}); err != nil || r.Total.Value != 0 {
		t.Fatal(r, err)
	}
}
