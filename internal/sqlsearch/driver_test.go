package sqlsearch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/goforj/search"
)

// stateError carries a synthetic SQLSTATE with private prose to verify the redaction boundary.
type stateError string

// Error simulates database diagnostics that must never reach application-facing errors.
func (e stateError) Error() string { return "private-database-query-and-document" }

// SQLState exposes only the portable PostgreSQL classification used by the driver.
func (e stateError) SQLState() string { return string(e) }

// TestErrorClassification keeps SQL state and cancellation identity without retaining private diagnostics.
func TestErrorClassification(t *testing.T) {
	for state, want := range map[string]error{"42P01": search.ErrIndexNotFound, "42501": search.ErrUnauthorized, "28P01": search.ErrUnauthorized, "28000": search.ErrUnauthorized, "42P07": search.ErrConflict, "23505": search.ErrConflict, "23514": search.ErrInvalidDocument, "40001": search.ErrRateLimited, "40P01": search.ErrRateLimited, "55P03": search.ErrRateLimited} {
		err := classify(stateError(state), true)
		if !errors.Is(err, want) || strings.Contains(err.Error(), "private") {
			t.Fatal(state, err)
		}
	}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		if err := classify(cause, false); !errors.Is(err, cause) {
			t.Fatal(err)
		}
		if err := classify(cause, true); !errors.Is(err, cause) || !errors.Is(err, search.ErrOutcomeUnknown) {
			t.Fatal(err)
		}
	}
	for message, want := range map[string]error{"no such table: private": search.ErrIndexNotFound, "database is locked: private": search.ErrRateLimited, "database table is locked: private": search.ErrRateLimited, "no such module: fts5": search.ErrUnsupported, "constraint failed: private": search.ErrInvalidDocument, "readonly database private": search.ErrUnauthorized} {
		if err := classify(errors.New(message), false); !errors.Is(err, want) {
			t.Fatal(err)
		}
	}
	if !errors.Is(classify(errors.New("private"), true), search.ErrOutcomeUnknown) {
		t.Fatal("unproven mutation was classified as failed")
	}
	if !errors.Is(classify(errors.New("private"), false), search.ErrUnavailable) {
		t.Fatal("unknown read failure lost classification")
	}
}

// TestNamespaceAndLifecycleFailures fail before any SQL I/O when configuration or lifecycle is invalid.
func TestNamespaceAndLifecycleFailures(t *testing.T) {
	for _, prefix := range []string{"../bad", "bad-name", "UPPER", strings.Repeat("a", 17)} {
		if _, err := New(nil, prefix, true); !errors.Is(err, search.ErrInvalidDefinition) {
			t.Fatal(err)
		}
	}
	driver, err := New(nil, "valid_", true)
	if err != nil {
		t.Fatal(err)
	}
	definition := search.Definition{Name: "records", Fields: []search.Field{{Name: "title", Type: search.TextField, Searchable: true}}}
	if _, err := driver.Search(context.Background(), definition, search.Query{}); !errors.Is(err, search.ErrIndexNotFound) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := driver.Search(ctx, definition, search.Query{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := driver.Close(); err != nil {
		t.Fatal(err)
	}
	if err := driver.CreateIndex(context.Background(), definition); !errors.Is(err, search.ErrClosed) {
		t.Fatal(err)
	}
	if err := driver.ValidateIndex(context.Background(), definition); !errors.Is(err, search.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := driver.Wait(context.Background(), search.Receipt{}); !errors.Is(err, search.ErrClosed) {
		t.Fatal(err)
	}
}

// TestFilterValuesRemainParameters prevents hostile keyword text from crossing the SQL syntax boundary.
func TestFilterValuesRemainParameters(t *testing.T) {
	for _, postgres := range []bool{true, false} {
		driver, _ := New(nil, "", postgres)
		keyword, err := search.Keyword("'; DROP TABLE records; --")
		if err != nil {
			t.Fatal(err)
		}
		compiler := compiler{driver: driver}
		statement := compiler.filter(search.Not(search.Equal("category", keyword)))
		if len(compiler.args) != 1 || strings.Contains(statement, "DROP") || !strings.Contains(statement, "coalesce") {
			t.Fatal(statement, compiler.args)
		}
	}
}
