//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/goforj/search"
	"github.com/goforj/search/driver/postgressearch"
	"github.com/goforj/search/driver/sqlitesearch"
	"github.com/goforj/search/searchtest"
	"strings"
	"testing"
	"time"
)

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
