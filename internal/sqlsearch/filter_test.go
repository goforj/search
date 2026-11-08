package sqlsearch

import (
	"github.com/goforj/search"
	"strings"
	"testing"
)

// TestFilterProtocol verifies Boolean identities and bound argument counts for both dialects.
func TestFilterProtocol(t *testing.T) {
	keyword, _ := search.Keyword("';DROP TABLE records;--")
	one, _ := search.Integer(1)
	two, _ := search.Integer(2)
	cases := []struct {
		name      string
		filter    search.Filter
		contains  string
		arguments int
	}{
		{"empty", search.Filter{}, "1=1", 0},
		{"and-empty", search.And(), "1=1", 0},
		{"or-empty", search.Or(), "1=0", 0},
		{"and", search.And(search.Exists("x"), search.Exists("y")), " AND ", 0},
		{"or", search.Or(search.Exists("x"), search.Exists("y")), " OR ", 0},
		{"not", search.Not(search.Exists("x")), "NOT (", 0},
		{"any", search.ContainsAny("x", keyword, keyword), " OR ", 4},
		{"all", search.ContainsAll("x", keyword, keyword), " AND ", 4},
		{"in", search.In("x", keyword), " IN (", 1},
		{"between", search.Between("x", one, two), " BETWEEN ", 2},
		{"equal", search.Equal("x", keyword), "=", 1},
		{"less", search.LessThan("x", one), "<", 1},
		{"less-equal", search.LessThanOrEqual("x", one), "<=", 1},
		{"greater", search.GreaterThan("x", one), ">", 1},
		{"greater-equal", search.GreaterThanOrEqual("x", one), ">=", 1},
	}
	for _, postgres := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				d, def, s := protocolFixture(t, postgres)
				_ = def
				_ = s
				c := compiler{driver: d}
				q := c.filter(tc.filter)
				if !strings.Contains(q, tc.contains) || strings.Contains(q, "DROP") || len(c.args) != tc.arguments {
					t.Fatal(q, c.args)
				}
				if tc.arguments > 0 && !strings.Contains(q, d.bind(1)) {
					t.Fatal("missing bound placeholder", q)
				}
			})
		}
	}
}
