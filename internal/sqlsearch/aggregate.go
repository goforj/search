package sqlsearch

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// aggregateStatement shares text matching without staging filter-only scans or document bodies.
func (c *compiler) aggregateStatement(def search.Definition, query search.Query, from, where string) string {
	prefix := ""
	if query.Text != "" {
		requested := make(map[search.FieldName]bool, len(query.Facets))
		for _, facet := range query.Facets {
			requested[facet.Field] = true
		}
		columns := []string{"d.owner"}
		for _, field := range def.Fields {
			if requested[field.Name] && field.Type != search.KeywordsField {
				columns = append(columns, "d."+fieldKey(field.Name))
			}
		}
		selection := "SELECT " + strings.Join(columns, ",") + " FROM " + from + " WHERE " + where
		if !c.driver.postgres {
			// An unbounded LIMIT prevents SQLite from flattening the CTE without requiring newer MATERIALIZED syntax.
			selection += " LIMIT -1"
		}
		prefix = "WITH matches AS (" + selection + ") "
		from, where = "matches d", "1=1"
	}
	// Filter-only aggregates retain independent index access instead of staging every matching row.
	statements := []string{"SELECT -1 AS section,'' AS value,count(*) AS amount FROM " + from + " WHERE " + where}
	for n, facet := range query.Facets {
		statement := c.facet(def, facet, from, where)
		statements = append(statements, "SELECT "+strconv.Itoa(n)+" AS section,f.value,f.amount FROM ("+statement+") f")
	}
	return prefix + strings.Join(statements, " UNION ALL ")
}

// aggregates keeps candidate materialization inside the database instead of retaining every matching document in Go.
func (d *Driver) aggregates(ctx context.Context, tx *sql.Tx, def search.Definition, query search.Query, c compiler, from, where string) (search.Total, []search.FacetResult, error) {
	// SQLite expands CTE references while preparing compound queries, so raised facet limits need bounded groups.
	const batchSize = 32
	if d.postgres || len(query.Facets) <= batchSize {
		return d.aggregateBatch(ctx, tx, def, query, c, from, where)
	}
	var total search.Total
	facets := make([]search.FacetResult, 0, len(query.Facets))
	for start := 0; start < len(query.Facets); start += batchSize {
		part := query
		part.Facets = query.Facets[start:min(start+batchSize, len(query.Facets))]
		current, group, err := d.aggregateBatch(ctx, tx, def, part, c, from, where)
		if err != nil {
			return search.Total{}, nil, err
		}
		if start > 0 && total != current {
			return search.Total{}, nil, search.ErrUnavailable
		}
		total = current
		facets = append(facets, group...)
	}
	return total, facets, nil
}

// aggregateBatch bounds native compiler work while keeping exact totals and bucket counts in the transaction snapshot.
func (d *Driver) aggregateBatch(ctx context.Context, tx *sql.Tx, def search.Definition, query search.Query, c compiler, from, where string) (search.Total, []search.FacetResult, error) {
	c.args = append([]any(nil), c.args...)
	statement := c.aggregateStatement(def, query, from, where)
	rows, err := tx.QueryContext(ctx, statement, c.args...)
	if err != nil {
		return search.Total{}, nil, classify(err, false)
	}
	defer rows.Close()
	counts := make([]map[string]uint64, len(query.Facets))
	for n := range counts {
		counts[n] = map[string]uint64{}
	}
	total := search.Total{Relation: search.TotalExact}
	seen := false
	for rows.Next() {
		var section int
		var value string
		var count uint64
		if err := rows.Scan(&section, &value, &count); err != nil {
			return search.Total{}, nil, classify(err, false)
		}
		if section == -1 {
			if seen || value != "" {
				return search.Total{}, nil, search.ErrUnavailable
			}
			total.Value, seen = count, true
			continue
		}
		if section < 0 || section >= len(counts) || count == 0 {
			return search.Total{}, nil, search.ErrUnavailable
		}
		if _, exists := counts[section][value]; exists {
			return search.Total{}, nil, search.ErrUnavailable
		}
		counts[section][value] = count
	}
	if err := rows.Err(); err != nil {
		return search.Total{}, nil, classify(err, false)
	}
	if !seen {
		return search.Total{}, nil, search.ErrUnavailable
	}
	facets := make([]search.FacetResult, 0, len(query.Facets))
	for n, request := range query.Facets {
		facet, err := driverutil.Facet(request, def, counts[n], search.CountExact)
		if err != nil {
			return search.Total{}, nil, err
		}
		facets = append(facets, facet)
	}
	return total, facets, nil
}
