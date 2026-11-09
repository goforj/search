package sqlsearch

import (
	"strings"
	"testing"

	"github.com/goforj/search"
)

// TestScalarIndexes keeps wide keywords out of PostgreSQL B-tree tuples while indexing bounded scalar ranges.
func TestScalarIndexes(t *testing.T) {
	definition := search.Definition{Fields: []search.Field{
		{Name: "text", Type: search.TextField},
		{Name: "tags", Type: search.KeywordsField, Filterable: true},
		{Name: "stored", Type: search.KeywordField},
		{Name: "keyword", Type: search.KeywordField, Filterable: true, Sortable: true},
		{Name: "wide", Type: search.KeywordField, Sortable: true},
		{Name: "number", Type: search.IntegerField, Filterable: true},
		{Name: "flag", Type: search.BooleanField, Sortable: true},
	}}
	for _, postgres := range []bool{false, true} {
		d := Driver{postgres: postgres}
		statements := strings.Join(d.ddl("s_records", definition), "\n")
		for _, field := range definition.Fields {
			key := fieldKey(field.Name)
			indexed := strings.Contains(statements, " ON s_records ("+key)
			want := field.Name == "keyword" || field.Name == "wide" || field.Name == "number" || field.Name == "flag"
			if postgres {
				want = field.Name == "number" || field.Name == "flag"
			}
			if indexed != want {
				t.Fatalf("postgres=%t field=%s direct index=%t, want %t", postgres, field.Name, indexed, want)
			}
			if postgres && strings.Contains(statements, "(md5("+key+"))") != field.Filterable && field.Type != search.KeywordsField {
				t.Fatalf("incorrect digest index for %s", field.Name)
			}
		}
		if strings.Contains(statements, " ON s_records (id)") == postgres {
			t.Fatal("wide document IDs must only use direct indexes on SQLite")
		}
	}
	name := indexName(strings.Repeat("x", 60), strings.Repeat("y", 100))
	if len(name) > 63 || name != indexName(strings.Repeat("x", 60), strings.Repeat("y", 100)) || name == indexName(strings.Repeat("x", 60), "other") {
		t.Fatal("native index names must be bounded, deterministic and distinct")
	}
}

// TestDigestFiltersRetainExactComparison prevents digest collisions from becoming false equality matches.
func TestDigestFiltersRetainExactComparison(t *testing.T) {
	value, _ := search.Keyword(strings.Repeat("wide", 2000))
	for _, filter := range []search.Filter{search.Equal("keyword", value), search.In("keyword", value, value)} {
		c := compiler{driver: &Driver{postgres: true}}
		statement := c.filter(filter)
		if !strings.Contains(statement, "md5(d."+fieldKey("keyword")+")") || !strings.Contains(statement, "d."+fieldKey("keyword")+" IS NOT NULL") || !strings.Contains(statement, "$1") || strings.Contains(statement, "wide") {
			t.Fatal(statement)
		}
		if !strings.Contains(statement, "d."+fieldKey("keyword")+"=") && !strings.Contains(statement, "d."+fieldKey("keyword")+" IN (") {
			t.Fatal("missing exact comparison", statement)
		}
	}
}
