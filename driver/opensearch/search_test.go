package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
	"net/http"
	"testing"
)

// TestSearchProtocol verifies request policy and every stored-hit and facet decoding boundary.
func TestSearchProtocol(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		change func(map[string]any)
		want   error
	}{
		{name: "text", text: "RED"},
		{name: "browse"},
		{name: "lower-bound", change: func(r map[string]any) { r["hits"].(map[string]any)["total"].(map[string]any)["relation"] = "gte" }},
		{name: "missing-identity", change: func(r map[string]any) {
			delete(r["hits"].(map[string]any)["hits"].([]any)[0].(map[string]any)["_source"].(map[string]any), "_forjoriginal")
		}, want: search.ErrUnavailable},
		{name: "wrong-index", change: func(r map[string]any) {
			r["hits"].(map[string]any)["hits"].([]any)[0].(map[string]any)["_index"] = "foreign"
		}, want: search.ErrUnavailable},
		{name: "invalid-body", change: func(r map[string]any) {
			r["hits"].(map[string]any)["hits"].([]any)[0].(map[string]any)["_source"].(map[string]any)["_forjbody"] = "[]"
		}, want: search.ErrUnavailable},
		{name: "missing-aggregation", change: func(r map[string]any) { delete(r, "aggregations") }, want: search.ErrUnavailable},
		{name: "corrupt-bucket", change: func(r map[string]any) {
			r["aggregations"].(map[string]any)["categories"].(map[string]any)["buckets"].([]any)[0].(map[string]any)["key"] = "invalid"
		}, want: search.ErrUnavailable},
		{name: "estimated", change: func(r map[string]any) {
			r["aggregations"].(map[string]any)["categories"].(map[string]any)["doc_count_error_upper_bound"] = 1
		}},
		{name: "bucket-estimated", change: func(r map[string]any) {
			r["aggregations"].(map[string]any)["categories"].(map[string]any)["buckets"].([]any)[0].(map[string]any)["doc_count_error_upper_bound"] = 1
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var d *Driver
			var request map[string]any
			d, def := fixtureDriver(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Errorf("method %s", r.Method)
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				response := map[string]any{
					"_shards":      map[string]any{"total": 1, "successful": 1, "failed": 0},
					"hits":         map[string]any{"total": map[string]any{"value": 1, "relation": "eq"}, "hits": []any{map[string]any{"_id": "i6f6e65", "_index": d.name, "_score": 1.5, "_source": map[string]any{"_forjoriginal": "one", "_forjid": "i6f6e65", "_forjbody": `{"title":"red","active":true,"rank":1,"code":"a","category":"cup"}`}}}},
					"aggregations": map[string]any{"categories": map[string]any{"buckets": []any{map[string]any{"key": "v637570", "doc_count": 1}}, "sum_other_doc_count": 2}},
				}
				if tc.change != nil {
					tc.change(response)
				}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Error(err)
				}
			})
			q := search.Query{Text: tc.text, Fields: []search.FieldName{"title"}, Limit: 1, Facets: []search.TermFacet{{Name: "categories", Field: "category", Limit: 1}}}
			if tc.text == "" {
				q.Sort = []search.Sort{{Field: "rank", Direction: search.Descending}, {Field: "code", Direction: search.Ascending}}
			}
			got, err := d.Search(context.Background(), def, q)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if err != nil {
				return
			}
			if len(got.Hits) != 1 || got.Hits[0].Document.ID != "one" || (got.Hits[0].Score != nil) != (tc.text != "") || len(got.Facets) != 1 || !got.Facets[0].Truncated {
				t.Fatal(got)
			}
			if tc.name == "lower-bound" && got.Total.Relation != search.TotalLowerBound {
				t.Fatal(got.Total)
			}
			if (tc.name == "estimated" || tc.name == "bucket-estimated") && got.Facets[0].Precision != search.CountEstimated {
				t.Fatal(got.Facets)
			}
			if request["track_total_hits"] != true || request["size"] != float64(1) {
				t.Fatal(request)
			}
			sorts := request["sort"].([]any)
			if _, ok := sorts[len(sorts)-1].(map[string]any)["_forjid"]; !ok {
				t.Fatal(sorts)
			}
			if tc.text == "" && sorts[0].(map[string]any)[driverutil.FieldKey("rank")] != "desc" {
				t.Fatal(sorts)
			}
		})
	}
	for _, code := range []int{400, 503} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			d, def := fixtureDriver(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) })
			_, err := d.Search(context.Background(), def, search.Query{})
			want := search.ErrUnavailable
			if code == 400 {
				want = search.ErrInvalidQuery
			}
			if !errors.Is(err, want) {
				t.Fatal(err)
			}
		})
	}
	d, def := fixtureDriver(t, func(http.ResponseWriter, *http.Request) { t.Error("punctuation-only query reached network") })
	got, err := d.Search(context.Background(), def, search.Query{Text: "!!", Facets: []search.TermFacet{{Name: "categories", Field: "category", Limit: 1}}})
	if err != nil || len(got.Facets) != 1 || got.Total.Value != 0 {
		t.Fatal(got, err)
	}
}
