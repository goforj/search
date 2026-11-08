package typesensesearch

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// Search disables implicit token dropping, fuzzy expansion, and prefix matching.
// @group Queries
func (d *Driver) Search(ctx context.Context, def search.Definition, q search.Query) (search.Result, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := d.ready(ctx, def); err != nil {
		return search.Result{}, err
	}
	if q.Limit > 250 || len(driverutil.Terms(q.Text)) > 64 {
		return search.Result{}, search.ErrInvalidQuery
	}
	terms := driverutil.Terms(q.Text)
	text := strings.Join(terms, " ")
	if q.Text != "" && text == "" {
		return driverutil.EmptyResult(q), nil
	}
	if text == "" {
		text = "*"
	}
	fields, facets, sorts := []string{}, []string{}, []string{}
	for _, f := range q.Fields {
		fields = append(fields, driverutil.FieldKey(f))
	}
	for _, f := range q.Facets {
		facets = append(facets, driverutil.FieldKey(f.Field))
	}
	for _, s := range q.Sort {
		direction := "asc"
		if s.Direction == search.Descending {
			direction = "desc"
		}
		sorts = append(sorts, driverutil.FieldKey(s.Field)+":"+direction)
	}
	if len(sorts) == 0 && q.Text != "" {
		sorts = append(sorts, "_text_match:desc")
	}
	sorts = append(sorts, "_forjid:asc")
	params := url.Values{
		"q": {text}, "query_by": {strings.Join(fields, ",")},
		"filter_by": {compileFilter(q.Filter, false)},
		"sort_by":   {strings.Join(sorts, ",")},
		"offset":    {strconv.FormatUint(q.Offset, 10)}, "limit": {strconv.FormatUint(q.Limit, 10)},
		"facet_by": {strings.Join(facets, ",")}, "max_facet_values": {"1601"},
		"facet_strategy": {"exhaustive"}, "facet_sample_percent": {"100"},
		"num_typos": {"0"}, "prefix": {"false"}, "drop_tokens_threshold": {"0"},
		"enable_overrides": {"false"}, "enable_synonyms": {"false"},
		"split_join_tokens": {"off"}, "exhaustive_search": {"false"},
	}
	var raw struct {
		Found        uint64
		SearchCutoff bool `json:"search_cutoff"`
		Hits         []struct {
			Document  map[string]json.RawMessage
			TextMatch float64 `json:"text_match"`
		}
		FacetCounts []struct {
			FieldName string `json:"field_name"`
			Counts    []struct {
				Value string
				Count uint64
			}
		} `json:"facet_counts"`
	}
	if err := d.http.Do(ctx, "GET", "/collections/"+d.name+"/documents/search?"+params.Encode(), nil, &raw, false); err != nil {
		return search.Result{}, err
	}
	if raw.SearchCutoff {
		return search.Result{}, search.ErrUnavailable
	}
	out := search.Result{Total: search.Total{Value: raw.Found, Relation: search.TotalExact}}
	for _, hit := range raw.Hits {
		var id, body, physicalID string
		_ = json.Unmarshal(hit.Document["_forjoriginal"], &id)
		_ = json.Unmarshal(hit.Document["_forjbody"], &body)
		_ = json.Unmarshal(hit.Document["id"], &physicalID)
		if driverutil.IDKey(search.DocumentID(id)) != physicalID {
			return search.Result{}, search.ErrUnavailable
		}
		var score *float64
		if q.Text != "" {
			s := hit.TextMatch
			score = &s
		}
		h, err := driverutil.Hit(id, body, def, q, score)
		if err != nil {
			return search.Result{}, err
		}
		out.Hits = append(out.Hits, h)
	}
	for _, f := range q.Facets {
		counts := map[string]uint64{}
		for _, facet := range raw.FacetCounts {
			if facet.FieldName == driverutil.FieldKey(f.Field) {
				for _, bucket := range facet.Counts {
					counts[bucket.Value] = bucket.Count
				}
			}
		}
		result, err := driverutil.Facet(f, def, counts, search.CountExact)
		if err != nil {
			return search.Result{}, err
		}
		out.Facets = append(out.Facets, result)
	}
	return out, nil
}
