package blevesearch

import (
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// compileFilter preserves two-valued negation by anchoring complements to match-all.
func compileFilter(f search.Filter) query.Query {
	v := f.View()
	if v.Kind == "" {
		return bleve.NewMatchAllQuery()
	}
	k := driverutil.FieldKey(v.Field)
	term := func(value string) query.Query {
		q := bleve.NewTermQuery(value)
		q.SetField(k)
		return q
	}
	switch v.Kind {
	case search.FilterAnd, search.FilterOr:
		parts := []query.Query{}
		for _, c := range v.Children {
			parts = append(parts, compileFilter(c))
		}
		if v.Kind == search.FilterAnd {
			if len(parts) == 0 {
				return bleve.NewMatchAllQuery()
			}
			return bleve.NewConjunctionQuery(parts...)
		}
		if len(parts) == 0 {
			return bleve.NewMatchNoneQuery()
		}
		return bleve.NewDisjunctionQuery(parts...)
	case search.FilterNot:
		b := bleve.NewBooleanQuery()
		b.AddMust(bleve.NewMatchAllQuery())
		b.AddMustNot(compileFilter(v.Children[0]))
		return b
	case search.FilterExists:
		q := bleve.NewTermQuery("1")
		q.SetField(k + "_exists")
		return q
	case search.FilterEqual:
		return term(driverutil.Scalar(v.Values[0]))
	case search.FilterIn, search.FilterContainsAny, search.FilterContainsAll:
		parts := []query.Query{}
		for _, value := range v.Values {
			parts = append(parts, term(driverutil.Scalar(value)))
		}
		if v.Kind == search.FilterContainsAll {
			return bleve.NewConjunctionQuery(parts...)
		}
		return bleve.NewDisjunctionQuery(parts...)
	}
	lo, hi := "", ""
	includeLo, includeHi := true, true
	switch v.Kind {
	case search.FilterLessThan:
		hi = driverutil.Scalar(v.Values[0])
		includeHi = false
	case search.FilterLessThanOrEqual:
		hi = driverutil.Scalar(v.Values[0])
	case search.FilterGreaterThan:
		lo = driverutil.Scalar(v.Values[0])
		includeLo = false
	case search.FilterGreaterThanOrEqual:
		lo = driverutil.Scalar(v.Values[0])
	case search.FilterBetween:
		lo = driverutil.Scalar(v.Values[0])
		hi = driverutil.Scalar(v.Values[1])
	}
	r := bleve.NewTermRangeInclusiveQuery(lo, hi, &includeLo, &includeHi)
	r.SetField(k)
	return r
}
