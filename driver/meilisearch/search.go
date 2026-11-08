package meilisearch

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// Search disables progressive term dropping and makes declared field sorts primary.
// @group Queries
func (d *Driver) Search(ctx context.Context, def search.Definition, q search.Query) (search.Result, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if e := d.ready(ctx, def); e != nil {
		return search.Result{}, e
	}
	terms := driverutil.Terms(q.Text)
	if !withinTermBudget(terms) {
		return search.Result{}, search.ErrInvalidQuery
	}
	sorts := []string{}
	for _, s := range q.Sort {
		dir := "asc"
		if s.Direction == search.Descending {
			dir = "desc"
		}
		sorts = append(sorts, driverutil.FieldKey(s.Field)+":"+dir)
	}
	if len(sorts) > 0 || q.Text == "" {
		sorts = append(sorts, "_forjid:asc")
	}
	fields := []string{}
	for _, f := range q.Fields {
		fields = append(fields, driverutil.FieldKey(f))
	}
	facets := []string{}
	for _, f := range q.Facets {
		facets = append(facets, driverutil.FieldKey(f.Field))
	}
	quoted := make([]string, len(terms))
	for n, term := range terms {
		quoted[n] = "\"" + term + "\""
	}
	text := strings.Join(quoted, " ")
	if q.Text != "" && text == "" {
		return driverutil.EmptyResult(q), nil
	}
	body := map[string]any{
		"q":                text,
		"matchingStrategy": "all",
		"filter":           "_forjkind = document AND (" + compileFilter(q.Filter, false) + ")",
		"sort":             sorts,
		"offset":           q.Offset,
		"limit":            q.Limit,
		"facets":           facets,
		"showRankingScore": true,
	}
	if q.Text != "" {
		body["attributesToSearchOn"] = fields
	}

	var raw struct {
		ProcessingTimeMs   *uint64 `json:"processingTimeMs"`
		Hits               []map[string]json.RawMessage
		EstimatedTotalHits *uint64
		FacetDistribution  map[string]map[string]uint64
	}
	if e := d.http.Do(ctx, "POST", "/indexes/"+d.name+"/search", body, &raw, false); e != nil {
		return search.Result{}, e
	}
	if raw.ProcessingTimeMs == nil || *raw.ProcessingTimeMs >= nativeSearchCutoff || raw.EstimatedTotalHits == nil {
		return search.Result{}, search.ErrUnavailable
	}
	out := search.Result{Total: search.Total{Value: *raw.EstimatedTotalHits, Relation: search.TotalEstimated}}
	for _, hit := range raw.Hits {
		var id, stored, physicalID string
		_ = json.Unmarshal(hit["_forjoriginal"], &id)
		_ = json.Unmarshal(hit["_forjbody"], &stored)
		_ = json.Unmarshal(hit["id"], &physicalID)
		if driverutil.IDKey(search.DocumentID(id)) != physicalID {
			return search.Result{}, search.ErrUnavailable
		}
		var score *float64
		if text != "" {
			if err := json.Unmarshal(hit["_rankingScore"], &score); err != nil || score == nil {
				return search.Result{}, search.ErrUnavailable
			}
		}
		h, e := driverutil.Hit(id, stored, def, q, score)
		if e != nil {
			return search.Result{}, e
		}
		out.Hits = append(out.Hits, h)
	}
	for _, f := range q.Facets {
		// Meilisearch's native count cutoff does not preserve lexical ties; collect a complete bounded distribution instead.
		if len(raw.FacetDistribution[driverutil.FieldKey(f.Field)]) > MaxFacetValues {
			return search.Result{}, search.ErrUnsupported
		}
		fr, e := driverutil.Facet(f, def, raw.FacetDistribution[driverutil.FieldKey(f.Field)], search.CountExact)
		if e != nil {
			return search.Result{}, e
		}
		out.Facets = append(out.Facets, fr)
	}
	return out, nil
}

// withinTermBudget conservatively bounds scripts whose native word segmentation is finer than whitespace.
func withinTermBudget(terms []string) bool {
	count := 0
	for _, term := range terms {
		single := true
		runes := 0
		for _, r := range term {
			runes++
			if !unicode.In(r, unicode.Latin) && !unicode.IsDigit(r) {
				single = false
			}
		}
		if single {
			count++
		} else {
			count += runes
		}
		if count > 10 {
			return false
		}
	}
	return true
}
