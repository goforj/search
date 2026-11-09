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
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+from+" WHERE "+where, compiler.args...).Scan(&result.Total.Value); err != nil {
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
	arguments := append([]any(nil), compiler.args...)
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
	for _, facet := range query.Facets {
		c := compiler
		c.args = append([]any(nil), arguments...)
		field := c.parameter(fieldKey(facet.Field))
		limit := c.parameter(facet.Limit + 1)
		statement := "SELECT v.value,count(DISTINCT v.owner) FROM " + from + " JOIN " + d.name + "_values v ON v.owner=d.owner WHERE v.field=" + field + " AND (" + where + ") GROUP BY v.value ORDER BY count(DISTINCT v.owner) DESC,v.value ASC LIMIT " + limit
		if !d.postgres {
			// DISTINCT prevents SQLite from flattening the match set and repeating the FTS scan for each facet value.
			statement = "SELECT v.value,count(DISTINCT v.owner) FROM (SELECT DISTINCT d.owner FROM " + from + " WHERE " + where + ") d JOIN " + d.name + "_values v ON v.owner=d.owner WHERE v.field=" + field + " GROUP BY v.value ORDER BY count(DISTINCT v.owner) DESC,v.value ASC LIMIT " + limit
		}
		rows, err := tx.QueryContext(ctx, statement, c.args...)
		if err != nil {
			return search.Result{}, classify(err, false)
		}
		counts := map[string]uint64{}
		for rows.Next() {
			var key string
			var count uint64
			if err := rows.Scan(&key, &count); err != nil {
				rows.Close()
				return search.Result{}, classify(err, false)
			}
			counts[key] = count
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return search.Result{}, classify(err, false)
		}
		value, err := driverutil.Facet(facet, def, counts, search.CountExact)
		if err != nil {
			return search.Result{}, err
		}
		result.Facets = append(result.Facets, value)
	}
	return result, nil
}
