package opensearch

import (
	"encoding/json"
	"github.com/goforj/search"
	"testing"
)

// TestFilterProtocol verifies typed clauses, empty-set identities, and encoded hostile values.
func TestFilterProtocol(t *testing.T) {
	keyword, _ := search.Keyword("a")
	one, _ := search.Integer(1)
	two, _ := search.Integer(2)
	cases := []struct {
		name   string
		filter search.Filter
		want   string
	}{
		{"empty", search.Filter{}, `{"match_all":{}}`},
		{"and-empty", search.And(), `{"match_all":{}}`},
		{"or-empty", search.Or(), `{"match_none":{}}`},
		{"equal", search.Equal("x", keyword), `{"term":{"f78":"v61"}}`},
		{"integer", search.Equal("x", one), `{"term":{"f78":1}}`},
		{"exists", search.Exists("x"), `{"exists":{"field":"f78_exists"}}`},
		{"in", search.In("x", keyword), `{"terms":{"f78":["v61"]}}`},
		{"any", search.ContainsAny("x", keyword), `{"terms":{"f78":["v61"]}}`},
		{"all", search.ContainsAll("x", keyword), `{"bool":{"filter":[{"term":{"f78":"v61"}}]}}`},
		{"between", search.Between("x", one, two), `{"range":{"f78":{"gte":1,"lte":2}}}`},
		{"less", search.LessThan("x", one), `{"range":{"f78":{"lt":1}}}`},
		{"less-equal", search.LessThanOrEqual("x", one), `{"range":{"f78":{"lte":1}}}`},
		{"greater", search.GreaterThan("x", one), `{"range":{"f78":{"gt":1}}}`},
		{"greater-equal", search.GreaterThanOrEqual("x", one), `{"range":{"f78":{"gte":1}}}`},
		{"and", search.And(search.Exists("x")), `{"bool":{"filter":[{"exists":{"field":"f78_exists"}}]}}`},
		{"or", search.Or(search.Exists("x")), `{"bool":{"minimum_should_match":1,"should":[{"exists":{"field":"f78_exists"}}]}}`},
		{"not", search.Not(search.Exists("x")), `{"bool":{"must":[{"match_all":{}}],"must_not":[{"exists":{"field":"f78_exists"}}]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(compileFilter(tc.filter))
			if err != nil || string(got) != tc.want {
				t.Fatalf("got %s, want %s: %v", got, tc.want, err)
			}
		})
	}
}
