package redissearch

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// Search collects a bounded complete native match set before portable sorting and exact pre-page facets.
// @group Queries
func (d *Driver) Search(ctx context.Context, def search.Definition, query search.Query) (search.Result, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := d.ready(ctx, def); err != nil {
		return search.Result{}, err
	}
	terms := driverutil.Terms(query.Text)
	if query.Text != "" && len(terms) == 0 {
		return driverutil.EmptyResult(query), nil
	}
	native := compileFilter(query.Filter)
	if len(terms) > 0 {
		fields := make([]string, len(query.Fields))
		for n, field := range query.Fields {
			fields[n] = driverutil.FieldKey(field)
		}
		words := make([]string, len(terms))
		for n, term := range terms {
			words[n] = `"` + term + `"`
		}
		text := "@" + strings.Join(fields, "|") + ":(" + strings.Join(words, " ") + ")"
		if native == "*" {
			native = text
		} else {
			native = "(" + native + ") " + text
		}
	}
	value, err := d.command(ctx, false, "FT.SEARCH", d.name, native, "WITHSCORES", "VERBATIM", "NOSTOPWORDS", "RETURN", "2", "_forjoriginal", "_forjbody", "LIMIT", "0", strconv.Itoa(MaxMatches+1), "TIMEOUT", "60000", "DIALECT", "2")
	if err != nil {
		return search.Result{}, err
	}
	values, ok := value.([]any)
	if !ok || len(values) == 0 {
		return search.Result{}, search.ErrUnavailable
	}
	total, ok := values[0].(int64)
	if !ok || total < 0 {
		return search.Result{}, search.ErrUnavailable
	}
	if total > MaxMatches {
		return search.Result{}, search.ErrUnsupported
	}
	if len(values) != 1+3*int(total) {
		return search.Result{}, search.ErrUnavailable
	}
	hits := make([]search.Hit, 0, total)
	seen := map[search.DocumentID]bool{}
	for n := 1; n < len(values); n += 3 {
		key, ok := values[n].(string)
		if !ok {
			return search.Result{}, search.ErrUnavailable
		}
		scoreText, ok := values[n+1].(string)
		if !ok {
			return search.Result{}, search.ErrUnavailable
		}
		score, err := strconv.ParseFloat(scoreText, 64)
		if len(terms) > 0 && (err != nil || math.IsNaN(score) || math.IsInf(score, 0)) {
			return search.Result{}, search.ErrUnavailable
		}
		fields, err := pairs(values[n+2])
		if err != nil {
			return search.Result{}, err
		}
		id, ok := fields["_forjoriginal"].(string)
		if !ok {
			return search.Result{}, search.ErrUnavailable
		}
		body, ok := fields["_forjbody"].(string)
		if !ok || key != d.name+":doc:"+driverutil.IDKey(search.DocumentID(id)) || seen[search.DocumentID(id)] {
			return search.Result{}, search.ErrUnavailable
		}
		seen[search.DocumentID(id)] = true
		var relevance *float64
		if len(terms) > 0 {
			relevance = &score
		}
		hit, err := driverutil.Hit(id, body, def, query, relevance)
		if err != nil {
			return search.Result{}, err
		}
		hits = append(hits, hit)
	}
	result := search.Result{Total: search.Total{Value: uint64(total), Relation: search.TotalExact}}
	for _, facet := range query.Facets {
		counts := map[string]uint64{}
		for _, hit := range hits {
			object := driverutil.Object(hit.Document)
			for _, field := range def.Fields {
				if field.Name != facet.Field {
					continue
				}
				value, ok := driverutil.At(object, field.Path)
				if !ok {
					continue
				}
				values := []any{value}
				if field.Type == search.KeywordsField {
					values = value.([]any)
				}
				unique := map[string]bool{}
				for _, value := range values {
					unique[driverutil.Scalar(driverutil.NativeValue(value, field.Type))] = true
				}
				for key := range unique {
					counts[key]++
				}
			}
		}
		value, err := driverutil.Facet(facet, def, counts, search.CountExact)
		if err != nil {
			return search.Result{}, err
		}
		result.Facets = append(result.Facets, value)
	}
	sort.Slice(hits, func(i, j int) bool {
		for n, criterion := range query.Sort {
			a, b := driverutil.Scalar(hits[i].SortValues[n]), driverutil.Scalar(hits[j].SortValues[n])
			if a != b {
				if criterion.Direction == search.Descending {
					return a > b
				}
				return a < b
			}
		}
		if len(query.Sort) == 0 && len(terms) > 0 && *hits[i].Score != *hits[j].Score {
			return *hits[i].Score > *hits[j].Score
		}
		return hits[i].Document.ID < hits[j].Document.ID
	})
	start := int(query.Offset)
	if start > len(hits) {
		start = len(hits)
	}
	end := start + int(query.Limit)
	if end > len(hits) {
		end = len(hits)
	}
	result.Hits = hits[start:end]
	return result, nil
}
