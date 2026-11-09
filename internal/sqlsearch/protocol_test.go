package sqlsearch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/goforj/search"
	"github.com/goforj/search/searchtest"
)

// sqlProtocol scripts database/sql responses without replacing the SQL compiler or transaction boundaries.
type sqlProtocol struct {
	fingerprint   string
	exists        bool
	calls         int
	failAt        int
	mode          string
	queries       map[int]string
	statements    []string
	aggregateRows [][]driver.Value
}

// Connect gives each operation a connection to its isolated protocol fixture.
func (s *sqlProtocol) Connect(context.Context) (driver.Conn, error) {
	return &protocolConnection{s: s}, nil
}

// Driver satisfies Connector without registering a process-global SQL driver.
func (s *sqlProtocol) Driver() driver.Driver { return protocolDriver{} }

// protocolDriver rejects unscoped connection attempts outside the fixture connector.
type protocolDriver struct{}

// Open refuses connections that bypass the explicitly scoped connector.
func (protocolDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use the fixture connector")
}

// protocolConnection retains the transaction's deterministic response sequence.
type protocolConnection struct{ s *sqlProtocol }

// Prepare rejects accidental fallback to interpolated statements.
func (c *protocolConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("expected context-aware statements")
}

// Close leaves the test-owned fixture available for assertions.
func (c *protocolConnection) Close() error { return nil }

// Begin forwards legacy transactions through the same fixture boundary.
func (c *protocolConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

// step injects private transport failures at each externally observable operation.
func (s *sqlProtocol) step(label string) error {
	s.calls++
	s.statements = append(s.statements, label)
	if s.calls == s.failAt && s.mode == "call" {
		return errors.New("private database failure")
	}
	return nil
}

// BeginTx exposes transaction admission failures independently of statement failures.
func (c *protocolConnection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	if err := c.s.step("begin"); err != nil {
		return nil, err
	}
	return &protocolTransaction{s: c.s}, nil
}

// ExecContext records parameterized SQL and simulates successful affected-row counts.
func (c *protocolConnection) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if err := c.s.step(q); err != nil {
		return nil, err
	}
	return driver.RowsAffected(1), nil
}

// QueryContext returns native row shapes for schema inspection, hits, totals, and facets.
func (c *protocolConnection) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	s := c.s
	if err := s.step(q); err != nil {
		return nil, err
	}
	s.queries[s.calls] = q
	var rows [][]driver.Value
	switch {
	case strings.HasPrefix(q, "SELECT fingerprint"):
		sum := sha256.Sum256([]byte("stable layout\n"))
		rows = [][]driver.Value{{s.fingerprint, hex.EncodeToString(sum[:])}}
	case strings.Contains(q, "sqlite_master WHERE name=") || strings.Contains(q, "FROM pg_tables"):
		count := int64(0)
		if s.exists {
			count = 1
		}
		rows = [][]driver.Value{{count}}
	case strings.HasPrefix(q, "WITH matches AS") || strings.HasPrefix(q, "SELECT -1 AS section"):
		rows = [][]driver.Value{{int64(-1), "", int64(1)}, {int64(0), "v637570", int64(1)}}
		if s.aggregateRows != nil {
			rows = s.aggregateRows
		}
	case strings.HasPrefix(q, "SELECT count(*)"):
		rows = [][]driver.Value{{int64(1)}}
	case strings.HasPrefix(q, "SELECT d.original"):
		rows = [][]driver.Value{{"one", `{"title":"red cup","category":"cup","tags":["a","a"],"active":true,"rank":1,"code":"a"}`, float64(1)}}
	case strings.HasPrefix(q, "SELECT v.value") || strings.Contains(q, " AS value,count(*)"):
		rows = [][]driver.Value{{"v637570", int64(1)}}
	default:
		rows = [][]driver.Value{{"stable layout"}}
	}
	nextError := false
	if s.calls == s.failAt {
		switch s.mode {
		case "scan":
			for n := range rows[0] {
				rows[0][n] = nil
			}
		case "rows":
			nextError = true
		case "empty":
			rows = nil
		case "corrupt":
			rows[0][0] = "corrupt"
		case "body":
			rows[0][1] = "[]"
		case "total":
			rows[0][2] = int64(2)
		}
	}
	cols := 1
	if len(rows) > 0 {
		cols = len(rows[0])
	}
	return &protocolRows{rows: rows, columns: cols, nextError: nextError}, nil
}

