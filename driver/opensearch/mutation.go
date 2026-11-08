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
	if result.ID != driverutil.IDKey(document.ID) || result.Index != d.name || result.Shards.Failed != 0 || result.Shards.Total == 0 || result.Shards.Successful != result.Shards.Total || (result.Result != "created" && result.Result != "updated") {
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
	} else if result.ID != driverutil.IDKey(id) || result.Index != d.name || result.Result != "deleted" || result.Shards.Failed != 0 || result.Shards.Total == 0 || result.Shards.Successful != result.Shards.Total {
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
	if err := json.Unmarshal(response, &result); err != nil || len(result.Items) != len(mutations) {
		return search.BulkResult{}, search.ErrOutcomeUnknown
	}
	items := make([]search.ItemResult, len(mutations))
	terminal := make([]search.ItemResult, len(mutations))
	state := search.OutcomeSucceeded
	for position, entry := range result.Items {
		action := "delete"
		if mutations[position].Kind() == search.MutationUpsert {
			action = "index"
		}
		proof, ok := entry[action]
		if !ok || len(entry) != 1 || proof.ID != driverutil.IDKey(mutations[position].ID()) || proof.Index != d.name {
			return search.BulkResult{}, search.ErrOutcomeUnknown
		}
		item := search.ItemResult{Position: position, ID: mutations[position].ID(), Status: search.ItemAccepted}
		final := item
		final.Status = search.ItemSucceeded
		missingDelete := action == "delete" && proof.Status == 404 && proof.Result == "not_found" && len(proof.Error) == 0
		if proof.Shards.Failed > 0 || proof.Status >= 500 || proof.Status >= 200 && proof.Status < 300 && (proof.Shards.Total == 0 || proof.Shards.Successful != proof.Shards.Total) {
			item.Status, final.Status = search.ItemUnknown, search.ItemUnknown
			item.Error, final.Error = search.ErrOutcomeUnknown, search.ErrOutcomeUnknown
			state = search.OutcomeUnknown
		} else if !missingDelete && (proof.Status < 200 || proof.Status >= 300 || len(proof.Error) != 0) {
			item.Status, final.Status = search.ItemRejected, search.ItemFailed
			item.Error = classifyStatus(proof.Status)
			final.Error = item.Error
			if state != search.OutcomeUnknown {
				state = search.OutcomeFailed
			}
		}
		items[position], terminal[position] = item, final
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
