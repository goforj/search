package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// shardResult retains write replication and search completeness independently of HTTP status.
type shardResult struct {
	Total      int `json:"total"`
	Successful int `json:"successful"`
	Failed     int `json:"failed"`
}

// writeResult includes only safe protocol evidence; backend prose is deliberately ignored.
type writeResult struct {
	ID     string          `json:"_id"`
	Index  string          `json:"_index"`
	Status int             `json:"status"`
	Result string          `json:"result"`
	Shards shardResult     `json:"_shards"`
	Error  json.RawMessage `json:"error"`
}

// physical projects exact JSON scalars while keeping native keyword terms within Lucene's byte budget.
func physical(document search.Document, definition search.Definition) (map[string]any, error) {
	object := driverutil.Object(document)
	for _, field := range definition.Fields {
		value, ok := driverutil.At(object, field.Path)
		if !ok {
			continue
		}
		if field.Type == search.KeywordField {
			text, ok := value.(string)
			if !ok || len(text) > MaxKeywordBytes {
				return nil, search.ErrInvalidDocument
			}
		}
		if field.Type == search.KeywordsField {
			values, ok := value.([]any)
			if !ok {
				return nil, search.ErrInvalidDocument
			}
			for _, value := range values {
				text, ok := value.(string)
				if !ok || len(text) > MaxKeywordBytes {
					return nil, search.ErrInvalidDocument
				}
			}
		}
	}
	projected := driverutil.Physical(document, definition)
	for _, field := range definition.Fields {
		if field.Type == search.IntegerField {
			if value, ok := driverutil.At(object, field.Path); ok {
				projected[driverutil.FieldKey(field.Name)] = value
			}
		}
	}
	return projected, nil
}

// Upsert replaces the complete document and waits for refresh on every participating shard.
// @group Mutations
func (d *Driver) Upsert(ctx context.Context, definition search.Definition, document search.Document) (search.Receipt, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := d.ready(ctx, definition); err != nil {
		return search.Receipt{}, err
	}
	body, err := physical(document, definition)
	if err != nil {
		return search.Receipt{}, err
	}
	var result writeResult
	path := "/" + d.name + "/_doc/" + driverutil.IDKey(document.ID) + "?refresh=wait_for&wait_for_active_shards=all"
	if err := d.http.Do(ctx, "PUT", path, body, &result, true); err != nil {
		return search.Receipt{}, documentError(err)
	}
	if result.ID != driverutil.IDKey(document.ID) || result.Index != d.name || !result.visible("index") {
		return search.Receipt{}, search.ErrOutcomeUnknown
	}
	return d.ledger.Complete(search.Outcome{State: search.OutcomeSucceeded})
}

// Delete treats missing documents idempotently while refusing to hide a removed index.
// @group Mutations
func (d *Driver) Delete(ctx context.Context, definition search.Definition, id search.DocumentID) (search.Receipt, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := d.ready(ctx, definition); err != nil {
		return search.Receipt{}, err
	}
	var result writeResult
	path := "/" + d.name + "/_doc/" + driverutil.IDKey(id) + "?refresh=wait_for&wait_for_active_shards=all"
	err := d.http.Do(ctx, "DELETE", path, nil, &result, true)
	if errors.Is(err, search.ErrIndexNotFound) {
		var mapping map[string]any
		if err := d.http.Do(ctx, "GET", "/"+d.name+"/_mapping", nil, &mapping, false); err != nil {
			return search.Receipt{}, err
		}
	} else if err != nil {
		return search.Receipt{}, err
	} else if result.ID != driverutil.IDKey(id) || result.Index != d.name || !result.visible("delete") {
		return search.Receipt{}, search.ErrOutcomeUnknown
	}
	return d.ledger.Complete(search.Outcome{State: search.OutcomeSucceeded})
}

