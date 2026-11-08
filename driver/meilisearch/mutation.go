package meilisearch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// physical adds explicit presence and a kind discriminator to keep metadata out of searches.
func physical(doc search.Document, def search.Definition) map[string]any {
	m := driverutil.Physical(doc, def)
	m["id"] = driverutil.IDKey(doc.ID)
	m["_forjkind"] = "document"
	obj := driverutil.Object(doc)
	for _, f := range def.Fields {
		k := driverutil.FieldKey(f.Name)
		if _, ok := m[k+"_exists"]; ok {
			m[k+"_exists"] = 1
		} else {
			m[k+"_exists"] = 0
		}
		if f.Type == search.IntegerField {
			if v, ok := driverutil.At(obj, f.Path); ok {
				m[k] = v
			}
		}
	}
	return m
}

// remember retains receipt-to-index binding and bounded batch submission evidence.
func (d *Driver) remember(id *uint64, items []search.ItemResult) (search.Receipt, error) {
	if id == nil {
		return search.Receipt{}, search.ErrOutcomeUnknown
	}
	return d.rememberTasks([]uint64{*id}, items), nil
}

// rememberTasks binds accepted tasks to a content-free process-local receipt.
func (d *Driver) rememberTasks(tasks []uint64, items []search.ItemResult) search.Receipt {
	var random [16]byte
	rand.Read(random[:])
	token := hex.EncodeToString(random[:])
	if len(d.order) >= 4096 {
		delete(d.pending, d.order[0])
		d.order = d.order[1:]
	}
	d.order = append(d.order, token)
	d.pending[token] = &pending{items: slices.Clone(items), tasks: slices.Clone(tasks)}
	receipt, _ := search.NewReceipt([]byte(token))
	return receipt
}

// Upsert submits a complete replacement rather than a partial document merge.
// @group Mutations
func (d *Driver) Upsert(ctx context.Context, def search.Definition, doc search.Document) (search.Receipt, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if e := d.ready(ctx, def); e != nil {
		return search.Receipt{}, e
	}
	var task taskRef
	if e := d.http.Do(ctx, "POST", "/indexes/"+d.name+"/documents", []any{physical(doc, def)}, &task, true); e != nil {
		return search.Receipt{}, e
	}
	return d.remember(task.TaskUID, nil)
}

// Delete submits an idempotent document removal.
// @group Mutations
func (d *Driver) Delete(ctx context.Context, def search.Definition, id search.DocumentID) (search.Receipt, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if e := d.ready(ctx, def); e != nil {
		return search.Receipt{}, e
	}
	var task taskRef
	if e := d.http.Do(ctx, "DELETE", "/indexes/"+d.name+"/documents/"+driverutil.IDKey(id), nil, &task, true); e != nil {
		return search.Receipt{}, e
	}
	return d.remember(task.TaskUID, nil)
}

// Bulk submits contiguous operation groups and retains every accepted task.
// @group Mutations
func (d *Driver) Bulk(ctx context.Context, def search.Definition, mutations []search.Mutation) (search.BulkResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ready(ctx, def); err != nil {
		return search.BulkResult{}, err
	}
	tasks := []uint64{}
	items := make([]search.ItemResult, len(mutations))
	for n, m := range mutations {
		items[n] = search.ItemResult{Position: n, ID: m.ID(), Status: search.ItemAccepted}
	}
	for start := 0; start < len(mutations); {
		end := start + 1
		for end < len(mutations) && mutations[end].Kind() == mutations[start].Kind() {
			end++
		}
		body := []any{}
		path := "/indexes/" + d.name + "/documents"
		for _, mutation := range mutations[start:end] {
			if doc, ok := mutation.Document(); ok {
				body = append(body, physical(doc, def))
			} else {
				body = append(body, driverutil.IDKey(mutation.ID()))
			}
		}
		if mutations[start].Kind() == search.MutationDelete {
			path += "/delete-batch"
		}
		var task taskRef
		if err := d.http.Do(ctx, "POST", path, body, &task, true); err != nil {
			return search.BulkResult{}, d.interruptedTasks(tasks, items, err)
		}
		if task.TaskUID == nil {
			return search.BulkResult{}, d.interruptedTasks(tasks, items, search.ErrOutcomeUnknown)
		}
		tasks = append(tasks, *task.TaskUID)
		start = end
	}
	receipt := d.rememberTasks(tasks, items)
	return search.BulkResult{Receipt: receipt, Items: items, Precision: search.BatchOnly}, nil
}

// Wait reports aggregate task precision without fabricating per-document success.
// @group Receipts
func (d *Driver) Wait(ctx context.Context, r search.Receipt) (search.Outcome, error) {
	if err := ctx.Err(); err != nil {
		return search.Outcome{}, err
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.closed {
		return search.Outcome{}, search.ErrClosed
	}
	p, ok := d.pending[string(r.Token())]
	if !ok {
		return search.Outcome{State: search.OutcomeUnknown, Error: search.ErrReceiptExpired}, nil
	}
	o := search.Outcome{State: search.OutcomeSucceeded}
	for _, task := range p.tasks {
		err := d.waitTask(ctx, &task)
		if err == nil {
			continue
		}
		if errors.Is(err, search.ErrMutationFailed) {
			if o.State != search.OutcomeUnknown {
				o.State = search.OutcomeFailed
				o.Error = err
			}
		} else if errors.Is(err, search.ErrIndexNotFound) {
			o.State = search.OutcomeUnknown
			o.Error = search.ErrReceiptExpired
		} else {
			return search.Outcome{}, err
		}
	}
	if p.uncertain {
		o.State = search.OutcomeUnknown
		o.Error = search.ErrOutcomeUnknown
	}
	if p.items != nil {
		items := slices.Clone(p.items)
		for x := range items {
			items[x].Status = search.ItemUnknown
		}
		o.Bulk = &search.BulkOutcome{Items: items, Precision: search.BatchOnly}
	}
	return o, nil
}

// interruptedTasks preserves acknowledged work even when a later task response is malformed.
func (d *Driver) interruptedTasks(tasks []uint64, items []search.ItemResult, cause error) error {
	if len(tasks) == 0 {
		return cause
	}
	receipt := d.rememberTasks(tasks, items)
	d.pending[string(receipt.Token())].uncertain = true
	return errors.Join(search.NewUnknownOutcomeError(receipt), search.SafeError(cause))
}
