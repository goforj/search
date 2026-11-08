package search

import (
	"context"
	"errors"
)

// ErrNotFound identifies a missing document.
var ErrNotFound = errors.New("search: document not found")

// ErrIndexNotFound identifies an unprovisioned index.
var ErrIndexNotFound = errors.New("search: index not found")

// ErrUnsupported identifies an unavailable backend capability.
var ErrUnsupported = errors.New("search: unsupported operation")

// ErrInvalidDefinition identifies an invalid schema.
var ErrInvalidDefinition = errors.New("search: invalid definition")

// ErrInvalidDocument identifies invalid document input.
var ErrInvalidDocument = errors.New("search: invalid document")

// ErrInvalidQuery identifies invalid query input.
var ErrInvalidQuery = errors.New("search: invalid query")

// ErrInvalidReceipt identifies a forged, foreign, or empty receipt.
var ErrInvalidReceipt = errors.New("search: invalid receipt")

// ErrSchemaDrift identifies a difference from the provisioned schema.
var ErrSchemaDrift = errors.New("search: schema drift")

// ErrConflict identifies a competing owner or conflicting request.
var ErrConflict = errors.New("search: conflict")

// ErrOutcomeUnknown means work may have been accepted and must be reconciled.
var ErrOutcomeUnknown = errors.New("search: mutation outcome unknown")

// ErrUnavailable identifies a failed connection or incomplete backend response.
var ErrUnavailable = errors.New("search: unavailable")

// ErrUnauthorized identifies rejected credentials or permissions.
var ErrUnauthorized = errors.New("search: unauthorized")

// ErrRateLimited identifies throttling.
var ErrRateLimited = errors.New("search: rate limited")

// ErrMutationFailed identifies accepted work that failed.
var ErrMutationFailed = errors.New("search: mutation failed")

// ErrReceiptExpired identifies accepted work whose result is no longer retained.
var ErrReceiptExpired = errors.New("search: receipt expired")

// ErrClosed identifies a closed driver.
var ErrClosed = errors.New("search: closed")

// SafeError removes backend prose and unwrap chains while retaining known classifications.
// @group Backend
func SafeError(err error) error {
	if err == nil {
		return nil
	}
	var unknown *UnknownOutcomeError
	if errors.As(err, &unknown) {
		return withContextError(NewUnknownOutcomeError(unknown.receipt), err)
	}
	if errors.Is(err, ErrOutcomeUnknown) {
		return withContextError(ErrOutcomeUnknown, err)
	}
	for _, known := range []error{context.Canceled, context.DeadlineExceeded, ErrNotFound, ErrIndexNotFound, ErrUnsupported, ErrInvalidDefinition, ErrInvalidDocument, ErrInvalidQuery, ErrInvalidReceipt, ErrSchemaDrift, ErrConflict, ErrOutcomeUnknown, ErrUnavailable, ErrUnauthorized, ErrRateLimited, ErrMutationFailed, ErrReceiptExpired, ErrClosed} {
		if errors.Is(err, known) {
			return known
		}
	}
	return ErrUnavailable
}

// UnknownOutcomeError retains recoverable accepted-work evidence without exposing backend prose.
type UnknownOutcomeError struct {
	receipt Receipt
}

// NewUnknownOutcomeError lets a driver return recoverable evidence after uncertain submission.
// @group Receipts
func NewUnknownOutcomeError(receipt Receipt) *UnknownOutcomeError {
	return &UnknownOutcomeError{receipt: receipt}
}

// Error reports uncertainty without serializing the opaque token.
// @group Receipts
func (e *UnknownOutcomeError) Error() string {
	return ErrOutcomeUnknown.Error()
}

// Unwrap preserves the stable uncertainty classification.
// @group Receipts
func (e *UnknownOutcomeError) Unwrap() error {
	return ErrOutcomeUnknown
}

// Receipt returns immutable recovery evidence when the driver obtained a trustworthy token.
// @group Receipts
func (e *UnknownOutcomeError) Receipt() (Receipt, bool) {
	return e.receipt, len(e.receipt.token) != 0
}

// withContextError preserves cancellation identity alongside accepted-work uncertainty without retaining raw causes.
func withContextError(base, cause error) error {
	if errors.Is(cause, context.Canceled) {
		return errors.Join(base, context.Canceled)
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return errors.Join(base, context.DeadlineExceeded)
	}
	return base
}
