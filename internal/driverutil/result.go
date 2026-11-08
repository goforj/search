package driverutil

import "github.com/goforj/search"

// EmptyResult preserves every requested facet when literal text compiles to no searchable tokens.
func EmptyResult(query search.Query) search.Result {
	result := search.Result{Total: search.Total{Relation: search.TotalExact}}
	for _, facet := range query.Facets {
		result.Facets = append(result.Facets, search.FacetResult{Name: facet.Name, Field: facet.Field, Precision: search.CountExact})
	}
	return result
}
