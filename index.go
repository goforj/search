package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"time"
)

// Backend is the consumer-owned request-path contract implemented by concrete drivers.
// Implementations must support concurrent calls and return caller-owned response collections.
type Backend interface {
	Search(context.Context, Definition, Query) (Result, error)
	Upsert(context.Context, Definition, Document) (Receipt, error)
	Delete(context.Context, Definition, DocumentID) (Receipt, error)
	Bulk(context.Context, Definition, []Mutation) (BulkResult, error)
	Wait(context.Context, Receipt) (Outcome, error)
}

// Option configures domain limits or safe observation before the handle is published.
type Option func(*Index) error

// Index validates calls and owns its immutable definition. It is safe for concurrent use.
type Index struct {
	backend    Backend
	definition Definition
	limits     Limits
	observer   Observer
}

// WithLimits replaces nonzero limits, up to sixteen times each default ceiling.
// @group Construction
func WithLimits(l Limits) Option {
	return func(i *Index) error {
		n, err := mergeLimits(l)
		i.limits = n
		return err
	}
}

// WithObserver attaches synchronous, panic-isolated observation without retaining contexts.
// @group Construction
func WithObserver(o Observer) Option {
	return func(i *Index) error {
		i.observer = o
		return nil
	}
}

// NewIndex validates a definition without performing backend I/O or schema mutation.
// @group Construction
func NewIndex(b Backend, d Definition, opts ...Option) (*Index, error) {
	i := &Index{backend: b, limits: DefaultLimits()}
	for _, o := range opts {
		if err := o(i); err != nil {
			return nil, err
		}
	}
	n, err := normalizeDefinition(d, i.limits)
	if err != nil {
		return nil, err
	}
	i.definition = n
	return i, nil
}

// Definition returns an owned copy of the normalized definition.
// @group Definitions
func (i *Index) Definition() Definition {
	return cloneDefinition(i.definition)
}

// Search validates literal text, filters, limits, and result documents before exposure.
// @group Queries
func (i *Index) Search(ctx context.Context, q Query) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	q, err := normalizeQuery(q, i.definition, i.limits)
	if err != nil {
		return Result{}, err
	}
	start := time.Now()
	r, err := i.backend.Search(ctx, i.Definition(), q)
	r.Took = time.Since(start)
	if err == nil {
		for x, h := range r.Hits {
			d, _, e := normalizeDocument(h.Document, i.definition, i.limits)
			if e != nil {
				err = ErrUnavailable
				break
			}
			r.Hits[x].Document = d
		}
		if uint64(len(r.Hits)) > q.Limit {
			err = ErrUnavailable
		}
	}
	i.emit(ctx, Event{Kind: "search", Phase: PhaseSearchComplete, Duration: r.Took, HitCount: len(r.Hits), TotalRelation: r.Total.Relation}, err)
	if err != nil {
		return Result{}, SafeError(err)
	}
	return r, nil
}

// Upsert replaces the complete document and returns accepted-work evidence.
// @group Mutations
func (i *Index) Upsert(ctx context.Context, d Document) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	n, _, err := normalizeDocument(d, i.definition, i.limits)
	if err != nil {
		return Receipt{}, err
	}
	start := time.Now()
	r, err := i.backend.Upsert(ctx, i.Definition(), n)
	if err == nil {
		r, err = i.bind(r)
	}
	i.emit(ctx, Event{Kind: "upsert", Phase: PhaseSubmission, Duration: time.Since(start), DocumentCount: 1, ReceiptID: receiptID(r)}, err)
	if err != nil {
		return Receipt{}, i.mutationError(err, nil)
	}
	return r, nil
}

// Delete removes an ID idempotently; visibility is established through Wait.
// @group Mutations
func (i *Index) Delete(ctx context.Context, id DocumentID) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if !validID(id, i.limits) {
		return Receipt{}, ErrInvalidDocument
	}
	start := time.Now()
	r, err := i.backend.Delete(ctx, i.Definition(), id)
	if err == nil {
		r, err = i.bind(r)
	}
	i.emit(ctx, Event{Kind: "delete", Phase: PhaseSubmission, Duration: time.Since(start), DocumentCount: 1, ReceiptID: receiptID(r)}, err)
	if err != nil {
		return Receipt{}, i.mutationError(err, nil)
	}
	return r, nil
}

