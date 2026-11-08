package search

import (
	"slices"
	"sync"
	"time"
)

// MutationKind distinguishes complete replacement from idempotent deletion.
type MutationKind string

const (
	// MutationUpsert replaces a complete document.
	MutationUpsert MutationKind = "upsert"
	// MutationDelete removes an ID if present.
	MutationDelete MutationKind = "delete"
)

// Mutation is an immutable tagged operation whose zero value is invalid.
type Mutation struct {
	kind MutationKind
	doc  Document
}

// UpsertMutation takes ownership of document bytes after structural validation.
// @group Mutations
func UpsertMutation(d Document) (Mutation, error) {
	n, _, err := normalizeDocument(d, Definition{}, DefaultLimits())
	if err != nil {
		return Mutation{}, err
	}
	return Mutation{MutationUpsert, n}, nil
}

// DeleteMutation validates an ID for idempotent deletion.
// @group Mutations
func DeleteMutation(id DocumentID) (Mutation, error) {
	if !validID(id, DefaultLimits()) {
		return Mutation{}, ErrInvalidDocument
	}
	return Mutation{kind: MutationDelete, doc: Document{ID: id}}, nil
}

// Kind identifies which operation this mutation contains.
// @group Mutations
func (m Mutation) Kind() MutationKind {
	return m.kind
}

// ID returns the explicit document identity.
// @group Mutations
func (m Mutation) ID() DocumentID {
	return m.doc.ID
}

// Document returns owned data only for upserts.
// @group Mutations
func (m Mutation) Document() (Document, bool) {
	return Document{m.doc.ID, slices.Clone(m.doc.Body)}, m.kind == MutationUpsert
}

// Receipt is an opaque, immutable process-local accepted-work token.
type Receipt struct {
	token []byte
	owner *Index
	state *receiptState
}

// NewReceipt creates a bounded driver token without attaching it to an Index.
// @group Receipts
func NewReceipt(token []byte) (Receipt, error) {
	if len(token) == 0 || len(token) > 512 {
		return Receipt{}, ErrInvalidReceipt
	}
	return Receipt{token: slices.Clone(token)}, nil
}

// Token returns an owned copy for the issuing backend.
// @group Receipts
func (r Receipt) Token() []byte {
	return slices.Clone(r.token)
}

// OutcomeState describes a trustworthy terminal mutation outcome.
type OutcomeState string

const (
	// OutcomeSucceeded proves visibility to subsequent queries on this index.
	OutcomeSucceeded OutcomeState = "succeeded"
	// OutcomeFailed identifies terminal failure.
	OutcomeFailed OutcomeState = "failed"
	// OutcomeUnknown identifies accepted work whose terminal result cannot be proved.
	OutcomeUnknown OutcomeState = "unknown"
)

// ItemStatus retains submission and terminal states separately.
type ItemStatus string

const (
	// ItemAccepted reports proven submission only.
	ItemAccepted ItemStatus = "accepted"
	// ItemRejected reports a submission rejection.
	ItemRejected ItemStatus = "rejected"
	// ItemUnknown reports an unprovable individual outcome.
	ItemUnknown ItemStatus = "unknown"
	// ItemSucceeded reports proven per-item visibility.
	ItemSucceeded ItemStatus = "succeeded"
	// ItemFailed reports proven per-item failure.
	ItemFailed ItemStatus = "failed"
)

// ItemOutcomePrecision prevents aggregate success from implying individual success.
type ItemOutcomePrecision string

const (
	// PerItem means every item has an individually proven outcome.
	PerItem ItemOutcomePrecision = "per_item"
	// BatchOnly means only the aggregate outcome is known.
	BatchOnly ItemOutcomePrecision = "batch_only"
)

// ItemResult preserves the input position even when the backend reorders work.
type ItemResult struct {
	Position int
	ID       DocumentID
	Status   ItemStatus
	Error    error
}

// BulkResult retains submission precision and accepted-work receipt.
type BulkResult struct {
	Receipt   Receipt
	Items     []ItemResult
	Precision ItemOutcomePrecision
}

// BulkOutcome retains terminal per-item evidence without inventing precision.
type BulkOutcome struct {
	Items     []ItemResult
	Precision ItemOutcomePrecision
}

// Outcome returns terminal failure as data, keeping any bulk details available.
type Outcome struct {
	State     OutcomeState
	VisibleAt time.Time
	Bulk      *BulkOutcome
	Error     error
}

// receiptState keeps monotonic evidence alive only as long as its receipt.
type receiptState struct {
	bulk    []ItemResult
	mu      sync.Mutex
	outcome *Outcome
}
