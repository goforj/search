package sqlsearch

import (
	"strings"
	"testing"

	"github.com/goforj/search"
)

// TestFacetProjection keeps scalar grouping independent of the membership table and preserves bound limits.
func TestFacetProjection(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		for _, kind := range []search.FieldType{search.KeywordField, search.BooleanField, search.KeywordsField} {
			c := compiler{driver: &Driver{postgres: postgres, name: "records"}}
			definition := search.Definition{Fields: []search.Field{{Name: "value", Type: kind}}}
			statement := c.facet(definition, search.TermFacet{Field: "value", Limit: 2}, "records d", "1=1")
			if !strings.Contains(statement, "LIMIT "+c.driver.bind(len(c.args))) || c.args[len(c.args)-1] != uint64(3) {
				t.Fatal(statement, c.args)
			}
			if kind == search.KeywordsField {
				if !strings.Contains(statement, "records_values") || len(c.args) != 2 {
					t.Fatal("missing array membership aggregation", statement)
				}
			} else if strings.Contains(statement, "records_values") || strings.Contains(statement, "DISTINCT") || !strings.Contains(statement, " IS NOT NULL") || len(c.args) != 1 {
				t.Fatal("scalar grouping must skip nulls without membership joins", statement)
			}
		}
	}
}