// Bulk submits native NDJSON and checks every item even when the HTTP request succeeds.
// @group Mutations
func (d *Driver) Bulk(ctx context.Context, definition search.Definition, mutations []search.Mutation) (search.BulkResult, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := d.ready(ctx, definition); err != nil {
		return search.BulkResult{}, err
	}
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	for _, mutation := range mutations {
		action := "delete"
		if mutation.Kind() == search.MutationUpsert {
			action = "index"
		}
		if err := encoder.Encode(map[string]any{action: map[string]any{"_index": d.name, "_id": driverutil.IDKey(mutation.ID())}}); err != nil {
			return search.BulkResult{}, search.ErrInvalidDocument
		}
		if document, ok := mutation.Document(); ok {
			projected, err := physical(document, definition)
			if err != nil {
				return search.BulkResult{}, err
			}
			if err := encoder.Encode(projected); err != nil {
				return search.BulkResult{}, search.ErrInvalidDocument
			}
		}
	}
	response, err := d.http.DoBytes(ctx, "POST", "/_bulk?refresh=wait_for&wait_for_active_shards=all", "application/x-ndjson", body.Bytes(), true)
	if err != nil {
		return search.BulkResult{}, documentError(err)
	}
	var result struct {
		Items []map[string]writeResult `json:"items"`
	}
	if err := json.Unmarshal(response, &result); err != nil {
		return search.BulkResult{}, search.ErrOutcomeUnknown
	}
	items := make([]search.ItemResult, len(mutations))
	terminal := make([]search.ItemResult, len(mutations))
	state := search.OutcomeSucceeded
	protocolMismatch := len(result.Items) != len(mutations)
	for position, entry := range result.Items[:min(len(result.Items), len(mutations))] {
		action := "delete"
		if mutations[position].Kind() == search.MutationUpsert {
			action = "index"
		}
		proof, ok := entry[action]
		if !ok || len(entry) != 1 || proof.ID != driverutil.IDKey(mutations[position].ID()) || proof.Index != d.name {
			return d.partialBulkOutcome(mutations, terminal)
		}
		item := search.ItemResult{Position: position, ID: mutations[position].ID(), Status: search.ItemAccepted}
		final := item
		final.Status = search.ItemSucceeded
		hasError := proof.hasError()
		missingDelete := action == "delete" && proof.Status == 404 && proof.Result == "not_found" && !hasError
		successfulStatus := proof.Status >= 200 && proof.Status < 300
		unprovenSuccess := successfulStatus && !proof.visible(action)
		if proof.Shards.Total < 0 || proof.Shards.Successful < 0 || proof.Shards.Failed != 0 || proof.Status < 200 || proof.Status >= 300 && proof.Status < 400 || proof.Status >= 500 || unprovenSuccess {
			item.Status, final.Status = search.ItemUnknown, search.ItemUnknown
			item.Error, final.Error = search.ErrOutcomeUnknown, search.ErrOutcomeUnknown
			state = search.OutcomeUnknown
		} else if !missingDelete && !successfulStatus {
			item.Status, final.Status = search.ItemRejected, search.ItemFailed
			item.Error = classifyStatus(proof.Status)
			final.Error = item.Error
			if state != search.OutcomeUnknown {
				state = search.OutcomeFailed
			}
		}
		items[position], terminal[position] = item, final
	}
	if protocolMismatch {
		return d.partialBulkOutcome(mutations, terminal)
	}
	var failure error
	if state == search.OutcomeFailed {
		failure = search.ErrMutationFailed
	}
	if state == search.OutcomeUnknown {
		failure = search.ErrOutcomeUnknown
	}
	receipt, err := d.ledger.Complete(search.Outcome{State: state, Error: failure, Bulk: &search.BulkOutcome{Items: terminal, Precision: search.PerItem}})
	return search.BulkResult{Receipt: receipt, Items: items, Precision: search.PerItem}, err
}

// classifyStatus keeps only stable allowlisted classifications from native bulk failures.
func classifyStatus(status int) error {
	switch status {
	case 400, 422:
		return search.ErrInvalidDocument
	case 401, 403:
		return search.ErrUnauthorized
	case 404:
		return search.ErrIndexNotFound
	case 409:
		return search.ErrConflict
	case 429:
		return search.ErrRateLimited
	default:
		return search.ErrMutationFailed
	}
}

// documentError translates protocol status without exposing backend reasons or document content.
func documentError(err error) error {
	var status *driverutil.StatusError
	if errors.As(err, &status) && (status.Code == 400 || status.Code == 413 || status.Code == 422) {
		return search.ErrInvalidDocument
	}
	return err
}

// Wait returns visibility evidence established by the refresh-waiting write, including exact bulk outcomes.
// @group Receipts
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

// partialBulkOutcome retains proven item evidence when a later native response cannot be correlated safely.
func (d *Driver) partialBulkOutcome(mutations []search.Mutation, terminal []search.ItemResult) (search.BulkResult, error) {
	known := false
	for _, item := range terminal {
		if item.Status == search.ItemSucceeded || item.Status == search.ItemFailed {
			known = true
		}
	}
	if !known {
		return search.BulkResult{}, search.ErrOutcomeUnknown
	}
	for position, mutation := range mutations {
		if terminal[position].Status == "" {
			terminal[position] = search.ItemResult{Position: position, ID: mutation.ID(), Status: search.ItemUnknown, Error: search.ErrOutcomeUnknown}
		}
	}
	receipt, err := d.ledger.Complete(search.Outcome{State: search.OutcomeUnknown, Error: search.ErrOutcomeUnknown, Bulk: &search.BulkOutcome{Precision: search.PerItem, Items: terminal}})
	if err != nil {
		return search.BulkResult{}, search.ErrOutcomeUnknown
	}
	return search.BulkResult{}, search.NewUnknownOutcomeError(receipt)
}

// hasError treats an explicit JSON null as absence while retaining contradictory native error objects.
func (r writeResult) hasError() bool {
	return len(r.Error) != 0 && !bytes.Equal(r.Error, []byte("null"))
}

// visible requires coherent operation results and successful nonnegative shard acknowledgement.
func (r writeResult) visible(action string) bool {
	result := action == "index" && (r.Result == "created" || r.Result == "updated") || action == "delete" && r.Result == "deleted"
	return result && !r.hasError() && r.Shards.Total > 0 && r.Shards.Successful == r.Shards.Total && r.Shards.Failed == 0
}
