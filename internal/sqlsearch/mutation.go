package sqlsearch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// remove keeps relational projections and native FTS rows in the same transaction as the source body.
func (d *Driver) remove(ctx context.Context, tx *sql.Tx, id search.DocumentID) error {
	for _, table := range []string{d.name + "_values", d.name} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE owner="+d.bind(1), ownerKey(id)); err != nil {
			return err
		}
	}
	if !d.postgres {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+d.name+"_fts WHERE id="+d.bind(1), driverutil.IDKey(id)); err != nil {
			return err
		}
	}
	return nil
}

// put derives exact scalar and membership projections from the root-validated canonical document.
func (d *Driver) put(ctx context.Context, tx *sql.Tx, def search.Definition, document search.Document) error {
	if err := d.remove(ctx, tx, document.ID); err != nil {
		return err
	}
	object := driverutil.Object(document)
	columns := []string{"id", "owner", "original", "body"}
	arguments := []any{driverutil.IDKey(document.ID), ownerKey(document.ID), string(document.ID), string(document.Body)}
	textColumns, textArguments := []string{"id"}, []any{driverutil.IDKey(document.ID)}
	for _, field := range def.Fields {
		key := fieldKey(field.Name)
		value, exists := driverutil.At(object, field.Path)
		if !exists {
			continue
		}
		columns = append(columns, key+"_exists")
		arguments = append(arguments, 1)
		if field.Type == search.TextField {
			columns = append(columns, key)
			arguments = append(arguments, value)
			textColumns = append(textColumns, key)
			textArguments = append(textArguments, value)
			continue
		}
		values := []any{value}
		if field.Type == search.KeywordsField {
			values = value.([]any)
		} else {
			columns = append(columns, key)
			arguments = append(arguments, driverutil.Scalar(driverutil.NativeValue(value, field.Type)))
		}
		for _, scalar := range values {
			encoded := driverutil.Scalar(driverutil.NativeValue(scalar, field.Type))
			digest := sha256.Sum256([]byte(encoded))
			query := "INSERT INTO " + d.name + "_values (owner,field,value,digest) VALUES (" + d.bind(1) + "," + d.bind(2) + "," + d.bind(3) + "," + d.bind(4) + ") ON CONFLICT DO NOTHING"
			if _, err := tx.ExecContext(ctx, query, ownerKey(document.ID), key, encoded, hex.EncodeToString(digest[:])); err != nil {
				return err
			}
		}
	}
	if err := d.insert(ctx, tx, d.name, columns, arguments); err != nil {
		return err
	}
	if !d.postgres {
		return d.insert(ctx, tx, d.name+"_fts", textColumns, textArguments)
	}
	return nil
}

// insert binds every value independently of generated identifiers.
func (d *Driver) insert(ctx context.Context, tx *sql.Tx, table string, columns []string, args []any) error {
	placeholders := make([]string, len(args))
	for n := range args {
		placeholders[n] = d.bind(n + 1)
	}
	query := "INSERT INTO " + table + " (" + strings.Join(columns, ",") + ") VALUES (" + strings.Join(placeholders, ",") + ")"
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

// Upsert commits complete replacement and index visibility atomically before issuing a terminal receipt.
func (d *Driver) Upsert(ctx context.Context, def search.Definition, document search.Document) (search.Receipt, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ready(ctx, def); err != nil {
		return search.Receipt{}, err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return search.Receipt{}, classify(err, false)
	}
	defer tx.Rollback()
	if err := d.put(ctx, tx, def, document); err != nil {
		return search.Receipt{}, classify(err, true)
	}
	if err := tx.Commit(); err != nil {
		return search.Receipt{}, classify(err, true)
	}
	return d.ledger.Complete(search.Outcome{State: search.OutcomeSucceeded})
}

// Delete atomically removes every projection while retaining idempotence for absent documents.
func (d *Driver) Delete(ctx context.Context, def search.Definition, id search.DocumentID) (search.Receipt, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ready(ctx, def); err != nil {
		return search.Receipt{}, err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return search.Receipt{}, classify(err, false)
	}
	defer tx.Rollback()
	if err := d.remove(ctx, tx, id); err != nil {
		return search.Receipt{}, classify(err, true)
	}
	if err := tx.Commit(); err != nil {
		return search.Receipt{}, classify(err, true)
	}
	return d.ledger.Complete(search.Outcome{State: search.OutcomeSucceeded})
}

// Bulk commits ordered mutations as one transaction, so no partial successful prefix can escape rollback.
func (d *Driver) Bulk(ctx context.Context, def search.Definition, mutations []search.Mutation) (search.BulkResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ready(ctx, def); err != nil {
		return search.BulkResult{}, err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return search.BulkResult{}, classify(err, false)
	}
	defer tx.Rollback()
	for _, mutation := range mutations {
		if document, ok := mutation.Document(); ok {
			err = d.put(ctx, tx, def, document)
		} else {
			err = d.remove(ctx, tx, mutation.ID())
		}
		if err != nil {
			return search.BulkResult{}, classify(err, true)
		}
	}
	if err := tx.Commit(); err != nil {
		return search.BulkResult{}, classify(err, true)
	}
	items, terminal := make([]search.ItemResult, len(mutations)), make([]search.ItemResult, len(mutations))
	for n, mutation := range mutations {
		items[n] = search.ItemResult{Position: n, ID: mutation.ID(), Status: search.ItemAccepted}
		terminal[n] = search.ItemResult{Position: n, ID: mutation.ID(), Status: search.ItemSucceeded}
	}
	receipt, err := d.ledger.Complete(search.Outcome{State: search.OutcomeSucceeded, Bulk: &search.BulkOutcome{Items: terminal, Precision: search.PerItem}})
	return search.BulkResult{Receipt: receipt, Items: items, Precision: search.PerItem}, err
}

// Wait reports only visibility already established by the transaction commit.
func (d *Driver) Wait(ctx context.Context, receipt search.Receipt) (search.Outcome, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return search.Outcome{}, err
	}
	if d.closed {
		return search.Outcome{}, search.ErrClosed
	}
	return d.ledger.Wait(ctx, receipt)
}

// ownerKey bounds primary-key width even when applications increase the portable ID budget.
func ownerKey(id search.DocumentID) string {
	hash := sha256.Sum256([]byte(id))
	return hex.EncodeToString(hash[:])
}
