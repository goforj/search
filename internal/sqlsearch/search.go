package sqlsearch

import (
	"context"
	"database/sql"
	"strings"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// text builds literal all-term native predicates and a native relevance expression for selected fields.
func (c *compiler) text(query search.Query, terms []string) (string, string, string) {
	if len(terms) == 0 {
		return "1=1", "0", ""
	}
	if !c.driver.postgres {
		fields := make([]string, len(query.Fields))
		for n, field := range query.Fields {
			fields[n] = fieldKey(field)
		}
		words := make([]string, len(terms))
		for n, term := range terms {
			words[n] = `"` + term + `"`
		}
		expression := "{" + strings.Join(fields, " ") + "} : (" + strings.Join(words, " AND ") + ")"
		table := c.driver.name + "_fts"
		return table + " MATCH " + c.parameter(expression), "-bm25(" + table + ")", " JOIN " + table + " ON " + table + ".id=d.id"
	}
	predicates, scores := []string{}, []string{}
	for _, term := range terms {
		alternatives := []string{}
		value := c.parameter(term)
		for _, field := range query.Fields {
			vector := "d." + fieldKey(field) + "_ts"
			tsquery := "plainto_tsquery('simple'," + value + ")"
			alternatives = append(alternatives, vector+" @@ "+tsquery)
			scores = append(scores, "ts_rank("+vector+","+tsquery+")")
		}
		predicates = append(predicates, "("+strings.Join(alternatives, " OR ")+")")
	}
	return strings.Join(predicates, " AND "), strings.Join(scores, "+"), ""
}

// Search computes hits, totals, and exact pre-pagination facets from one repeatable database snapshot.
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
	options := &sql.TxOptions{}
	if d.postgres {
		options.Isolation = sql.LevelRepeatableRead
		options.ReadOnly = true
	}
	tx, err := d.db.BeginTx(ctx, options)
	if err != nil {
		return search.Result{}, classify(err, false)
	}
	defer tx.Rollback()
	compiler := compiler{driver: d}
	predicate := compiler.filter(query.Filter)
	text, score, join := compiler.text(query, terms)
	where := "(" + predicate + ") AND (" + text + ")"
	from := d.name + " d" + join
	result := search.Result{Total: search.Total{Relation: search.TotalExact}}
	if len(query.Facets) > 0 {
		result.Total, result.Facets, err = d.aggregates(ctx, tx, def, query, compiler, from, where)
		if err != nil {
			return search.Result{}, err
		}
	} else if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+from+" WHERE "+where, compiler.args...).Scan(&result.Total.Value); err != nil {
		return search.Result{}, classify(err, false)
	}
	order := []string{}
	for _, criterion := range query.Sort {
		direction := "ASC"
		if criterion.Direction == search.Descending {
			direction = "DESC"
		}
		order = append(order, "d."+fieldKey(criterion.Field)+" "+direction)
	}
	if len(order) == 0 && len(terms) > 0 {
		order = append(order, "score DESC")
	}
	order = append(order, "d.id ASC")
	limit, offset := compiler.parameter(query.Limit), compiler.parameter(query.Offset)
	statement := "SELECT d.original,d.body," + score + " AS score FROM " + from + " WHERE " + where + " ORDER BY " + strings.Join(order, ",") + " LIMIT " + limit + " OFFSET " + offset
	rows, err := tx.QueryContext(ctx, statement, compiler.args...)
	if err != nil {
		return search.Result{}, classify(err, false)
	}
	for rows.Next() {
		var id, body string
		var score float64
		if err := rows.Scan(&id, &body, &score); err != nil {
			rows.Close()
			return search.Result{}, classify(err, false)
		}
		var relevance *float64
		if len(terms) > 0 {
			relevance = &score
		}
		hit, err := driverutil.Hit(id, body, def, query, relevance)
		if err != nil {
			rows.Close()
			return search.Result{}, err
		}
		result.Hits = append(result.Hits, hit)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return search.Result{}, classify(err, false)
	}

	return result, nil
}

// facet groups scalar projections directly while retaining deduplicated document membership for arrays.
func (c *compiler) facet(def search.Definition, facet search.TermFacet, from, where string) string {
	for _, field := range def.Fields {
		if field.Name == facet.Field && field.Type != search.KeywordsField {
			key := "d." + fieldKey(field.Name)
			limit := c.parameter(facet.Limit + 1)
			return "SELECT " + key + " AS value,count(*) AS amount FROM " + from + " WHERE (" + where + ") AND " + key + " IS NOT NULL GROUP BY " + key + " ORDER BY count(*) DESC," + key + " ASC LIMIT " + limit
		}
	}
	field := c.parameter(fieldKey(facet.Field))
	limit := c.parameter(facet.Limit + 1)
	if !c.driver.postgres {
		// DISTINCT prevents flattening from repeating the FTS scan for each array value.
		return "SELECT v.value,count(DISTINCT v.owner) AS amount FROM (SELECT DISTINCT d.owner FROM " + from + " WHERE " + where + ") d JOIN " + c.driver.name + "_values v ON v.owner=d.owner WHERE v.field=" + field + " GROUP BY v.value ORDER BY count(DISTINCT v.owner) DESC,v.value ASC LIMIT " + limit
	}
	return "SELECT v.value,count(DISTINCT v.owner) AS amount FROM " + from + " JOIN " + c.driver.name + "_values v ON v.owner=d.owner WHERE v.field=" + field + " AND (" + where + ") GROUP BY v.value ORDER BY count(DISTINCT v.owner) DESC,v.value ASC LIMIT " + limit
}