// Bulk rejects duplicate IDs and validates the entire bounded batch before submission.
// @group Mutations
func (i *Index) Bulk(ctx context.Context, ms []Mutation) (BulkResult, error) {
	if err := ctx.Err(); err != nil {
		return BulkResult{}, err
	}
	if len(ms) == 0 || len(ms) > i.limits.BulkDocuments {
		return BulkResult{}, ErrInvalidDocument
	}
	out := make([]Mutation, len(ms))
	seen := map[DocumentID]bool{}
	size := 0
	for x, m := range ms {
		if seen[m.ID()] || !validID(m.ID(), i.limits) {
			return BulkResult{}, ErrInvalidDocument
		}
		seen[m.ID()] = true
		switch m.kind {
		case MutationUpsert:
			d, _, err := normalizeDocument(m.doc, i.definition, i.limits)
			if err != nil {
				return BulkResult{}, err
			}
			m.doc = d
		case MutationDelete:
		default:
			return BulkResult{}, ErrInvalidDocument
		}
		size += len(m.doc.Body) + len(m.ID())
		if size > i.limits.BulkBytes {
			return BulkResult{}, ErrInvalidDocument
		}
		out[x] = m
	}
	start := time.Now()
	r, err := i.backend.Bulk(ctx, i.Definition(), out)
	r.Items = slices.Clone(r.Items)
	if err == nil {
		r.Receipt, err = i.bind(r.Receipt)
	}
	if err == nil {
		if len(r.Items) != len(ms) || (r.Precision != PerItem && r.Precision != BatchOnly) {
			err = ErrUnavailable
		} else {
			for x, item := range r.Items {
				if item.Position != x || item.ID != ms[x].ID() || (item.Status != ItemAccepted && item.Status != ItemRejected && item.Status != ItemUnknown) {
					err = ErrUnavailable
					break
				}
				r.Items[x].Error = SafeError(item.Error)
			}
		}
	}
	if err == nil {
		r.Receipt.state.bulk = slices.Clone(r.Items)
	}
	i.emit(ctx, Event{Kind: "bulk", Phase: PhaseSubmission, Duration: time.Since(start), DocumentCount: len(ms), ReceiptID: receiptID(r.Receipt)}, err)
	if err != nil {
		return BulkResult{}, i.mutationError(err, out)
	}
	return r, nil
}

// Wait returns terminal failure as Outcome data; method errors mean no trustworthy outcome yet.
// @group Receipts
func (i *Index) Wait(ctx context.Context, r Receipt) (Outcome, error) {
	if err := ctx.Err(); err != nil {
		return Outcome{}, err
	}
	if r.owner != i || len(r.token) == 0 {
		return Outcome{}, ErrInvalidReceipt
	}
	if r.state == nil {
		return Outcome{}, ErrInvalidReceipt
	}
	r.state.mu.Lock()
	if r.state.outcome != nil {
		cached := cloneOutcome(*r.state.outcome)
		r.state.mu.Unlock()
		return cached, nil
	}
	r.state.mu.Unlock()
	start := time.Now()
	o, err := i.backend.Wait(ctx, r)
	if err != nil {
		return Outcome{}, SafeError(err)
	}
	if o.State != OutcomeSucceeded && o.State != OutcomeFailed && o.State != OutcomeUnknown {
		return Outcome{}, ErrUnavailable
	}
	o = cloneOutcome(o)
	o.Error = SafeError(o.Error)
	if o.State == OutcomeSucceeded {
		o.Error = nil
		o.VisibleAt = time.Now()
	} else {
		o.VisibleAt = time.Time{}
		if o.Error == nil {
			o.Error = ErrMutationFailed
			if o.State == OutcomeUnknown {
				o.Error = ErrOutcomeUnknown
			}
		}
	}
	if o.Bulk == nil && len(r.state.bulk) != 0 {
		if o.State != OutcomeUnknown || !errors.Is(o.Error, ErrReceiptExpired) {
			return Outcome{}, ErrUnavailable
		}
		items := slices.Clone(r.state.bulk)
		for n := range items {
			if items[n].Status != ItemRejected {
				items[n].Status = ItemUnknown
				items[n].Error = ErrReceiptExpired
			}
		}
		o.Bulk = &BulkOutcome{Items: items, Precision: BatchOnly}
	}
	if !validBulkOutcome(o, r.state.bulk) {
		return Outcome{}, ErrUnavailable
	}
	if o.Bulk != nil {
		for x := range o.Bulk.Items {
			o.Bulk.Items[x].Error = SafeError(o.Bulk.Items[x].Error)
		}
	}
	r.state.mu.Lock()
	if r.state.outcome != nil {
		o = cloneOutcome(*r.state.outcome)
	} else {
		cached := cloneOutcome(o)
		r.state.outcome = &cached
	}
	r.state.mu.Unlock()
	i.emit(ctx, Event{Kind: "wait", Phase: PhaseTerminal, Duration: time.Since(start), ReceiptID: receiptID(r), Outcome: string(o.State)}, o.Error)
	return cloneOutcome(o), nil
}

