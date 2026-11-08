package meilisearch

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
	for _, scenario := range []string{"text", "browse", "wrong-id", "corrupt-body", "corrupt-facet", "missing-score", "null-score", "invalid-score", "missing-total", "null-total", "transport", "too-many-facets"} {
		t.Run(scenario, func(t *testing.T) {
			d, def := fixtureDriver(t, func(w http.ResponseWriter, r *http.Request) {
				if scenario == "transport" {
					w.WriteHeader(503)
					return
				}

				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request["matchingStrategy"] != "all" || request["showRankingScore"] != true {
					t.Error(request)
				}
				if scenario != "browse" && request["q"] != `"red" "cup"` {
					t.Error(request)
				}
				hit := map[string]any{"id": "i6f6e65", "_forjoriginal": "one", "_forjbody": `{"title":"red cup","active":true,"rank":1,"code":"a","category":"cup"}`, "_rankingScore": 0.5}
				if scenario == "wrong-id" {
					hit["id"] = "foreign"
				}
				if scenario == "corrupt-body" {
					hit["_forjbody"] = "[]"
				}
				switch scenario {
				case "missing-score":
					delete(hit, "_rankingScore")
				case "null-score":
					hit["_rankingScore"] = nil
				case "invalid-score":
					hit["_rankingScore"] = "not a score"
				}
				counts := map[string]uint64{"v637570": 1}
				if scenario == "corrupt-facet" {
					counts = map[string]uint64{"invalid": 1}
				}
				if scenario == "too-many-facets" {
					for n := 0; n <= MaxFacetValues; n++ {
						counts[strings.Repeat("a", n+1)] = 1
					}
				}
				response := map[string]any{"processingTimeMs": 1, "estimatedTotalHits": 1, "hits": []any{hit}, "facetDistribution": map[string]any{driverutil.FieldKey("category"): counts}}
				if scenario == "missing-total" {
					delete(response, "estimatedTotalHits")
				}
				if scenario == "null-total" {
					response["estimatedTotalHits"] = nil
				}
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
			case "wrong-id", "corrupt-body", "corrupt-facet", "missing-score", "null-score", "invalid-score", "missing-total", "null-total", "transport":
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
