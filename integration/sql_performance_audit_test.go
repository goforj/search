//go:build integration && performance

package integration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goforj/search"
	"github.com/goforj/search/driver/postgressearch"
	"github.com/goforj/search/driver/sqlitesearch"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/mattn/go-sqlite3"
)

// sqlAuditStatement preserves the actual SQL and bound values emitted by the production driver.
type sqlAuditStatement struct {
	SQL  string   `json:"sql"`
	Args []any    `json:"args"`
	Plan []string `json:"plan"`
}

// sqlAuditTrace captures selected statements and enables an explicitly separate diagnostic predicate control.
type sqlAuditTrace struct {
	mu          sync.Mutex
	capture     bool
	rewrite     bool
	rowid       bool
	filterFirst bool
	statements  []sqlAuditStatement
}

// query observes production SQL without adding native calls to the timed request path.
func (a *sqlAuditTrace) query(query string, values []driver.NamedValue) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.rewrite {
		for _, bind := range []string{"$1", "?1"} {
			predicate := "d." + sqlAuditField("partition") + "=" + bind
			query = strings.ReplaceAll(query, "coalesce("+predicate+",false)", "("+predicate+")")
		}
	}
	if a.rowid {
		query = strings.ReplaceAll(query, "_fts.id=d.id", "_fts.rowid=d.rowid")
	}
	if a.filterFirst && strings.Contains(query, "d."+sqlAuditField("partition")+"=") {
		query = strings.ReplaceAll(query, " d JOIN ", " d CROSS JOIN ")
	}
	if a.capture && (strings.HasPrefix(query, "WITH matches AS") || strings.HasPrefix(query, "SELECT count(*) FROM s_") || strings.HasPrefix(query, "SELECT d.original,") || strings.HasPrefix(query, "SELECT v.value,count(")) {
		args := make([]any, len(values))
		for n, value := range values {
			args[n] = value.Value
		}
		a.statements = append(a.statements, sqlAuditStatement{SQL: query, Args: args})
	}
	return query
}

// sqlAuditConnector wraps the same native database/sql drivers used by the integration fixtures.
type sqlAuditConnector struct {
	base  driver.Driver
	dsn   string
	trace *sqlAuditTrace
}

// Connect preserves context-aware connection establishment when the native driver supports it.
func (c *sqlAuditConnector) Connect(ctx context.Context) (driver.Conn, error) {
	var connection driver.Conn
	var err error
	if contextual, ok := c.base.(driver.DriverContext); ok {
		var connector driver.Connector
		connector, err = contextual.OpenConnector(c.dsn)
		if err == nil {
			connection, err = connector.Connect(ctx)
		}
	} else {
		connection, err = c.base.Open(c.dsn)
	}
	if err != nil {
		return nil, err
	}
	return &sqlAuditConnection{Conn: connection, trace: c.trace}, nil
}

// Driver retains the original driver's conversion behavior.
func (c *sqlAuditConnector) Driver() driver.Driver { return c.base }

// sqlAuditConnection preserves the optional database/sql interfaces while capturing query arguments.
type sqlAuditConnection struct {
	driver.Conn
	trace *sqlAuditTrace
}

// QueryContext delegates actual execution and row decoding to the native driver.
func (c *sqlAuditConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, c.trace.query(query, args), args)
}

// ExecContext preserves native transaction and mutation behavior during the corpus load.
func (c *sqlAuditConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
}

// BeginTx preserves the production isolation level and read-only transaction options.
func (c *sqlAuditConnection) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, options)
}

// CheckNamedValue preserves native conversions or the database/sql fallback for optional implementations.
func (c *sqlAuditConnection) CheckNamedValue(value *driver.NamedValue) error {
	if checker, ok := c.Conn.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(value)
	}
	return driver.ErrSkip
}

// ResetSession preserves native pool reuse hooks when implemented by the underlying driver.
func (c *sqlAuditConnection) ResetSession(ctx context.Context) error {
	if resetter, ok := c.Conn.(driver.SessionResetter); ok {
		return resetter.ResetSession(ctx)
	}
	return nil
}

