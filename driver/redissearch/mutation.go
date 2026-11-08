package redissearch

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// mutationScript applies ordered complete hash replacements without exposing a partially updated document.
const mutationScript = `
if redis.call('EXISTS',KEYS[1]) == 0 then return redis.error_reply('INDEX_MISSING') end
local position = 1
for key = 2,#KEYS do
 local count = tonumber(ARGV[position])
 position = position + 1
 redis.call('DEL',KEYS[key])
 if count > 0 then
  local fields = {}
  for value = 1,count*2 do
   fields[value] = ARGV[position]
   position = position + 1
  end
  redis.call('HSET',KEYS[key],unpack(fields))
 end
end
return #KEYS-1
`

// projection keeps numeric comparisons native while making keyword and array TAG values syntax-inert.
func projection(document search.Document, def search.Definition) map[string]string {
	values := driverutil.Physical(document, def)
	result := make(map[string]string, len(values))
	for key, value := range values {
		switch value := value.(type) {
		case string:
			result[key] = value
		case []string:
			result[key] = strings.Join(value, ",")
		}
	}
	object := driverutil.Object(document)
	for _, field := range def.Fields {
		if field.Type != search.IntegerField {
			continue
		}
		if value, ok := driverutil.At(object, field.Path); ok {
			result[driverutil.FieldKey(field.Name)] = fmt.Sprint(value)
		}
	}
	return result
}

// submit checks native existence and uses one atomic Lua command for all ordered writes.
func (d *Driver) submit(ctx context.Context, def search.Definition, mutations []search.Mutation, bulk bool) (search.Receipt, error) {
	if _, err := d.command(ctx, false, "FT.INFO", d.name); err != nil {
		return search.Receipt{}, err
	}
	keys := []string{d.name + ":meta"}
	values := []string{}
	for _, mutation := range mutations {
		keys = append(keys, d.name+":doc:"+driverutil.IDKey(mutation.ID()))
		if document, ok := mutation.Document(); ok {
			projected := projection(document, def)
			fields := make([]string, 0, len(projected))
			for field := range projected {
				fields = append(fields, field)
			}
			sort.Strings(fields)
			values = append(values, strconv.Itoa(len(fields)))
			for _, field := range fields {
				values = append(values, field, projected[field])
			}
		} else {
			values = append(values, "0")
		}
	}
	args := []string{"EVAL", mutationScript, strconv.Itoa(len(keys))}
	args = append(args, keys...)
	args = append(args, values...)
	result, err := d.command(ctx, true, args...)
	if err != nil {
		return search.Receipt{}, err
	}
	count, ok := result.(int64)
	if !ok || count != int64(len(mutations)) {
		return search.Receipt{}, search.ErrOutcomeUnknown
	}
	terminal := make([]search.ItemResult, len(mutations))
	for n, mutation := range mutations {
		terminal[n] = search.ItemResult{Position: n, ID: mutation.ID(), Status: search.ItemSucceeded}
	}
	outcome := search.Outcome{State: search.OutcomeSucceeded}
	if bulk {
		outcome.Bulk = &search.BulkOutcome{Items: terminal, Precision: search.PerItem}
	}
	return d.ledger.Complete(outcome)
}

// Upsert replaces the entire native hash atomically before issuing a visibility receipt.
// @group Mutations
func (d *Driver) Upsert(ctx context.Context, def search.Definition, document search.Document) (search.Receipt, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := d.ready(ctx, def); err != nil {
		return search.Receipt{}, err
	}
	mutation, err := search.UpsertMutation(document)
	if err != nil {
		return search.Receipt{}, err
	}
	return d.submit(ctx, def, []search.Mutation{mutation}, false)
}

// Delete removes the complete native hash idempotently while checking index existence.
// @group Mutations
func (d *Driver) Delete(ctx context.Context, def search.Definition, id search.DocumentID) (search.Receipt, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := d.ready(ctx, def); err != nil {
		return search.Receipt{}, err
	}
	mutation, err := search.DeleteMutation(id)
	if err != nil {
		return search.Receipt{}, err
	}
	return d.submit(ctx, def, []search.Mutation{mutation}, false)
}

// Bulk atomically applies ordered mutations and retains exact per-item terminal success evidence.
// @group Mutations
func (d *Driver) Bulk(ctx context.Context, def search.Definition, mutations []search.Mutation) (search.BulkResult, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if err := d.ready(ctx, def); err != nil {
		return search.BulkResult{}, err
	}
	receipt, err := d.submit(ctx, def, mutations, true)
	if err != nil {
		return search.BulkResult{}, err
	}
	items := make([]search.ItemResult, len(mutations))
	for n, mutation := range mutations {
		items[n] = search.ItemResult{Position: n, ID: mutation.ID(), Status: search.ItemAccepted}
	}
	return search.BulkResult{Receipt: receipt, Items: items, Precision: search.PerItem}, nil
}

// Wait returns only visibility established by an acknowledged atomic mutation command.
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
