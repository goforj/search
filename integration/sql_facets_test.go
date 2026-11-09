//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/goforj/search"
	"github.com/goforj/search/driver/postgressearch"
	"github.com/goforj/search/driver/sqlitesearch"
)

// TestSQLMixedFacets preserves typed, pre-page counts across nullable scalars, wide keywords, and repeated array terms.
func TestSQLMixedFacets(t *testing.T) {
	for _, engine := range []string{"postgres", "sqlite"} {
		t.Run(engine, func(t *testing.T) {
			db := sqlDatabase(t, engine)
			var backend administeredBackend
			var err error
			if engine == "postgres" {
				backend, err = postgressearch.New(postgressearch.Config{DB: db, Prefix: fixturePrefix(t)})
			} else {
				backend, err = sqlitesearch.New(sqlitesearch.Config{DB: db, Prefix: fixturePrefix(t)})
			}
			if err != nil {
				t.Fatal(err)
			}
			defer backend.Close()
			definition := search.Definition{Name: "mixed_facets", Fields: []search.Field{
				{Name: "title", Type: search.TextField, Searchable: true},
				{Name: "code", Type: search.KeywordField, Facetable: true},
				{Name: "number", Type: search.IntegerField},
				{Name: "flag", Type: search.BooleanField, Facetable: true},
				{Name: "tags", Type: search.KeywordsField, Facetable: true},
				{Name: "rank", Type: search.IntegerField, Sortable: true, Required: true},
			}}
			ctx := context.Background()
			if err := backend.CreateIndex(ctx, definition); err != nil {
				t.Fatal(err)
			}
			index, err := search.NewIndex(backend, definition)
			if err != nil {
				t.Fatal(err)
			}
			wide := strings.Repeat("keyword", 1000)
			bodies := []string{
				`{"title":"portable","code":"alpha","number":0,"flag":false,"tags":["a","a","b"],"rank":1}`,
				`{"title":"portable","code":"alpha","number":0,"flag":false,"tags":["a"],"rank":2}`,
				`{"title":"portable","code":"beta","number":2,"flag":true,"tags":["b","b"],"rank":3}`,
				`{"title":"portable","code":null,"number":null,"flag":null,"tags":null,"rank":4}`,
				`{"title":"other","rank":5}`,
				fmt.Sprintf(`{"title":"portable","code":"%s","rank":6}`, wide),
			}
			for n, body := range bodies {
				if _, err := index.Upsert(ctx, search.Document{ID: search.DocumentID(fmt.Sprint(n)), Body: []byte(body)}); err != nil {
					t.Fatal(err)
				}
			}
			facets := []search.TermFacet{
				{Name: "codes", Field: "code", Limit: 8},
				{Name: "flags", Field: "flag", Limit: 8},
				{Name: "tags", Field: "tags", Limit: 8},
				{Name: "top", Field: "code", Limit: 1},
			}
			expected := []map[string]uint64{
				{`"alpha"`: 2, `"beta"`: 1, `"` + wide + `"`: 1},
				{"false": 2, "true": 1},
				{`"a"`: 2, `"b"`: 2},
				{`"alpha"`: 2},
			}
			for _, query := range []search.Query{
				{Text: "portable", Offset: 1, Limit: 1, Facets: facets},
				{Text: "portable", Offset: 1, Limit: 1, Facets: facets, Sort: []search.Sort{{Field: "rank", Direction: search.Descending}}},
				{Offset: 100, Limit: 1, Facets: facets},
				{Text: "absent", Facets: facets},
			} {
				result, err := index.Search(ctx, query)
				if err != nil {
					t.Fatal(err)
				}
				wantTotal, wantHits := uint64(5), 1
				if query.Text == "" {
					wantTotal, wantHits = 6, 0
				} else if query.Text == "absent" {
					wantTotal, wantHits = 0, 0
				}
				if result.Total.Value != wantTotal || result.Total.Relation != search.TotalExact || len(result.Hits) != wantHits || len(result.Facets) != len(facets) {
					t.Fatal(result.Total, len(result.Hits), len(result.Facets))
				}
				if len(query.Sort) > 0 && result.Hits[0].Document.ID != "3" {
					t.Fatal("incorrect sorted page", result.Hits)
				}
				for n, facet := range result.Facets {
					got := map[string]uint64{}
					for _, bucket := range facet.Buckets {
						encoded, err := json.Marshal(bucket.Value)
						if err != nil {
							t.Fatal(err)
						}
						got[string(encoded)] = bucket.Count
					}
					want := expected[n]
					if wantTotal == 0 {
						want = map[string]uint64{}
					}
					if !reflect.DeepEqual(got, want) || facet.Precision != search.CountExact || facet.Truncated != (n == 3 && wantTotal > 0) {
						t.Fatal("incorrect facet", facet.Name, got, facet.Truncated)
					}
				}
			}
			large, err := search.NewIndex(backend, definition, search.WithLimits(search.Limits{Facets: 256}))
			if err != nil {
				t.Fatal(err)
			}
			many := make([]search.TermFacet, 256)
			for n := range many {
				many[n] = search.TermFacet{Name: fmt.Sprintf("facet%d", n), Field: []search.FieldName{"code", "flag", "tags"}[n%3], Limit: 1}
			}
			for _, text := range []string{"", "portable"} {
				result, err := large.Search(ctx, search.Query{Text: text, Facets: many})
				want := uint64(6)
				if text != "" {
					want = 5
				}
				if err != nil || result.Total.Value != want || result.Total.Relation != search.TotalExact || len(result.Facets) != len(many) {
					t.Fatal("maximum supported facet composition failed", result.Total, len(result.Facets), err)
				}
				for n, facet := range result.Facets {
					if facet.Name != many[n].Name || facet.Precision != search.CountExact || !facet.Truncated || len(facet.Buckets) != 1 || facet.Buckets[0].Count != 2 {
						t.Fatal("incorrect facet in maximum composition", n, facet)
					}
				}
			}
		})
	}
}
