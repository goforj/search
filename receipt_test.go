package search_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/goforj/search"
	"github.com/goforj/search/searchfake"
)

// uncertainBackend simulates accepted work whose acknowledgement was interrupted.
type uncertainBackend struct{ search.Backend }

// Upsert retains the real fake receipt while returning classified submission uncertainty.
func (b uncertainBackend) Upsert(ctx context.Context, def search.Definition, doc search.Document) (search.Receipt, error) {
	receipt, err := b.Backend.Upsert(ctx, def, doc)
	if err != nil {
		return search.Receipt{}, err
	}
	return search.Receipt{}, search.NewUnknownOutcomeError(receipt)
}

// TestRecoverableUnknownReceipt binds accepted recovery evidence to its requesting Index.
func TestRecoverableUnknownReceipt(t *testing.T) {
	index, err := search.NewIndex(uncertainBackend{Backend: searchfake.New()}, minimalDefinition())
	if err != nil {
		t.Fatal(err)
	}
	_, err = search.Upsert(context.Background(), index, "one", map[string]string{"title": "red"})
	var unknown *search.UnknownOutcomeError
	if !errors.As(err, &unknown) || !errors.Is(err, search.ErrOutcomeUnknown) || unknown.Error() != search.ErrOutcomeUnknown.Error() {
		t.Fatal(err)
	}
	receipt, ok := unknown.Receipt()
	if !ok {
		t.Fatal("recoverable receipt lost")
	}
	outcome, err := index.Wait(context.Background(), receipt)
	if err != nil || outcome.State != search.OutcomeSucceeded {
		t.Fatal(outcome, err)
	}
	if _, ok := search.NewUnknownOutcomeError(search.Receipt{}).Receipt(); ok {
		t.Fatal("empty receipt accepted")
	}
}

// outcomeBackend injects only terminal evidence, leaving all submission behavior wired to the fake.
type outcomeBackend struct {
	search.Backend
	outcome search.Outcome
	err     error
}

// Wait models terminal failures independently from an inability to inspect accepted work.
func (b outcomeBackend) Wait(context.Context, search.Receipt) (search.Outcome, error) {
	return b.outcome, b.err
}

