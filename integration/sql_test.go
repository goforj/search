//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/goforj/search"
	"github.com/goforj/search/driver/postgressearch"
	"github.com/goforj/search/driver/sqlitesearch"
	"github.com/goforj/search/searchtest"
	"strings"
	"testing"
	"time"
)

// TestSQLiteLargeTextFacets prevents facet joins from repeating a full-text scan for each matching value.
func TestSQLiteLargeTextFacets(t *testing.T) {
	driver, err := sqlitesearch.New(sqlitesearch.Config{DB: sqlDatabase(t, "sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := driver.Close(); err != nil {
			t.Error(err)
		}
	})
	definition := search.Definition{Name: "largefacets", Fields: []search.Field{
		{Name: "title", Type: search.TextField, Searchable: true},
		{Name: "category", Type: search.KeywordField, Filterable: true, Facetable: true},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := driver.CreateIndex(ctx, definition); err != nil {
		t.Fatal(err)
	}
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		t.Fatal(err)
	}
	for start := 0; start < 10000; start += 1000 {
		mutations := make([]search.Mutation, 1000)
		for n := range mutations {
			category := "even"
			if n%2 != 0 {
				category = "odd"
			}
			mutation, err := search.UpsertMutation(search.Document{ID: search.DocumentID(fmt.Sprintf("document%d", start+n)), Body: []byte(fmt.Sprintf(`{"title":"portable cup","category":"%s"}`, category))})
			if err != nil {
				t.Fatal(err)
			}
			mutations[n] = mutation
		}
		result, err := index.Bulk(ctx, mutations)
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := index.Wait(ctx, result.Receipt)
		if err != nil || outcome.State != search.OutcomeSucceeded {
			t.Fatal(outcome, err)
		}
	}
	queryCtx, queryCancel := context.WithTimeout(ctx, 10*time.Second)
	defer queryCancel()
	category, _ := search.Keyword("even")
	result, err := index.Search(queryCtx, search.Query{Text: "portable", Fields: []search.FieldName{"title"}, Filter: search.Equal("category", category), Facets: []search.TermFacet{{Name: "categories", Field: "category", Limit: 8}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total.Value != 5000 || result.Total.Relation != search.TotalExact || len(result.Facets) != 1 || len(result.Facets[0].Buckets) != 1 || result.Facets[0].Buckets[0].Count != 5000 {
		t.Fatal("incomplete large-match facets", result.Total, result.Facets)
	}
	value, ok := result.Facets[0].Buckets[0].Value.Keyword()
	if !ok || value != "even" {
		t.Fatal("incorrect facet term")
	}
}

// TestSQLLifecycleAndDrift checks ownership, actual schema drift, and connection-pool lifetime on both SQL engines.
func TestSQLLifecycleAndDrift(t *testing.T) {
	for _, engine := range []string{"postgres", "sqlite"} {
		t.Run(engine, func(t *testing.T) {
			db := sqlDatabase(t, engine)
			var driver administeredBackend
			var err error
			if engine == "postgres" {
				driver, err = postgressearch.New(postgressearch.Config{DB: db, Prefix: fixturePrefix(t)})
			} else {
				driver, err = sqlitesearch.New(sqlitesearch.Config{DB: db, Prefix: fixturePrefix(t)})
			}
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			ctx := context.Background()
			definition := searchtest.Definition()
			if err := driver.ValidateIndex(ctx, definition); !errors.Is(err, search.ErrIndexNotFound) {
				t.Fatal(err)
			}
			if err := driver.CreateIndex(ctx, definition); err != nil {
				t.Fatal(err)
			}
			changed := definition
			changed.Fields = append(changed.Fields, search.Field{Name: "extra", Type: search.KeywordField})
			if err := driver.CreateIndex(ctx, changed); !errors.Is(err, search.ErrSchemaDrift) {
				t.Fatal(err)
			}
			query := "SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 's_%_meta'"
			if engine == "postgres" {
				query = "SELECT tablename FROM pg_tables WHERE schemaname=current_schema() AND tablename LIKE 's_" + fixturePrefix(t) + "%_meta'"
			}
			var table string
			if err := db.QueryRowContext(ctx, query).Scan(&table); err != nil {
				t.Fatal(err)
			}
			table = strings.TrimSuffix(table, "_meta")
			if _, err := db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN unexpected TEXT"); err != nil {
				t.Fatal(err)
			}
			if err := driver.ValidateIndex(ctx, definition); !errors.Is(err, search.ErrSchemaDrift) {
				t.Fatal(err)
			}
			if err := driver.Close(); err != nil {
				t.Fatal(err)
			}
			if err := db.PingContext(ctx); err != nil {
				t.Fatal("driver closed application pool")
			}
			if _, err := driver.Search(ctx, definition, search.Query{}); !errors.Is(err, search.ErrClosed) {
				t.Fatal(err)
			}
		})
	}
}

// TestSQLLongValuesAndRollback proves exact wide values and atomic rollback after a native row rejection.
func TestSQLLongValuesAndRollback(t *testing.T) {
	for _, engine := range []string{"postgres", "sqlite"} {
		t.Run(engine, func(t *testing.T) {
			db := sqlDatabase(t, engine)
			var driver administeredBackend
			var err error
			if engine == "postgres" {
				driver, err = postgressearch.New(postgressearch.Config{DB: db, Prefix: fixturePrefix(t)})
			} else {
				driver, err = sqlitesearch.New(sqlitesearch.Config{DB: db, Prefix: fixturePrefix(t)})
			}
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			definition := searchtest.Definition()
			if err := driver.CreateIndex(ctx, definition); err != nil {
				t.Fatal(err)
			}
			index, err := search.NewIndex(driver, definition, search.WithLimits(search.Limits{NameBytes: 2048}))
			if err != nil {
				t.Fatal(err)
			}
			id := search.DocumentID(strings.Repeat("i", 1800))
			code := strings.Repeat("keyword", 1000)
			body, _ := json.Marshal(map[string]any{"active": true, "rank": 1, "code": code})
			if _, err := index.Upsert(ctx, search.Document{ID: id, Body: body}); err != nil {
				t.Fatal(err)
			}
			keyword, err := search.Keyword(code)
			if err != nil {
				t.Fatal(err)
			}
			result, err := index.Search(ctx, search.Query{Filter: search.Equal("code", keyword)})
			if err != nil || len(result.Hits) != 1 || result.Hits[0].Document.ID != id {
				t.Fatal(result, err)
			}
			query := "SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 's_%_meta'"
			if engine == "postgres" {
				query = "SELECT tablename FROM pg_tables WHERE schemaname=current_schema() AND tablename LIKE 's_" + fixturePrefix(t) + "%_meta'"
			}
			var table string
			if err := db.QueryRowContext(ctx, query).Scan(&table); err != nil {
				t.Fatal(err)
			}
			table = strings.TrimSuffix(table, "_meta")
			statement := "CREATE TRIGGER reject_marker BEFORE INSERT ON " + table + " WHEN instr(new.body,'reject-marker')>0 BEGIN SELECT RAISE(ABORT,'private constraint failed'); END"
			if engine == "postgres" {
				statement = "ALTER TABLE " + table + " ADD CONSTRAINT reject_marker CHECK (body NOT LIKE '%reject-marker%')"
			}
			if _, err := db.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
			good, _ := search.UpsertMutation(search.Document{ID: "rolled-back", Body: []byte(`{"active":true,"rank":1,"code":"accepted"}`)})
			bad, _ := search.UpsertMutation(search.Document{ID: "rejected", Body: []byte(`{"active":true,"rank":1,"code":"reject-marker"}`)})
			if _, err := index.Bulk(ctx, []search.Mutation{good, bad}); !errors.Is(err, search.ErrInvalidDocument) {
				t.Fatal(err)
			}
			result, err = index.Search(ctx, search.Query{})
			if err != nil || result.Total.Value != 1 || result.Hits[0].Document.ID != id {
				t.Fatal("transaction leaked a successful prefix", result, err)
			}
		})
	}
}

// TestSQLCorruptStoredBody fails safely when stored JSON no longer agrees with declared highlight types.
func TestSQLCorruptStoredBody(t *testing.T) {
	for _, engine := range []string{"postgres", "sqlite"} {
		t.Run(engine, func(t *testing.T) {
			db := sqlDatabase(t, engine)
			var driver administeredBackend
			var err error
			if engine == "postgres" {
				driver, err = postgressearch.New(postgressearch.Config{DB: db, Prefix: fixturePrefix(t)})
			} else {
				driver, err = sqlitesearch.New(sqlitesearch.Config{DB: db, Prefix: fixturePrefix(t)})
			}
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			definition := searchtest.Definition()
			if err := driver.CreateIndex(ctx, definition); err != nil {
				t.Fatal(err)
			}
			index, err := search.NewIndex(driver, definition)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := index.Upsert(ctx, search.Document{ID: "one", Body: []byte(`{"title":"blue mug","active":true,"rank":1,"code":"x"}`)}); err != nil {
				t.Fatal(err)
			}
			query := "SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 's_%_meta'"
			if engine == "postgres" {
				query = "SELECT tablename FROM pg_tables WHERE schemaname=current_schema() AND tablename LIKE 's_" + fixturePrefix(t) + "%_meta'"
			}
			var table string
			if err := db.QueryRowContext(ctx, query).Scan(&table); err != nil {
				t.Fatal(err)
			}
			table = strings.TrimSuffix(table, "_meta")
			placeholder := "?1"
			if engine == "postgres" {
				placeholder = "$1"
			}
			if _, err := db.ExecContext(ctx, "UPDATE "+table+" SET body="+placeholder, `{"title":7,"active":true,"rank":1,"code":"x"}`); err != nil {
				t.Fatal(err)
			}
			if _, err := index.Search(ctx, search.Query{Text: "blue", Highlight: []search.Highlight{{Field: "title"}}}); !errors.Is(err, search.ErrUnavailable) {
				t.Fatal(err)
			}
		})
	}
}
