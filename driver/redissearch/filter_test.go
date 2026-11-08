package redissearch

import (
	"github.com/goforj/search"
	"testing"
)

// TestFilterProtocol verifies Redis query syntax and Boolean simplification against independent fixtures.
func TestFilterProtocol(t *testing.T) {
	keyword, _ := search.Keyword("a")
	one, _ := search.Integer(1)
	two, _ := search.Integer(2)
	cases := []struct {
		name   string
		filter search.Filter
		want   string
	}{
		{"empty", search.Filter{}, `*`},
		{"and-empty", search.And(), `*`},
		{"or-empty", search.Or(), `@_forjid:{none}`},
		{"and-false", search.And(search.Or(), search.Exists("x")), `@_forjid:{none}`},
		{"or-true", search.Or(search.And(), search.Exists("x")), `*`},
		{"and-identity", search.And(search.And(), search.Exists("x")), `((@f78_exists:{1}))`},
		{"or-identity", search.Or(search.Or(), search.Exists("x")), `((@f78_exists:{1}))`},
		{"not-true", search.Not(search.And()), `@_forjid:{none}`},
		{"not-false", search.Not(search.Or()), `*`},
		{"not", search.Not(search.Exists("x")), `-(@f78_exists:{1})`},
		{"exists", search.Exists("x"), `@f78_exists:{1}`},
		{"keyword", search.Equal("x", keyword), `@f78:{v61}`},
		{"integer", search.Equal("x", one), `@f78:[1 1]`},
		{"in", search.In("x", keyword), `(@f78:{v61})`},
		{"integer-in", search.In("x", one), `(@f78:[1 1])`},
		{"any", search.ContainsAny("x", keyword), `(@f78:{v61})`},
		{"all", search.ContainsAll("x", keyword), `(@f78:{v61})`},
		{"between", search.Between("x", one, two), `@f78:[1 2]`},
		{"less", search.LessThan("x", one), `@f78:[-inf (1]`},
		{"less-equal", search.LessThanOrEqual("x", one), `@f78:[-inf 1]`},
		{"greater", search.GreaterThan("x", one), `@f78:[(1 +inf]`},
		{"greater-equal", search.GreaterThanOrEqual("x", one), `@f78:[1 +inf]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := compileFilter(tc.filter); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}