// IsValid preserves native connection validation and database/sql's default for optional validators.
func (c *sqlAuditConnection) IsValid() bool {
	if validator, ok := c.Conn.(driver.Validator); ok {
		return validator.IsValid()
	}
	return true
}

// sqlAuditPhase separates original-driver measurements from controlled statistics, indexing, and SQL experiments.
type sqlAuditPhase struct {
	Name       string              `json:"name"`
	Queries    []performanceQuery  `json:"queries"`
	Statements []sqlAuditStatement `json:"statements"`
}

// sqlAuditReport binds query plans and controlled comparisons to the original deterministic corpus.
type sqlAuditReport struct {
	Engine       string          `json:"engine"`
	Documents    int             `json:"documents"`
	CorpusSHA256 string          `json:"corpus_sha256"`
	LoadSeconds  float64         `json:"load_seconds"`
	Go           string          `json:"go"`
	Version      string          `json:"version"`
	Dataset      string          `json:"dataset"`
	Revision     string          `json:"revision"`
	Phases       []sqlAuditPhase `json:"phases"`
}

// TestSQLPerformanceAudit measures the real request path and explains every native statement on a disposable corpus.
func TestSQLPerformanceAudit(t *testing.T) {
	engine := os.Getenv("SEARCH_BENCH_DRIVER")
	count := performanceInt(t, "SEARCH_BENCH_DOCUMENTS", 100_000, 1024, 100_000_000)
	samples := performanceInt(t, "SEARCH_BENCH_SAMPLES", 10, 1, 200)
	output := os.Getenv("SEARCH_BENCH_OUTPUT")
	if output == "" {
		t.Fatal("SEARCH_BENCH_OUTPUT is required")
	}
	trace := &sqlAuditTrace{}
	connector := &sqlAuditConnector{trace: trace}
	switch engine {
	case "postgres":
		connector.base = stdlib.GetDefaultDriver()
		connector.dsn = fmt.Sprintf("postgres://search:%s@%s/search?sslmode=disable", testAPIKey, startTCPServer(t, "postgres"))
	case "sqlite":
		connector.base = &sqlite3.SQLiteDriver{}
		connector.dsn = filepath.Join(t.TempDir(), "audit.db") + "?_busy_timeout=5000&_journal_mode=WAL"
	default:
		t.Fatal("audit requires postgres or sqlite")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(4)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	var backend administeredBackend
	var err error
	if engine == "postgres" {
		backend, err = postgressearch.New(postgressearch.Config{DB: db})
	} else {
		backend, err = sqlitesearch.New(sqlitesearch.Config{DB: db})
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx := context.Background()
	versionQuery := "SELECT sqlite_version()"
	if engine == "postgres" {
		versionQuery = "SHOW server_version"
	}
	var version string
	if err := db.QueryRowContext(ctx, versionQuery).Scan(&version); err != nil {
		t.Fatal(err)
	}
	definition := performanceDefinition()
	if err := backend.CreateIndex(ctx, definition); err != nil {
		t.Fatal(err)
	}
	index, err := search.NewIndex(backend, definition)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	start := time.Now()
	for offset := 0; offset < count; offset += 1000 {
		var mutations []search.Mutation
		for ordinal := offset; ordinal < min(offset+1000, count); ordinal++ {
			document := performanceDocument(ordinal)
			fmt.Fprintf(hash, "%s\n%s\n", document.ID, document.Body)
			mutation, err := search.UpsertMutation(document)
			if err != nil {
				t.Fatal(err)
			}
			mutations = append(mutations, mutation)
		}
		batchCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		result, err := index.Bulk(batchCtx, mutations)
		if err == nil {
			var outcome search.Outcome
			outcome, err = index.Wait(batchCtx, result.Receipt)
			if err == nil && outcome.State != search.OutcomeSucceeded {
				err = fmt.Errorf("load outcome %s", outcome.State)
			}
		}
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if min(offset+1000, count)%100_000 == 0 {
			t.Logf("%s loaded %d/%d", engine, min(offset+1000, count), count)
		}
	}
	report := sqlAuditReport{Engine: engine, Documents: count, CorpusSHA256: hex.EncodeToString(hash.Sum(nil)), LoadSeconds: time.Since(start).Seconds(), Go: runtime.Version(), Version: version, Dataset: "catalog-v1", Revision: strings.TrimSpace(performanceCommand("git", "rev-parse", "HEAD"))}
	tableHash := sha256.Sum256([]byte(definition.Name))
	table := "s_" + hex.EncodeToString(tableHash[:12])
	phases := []string{"baseline", "analyzed", "indexed", "predicate_control"}
	if engine == "sqlite" {
		phases = append(phases, "rowid_control")
		if os.Getenv("SEARCH_SQL_AUDIT_FILTER_FIRST") == "true" {
			phases = append(phases, "filter_first_control")
		}
	}
	for _, phase := range phases {
		if phase == "indexed" {
			for n, columns := range []string{"id", sqlAuditField("partition"), "field,owner,value"} {
				target := table
				if n == 2 {
					target += "_values"
				}
				if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE INDEX audit_%d ON %s (%s)", n, target, columns)); err != nil {
					t.Fatal(err)
				}
			}
		}
		if phase == "analyzed" || phase == "indexed" {
			for _, target := range []string{table, table + "_values"} {
				if _, err := db.ExecContext(ctx, "ANALYZE "+target); err != nil {
					t.Fatal(err)
				}
			}
		}
		trace.rewrite = phase == "predicate_control" || phase == "rowid_control" || phase == "filter_first_control"
		trace.filterFirst = phase == "filter_first_control"
		if phase == "rowid_control" {
			var mismatches int
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" d JOIN "+table+"_fts f ON f.id=d.id WHERE f.rowid<>d.rowid").Scan(&mismatches); err != nil {
				t.Fatal(err)
			}
			if mismatches != 0 {
				t.Fatalf("diagnostic rowid control is not applicable: %d mismatches", mismatches)
			}
			trace.rowid = true
		}
		measurement := sqlAuditPhase{Name: phase}
		for _, name := range []string{"literal", "filter_sort", "facets"} {
			querySamples := performanceQuery{Name: name}
			for sample := -3; sample < samples; sample++ {
				partition := ((sample+30)*7919 + 17) % ((count + 1023) / 1024)
				trace.capture = sample == -3
				queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				started := time.Now()
				result, err := index.Search(queryCtx, performanceRequest(name, partition))
				elapsed := time.Since(started).Nanoseconds()
				cancel()
				trace.capture = false
				if err != nil {
					t.Fatal(phase, name, err)
				}
				performanceVerify(t, name, partition, count, result)
				if sample >= 0 {
					querySamples.NS = append(querySamples.NS, elapsed)
				}
				querySamples.TotalRelation = result.Total.Relation
			}
			measurement.Queries = append(measurement.Queries, querySamples)
			t.Logf("%s %s %s: %v", engine, phase, name, querySamples.NS)
		}
		measurement.Statements = trace.statements
		trace.statements = nil
		for n, statement := range measurement.Statements {
			measurement.Statements[n].Plan = sqlAuditExplain(t, db, engine, statement)
		}
		report.Phases = append(report.Phases, measurement)
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// sqlAuditField reproduces physical field names only for the explicitly labeled diagnostic index controls.
func sqlAuditField(name string) string {
	sum := sha256.Sum256([]byte(name))
	return "f" + hex.EncodeToString(sum[:8])
}

// sqlAuditExplain obtains native plans for captured production statements after timing has stopped.
func sqlAuditExplain(t *testing.T, db *sql.DB, engine string, statement sqlAuditStatement) []string {
	t.Helper()
	prefix := "EXPLAIN QUERY PLAN "
	if engine == "postgres" {
		prefix = "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	rows, err := db.QueryContext(ctx, prefix+statement.SQL, statement.Args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		if engine == "postgres" {
			err = rows.Scan(&line)
		} else {
			var id, parent, unused int
			err = rows.Scan(&id, &parent, &unused, &line)
		}
		if err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	return plan
}
