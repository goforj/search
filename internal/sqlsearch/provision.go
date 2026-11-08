package sqlsearch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/goforj/search"
)

// ddl makes physical encodings explicit so database collation never changes portable ordering.
func (d *Driver) ddl(name string, def search.Definition) []string {
	collation := "BINARY"
	if d.postgres {
		collation = `"C"`
	}
	scalar := "TEXT COLLATE " + collation
	columns := []string{"id " + scalar + " NOT NULL", "owner TEXT PRIMARY KEY", "original TEXT NOT NULL", "body TEXT NOT NULL"}
	texts := []string{}
	for _, field := range def.Fields {
		key := fieldKey(field.Name)
		columns = append(columns, key+" "+scalar, key+"_exists INTEGER")
		if field.Type == search.TextField {
			texts = append(texts, key)
			if d.postgres {
				columns = append(columns, key+"_ts TSVECTOR GENERATED ALWAYS AS (to_tsvector('simple',coalesce("+key+",''))) STORED")
			}
		}
	}
	statements := []string{
		"CREATE TABLE " + name + " (" + strings.Join(columns, ",") + ")",
		"CREATE TABLE " + name + "_values (owner TEXT,field " + scalar + ",value " + scalar + ",digest TEXT,PRIMARY KEY(owner,field,digest))",
		"CREATE TABLE " + name + "_meta (id INTEGER PRIMARY KEY,fingerprint TEXT NOT NULL,layout TEXT NOT NULL)",
	}
	if d.postgres {
		for _, key := range texts {
			statements = append(statements, "CREATE INDEX "+name+"_g"+key+" ON "+name+" USING GIN ("+key+"_ts)")
		}
	} else {
		if len(texts) == 0 {
			texts = []string{"dummy"}
		}
		statements = append(statements, "CREATE VIRTUAL TABLE "+name+"_fts USING fts5(id UNINDEXED,"+strings.Join(texts, ",")+",tokenize='unicode61 remove_diacritics 0')")
	}
	return statements
}

// layout hashes materialized columns, constraints, indexes, and native FTS definitions independently of metadata.
func (d *Driver) layout(ctx context.Context, tx *sql.Tx, name string) (string, error) {
	var query string
	var args []any
	if d.postgres {
		query = `SELECT definition FROM (
   SELECT 'column:'||c.relname||':'||a.attname||':'||format_type(a.atttypid,a.atttypmod)||':'||a.attnotnull||':'||a.attcollation::text||':'||coalesce(pg_get_expr(ad.adbin,ad.adrelid),'') AS definition
   FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_attribute a ON a.attrelid=c.oid
   LEFT JOIN pg_attrdef ad ON ad.adrelid=c.oid AND ad.adnum=a.attnum
   WHERE n.nspname=current_schema() AND c.relname IN ($1,$2,$3) AND a.attnum>0 AND NOT a.attisdropped
   UNION ALL SELECT 'constraint:'||c.relname||':'||pg_get_constraintdef(p.oid) FROM pg_constraint p JOIN pg_class c ON c.oid=p.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace
   WHERE n.nspname=current_schema() AND c.relname IN ($1,$2,$3)
   UNION ALL SELECT 'index:'||tablename||':'||indexdef FROM pg_indexes WHERE schemaname=current_schema() AND tablename IN ($1,$2,$3)
   ) AS definitions ORDER BY definition COLLATE "C"`
		args = []any{name, name + "_values", name + "_meta"}
	} else {
		query = "SELECT type||':'||name||':'||coalesce(sql,'') FROM sqlite_master WHERE tbl_name IN (?1,?2,?3,?4) ORDER BY type,name"
		args = []any{name, name + "_values", name + "_meta", name + "_fts"}
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	hash := sha256.New()
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return "", err
		}
		fmt.Fprintln(hash, line)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// exists distinguishes absence from permission and transport failures before provisioning.
func (d *Driver) exists(ctx context.Context, tx *sql.Tx, name string) (bool, error) {
	var count int
	query := "SELECT count(*) FROM sqlite_master WHERE name=?1 AND type='table'"
	if d.postgres {
		query = "SELECT count(*) FROM pg_tables WHERE schemaname=current_schema() AND tablename=$1"
	}
	err := tx.QueryRowContext(ctx, query, name).Scan(&count)
	return count > 0, err
}

// CreateIndex creates all physical objects in one transaction and never patches an existing schema.
func (d *Driver) CreateIndex(ctx context.Context, def search.Definition) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return search.ErrClosed
	}
	index, err := search.NewIndex(d, def)
	if err != nil {
		return err
	}
	def = index.Definition()
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return classify(err, false)
	}
	defer tx.Rollback()
	name := d.target(def)
	exists, err := d.exists(ctx, tx, name)
	if err != nil {
		return classify(err, false)
	}
	if exists {
		return d.validate(ctx, tx, def)
	}
	for _, statement := range d.ddl(name, def) {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return classify(err, false)
		}
	}
	layout, err := d.layout(ctx, tx, name)
	if err != nil {
		return classify(err, false)
	}
	fingerprint, _ := def.Fingerprint()
	query := "INSERT INTO " + name + "_meta (id,fingerprint,layout) VALUES (1," + d.bind(1) + "," + d.bind(2) + ")"
	if _, err := tx.ExecContext(ctx, query, fingerprint, layout); err != nil {
		return classify(err, false)
	}
	if err := tx.Commit(); err != nil {
		return classify(err, true)
	}
	d.name, d.fingerprint = name, fingerprint
	return nil
}

// ValidateIndex checks both stored ownership and current native schema without changing either.
func (d *Driver) ValidateIndex(ctx context.Context, def search.Definition) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return search.ErrClosed
	}
	index, err := search.NewIndex(d, def)
	if err != nil {
		return err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return classify(err, false)
	}
	defer tx.Rollback()
	return d.validate(ctx, tx, index.Definition())
}

// validate compares the original physical layout even when the portable fingerprint has not changed.
func (d *Driver) validate(ctx context.Context, tx *sql.Tx, def search.Definition) error {
	name := d.target(def)
	var fingerprint, wanted string
	if err := tx.QueryRowContext(ctx, "SELECT fingerprint,layout FROM "+name+"_meta WHERE id=1").Scan(&fingerprint, &wanted); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return search.ErrSchemaDrift
		}
		return classify(err, false)
	}
	expected, _ := def.Fingerprint()
	if fingerprint != expected {
		return search.ErrSchemaDrift
	}
	actual, err := d.layout(ctx, tx, name)
	if err != nil {
		return classify(err, false)
	}
	if wanted != actual {
		return search.ErrSchemaDrift
	}
	d.name, d.fingerprint = name, fingerprint
	return nil
}
