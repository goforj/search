// Package sqlsearch implements transactional storage shared by native SQL full-text drivers.
package sqlsearch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// Driver keeps caller-owned SQL connections separate from search lifecycle state.
type Driver struct {
	mu          sync.RWMutex
	db          *sql.DB
	postgres    bool
	prefix      string
	name        string
	fingerprint string
	closed      bool
	ledger      driverutil.Ledger
}

// New validates namespace policy without opening a connection or changing caller pool settings.
func New(db *sql.DB, prefix string, postgres bool) (*Driver, error) {
	if len(prefix) > 16 {
		return nil, search.ErrInvalidDefinition
	}
	for _, r := range prefix {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return nil, search.ErrInvalidDefinition
		}
	}
	return &Driver{db: db, prefix: prefix, postgres: postgres}, nil
}

// target confines identifiers to generated ASCII names below PostgreSQL's identifier limit.
func (d *Driver) target(def search.Definition) string {
	sum := sha256.Sum256([]byte(def.Name))
	return "s_" + d.prefix + hex.EncodeToString(sum[:12])
}

// bind uses explicit ordinals so projection and predicate expressions can share one argument list.
func (d *Driver) bind(n int) string {
	if d.postgres {
		return fmt.Sprintf("$%d", n)
	}
	return fmt.Sprintf("?%d", n)
}

// ready refuses unvalidated definitions without issuing runtime schema mutations.
func (d *Driver) ready(ctx context.Context, def search.Definition) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.closed {
		return search.ErrClosed
	}
	if d.name == "" {
		return search.ErrIndexNotFound
	}
	fingerprint, err := def.Fingerprint()
	if err != nil {
		return err
	}
	if fingerprint != d.fingerprint {
		return search.ErrSchemaDrift
	}
	return nil
}

// Close waits for admitted operations and leaves the injected database pool owned by the application.
func (d *Driver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

// classify retains SQL state categories without exposing SQL, records, or database error prose.
func classify(err error, mutation bool) error {
	if err == nil {
		return nil
	}
	for _, cancellation := range []error{context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, cancellation) {
			if mutation {
				return errors.Join(cancellation, search.ErrOutcomeUnknown)
			}
			return cancellation
		}
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		if strings.HasPrefix(state.SQLState(), "23") && state.SQLState() != "23505" {
			return search.ErrInvalidDocument
		}
		switch state.SQLState() {
		case "42P01":
			return search.ErrIndexNotFound
		case "42501", "28P01", "28000":
			return search.ErrUnauthorized
		case "42P07", "23505":
			return search.ErrConflict
		case "40001", "40P01", "55P03":
			return search.ErrRateLimited
		}
	}
	// SQLite exposes numeric codes through driver-specific interfaces; prose is inspected only locally.
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "constraint failed") {
		return search.ErrInvalidDocument
	}
	if strings.Contains(message, "readonly database") {
		return search.ErrUnauthorized
	}
	if strings.Contains(message, "no such table") {
		return search.ErrIndexNotFound
	}
	if strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked") {
		return search.ErrRateLimited
	}
	if strings.Contains(message, "no such module: fts5") {
		return search.ErrUnsupported
	}
	if mutation {
		return search.ErrOutcomeUnknown
	}
	return search.ErrUnavailable
}