// TestTerminalOutcomesPreserveFailureData distinguishes failed accepted work from inspection failures.
func TestTerminalOutcomesPreserveFailureData(t *testing.T) {
	for _, state := range []search.OutcomeState{search.OutcomeFailed, search.OutcomeUnknown, search.OutcomeSucceeded, "invalid"} {
		t.Run(string(state), func(t *testing.T) {
			index, err := search.NewIndex(outcomeBackend{Backend: searchfake.New(), outcome: search.Outcome{State: state}}, minimalDefinition())
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := index.Delete(context.Background(), "one")
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := index.Wait(context.Background(), receipt)
			if state == "invalid" {
				if !errors.Is(err, search.ErrUnavailable) {
					t.Fatal(err)
				}
				return
			}
			if err != nil || outcome.State != state {
				t.Fatal(outcome, err)
			}
			if state != search.OutcomeSucceeded && (outcome.Error == nil || !outcome.VisibleAt.IsZero()) {
				t.Fatal("terminal failure lost its classification")
			}
		})
	}
	index, err := search.NewIndex(outcomeBackend{Backend: searchfake.New(), err: errors.New("private endpoint")}, minimalDefinition())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := index.Delete(context.Background(), "one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.Wait(context.Background(), receipt); !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
}

// TestReceiptTokensAndMutationOwnership prevents caller-owned bytes from altering accepted operations.
func TestReceiptTokensAndMutationOwnership(t *testing.T) {
	for _, token := range [][]byte{nil, make([]byte, 513)} {
		if _, err := search.NewReceipt(token); !errors.Is(err, search.ErrInvalidReceipt) {
			t.Fatal(err)
		}
	}
	token := []byte("token")
	receipt, err := search.NewReceipt(token)
	if err != nil {
		t.Fatal(err)
	}
	token[0] = 'x'
	copy := receipt.Token()
	copy[0] = 'y'
	if string(receipt.Token()) != "token" {
		t.Fatal("token aliases caller bytes")
	}
	doc, err := search.Encode("one", map[string]string{"title": "red"})
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := search.UpsertMutation(doc)
	if err != nil {
		t.Fatal(err)
	}
	doc.Body[0] = '['
	owned, ok := mutation.Document()
	if !ok || mutation.Kind() != search.MutationUpsert || owned.Body[0] != '{' {
		t.Fatal(mutation)
	}
	owned.Body[0] = '['
	owned, _ = mutation.Document()
	if owned.Body[0] != '{' {
		t.Fatal("document accessor aliases mutation state")
	}
	if _, err := search.DeleteMutation(""); !errors.Is(err, search.ErrInvalidDocument) {
		t.Fatal(err)
	}
	if _, err := search.UpsertMutation(search.Document{}); !errors.Is(err, search.ErrInvalidDocument) {
		t.Fatal(err)
	}
	if _, err := search.Encode("one", func() {}); !errors.Is(err, search.ErrInvalidDocument) {
		t.Fatal(err)
	}
	if _, err := search.Decode[map[string]string](search.Document{ID: "one", Body: []byte(`[]`)}); !errors.Is(err, search.ErrInvalidDocument) {
		t.Fatal(err)
	}
	if _, err := search.Decode[map[string]int](search.Document{ID: "one", Body: []byte(`{"title":"red"}`)}); !errors.Is(err, search.ErrInvalidDocument) {
		t.Fatal(err)
	}
	if !strings.Contains((&search.DecodeError{ID: "one"}).Error(), "decode failed") {
		t.Fatal("missing typed error description")
	}
}

// TestLimitOverridesRejectInvalidPolicy proves bad resource budgets fail before backend calls.
func TestLimitOverridesRejectInvalidPolicy(t *testing.T) {
	for _, limits := range []search.Limits{{NameBytes: -1}, {ResultLimit: 1601}, {ResultWindow: 16001}, {ResultLimit: 30, ResultWindow: 20}, {JSONDepth: 513}} {
		if _, err := search.NewIndex(searchfake.New(), minimalDefinition(), search.WithLimits(limits)); !errors.Is(err, search.ErrInvalidDefinition) {
			t.Fatal(err)
		}
	}
	backend := searchfake.New()
	backend.QueueSearch(search.Result{Total: search.Total{Relation: search.TotalExact}}, nil)
	index, err := search.NewIndex(backend, minimalDefinition(), search.WithLimits(search.Limits{ResultLimit: 1, ResultWindow: 10, BulkDocuments: 1, BulkBytes: 1}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.Search(context.Background(), search.Query{}); err != nil {
		t.Fatal(err)
	}
	if backend.Calls()[0].Query.Limit != 1 {
		t.Fatal("default page ignored configured limit")
	}
	first, err := search.DeleteMutation("one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := search.DeleteMutation("two")
	if err != nil {
		t.Fatal(err)
	}
	for _, batch := range [][]search.Mutation{{first, second}, {first}} {
		if _, err := index.Bulk(context.Background(), batch); !errors.Is(err, search.ErrInvalidDocument) {
			t.Fatal(err)
		}
	}
}

// TestExpiredBulkReceiptRetainsItemIdentity preserves reconciliation metadata after backend evidence expires.
func TestExpiredBulkReceiptRetainsItemIdentity(t *testing.T) {
	index, err := search.NewIndex(outcomeBackend{Backend: searchfake.New(), outcome: search.Outcome{State: search.OutcomeUnknown, Error: search.ErrReceiptExpired}}, minimalDefinition())
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := search.DeleteMutation("one")
	if err != nil {
		t.Fatal(err)
	}
	batch, err := index.Bulk(context.Background(), []search.Mutation{mutation})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := index.Wait(context.Background(), batch.Receipt)
	if err != nil || outcome.Bulk == nil || outcome.Bulk.Items[0].ID != "one" || outcome.Bulk.Items[0].Status != search.ItemUnknown || outcome.Bulk.Precision != search.BatchOnly {
		t.Fatal(outcome, err)
	}
}

// TestTypedHelpersHonorIndexLimits prevents standalone encoding defaults from overriding an Index's explicit policy.
func TestTypedHelpersHonorIndexLimits(t *testing.T) {
	backend := searchfake.New()
	index, err := search.NewIndex(backend, minimalDefinition(), search.WithLimits(search.Limits{DocumentBytes: 2 << 20, TextBytes: 2 << 20}))
	if err != nil {
		t.Fatal(err)
	}
	value := struct {
		Title string `json:"title"`
	}{Title: strings.Repeat("x", (1<<20)+16)}
	receipt, err := search.Upsert(context.Background(), index, "large", value)
	if err != nil {
		t.Fatal(err)
	}
	if outcome, err := index.Wait(context.Background(), receipt); err != nil || outcome.State != search.OutcomeSucceeded {
		t.Fatal(outcome, err)
	}
	document := backend.Calls()[0].Document
	backend.QueueSearch(search.Result{Hits: []search.Hit{{Document: document}}, Total: search.Total{Value: 1, Relation: search.TotalExact}}, nil)
	page, err := search.SearchAs[struct {
		Title string `json:"title"`
	}](context.Background(), index, search.Query{})
	if err != nil || len(page.Hits) != 1 || page.Hits[0].Value.Title != value.Title {
		t.Fatal("typed decode ignored configured document budget", err)
	}
	if _, err := search.Upsert(context.Background(), index, "invalid", func() {}); !errors.Is(err, search.ErrInvalidDocument) {
		t.Fatal(err)
	}
}

// TestBulkOutcomeIntegrity rejects terminal evidence that cannot be correlated to the submitted operations.
func TestBulkOutcomeIntegrity(t *testing.T) {
	for name, bulk := range map[string]*search.BulkOutcome{
		"missing-items":           {Precision: search.PerItem},
		"foreign-id":              {Precision: search.PerItem, Items: []search.ItemResult{{ID: "other", Status: search.ItemSucceeded}}},
		"wrong-position":          {Precision: search.PerItem, Items: []search.ItemResult{{Position: 1, ID: "one", Status: search.ItemSucceeded}}},
		"unknown-precision":       {Precision: "invalid", Items: []search.ItemResult{{ID: "one", Status: search.ItemSucceeded}}},
		"submission-status":       {Precision: search.PerItem, Items: []search.ItemResult{{ID: "one", Status: search.ItemAccepted}}},
		"invented-rejection":      {Precision: search.PerItem, Items: []search.ItemResult{{ID: "one", Status: search.ItemRejected}}},
		"invented-item-precision": {Precision: search.BatchOnly, Items: []search.ItemResult{{ID: "one", Status: search.ItemSucceeded}}},
		"inconsistent-success":    {Precision: search.PerItem, Items: []search.ItemResult{{ID: "one", Status: search.ItemUnknown}}},
	} {
		t.Run(name, func(t *testing.T) {
			index, err := search.NewIndex(outcomeBackend{Backend: searchfake.New(), outcome: search.Outcome{State: search.OutcomeSucceeded, Bulk: bulk}}, minimalDefinition())
			if err != nil {
				t.Fatal(err)
			}
			mutation, err := search.DeleteMutation("one")
			if err != nil {
				t.Fatal(err)
			}
			batch, err := index.Bulk(context.Background(), []search.Mutation{mutation})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := index.Wait(context.Background(), batch.Receipt); !errors.Is(err, search.ErrUnavailable) {
				t.Fatal(err)
			}
		})
	}
}

// TestWaitDoesNotMutateBackendEvidence keeps redaction and caching from writing through a backend-owned slice.
func TestWaitDoesNotMutateBackendEvidence(t *testing.T) {
	private := errors.New("private backend detail")
	bulk := &search.BulkOutcome{Precision: search.PerItem, Items: []search.ItemResult{{ID: "one", Status: search.ItemFailed, Error: private}}}
	index, err := search.NewIndex(outcomeBackend{Backend: searchfake.New(), outcome: search.Outcome{State: search.OutcomeFailed, Bulk: bulk}}, minimalDefinition())
	if err != nil {
		t.Fatal(err)
	}
	mutation, _ := search.DeleteMutation("one")
	batch, err := index.Bulk(context.Background(), []search.Mutation{mutation})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := index.Wait(context.Background(), batch.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	if bulk.Items[0].Error != private || outcome.Bulk.Items[0].Error == private {
		t.Fatal("backend evidence was changed or private error leaked")
	}
}

// uncertainBulkBackend retains submission uncertainty while supplying deliberately malformed terminal evidence.
type uncertainBulkBackend struct{ outcomeBackend }

// Bulk models a backend that acknowledged work before losing the complete response.
func (b uncertainBulkBackend) Bulk(ctx context.Context, definition search.Definition, mutations []search.Mutation) (search.BulkResult, error) {
	batch, err := b.Backend.Bulk(ctx, definition, mutations)
	if err != nil {
		return search.BulkResult{}, err
	}
	return search.BulkResult{}, search.NewUnknownOutcomeError(batch.Receipt)
}

// TestRecoveredBulkStillChecksIdentity correlates recovery receipts with their original input IDs.
func TestRecoveredBulkStillChecksIdentity(t *testing.T) {
	backend := uncertainBulkBackend{outcomeBackend{Backend: searchfake.New(), outcome: search.Outcome{State: search.OutcomeSucceeded, Bulk: &search.BulkOutcome{Precision: search.PerItem, Items: []search.ItemResult{{ID: "other", Status: search.ItemSucceeded}}}}}}
	index, err := search.NewIndex(backend, minimalDefinition())
	if err != nil {
		t.Fatal(err)
	}
	mutation, _ := search.DeleteMutation("one")
	_, err = index.Bulk(context.Background(), []search.Mutation{mutation})
	var unknown *search.UnknownOutcomeError
	if !errors.As(err, &unknown) {
		t.Fatal(err)
	}
	receipt, ok := unknown.Receipt()
	if !ok {
		t.Fatal("recovery receipt was lost")
	}
	if _, err := index.Wait(context.Background(), receipt); !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
}
