package search

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Query specifies literal all-terms text, typed filtering, and bounded pagination.
type Query struct {
	Text      string
	Fields    []FieldName
	Filter    Filter
	Sort      []Sort
	Facets    []TermFacet
	Highlight []Highlight
	Offset    uint64
	Limit     uint64
}

// Highlight requests untrusted text segments for one searchable field.
type Highlight struct {
	Field FieldName
}

// TermFacet counts distinct per-document terms before hit pagination.
type TermFacet struct {
	Name  string
	Field FieldName
	Limit uint64
}

// normalizeQuery resolves defaults and validates every operation against the declared schema.
func normalizeQuery(q Query, d Definition, l Limits) (Query, error) {
	if !utf8.ValidString(q.Text) || len(q.Text) > l.QueryBytes || len(strings.FieldsFunc(q.Text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })) > l.QueryTerms || len(q.Fields) > l.QueryFields || len(q.Facets) > l.Facets || len(q.Highlight) > l.HighlightFields {
		return Query{}, ErrInvalidQuery
	}
	fields := map[FieldName]Field{}
	for _, f := range d.Fields {
		fields[f.Name] = f
	}
	q.Fields = slices.Clone(q.Fields)
	q.Sort = slices.Clone(q.Sort)
	q.Facets = slices.Clone(q.Facets)
	q.Highlight = slices.Clone(q.Highlight)
	if len(q.Fields) == 0 {
		q.Fields = slices.Clone(d.DefaultSearchable)
	}
	if q.Text != "" && len(q.Fields) == 0 {
		return Query{}, ErrInvalidQuery
	}
	seen := map[FieldName]bool{}
	for _, name := range q.Fields {
		f, ok := fields[name]
		if !ok || !f.Searchable || seen[name] {
			return Query{}, ErrInvalidQuery
		}
		seen[name] = true
	}
	if len(q.Sort) == 0 && q.Text == "" {
		q.Sort = slices.Clone(d.DefaultSort)
	}
	if !validSort(q.Sort, fields) {
		return Query{}, ErrInvalidQuery
	}
	if q.Limit == 0 {
		q.Limit = min(20, l.ResultLimit)
	}
	if q.Limit > l.ResultLimit || q.Offset > l.ResultWindow || q.Limit > l.ResultWindow-q.Offset {
		return Query{}, ErrInvalidQuery
	}
	names := map[string]bool{}
	for i, f := range q.Facets {
		field, ok := fields[f.Field]
		if !ok || !field.Facetable || !validName(f.Name, l.FacetNameBytes) || names[f.Name] {
			return Query{}, ErrInvalidQuery
		}
		names[f.Name] = true
		if f.Limit == 0 {
			f.Limit = l.ResultLimit
		}
		if f.Limit > l.ResultLimit {
			return Query{}, ErrInvalidQuery
		}
		q.Facets[i] = f
	}
	seen = map[FieldName]bool{}
	for _, h := range q.Highlight {
		f, ok := fields[h.Field]
		if !ok || !f.Searchable || seen[h.Field] {
			return Query{}, ErrInvalidQuery
		}
		seen[h.Field] = true
	}
	nodes, budget := 0, 0
	if q.Filter.node != nil {
		if !validateFilter(q.Filter, fields, l, 1, &nodes, &budget) {
			return Query{}, ErrInvalidQuery
		}
	}
	b, _ := json.Marshal(q)
	if len(b)+budget > l.QueryBytes {
		return Query{}, ErrInvalidQuery
	}
	return q, nil
}

// validateFilter bounds the complete immutable tree and enforces two-valued typed leaves.
func validateFilter(f Filter, fields map[FieldName]Field, l Limits, depth int, nodes, budget *int) bool {
	*nodes += 1
	if f.node == nil || depth > l.FilterDepth || *nodes > l.FilterNodes {
		return false
	}
	n := f.node
	*budget += len(n.kind) + len(n.field) + 16
	if *budget > l.QueryBytes {
		return false
	}
	switch n.kind {
	case FilterAnd, FilterOr:
		for _, child := range n.children {
			if !validateFilter(child, fields, l, depth+1, nodes, budget) {
				return false
			}
		}
		return true
	case FilterNot:
		return len(n.children) == 1 && validateFilter(n.children[0], fields, l, depth+1, nodes, budget)
	}
	field, ok := fields[n.field]
	if !ok || !field.Filterable {
		return false
	}
	if n.kind == FilterExists {
		return true
	}
	if len(n.values) == 0 || len(n.values) > l.FilterValues {
		return false
	}
	array := n.kind == FilterContainsAny || n.kind == FilterContainsAll
	if array != (field.Type == KeywordsField) {
		return false
	}
	switch n.kind {
	case FilterEqual:
		if len(n.values) != 1 {
			return false
		}
	case FilterIn, FilterContainsAny, FilterContainsAll:
	case FilterBetween:
		if field.Type != IntegerField || len(n.values) != 2 || n.values[0].number > n.values[1].number {
			return false
		}
	case FilterLessThan, FilterLessThanOrEqual, FilterGreaterThan, FilterGreaterThanOrEqual:
		if field.Type != IntegerField || len(n.values) != 1 {
			return false
		}
	default:
		return false
	}
	bytes := 0
	for _, v := range n.values {
		bytes += len(v.text) + 16
		switch field.Type {
		case KeywordField, KeywordsField:
			if v.kind != KeywordValue {
				return false
			}
		case BooleanField:
			if v.kind != BooleanValue {
				return false
			}
		case IntegerField:
			if v.kind != IntegerValue {
				return false
			}
		default:
			return false
		}
	}
	*budget += bytes
	return bytes <= l.FilterValueBytes && *budget <= l.QueryBytes
}
