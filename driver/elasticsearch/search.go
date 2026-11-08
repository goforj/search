package elasticsearch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// Search rejects timed-out or failed-shard responses and retains native count precision.
// @group Queries
func (d *Driver) Search(ctx context.Context, definition search.Definition, query search.Query) (search.Result, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := d.ready(ctx, definition); err != nil {
		return search.Result{}, err
	}
	terms := driverutil.Terms(query.Text)
	if query.Text != "" && len(terms) == 0 {
		return driverutil.EmptyResult(query), nil
	}
	clauses := map[string]any{"filter": []any{compileFilter(query.Filter)}}
	if len(terms) > 0 {
		fields := []string{}
		for _, field := range query.Fields {
			fields = append(fields, driverutil.FieldKey(field))
		}
		clauses["must"] = []any{map[string]any{"multi_match": map[string]any{
			"query": strings.Join(terms, " "), "fields": fields, "type": "cross_fields", "operator": "and",
		}}}
	}
	sorts := []any{}
	for _, criterion := range query.Sort {
		direction := "asc"
		if criterion.Direction == search.Descending {
			direction = "desc"
		}
		sorts = append(sorts, map[string]any{driverutil.FieldKey(criterion.Field): direction})
	}
	if len(sorts) == 0 && query.Text != "" {
		sorts = append(sorts, map[string]any{"_score": "desc"})
	}
	sorts = append(sorts, map[string]any{"_forjid": "asc"})
	aggregations := map[string]any{}
	for _, facet := range query.Facets {
		aggregations[facet.Name] = map[string]any{"terms": map[string]any{
			"field": driverutil.FieldKey(facet.Field), "size": facet.Limit + 1, "shard_size": facet.Limit*2 + 50,
			"show_term_doc_count_error": true,
			"order":                     []any{map[string]any{"_count": "desc"}, map[string]any{"_key": "asc"}},
		}}
	}
	body := map[string]any{
		"query": map[string]any{"bool": clauses}, "sort": sorts,
		"from": query.Offset, "size": query.Limit, "track_total_hits": true,
		"aggs": aggregations,
	}
	var response struct {
		TimedOut bool        `json:"timed_out"`
		Shards   shardResult `json:"_shards"`
		Hits     struct {
			Total struct {
				Value    uint64
				Relation string
			}
			Hits []struct {
				ID     string                     `json:"_id"`
				Index  string                     `json:"_index"`
				Score  *float64                   `json:"_score"`
				Source map[string]json.RawMessage `json:"_source"`
			}
		}
		Aggregations map[string]struct {
			Error   int64  `json:"doc_count_error_upper_bound"`
			Other   uint64 `json:"sum_other_doc_count"`
			Buckets []struct {
				Key   string
				Count uint64 `json:"doc_count"`
				Error int64  `json:"doc_count_error_upper_bound"`
			}
		}
	}
	if err := d.http.Do(ctx, "POST", "/"+d.name+"/_search?allow_partial_search_results=false", body, &response, false); err != nil {
		var status *driverutil.StatusError
		if errors.As(err, &status) && status.Code == 400 {
			return search.Result{}, search.ErrInvalidQuery
		}
		return search.Result{}, err
	}
	if response.TimedOut || response.Shards.Failed != 0 || response.Shards.Total <= 0 || response.Shards.Successful != response.Shards.Total {
		return search.Result{}, search.ErrUnavailable
	}
	relation := search.TotalExact
	if response.Hits.Total.Relation == "gte" {
		relation = search.TotalLowerBound
	} else if response.Hits.Total.Relation != "eq" {
		return search.Result{}, search.ErrUnavailable
	}
	result := search.Result{Total: search.Total{Value: response.Hits.Total.Value, Relation: relation}}
	for _, hit := range response.Hits.Hits {
		var id, body, physicalID string
		if json.Unmarshal(hit.Source["_forjoriginal"], &id) != nil || json.Unmarshal(hit.Source["_forjbody"], &body) != nil || json.Unmarshal(hit.Source["_forjid"], &physicalID) != nil {
			return search.Result{}, search.ErrUnavailable
		}
		if hit.Index != d.name || hit.ID != driverutil.IDKey(search.DocumentID(id)) || physicalID != hit.ID {
			return search.Result{}, search.ErrUnavailable
		}
		score := hit.Score
		if query.Text == "" {
			score = nil
		}
		value, err := driverutil.Hit(id, body, definition, query, score)
		if err != nil {
			return search.Result{}, err
		}
		result.Hits = append(result.Hits, value)
	}
	for _, facet := range query.Facets {
		aggregation, ok := response.Aggregations[facet.Name]
		if !ok {
			return search.Result{}, search.ErrUnavailable
		}
		counts := map[string]uint64{}
		precision := search.CountExact
		if aggregation.Error != 0 {
			precision = search.CountEstimated
		}
		for _, bucket := range aggregation.Buckets {
			counts[bucket.Key] = bucket.Count
			if bucket.Error != 0 {
				precision = search.CountEstimated
			}
		}
		value, err := driverutil.Facet(facet, definition, counts, precision)
		if err != nil {
			return search.Result{}, err
		}
		value.Truncated = value.Truncated || aggregation.Other > 0
		result.Facets = append(result.Facets, value)
	}
	return result, nil
}
