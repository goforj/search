package meilisearch

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// compileFilter emits only encoded field names and JSON-quoted scalars, pushing negation to leaves.
func compileFilter(f search.Filter, neg bool) string {
	v := f.View()
	if v.Kind == "" {
		if neg {
			return "_forjkind = impossible"
		}
		return "_forjkind = document"
	}
	if v.Kind == search.FilterNot {
		return compileFilter(v.Children[0], !neg)
	}
	if v.Kind == search.FilterAnd || v.Kind == search.FilterOr {
		and := v.Kind == search.FilterAnd
		if neg {
			and = !and
		}
		parts := []string{}
		for _, c := range v.Children {
			parts = append(parts, "("+compileFilter(c, neg)+")")
		}
		if len(parts) == 0 {
			if and {
				return "_forjkind = document"
			}
			return "_forjkind = impossible"
		}
		sep := " OR "
		if and {
			sep = " AND "
		}
		return strings.Join(parts, sep)
	}
	k := driverutil.FieldKey(v.Field)
	if v.Kind == search.FilterExists {
		if neg {
			return k + "_exists = 0"
		}
		return k + "_exists = 1"
	}
	atom := func(value search.Value, op string) string {
		var s string
		if value.Kind() == search.IntegerValue {
			n, _ := value.Integer()
			s = strconv.FormatInt(n, 10)
		} else {
			b, _ := json.Marshal(driverutil.Scalar(value))
			s = string(b)
		}
		return k + " " + op + " " + s
	}
	var expression string
	switch v.Kind {
	case search.FilterEqual:
		expression = atom(v.Values[0], "=")
	case search.FilterIn, search.FilterContainsAny, search.FilterContainsAll:
		parts := []string{}
		for _, value := range v.Values {
			parts = append(parts, atom(value, "="))
		}
		sep := " OR "
		if v.Kind == search.FilterContainsAll {
			sep = " AND "
		}
		expression = "(" + strings.Join(parts, sep) + ")"
	case search.FilterBetween:
		expression = "(" + atom(v.Values[0], ">=") + " AND " + atom(v.Values[1], "<=") + ")"
	default:
		ops := map[search.FilterKind]string{search.FilterLessThan: "<", search.FilterLessThanOrEqual: "<=", search.FilterGreaterThan: ">", search.FilterGreaterThanOrEqual: ">="}
		expression = atom(v.Values[0], ops[v.Kind])
	}
	if neg {
		return "(" + k + "_exists = 0 OR NOT (" + expression + "))"
	}
	return expression
}
