package sqlsearch

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/goforj/search"
)

// TestSharedFacetStatement evaluates native matching once for aggregate consumers without staging document bodies or relevance.
func TestSharedFacetStatement(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		d, def, _ := protocolFixture(t, postgres)
		c := compiler{driver: d}
		query := search.Query{Text: "red", Facets: []search.TermFacet{{Name: "categories", Field: "category", Limit: 1}, {Name: "tags", Field: "tags", Limit: 2}}}
		statement := c.aggregateStatement(def, query, "records d", "native_match_predicate")
		projection := strings.Split(statement, ") SELECT -1")[0]
		if strings.Count(statement, "native_match_predicate") != 1 || strings.Contains(projection, "body") || strings.Contains(projection, "score") || strings.Contains(projection, fieldKey("rank")) || len(c.args) != 3 {
			t.Fatal(statement, c.args)
		}
		if !strings.Contains(statement, "FROM matches") || !strings.Contains(statement, "UNION ALL") || !strings.Contains(statement, "SELECT 1 AS section") {
			t.Fatal(statement)
		}
	}
}

// TestFilterOnlyFacetStatement preserves native index choices when no expensive text match needs sharing.
func TestFilterOnlyFacetStatement(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		d, def, _ := protocolFixture(t, postgres)
		c := compiler{driver: d}
		query := search.Query{Facets: []search.TermFacet{{Name: "categories", Field: "category", Limit: 1}, {Name: "tags", Field: "tags", Limit: 2}}}
		statement := c.aggregateStatement(def, query, "records d", "native_filter")
		if strings.Contains(statement, "WITH matches") || strings.Count(statement, "native_filter") != 3 || len(c.args) != 3 {
			t.Fatal(statement, c.args)
		}
	}
}

// TestSharedFacetFrames rejects incomplete or contradictory aggregates instead of returning partial exact counts.
func TestSharedFacetFrames(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows [][]driver.Value
	}{
		{"no-total", [][]driver.Value{}},
		{"only-facet", [][]driver.Value{{int64(0), "v637570", int64(1)}}},
		{"duplicate-total", [][]driver.Value{{int64(-1), "", int64(1)}, {int64(-1), "", int64(1)}}},
		{"total-value", [][]driver.Value{{int64(-1), "private", int64(1)}}},
		{"negative-section", [][]driver.Value{{int64(-2), "", int64(1)}}},
		{"unknown-section", [][]driver.Value{{int64(1), "v637570", int64(1)}}},
		{"zero-bucket", [][]driver.Value{{int64(0), "v637570", int64(0)}}},
		{"negative-count", [][]driver.Value{{int64(-1), "", int64(-1)}}},
		{"duplicate-value", [][]driver.Value{{int64(0), "v637570", int64(1)}, {int64(0), "v637570", int64(1)}}},
		{"invalid-value", [][]driver.Value{{int64(-1), "", int64(1)}, {int64(0), "invalid", int64(1)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, def, s := protocolFixture(t, true)
			s.aggregateRows = tc.rows
			result, err := d.Search(context.Background(), def, search.Query{Limit: 1, Facets: []search.TermFacet{{Name: "categories", Field: "category", Limit: 1}}})
			if !errors.Is(err, search.ErrUnavailable) || strings.Contains(err.Error(), "private") || len(result.Hits) != 0 || len(result.Facets) != 0 {
				t.Fatal(result, err)
			}
		})
	}
}

// TestSQLiteAggregateBatches preserves raised facet limits and rejects partial or inconsistent batch results.
func TestSQLiteAggregateBatches(t *testing.T) {
	for _, mode := range []string{"valid", "call", "total"} {
		t.Run(mode, func(t *testing.T) {
			d, def, s := protocolFixture(t, false)
			s.aggregateRows = [][]driver.Value{{int64(-1), "", int64(1)}}
			if mode != "valid" {
				s.failAt, s.mode = 3, mode
			}
			query := search.Query{Text: "red"}
			for n := 0; n < 35; n++ {
				query.Facets = append(query.Facets, search.TermFacet{Name: fmt.Sprintf("facet%d", n), Field: "category", Limit: 1})
			}
			tx, err := d.db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			total, facets, err := d.aggregates(context.Background(), tx, def, query, compiler{driver: d}, d.name+" d", "1=1")
			if mode != "valid" {
				if !errors.Is(err, search.ErrUnavailable) || len(facets) != 0 || total.Value != 0 {
					t.Fatal(total, facets, err)
				}
				return
			}
			if err != nil || total.Value != 1 || len(facets) != 35 || s.calls != 3 {
				t.Fatal(total, len(facets), s.calls, err)
			}
			for n, facet := range facets {
				if facet.Name != query.Facets[n].Name || facet.Precision != search.CountExact {
					t.Fatal(n, facet)
				}
			}
		})
	}
}
