package driverutil

import (
	"github.com/goforj/search"
	"sort"
)

// Facet restores typed buckets in count-descending then portable-value order.
func Facet(f search.TermFacet, def search.Definition, counts map[string]uint64, p search.CountPrecision) (search.FacetResult, error) {
	var ft search.FieldType
	for _, field := range def.Fields {
		if field.Name == f.Field {
			ft = field.Type
		}
	}
	keys := []string{}
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	r := search.FacetResult{Name: f.Name, Field: f.Field, Precision: p, Truncated: uint64(len(keys)) > f.Limit}
	if uint64(len(keys)) > f.Limit {
		keys = keys[:f.Limit]
	}
	for _, k := range keys {
		v, e := DecodeScalar(k, ft)
		if e != nil {
			return search.FacetResult{}, e
		}
		r.Buckets = append(r.Buckets, search.Bucket{Value: v, Count: counts[k]})
	}
	return r, nil
}
