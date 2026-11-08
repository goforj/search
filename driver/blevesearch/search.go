package blevesearch

import (
	"context"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// Search compiles literal queries and typed filters without a backend query-string parser.
// @group Queries
func (d *Driver) Search(ctx context.Context, def search.Definition, q search.Query) (search.Result, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := d.ready(ctx, def); err != nil {
		return search.Result{}, err
	}
	parts := []query.Query{compileFilter(q.Filter)}
	terms := driverutil.Terms(q.Text)
	if q.Text != "" && len(terms) == 0 {
		parts = append(parts, bleve.NewMatchNoneQuery())
	}
	for _, t := range terms {
		alternatives := []query.Query{}
		for _, f := range q.Fields {
			m := bleve.NewMatchQuery(t)
			m.SetField(driverutil.FieldKey(f))
			alternatives = append(alternatives, m)
		}
		parts = append(parts, bleve.NewDisjunctionQuery(alternatives...))
	}
	req := bleve.NewSearchRequestOptions(bleve.NewConjunctionQuery(parts...), int(q.Limit), int(q.Offset), false)
	req.Fields = []string{"_forjbody", "_forjoriginal"}
	order := []string{}
	for _, s := range q.Sort {
		k := driverutil.FieldKey(s.Field)
		if s.Direction == search.Descending {
			k = "-" + k
		}
		order = append(order, k)
	}
	if len(order) == 0 && q.Text != "" {
		order = append(order, "-_score")
	}
	order = append(order, "_forjid")
	req.SortBy(order)
	for _, f := range q.Facets {
		req.AddFacet(f.Name, bleve.NewFacetRequest(driverutil.FieldKey(f.Field), int(f.Limit)+1))
	}
	raw, err := d.index.SearchInContext(ctx, req)
	if err != nil {
		return search.Result{}, search.SafeError(err)
	}
	if raw.Status.Failed > 0 {
		return search.Result{}, search.ErrUnavailable
	}
	out := search.Result{Total: search.Total{Value: raw.Total, Relation: search.TotalExact}}
	for _, hit := range raw.Hits {
		id, ok := hit.Fields["_forjoriginal"].(string)
		body, bok := hit.Fields["_forjbody"].(string)
		if !ok || !bok || driverutil.IDKey(search.DocumentID(id)) != hit.ID {
			return search.Result{}, search.ErrUnavailable
		}
		var score *float64
		if q.Text != "" {
			s := hit.Score
			score = &s
		}
		h, err := driverutil.Hit(id, body, def, q, score)
		if err != nil {
			return search.Result{}, err
		}
		out.Hits = append(out.Hits, h)
	}
	for _, f := range q.Facets {
		r := raw.Facets[f.Name]
		if r == nil {
			return search.Result{}, search.ErrUnavailable
		}
		fr := search.FacetResult{Name: f.Name, Field: f.Field, Precision: search.CountExact}
		var ft search.FieldType
		for _, field := range def.Fields {
			if field.Name == f.Field {
				ft = field.Type
			}
		}
		for _, bucket := range r.Terms.Terms() {
			v, e := driverutil.DecodeScalar(bucket.Term, ft)
			if e != nil {
				return search.Result{}, e
			}
			fr.Buckets = append(fr.Buckets, search.Bucket{Value: v, Count: uint64(bucket.Count)})
		}
		fr.Truncated = uint64(len(fr.Buckets)) > f.Limit || r.Other > 0
		if uint64(len(fr.Buckets)) > f.Limit {
			fr.Buckets = fr.Buckets[:f.Limit]
		}
		out.Facets = append(out.Facets, fr)
	}
	return out, nil
}
