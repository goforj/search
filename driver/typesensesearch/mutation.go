package typesensesearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// physical records explicit field presence so empty arrays differ from missing fields.
func physical(doc search.Document, def search.Definition) map[string]any {
	m := driverutil.Physical(doc, def)
	m["id"] = driverutil.IDKey(doc.ID)
	obj := driverutil.Object(doc)
	for _, f := range def.Fields {
		key := driverutil.FieldKey(f.Name)
		if _, ok := m[key+"_exists"]; ok {
			m[key+"_exists"] = 1
		} else {
			m[key+"_exists"] = 0
		}
		if f.Type == search.IntegerField {
			if value, ok := driverutil.At(obj, f.Path); ok {
				m[key] = value
			}
		}
	}
	return m
}

// Upsert replaces a complete document, including removal of previously indexed fields.
// @group Mutations
func (d *Driver) Upsert(ctx context.Context, def search.Definition, doc search.Document) (search.Receipt, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := d.ready(ctx, def); err != nil {
		return search.Receipt{}, err
	}
	var out map[string]any
	if err := d.http.Do(ctx, "POST", "/collections/"+d.name+"/documents?action=upsert&dirty_values=reject", physical(doc, def), &out, true); err != nil {
		return search.Receipt{}, err
	}
	if out["id"] != driverutil.IDKey(doc.ID) {
		return search.Receipt{}, search.ErrOutcomeUnknown
	}
	return d.ledger.Complete(search.Outcome{State: search.OutcomeSucceeded})
}

// Delete removes an ID idempotently and proves synchronous visibility.
// @group Mutations
func (d *Driver) Delete(ctx context.Context, def search.Definition, id search.DocumentID) (search.Receipt, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := d.ready(ctx, def); err != nil {
		return search.Receipt{}, err
	}
	var out map[string]any
	err := d.http.Do(ctx, "DELETE", "/collections/"+d.name+"/documents/"+driverutil.IDKey(id), nil, &out, true)
	if errors.Is(err, search.ErrIndexNotFound) {
		if verifyErr := d.http.Do(ctx, "GET", "/collections/"+d.name, nil, &out, false); verifyErr != nil {
			return search.Receipt{}, verifyErr
		}
	} else if err != nil {
		return search.Receipt{}, err
	}
	return d.ledger.Complete(search.Outcome{State: search.OutcomeSucceeded})
}

// Bulk parses every native import response line, retaining known per-item failures.
// @group Mutations
func (d *Driver) Bulk(ctx context.Context, def search.Definition, mutations []search.Mutation) (search.BulkResult, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := d.ready(ctx, def); err != nil {
		return search.BulkResult{}, err
	}
	items := make([]search.ItemResult, len(mutations))
	terminal := make([]search.ItemResult, len(mutations))
	for start := 0; start < len(mutations); {
		if mutations[start].Kind() == search.MutationDelete {
			var response map[string]any
			err := d.http.Do(ctx, "DELETE", "/collections/"+d.name+"/documents/"+driverutil.IDKey(mutations[start].ID()), nil, &response, true)
			if errors.Is(err, search.ErrIndexNotFound) {
				if verifyErr := d.http.Do(ctx, "GET", "/collections/"+d.name, nil, &response, false); verifyErr != nil {
					return d.partialOutcome(mutations, terminal, verifyErr)
				}
			} else if err != nil {
				return d.partialOutcome(mutations, terminal, search.ErrOutcomeUnknown)
			}
			items[start] = search.ItemResult{Position: start, ID: mutations[start].ID(), Status: search.ItemAccepted}
			terminal[start] = search.ItemResult{Position: start, ID: mutations[start].ID(), Status: search.ItemSucceeded}
			start++
			continue
		}
		end := start + 1
		for end < len(mutations) && mutations[end].Kind() == search.MutationUpsert {
			end++
		}
		var body bytes.Buffer
		encoder := json.NewEncoder(&body)
		for _, mutation := range mutations[start:end] {
			doc, _ := mutation.Document()
			if err := encoder.Encode(physical(doc, def)); err != nil {
				return d.partialOutcome(mutations, terminal, search.ErrInvalidDocument)
			}
		}
		response, err := d.http.DoBytes(ctx, "POST", "/collections/"+d.name+"/documents/import?action=upsert&dirty_values=reject&return_id=true", "text/plain", body.Bytes(), true)
		if err != nil {
			return d.partialOutcome(mutations, terminal, err)
		}
		lines := bytes.Split(bytes.TrimSpace(response), []byte("\n"))
		if len(lines) != end-start {
			return d.partialOutcome(mutations, terminal, search.ErrOutcomeUnknown)
		}
		for n, line := range lines {
			var result struct {
				Success *bool
				ID      string
			}
			if err := json.Unmarshal(line, &result); err != nil || result.Success == nil {
				return d.partialOutcome(mutations, terminal, search.ErrOutcomeUnknown)
			}
			position := start + n
			item := search.ItemResult{Position: position, ID: mutations[position].ID(), Status: search.ItemAccepted}
			final := item
			final.Status = search.ItemSucceeded
			if !*result.Success {
				item.Status, final.Status = search.ItemRejected, search.ItemFailed
				item.Error, final.Error = search.ErrMutationFailed, search.ErrMutationFailed
			} else if result.ID != driverutil.IDKey(item.ID) {
				return d.partialOutcome(mutations, terminal, search.ErrOutcomeUnknown)
			}
			items[position], terminal[position] = item, final
		}
		start = end
	}
	state := search.OutcomeSucceeded
	for _, item := range terminal {
		if item.Status != search.ItemSucceeded {
			state = search.OutcomeFailed
		}
	}
	var failure error
	if state != search.OutcomeSucceeded {
		failure = search.ErrMutationFailed
	}
	receipt, err := d.ledger.Complete(search.Outcome{State: state, Error: failure, Bulk: &search.BulkOutcome{Items: terminal, Precision: search.PerItem}})
	return search.BulkResult{Receipt: receipt, Items: items, Precision: search.PerItem}, err
}

// Wait reports retained per-item evidence or explicit receipt expiration.
// @group Receipts
func (d *Driver) Wait(ctx context.Context, receipt search.Receipt) (search.Outcome, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.closed {
		return search.Outcome{}, search.ErrClosed
	}
	return d.ledger.Wait(ctx, receipt)
}

// partialOutcome retains proven item results when later protocol loss makes the complete batch uncertain.
func (d *Driver) partialOutcome(mutations []search.Mutation, terminal []search.ItemResult, cause error) (search.BulkResult, error) {
	known := false
	for _, item := range terminal {
		if item.Status == search.ItemSucceeded || item.Status == search.ItemFailed {
			known = true
		}
	}
	if !known {
		return search.BulkResult{}, search.SafeError(cause)
	}
	for n, mutation := range mutations {
		if terminal[n].Status == "" {
			terminal[n] = search.ItemResult{Position: n, ID: mutation.ID(), Status: search.ItemUnknown, Error: search.ErrOutcomeUnknown}
		}
	}
	receipt, err := d.ledger.Complete(search.Outcome{State: search.OutcomeUnknown, Error: search.ErrOutcomeUnknown, Bulk: &search.BulkOutcome{Items: terminal, Precision: search.PerItem}})
	if err != nil {
		return search.BulkResult{}, search.ErrOutcomeUnknown
	}
	return search.BulkResult{}, errors.Join(search.NewUnknownOutcomeError(receipt), search.SafeError(cause))
}