// bind prevents forged and cross-handle receipts from reaching backend inspection.
func (i *Index) bind(r Receipt) (Receipt, error) {
	if len(r.token) == 0 || len(r.token) > 512 {
		return Receipt{}, ErrUnavailable
	}
	r.token = slices.Clone(r.token)
	r.owner = i
	r.state = &receiptState{}
	return r, nil
}

// emit enriches events only with safe root-owned identifiers and classifications.
func (i *Index) emit(ctx context.Context, e Event, err error) {
	if i.observer == nil {
		return
	}
	e.Index = i.definition.Name
	if err != nil {
		e.ErrorCode = SafeError(err).Error()
	}
	notify(ctx, i.observer, e)
}

// receiptID exposes a bounded one-way digest rather than backend task data.
func receiptID(r Receipt) string {
	if len(r.token) == 0 {
		return ""
	}
	sum := sha256.Sum256(r.token)
	return hex.EncodeToString(sum[:8])
}

// cloneOutcome prevents callers from corrupting cached terminal evidence.
func cloneOutcome(o Outcome) Outcome {
	if o.Bulk != nil {
		b := *o.Bulk
		b.Items = slices.Clone(b.Items)
		o.Bulk = &b
	}
	return o
}

// mutationError attaches recovered work to the same handle before exposing uncertainty.
func (i *Index) mutationError(err error, mutations []Mutation) error {
	var unknown *UnknownOutcomeError
	if errors.As(err, &unknown) {
		if receipt, ok := unknown.Receipt(); ok {
			bound, bindErr := i.bind(receipt)
			if bindErr == nil {
				for position, mutation := range mutations {
					bound.state.bulk = append(bound.state.bulk, ItemResult{Position: position, ID: mutation.ID(), Status: ItemUnknown})
				}
				return withContextError(NewUnknownOutcomeError(bound), err)
			}
		}
	}
	return SafeError(err)
}

// validBulkOutcome prevents malformed backend evidence from changing item identity or inventing precision.
func validBulkOutcome(outcome Outcome, submitted []ItemResult) bool {
	if outcome.Bulk == nil {
		return len(submitted) == 0
	}
	bulk := outcome.Bulk
	if len(submitted) == 0 || len(bulk.Items) != len(submitted) || (bulk.Precision != PerItem && bulk.Precision != BatchOnly) {
		return false
	}
	for position, item := range bulk.Items {
		if item.Position != position || item.ID != submitted[position].ID {
			return false
		}
		switch item.Status {
		case ItemSucceeded, ItemFailed:
			if bulk.Precision == BatchOnly {
				return false
			}
		case ItemUnknown:
		case ItemRejected:
			if submitted[position].Status != ItemRejected {
				return false
			}
		default:
			return false
		}
		if submitted[position].Status == ItemRejected && item.Status != ItemRejected && item.Status != ItemFailed {
			return false
		}
		if outcome.State == OutcomeSucceeded && bulk.Precision == PerItem && item.Status != ItemSucceeded {
			return false
		}
	}
	return true
}
