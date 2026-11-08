package opensearch

import (
	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// nativeValue keeps integer comparisons numeric while encoding exact keyword and Boolean values safely.
func nativeValue(value search.Value) any {
	if value.Kind() == search.IntegerValue {
		number, _ := value.Integer()
		return number
	}
	return driverutil.Scalar(value)
}

// compileFilter builds typed native clauses without accepting a query-string program.
func compileFilter(filter search.Filter) map[string]any {
	view := filter.View()
	key := driverutil.FieldKey(view.Field)
	switch view.Kind {
	case "":
		return map[string]any{"match_all": map[string]any{}}
	case search.FilterAnd, search.FilterOr:
		children := []any{}
		for _, child := range view.Children {
			children = append(children, compileFilter(child))
		}
		if len(children) == 0 {
			if view.Kind == search.FilterAnd {
				return map[string]any{"match_all": map[string]any{}}
			}
			return map[string]any{"match_none": map[string]any{}}
		}
		if view.Kind == search.FilterAnd {
			return map[string]any{"bool": map[string]any{"filter": children}}
		}
		return map[string]any{"bool": map[string]any{"should": children, "minimum_should_match": 1}}
	case search.FilterNot:
		return map[string]any{"bool": map[string]any{
			"must":     []any{map[string]any{"match_all": map[string]any{}}},
			"must_not": []any{compileFilter(view.Children[0])},
		}}
	case search.FilterExists:
		return map[string]any{"exists": map[string]any{"field": key + "_exists"}}
	case search.FilterEqual:
		return map[string]any{"term": map[string]any{key: nativeValue(view.Values[0])}}
	case search.FilterIn, search.FilterContainsAny:
		values := []any{}
		for _, value := range view.Values {
			values = append(values, nativeValue(value))
		}
		return map[string]any{"terms": map[string]any{key: values}}
	case search.FilterContainsAll:
		children := []any{}
		for _, value := range view.Values {
			children = append(children, map[string]any{"term": map[string]any{key: nativeValue(value)}})
		}
		return map[string]any{"bool": map[string]any{"filter": children}}
	case search.FilterBetween:
		return map[string]any{"range": map[string]any{key: map[string]any{"gte": nativeValue(view.Values[0]), "lte": nativeValue(view.Values[1])}}}
	default:
		operators := map[search.FilterKind]string{
			search.FilterLessThan: "lt", search.FilterLessThanOrEqual: "lte",
			search.FilterGreaterThan: "gt", search.FilterGreaterThanOrEqual: "gte",
		}
		return map[string]any{"range": map[string]any{key: map[string]any{operators[view.Kind]: nativeValue(view.Values[0])}}}
	}
}
