package typesensesearch

import (
	"github.com/goforj/search"
	"testing"
)

// TestFilterProtocol verifies exact filter syntax, De Morgan complements, and missing-value handling.
func TestFilterProtocol(t *testing.T) {
	keyword, _ := search.Keyword("a")
	one, _ := search.Integer(1)
	two, _ := search.Integer(2)
	cases := []struct {
		name   string
		filter search.Filter
		want   string
	}{
		{"empty", search.Filter{}, `_forjid:!=impossible`},
		{"and-empty", search.And(), `_forjid:!=impossible`},
		{"or-empty", search.Or(), `_forjid:=impossible`},
		{"equal", search.Equal("x", keyword), `f78:=v61`},
		{"integer", search.Equal("x", one), `f78:=1`},
		{"exists", search.Exists("x"), `f78_exists:=1`},
		{"in", search.In("x", keyword), `(f78:=v61)`},
		{"any", search.ContainsAny("x", keyword), `(f78:=v61)`},
		{"all", search.ContainsAll("x", keyword), `(f78:=v61)`},
		{"between", search.Between("x", one, two), `(f78:>=1 && f78:<=2)`},
		{"less", search.LessThan("x", one), `f78:<1`},
		{"less-equal", search.LessThanOrEqual("x", one), `f78:<=1`},
		{"greater", search.GreaterThan("x", one), `f78:>1`},
		{"greater-equal", search.GreaterThanOrEqual("x", one), `f78:>=1`},
		{"and", search.And(search.Exists("x"), search.Exists("y")), `(f78_exists:=1) && (f79_exists:=1)`},
		{"or", search.Or(search.Exists("x"), search.Exists("y")), `(f78_exists:=1) || (f79_exists:=1)`},
		{"not-exists", search.Not(search.Exists("x")), `f78_exists:=0`},
		{"not-empty", search.Not(search.Filter{}), `_forjid:=impossible`},
		{"not-range", search.Not(search.Between("x", one, two)), `(f78_exists:=0 || f78:<1 || f78:>2)`},
		{"not-and", search.Not(search.And(search.Exists("x"), search.Exists("y"))), `(f78_exists:=0) || (f79_exists:=0)`},
		{"not-all", search.Not(search.ContainsAll("x", keyword)), `((f78_exists:=0 || f78:!=v61))`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := compileFilter(tc.filter, false); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}
