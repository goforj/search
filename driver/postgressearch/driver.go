// Package postgressearch implements portable lexical search using PostgreSQL full-text search.
package postgressearch

import (
	"context"
	"database/sql"
	"github.com/goforj/search"
	"github.com/goforj/search/internal/sqlsearch"
)

// Config uses an application-owned pool and an optional lowercase ASCII table namespace.
// The application registers its chosen database/sql driver and owns pool sizing and connection policy.
type Config struct {
	DB     *sql.DB
	Prefix string
}

// Driver owns one logical index without taking ownership of the injected database pool.
type Driver struct{ backend *sqlsearch.Driver }

// New validates namespace policy without making connections or provisioning tables.
// @group Construction
func New(config Config) (*Driver, error) {
	backend, err := sqlsearch.New(config.DB, config.Prefix, true)
	if err != nil {
		return nil, err
	}
	return &Driver{backend: backend}, nil
}

// CreateIndex atomically provisions absent storage and validates existing schemas without patching them.
// @group Definitions
func (d *Driver) CreateIndex(ctx context.Context, definition search.Definition) error {
	return d.backend.CreateIndex(ctx, definition)
}

// ValidateIndex checks the portable fingerprint and actual materialized native schema.
// @group Definitions
func (d *Driver) ValidateIndex(ctx context.Context, definition search.Definition) error {
	return d.backend.ValidateIndex(ctx, definition)
}

// Search uses native full-text matching and exact snapshot-consistent totals and facets.
// @group Queries
func (d *Driver) Search(ctx context.Context, definition search.Definition, query search.Query) (search.Result, error) {
	return d.backend.Search(ctx, definition, query)
}

// Upsert commits replacement and native search visibility in one transaction.
// @group Mutations
func (d *Driver) Upsert(ctx context.Context, definition search.Definition, document search.Document) (search.Receipt, error) {
	return d.backend.Upsert(ctx, definition, document)
}

// Delete atomically removes the body and search projections, including absent IDs idempotently.
// @group Mutations
func (d *Driver) Delete(ctx context.Context, definition search.Definition, id search.DocumentID) (search.Receipt, error) {
	return d.backend.Delete(ctx, definition, id)
}

// Bulk commits ordered mutations as one transaction with per-item terminal success evidence.
// @group Mutations
func (d *Driver) Bulk(ctx context.Context, definition search.Definition, mutations []search.Mutation) (search.BulkResult, error) {
	return d.backend.Bulk(ctx, definition, mutations)
}

// Wait reports visibility established by transaction commit without issuing another database query.
// @group Receipts
func (d *Driver) Wait(ctx context.Context, receipt search.Receipt) (search.Outcome, error) {
	return d.backend.Wait(ctx, receipt)
}

// Close drains admitted operations and preserves the application-owned database pool.
// @group Backend
func (d *Driver) Close() error { return d.backend.Close() }

// Backend conformance is checked at compile time.
var _ search.Backend = (*Driver)(nil)