// protocolTransaction distinguishes commit uncertainty from statement rejection.
type protocolTransaction struct{ s *sqlProtocol }

// Commit makes the commit acknowledgement independently interruptible.
func (tx *protocolTransaction) Commit() error { return tx.s.step("commit") }

// Rollback permits cleanup after every aborted scenario.
func (tx *protocolTransaction) Rollback() error { return nil }

// protocolRows exposes scan errors and post-row stream failures separately.
type protocolRows struct {
	rows      [][]driver.Value
	columns   int
	nextError bool
}

// Columns retains the native result arity even for an empty stream.
func (r *protocolRows) Columns() []string {
	out := make([]string, r.columns)
	for n := range out {
		out[n] = fmt.Sprint(n)
	}
	return out
}

// Close allows database/sql to release the connection after partial reads.
func (r *protocolRows) Close() error { return nil }

// Next models a stream that may fail after delivering otherwise valid rows.
func (r *protocolRows) Next(dest []driver.Value) error {
	if len(r.rows) == 0 {
		if r.nextError {
			r.nextError = false
			return errors.New("private row stream failure")
		}
		return io.EOF
	}
	copy(dest, r.rows[0])
	r.rows = r.rows[1:]
	return nil
}

// protocolFixture normalizes the schema exactly as the public Index does before request calls.
func protocolFixture(t *testing.T, postgres bool) (*Driver, search.Definition, *sqlProtocol) {
	t.Helper()
	index, err := search.NewIndex(nil, searchtest.Definition())
	if err != nil {
		t.Fatal(err)
	}
	def := index.Definition()
	fp, err := def.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	script := &sqlProtocol{fingerprint: fp, queries: map[int]string{}}
	db := sql.OpenDB(script)
	t.Cleanup(func() { db.Close() })
	d, err := New(db, "test_", postgres)
	if err != nil {
		t.Fatal(err)
	}
	d.name, d.fingerprint = d.target(def), fp
	return d, def, script
}

// TestSQLProtocolFailures exercises every SQL call failure in both dialects and verifies redaction.
func TestSQLProtocolFailures(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(fmt.Sprint(postgres), func(t *testing.T) {
			scenarios := map[string]func(*Driver, search.Definition) error{
				"create":   func(d *Driver, def search.Definition) error { return d.CreateIndex(context.Background(), def) },
				"validate": func(d *Driver, def search.Definition) error { return d.ValidateIndex(context.Background(), def) },
				"upsert": func(d *Driver, def search.Definition) error {
					_, err := d.Upsert(context.Background(), def, search.Document{ID: "one", Body: []byte(`{"title":"red cup","category":"cup","tags":["a","a"],"active":true,"rank":1,"code":"a"}`)})
					return err
				},
				"delete": func(d *Driver, def search.Definition) error {
					_, err := d.Delete(context.Background(), def, "one")
					return err
				},
				"bulk": func(d *Driver, def search.Definition) error {
					upsert, err := search.UpsertMutation(search.Document{ID: "one", Body: []byte(`{"active":false,"rank":0,"code":"a"}`)})
					if err != nil {
						return err
					}
					del, err := search.DeleteMutation("two")
					if err != nil {
						return err
					}
					r, err := d.Bulk(context.Background(), def, []search.Mutation{upsert, del})
					if err == nil {
						outcome, inspectErr := d.Wait(context.Background(), r.Receipt)
						if inspectErr != nil {
							return inspectErr
						}
						if outcome.State != search.OutcomeSucceeded || len(outcome.Bulk.Items) != 2 {
							return errors.New("bad bulk outcome")
						}
					}
					return err
				},
				"search": func(d *Driver, def search.Definition) error {
					r, err := d.Search(context.Background(), def, search.Query{Text: "red cup", Fields: []search.FieldName{"title"}, Limit: 2, Facets: []search.TermFacet{{Name: "categories", Field: "category", Limit: 1}}})
					if err == nil && (len(r.Hits) != 1 || r.Hits[0].Score == nil || len(r.Facets) != 1) {
						return errors.New("bad search result")
					}
					return err
				},
				"browse": func(d *Driver, def search.Definition) error {
					r, err := d.Search(context.Background(), def, search.Query{Sort: []search.Sort{{Field: "rank", Direction: search.Descending}, {Field: "code", Direction: search.Ascending}}, Limit: 2})
					if err == nil && (len(r.Hits) != 1 || r.Hits[0].Score != nil) {
						return errors.New("bad browse result")
					}
					return err
				},
			}
			for name, call := range scenarios {
				t.Run(name, func(t *testing.T) {
					d, def, s := protocolFixture(t, postgres)
					if err := call(d, def); err != nil {
						t.Fatal(err)
					}
					count := s.calls
					for n := 1; n <= count; n++ {
						t.Run(fmt.Sprintf("failure-%d", n), func(t *testing.T) {
							d, def, s := protocolFixture(t, postgres)
							s.failAt, s.mode = n, "call"
							err := call(d, def)
							if err == nil || strings.Contains(err.Error(), "private") {
								t.Fatal("missing safe failure", n, err)
							}
						})
					}
					for n, q := range s.queries {
						for _, mode := range []string{"scan", "rows"} {
							if mode == "rows" && (strings.HasPrefix(q, "SELECT count") || strings.HasPrefix(q, "SELECT fingerprint")) {
								continue
							}
							t.Run(fmt.Sprintf("%s-%d", mode, n), func(t *testing.T) {
								d, def, s := protocolFixture(t, postgres)
								s.failAt, s.mode = n, mode
								if err := call(d, def); err == nil || strings.Contains(err.Error(), "private") {
									t.Fatal("missing safe row failure", n, err)
								}
							})
						}
					}
				})
			}
		})
	}
}

