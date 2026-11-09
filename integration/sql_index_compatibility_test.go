//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/goforj/search"
	"github.com/goforj/search/driver/postgressearch"
	"github.com/goforj/search/driver/sqlitesearch"
)

// TestSQLScalarNullComplements verifies indexable predicates retain two-valued logic on both native engines.
func TestSQLScalarNullComplements(t *testing.T) {
	for _, engine := range []string{"postgres", "sqlite"} {
		t.Run(engine, func(t *testing.T) {
			db := sqlDatabase(t, engine)
			var backend administeredBackend
			var err error
			if engine == "postgres" {
				backend, err = postgressearch.New(postgressearch.Config{DB: db, Prefix: fixturePrefix(t)})
			} else {
				backend, err = sqlitesearch.New(sqlitesearch.Config{DB: db, Prefix: fixturePrefix(t)})
			}
			if err != nil {
				t.Fatal(err)
			}
			defer backend.Close()
			definition := search.Definition{Name: "scalar_complements", Fields: []search.Field{{Name: "number", Type: search.IntegerField, Filterable: true}}}
			ctx := context.Background()
			if err := backend.CreateIndex(ctx, definition); err != nil {
				t.Fatal(err)
			}
			index, err := search.NewIndex(backend, definition)
			if err != nil {
				t.Fatal(err)
			}
			for n, body := range []string{`{}`, `{"number":null}`, `{"number":1}`, `{"number":2}`, `{"number":3}`} {
				if _, err := index.Upsert(ctx, search.Document{ID: search.DocumentID(fmt.Sprint(n)), Body: []byte(body)}); err != nil {
					t.Fatal(err)
				}
			}
			one, _ := search.Integer(1)
			two, _ := search.Integer(2)
			for _, filter := range []search.Filter{
				search.Equal("number", two), search.In("number", two), search.Between("number", two, two),
				search.LessThan("number", two), search.LessThanOrEqual("number", one),
				search.GreaterThan("number", two), search.GreaterThanOrEqual("number", two),
			} {
				positive, err := index.Search(ctx, search.Query{Filter: filter, Limit: 10})
				if err != nil {
					t.Fatal(err)
				}
				negative, err := index.Search(ctx, search.Query{Filter: search.Not(filter), Limit: 10})
				if err != nil {
					t.Fatal(err)
				}
				if positive.Total.Value+negative.Total.Value != 5 || positive.Total.Value == 0 {
					t.Fatal("incomplete scalar complement", filter.View().Kind, positive.Total, negative.Total)
				}
				seen := map[search.DocumentID]bool{}
				for _, hit := range positive.Hits {
					if hit.Document.ID == "0" || hit.Document.ID == "1" {
						t.Fatal("missing/null scalar matched positive filter", hit.Document.ID)
					}
					seen[hit.Document.ID] = true
				}
				for _, hit := range negative.Hits {
					if seen[hit.Document.ID] {
						t.Fatal("positive and negative filters overlap", hit.Document.ID)
					}
				}
			}
			// The stored layout is authoritative for existing indexes; new DDL must not force a migration.
			sum := sha256.Sum256([]byte(definition.Name))
			table := "s_" + fixturePrefix(t) + hex.EncodeToString(sum[:12])
			query := "SELECT name FROM sqlite_master WHERE type='index' AND tbl_name=?1 AND name LIKE 's_ix_%'"
			if engine == "postgres" {
				query = "SELECT indexname FROM pg_indexes WHERE schemaname=current_schema() AND tablename=$1 AND indexname LIKE 's_ix_%'"
			}
			rows, err := db.QueryContext(ctx, query, table)
			if err != nil {
				t.Fatal(err)
			}
			var indexes []string
			for rows.Next() {
				var name string
				if err := rows.Scan(&name); err != nil {
					t.Fatal(err)
				}
				indexes = append(indexes, name)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			if len(indexes) == 0 {
				t.Fatal("expected scalar indexes")
			}
			for _, name := range indexes {
				if _, err := db.ExecContext(ctx, "DROP INDEX "+name); err != nil {
					t.Fatal(err)
				}
			}
			layout := legacySQLLayout(t, db, engine, table)
			update := "UPDATE " + table + "_meta SET layout=?1"
			if engine == "postgres" {
				update = "UPDATE " + table + "_meta SET layout=$1"
			}
			if _, err := db.ExecContext(ctx, update, layout); err != nil {
				t.Fatal(err)
			}
			if err := backend.CreateIndex(ctx, definition); err != nil {
				t.Fatal("legacy persisted schema rejected", err)
			}
			if _, err := index.Upsert(ctx, search.Document{ID: "5", Body: []byte(`{"number":2}`)}); err != nil {
				t.Fatal(err)
			}
			result, err := index.Search(ctx, search.Query{Filter: search.Equal("number", two)})
			if err != nil || result.Total.Value != 2 {
				t.Fatal("legacy index read/write failed", result.Total, err)
			}
			if err := backend.ValidateIndex(ctx, definition); err != nil {
				t.Fatal("legacy index changed during use", err)
			}
		})
	}
}

// legacySQLLayout records the pre-optimization physical schema with the released layout fingerprint format.
func legacySQLLayout(t *testing.T, db *sql.DB, engine, table string) string {
	t.Helper()
	query := `SELECT type||':'||name||':'||coalesce(sql,'') FROM sqlite_master WHERE tbl_name IN (?1,?2,?3,?4) ORDER BY type,name`
	args := []any{table, table + "_values", table + "_meta", table + "_fts"}
	if engine == "postgres" {
		query = `SELECT definition FROM (
   SELECT 'column:'||c.relname||':'||a.attname||':'||format_type(a.atttypid,a.atttypmod)||':'||a.attnotnull||':'||a.attcollation::text||':'||coalesce(pg_get_expr(ad.adbin,ad.adrelid),'') AS definition
   FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_attribute a ON a.attrelid=c.oid
   LEFT JOIN pg_attrdef ad ON ad.adrelid=c.oid AND ad.adnum=a.attnum
   WHERE n.nspname=current_schema() AND c.relname IN ($1,$2,$3) AND a.attnum>0 AND NOT a.attisdropped
   UNION ALL SELECT 'constraint:'||c.relname||':'||pg_get_constraintdef(p.oid) FROM pg_constraint p JOIN pg_class c ON c.oid=p.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace
   WHERE n.nspname=current_schema() AND c.relname IN ($1,$2,$3)
   UNION ALL SELECT 'index:'||tablename||':'||indexdef FROM pg_indexes WHERE schemaname=current_schema() AND tablename IN ($1,$2,$3)
   ) AS definitions ORDER BY definition COLLATE "C"`
		args = args[:3]
	}
	rows, err := db.QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	hash := sha256.New()
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(hash, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(hash.Sum(nil))
}
