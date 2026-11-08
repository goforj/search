package typesensesearch

import (
	"strconv"
	"strings"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// compileFilter distributes negation to leaves and explicitly includes absent values in complements.
func compileFilter(f search.Filter, negated bool) string {
	v := f.View()
	if v.Kind == "" {
		if negated {
			return "_forjid:=impossible"
		}
		return "_forjid:!=impossible"
	}
	if v.Kind == search.FilterNot {
		return compileFilter(v.Children[0], !negated)
	}
	if v.Kind == search.FilterAnd || v.Kind == search.FilterOr {
		and := v.Kind == search.FilterAnd
		if negated {
			and = !and
		}
		parts := []string{}
		for _, child := range v.Children {
			parts = append(parts, "("+compileFilter(child, negated)+")")
		}
		if len(parts) == 0 {
			if and {
				return "_forjid:!=impossible"
			}
			return "_forjid:=impossible"
		}
		separator := " || "
		if and {
			separator = " && "
		}
		return strings.Join(parts, separator)
	}
	key := driverutil.FieldKey(v.Field)
	if v.Kind == search.FilterExists {
		if negated {
			return key + "_exists:=0"
		}
		return key + "_exists:=1"
	}
	atom := func(value search.Value, op string) string {
		s := driverutil.Scalar(value)
		if value.Kind() == search.IntegerValue {
			n, _ := value.Integer()
			s = strconv.FormatInt(n, 10)
		}
		return key + ":" + op + s
	}
	if v.Kind == search.FilterIn || v.Kind == search.FilterContainsAny || v.Kind == search.FilterContainsAll {
		and := v.Kind == search.FilterContainsAll
		if negated {
			and = !and
		}
		parts := []string{}
		for _, value := range v.Values {
			leaf := search.Equal(v.Field, value)
			parts = append(parts, "("+compileFilter(leaf, negated)+")")
		}
		separator := " || "
		if and {
			separator = " && "
		}
		return strings.Join(parts, separator)
	}
	if v.Kind == search.FilterBetween {
		if negated {
			return "(" + key + "_exists:=0 || " + atom(v.Values[0], "<") + " || " + atom(v.Values[1], ">") + ")"
		}
		return "(" + atom(v.Values[0], ">=") + " && " + atom(v.Values[1], "<=") + ")"
	}
	ops := map[search.FilterKind]string{
		search.FilterEqual: "=", search.FilterLessThan: "<", search.FilterLessThanOrEqual: "<=",
		search.FilterGreaterThan: ">", search.FilterGreaterThanOrEqual: ">=",
	}
	op := ops[v.Kind]
	if negated {
		op = map[string]string{"=": "!=", "<": ">=", "<=": ">", ">": "<=", ">=": "<"}[op]
		return "(" + key + "_exists:=0 || " + atom(v.Values[0], op) + ")"
	}
	return atom(v.Values[0], op)
}