// TestSQLSchemaAndLifecycleEdges verifies drift and closed-state checks without database side effects.
func TestSQLSchemaAndLifecycleEdges(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		d, def, s := protocolFixture(t, postgres)
		s.exists = true
		if err := d.CreateIndex(context.Background(), def); err != nil {
			t.Fatal(err)
		}
		if err := d.ValidateIndex(context.Background(), search.Definition{}); !errors.Is(err, search.ErrInvalidDefinition) {
			t.Fatal(err)
		}
		if err := d.CreateIndex(context.Background(), search.Definition{}); !errors.Is(err, search.ErrInvalidDefinition) {
			t.Fatal(err)
		}
		if err := d.ready(context.Background(), search.Definition{}); !errors.Is(err, search.ErrInvalidDefinition) {
			t.Fatal(err)
		}
		other := def
		other.Name = "different"
		if err := d.ready(context.Background(), other); !errors.Is(err, search.ErrSchemaDrift) {
			t.Fatal(err)
		}
		for _, mode := range []string{"empty", "corrupt"} {
			s.calls = 0
			s.failAt = 2
			s.mode = mode
			if err := d.ValidateIndex(context.Background(), def); !errors.Is(err, search.ErrSchemaDrift) {
				t.Fatal(mode, err)
			}
		}
		s.calls = 0
		s.failAt = 3
		s.mode = "corrupt"
		if err := d.ValidateIndex(context.Background(), def); !errors.Is(err, search.ErrSchemaDrift) {
			t.Fatal(err)
		}
		if got, err := d.Search(context.Background(), def, search.Query{Text: "!!!"}); err != nil || got.Total.Value != 0 {
			t.Fatal(got, err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := d.Wait(ctx, search.Receipt{}); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if err := d.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Upsert(context.Background(), def, search.Document{}); !errors.Is(err, search.ErrClosed) {
			t.Fatal(err)
		}
		if _, err := d.Delete(context.Background(), def, "one"); !errors.Is(err, search.ErrClosed) {
			t.Fatal(err)
		}
		if _, err := d.Bulk(context.Background(), def, nil); !errors.Is(err, search.ErrClosed) {
			t.Fatal(err)
		}
	}
}

// TestSQLProjectionCorruption rejects valid wire rows containing invalid stored bodies or facet encodings.
func TestSQLProjectionCorruption(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		d, def, s := protocolFixture(t, postgres)
		if classify(nil, false) != nil {
			t.Fatal("nil database error changed")
		}
		noText := def
		noText.Fields = noText.Fields[1:]
		if len(d.ddl(d.name, noText)) == 0 {
			t.Fatal("missing keyword-only layout")
		}
		for _, tc := range []struct {
			step int
			mode string
		}{{3, "body"}, {2, "corrupt"}} {
			s.calls = 0
			s.failAt, s.mode = tc.step, tc.mode
			_, err := d.Search(context.Background(), def, search.Query{Limit: 1, Facets: []search.TermFacet{{Name: "categories", Field: "category", Limit: 1}}})
			if !errors.Is(err, search.ErrUnavailable) {
				t.Fatal(tc, err)
			}
		}
	}
}
