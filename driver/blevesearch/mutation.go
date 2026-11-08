package blevesearch

import (
	"context"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// Upsert commits complete replacement data synchronously.
// @group Mutations
func (d *Driver) Upsert(ctx context.Context, def search.Definition, doc search.Document) (search.Receipt, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := d.ready(ctx, def); err != nil {
		return search.Receipt{}, err
	}
	if err := d.index.Index(driverutil.IDKey(doc.ID), driverutil.Physical(doc, def)); err != nil {
		return search.Receipt{}, search.ErrOutcomeUnknown
	}
	return d.ledger.Complete(search.Outcome{State: search.OutcomeSucceeded})
}

// Delete commits idempotent removal synchronously.
// @group Mutations
func (d *Driver) Delete(ctx context.Context, def search.Definition, id search.DocumentID) (search.Receipt, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := d.ready(ctx, def); err != nil {
		return search.Receipt{}, err
	}
	if err := d.index.Delete(driverutil.IDKey(id)); err != nil {
		return search.Receipt{}, search.ErrOutcomeUnknown
	}
	return d.ledger.Complete(search.Outcome{State: search.OutcomeSucceeded})
}

// Bulk commits one atomic Bleve batch and retains ordered per-item outcomes.
// @group Mutations
func (d *Driver) Bulk(ctx context.Context, def search.Definition, ms []search.Mutation) (search.BulkResult, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := d.ready(ctx, def); err != nil {
		return search.BulkResult{}, err
	}
	batch := d.index.NewBatch()
	return d.commitBatch(ctx, def, ms, batch, func() error { return d.index.Batch(batch) })
}

// commitBatch separates mapping failure from interrupted commit acknowledgement without weakening either error path.
func (d *Driver) commitBatch(ctx context.Context, def search.Definition, ms []search.Mutation, batch mutationBatch, commit func() error) (search.BulkResult, error) {
	items, terminal, err := fillBatch(batch, def, ms)
	if err != nil {
		return search.BulkResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return search.BulkResult{}, err
	}
	if err := commit(); err != nil {
		return search.BulkResult{}, search.ErrOutcomeUnknown
	}
	r, err := d.ledger.Complete(search.Outcome{State: search.OutcomeSucceeded, Bulk: &search.BulkOutcome{Items: terminal, Precision: search.PerItem}})
	return search.BulkResult{Receipt: r, Items: items, Precision: search.PerItem}, err
}

// mutationBatch preserves native mapping errors independently of the embedded index's commit operation.
type mutationBatch interface {
	Index(string, any) error
	Delete(string)
}

// fillBatch retains input order and stops before commit if native document mapping fails.
func fillBatch(batch mutationBatch, def search.Definition, ms []search.Mutation) ([]search.ItemResult, []search.ItemResult, error) {
	items := make([]search.ItemResult, len(ms))
	terminal := make([]search.ItemResult, len(ms))
	for x, m := range ms {
		if doc, ok := m.Document(); ok {
			if err := batch.Index(driverutil.IDKey(doc.ID), driverutil.Physical(doc, def)); err != nil {
				return nil, nil, search.ErrInvalidDocument
			}
		} else {
			batch.Delete(driverutil.IDKey(m.ID()))
		}
		items[x] = search.ItemResult{Position: x, ID: m.ID(), Status: search.ItemAccepted}
		terminal[x] = search.ItemResult{Position: x, ID: m.ID(), Status: search.ItemSucceeded}
	}
	return items, terminal, nil
}

// Wait reports synchronous commit evidence, including bounded receipt expiration.
// @group Receipts
func (d *Driver) Wait(ctx context.Context, r search.Receipt) (search.Outcome, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := d.check(ctx); err != nil {
		return search.Outcome{}, err
	}
	return d.ledger.Wait(ctx, r)
}
