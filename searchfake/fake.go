// Package searchfake supplies a scripted backend for isolated application tests.
// It records calls without pretending to reproduce production ranking or administration.
package searchfake

import (
	"context"
	"slices"
	"sync"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// Call records the operation and owned inputs received after root validation.
type Call struct {
	Kind       string
	Definition search.Definition
	Query      search.Query
	Document   search.Document
	ID         search.DocumentID
	Mutations  []search.Mutation
}

// response retains one queued search result and its intentional failure.
type response struct {
	result search.Result
	err    error
}

// Backend is a concurrent, explicitly scripted test double.
type Backend struct {
	mu        sync.Mutex
	calls     []Call
	responses []response
	ledger    driverutil.Ledger
}

// New creates a fake whose writes succeed and whose reads require queued responses.
// @group Testing
func New() *Backend {
	return &Backend{}
}

// QueueSearch appends a detached response for the next Search call.
// @group Testing
func (b *Backend) QueueSearch(result search.Result, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.responses = append(b.responses, response{result: cloneResult(result), err: err})
}

// Calls returns a detached snapshot so assertions cannot alter recorded evidence.
// @group Testing
func (b *Backend) Calls() []Call {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Call, len(b.calls))
	for n, call := range b.calls {
		out[n] = cloneCall(call)
	}
	return out
}

// Search consumes the next scripted result, failing explicitly when no response is queued.
// @group Testing
func (b *Backend) Search(ctx context.Context, def search.Definition, query search.Query) (search.Result, error) {
	if err := ctx.Err(); err != nil {
		return search.Result{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, cloneCall(Call{Kind: "search", Definition: def, Query: query}))
	if len(b.responses) == 0 {
		return search.Result{}, search.ErrUnavailable
	}
	r := b.responses[0]
	b.responses = b.responses[1:]
	return cloneResult(r.result), r.err
}

// Upsert records a replacement and issues synchronous success evidence.
// @group Testing
func (b *Backend) Upsert(ctx context.Context, def search.Definition, doc search.Document) (search.Receipt, error) {
	if err := ctx.Err(); err != nil {
		return search.Receipt{}, err
	}
	b.mu.Lock()
	b.calls = append(b.calls, cloneCall(Call{Kind: "upsert", Definition: def, Document: doc}))
	b.mu.Unlock()
	return b.ledger.Complete(search.Outcome{State: search.OutcomeSucceeded})
}

// Delete records an idempotent removal and issues synchronous success evidence.
// @group Testing
func (b *Backend) Delete(ctx context.Context, def search.Definition, id search.DocumentID) (search.Receipt, error) {
	if err := ctx.Err(); err != nil {
		return search.Receipt{}, err
	}
	b.mu.Lock()
	b.calls = append(b.calls, cloneCall(Call{Kind: "delete", Definition: def, ID: id}))
	b.mu.Unlock()
	return b.ledger.Complete(search.Outcome{State: search.OutcomeSucceeded})
}

// Bulk records input order and returns individually proven success for every item.
// @group Testing
func (b *Backend) Bulk(ctx context.Context, def search.Definition, mutations []search.Mutation) (search.BulkResult, error) {
	if err := ctx.Err(); err != nil {
		return search.BulkResult{}, err
	}
	b.mu.Lock()
	b.calls = append(b.calls, cloneCall(Call{Kind: "bulk", Definition: def, Mutations: mutations}))
	b.mu.Unlock()
	items, terminal := make([]search.ItemResult, len(mutations)), make([]search.ItemResult, len(mutations))
	for n, mutation := range mutations {
		items[n] = search.ItemResult{Position: n, ID: mutation.ID(), Status: search.ItemAccepted}
		terminal[n] = search.ItemResult{Position: n, ID: mutation.ID(), Status: search.ItemSucceeded}
	}
	r, err := b.ledger.Complete(search.Outcome{State: search.OutcomeSucceeded, Bulk: &search.BulkOutcome{Items: terminal, Precision: search.PerItem}})
	return search.BulkResult{Receipt: r, Items: items, Precision: search.PerItem}, err
}

// Wait respects cancellation and returns retained terminal evidence.
// @group Testing
func (b *Backend) Wait(ctx context.Context, receipt search.Receipt) (search.Outcome, error) {
	return b.ledger.Wait(ctx, receipt)
}

// cloneCall protects collection and JSON ownership at the assertion boundary.
func cloneCall(c Call) Call {
	c.Definition.Fields = slices.Clone(c.Definition.Fields)
	c.Definition.DefaultSearchable = slices.Clone(c.Definition.DefaultSearchable)
	c.Definition.DefaultSort = slices.Clone(c.Definition.DefaultSort)
	c.Query.Fields = slices.Clone(c.Query.Fields)
	c.Query.Sort = slices.Clone(c.Query.Sort)
	c.Query.Facets = slices.Clone(c.Query.Facets)
	c.Query.Highlight = slices.Clone(c.Query.Highlight)
	c.Document.Body = slices.Clone(c.Document.Body)
	c.Mutations = slices.Clone(c.Mutations)
	return c
}

// cloneResult detaches all mutable response data, including score pointers and highlight maps.
func cloneResult(r search.Result) search.Result {
	r.Hits = slices.Clone(r.Hits)
	for n, h := range r.Hits {
		h.Document.Body = slices.Clone(h.Document.Body)
		h.SortValues = slices.Clone(h.SortValues)
		if h.Score != nil {
			value := *h.Score
			h.Score = &value
		}
		if h.Highlights != nil {
			highlights := map[search.FieldName][]search.Fragment{}
			for field, fragments := range h.Highlights {
				fragments = slices.Clone(fragments)
				for x := range fragments {
					fragments[x].Segments = slices.Clone(fragments[x].Segments)
				}
				highlights[field] = fragments
			}
			h.Highlights = highlights
		}
		r.Hits[n] = h
	}
	r.Facets = slices.Clone(r.Facets)
	for n := range r.Facets {
		r.Facets[n].Buckets = slices.Clone(r.Facets[n].Buckets)
	}
	return r
}

// Backend conformance is checked at compile time.
var _ search.Backend = (*Backend)(nil)
