package search_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/goforj/search"
	"github.com/goforj/search/searchfake"
)

// minimalDefinition keeps API tests independent of any backend implementation.
func minimalDefinition() search.Definition {
	return search.Definition{Name: "items", Fields: []search.Field{{Name: "title", Type: search.TextField, Searchable: true}}}
}

// TestIndexReceiptOwnershipAndCancellation rejects forged tokens before backend inspection.
func TestIndexReceiptOwnershipAndCancellation(t *testing.T) {
	backend := searchfake.New()
	first, err := search.NewIndex(backend, minimalDefinition())
	if err != nil {
		t.Fatal(err)
	}
	second, err := search.NewIndex(backend, minimalDefinition())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	r, err := search.Upsert(ctx, first, "one", struct {
		Title string `json:"title"`
	}{"red"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = second.Wait(ctx, r); !errors.Is(err, search.ErrInvalidReceipt) {
		t.Fatal(err)
	}
	forged, err := search.NewReceipt(r.Token())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = first.Wait(ctx, forged); !errors.Is(err, search.ErrInvalidReceipt) {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, err := first.Wait(ctx, r)
			if err != nil || o.State != search.OutcomeSucceeded || o.VisibleAt.IsZero() {
				t.Error(o, err)
			}
		}()
	}
	wg.Wait()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := first.Wait(canceled, r); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := first.Search(canceled, search.Query{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := first.Delete(canceled, "one"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := first.Upsert(canceled, search.Document{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := first.Bulk(canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// TestIndexValidationNeverCallsBackend proves invalid input cannot trigger side effects.
func TestIndexValidationNeverCallsBackend(t *testing.T) {
	backend := searchfake.New()
	idx, err := search.NewIndex(backend, minimalDefinition())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := idx.Search(ctx, search.Query{Limit: 101}); err == nil {
		t.Fatal("invalid query accepted")
	}
	if _, err := idx.Upsert(ctx, search.Document{ID: "one", Body: json.RawMessage(`[]`)}); err == nil {
		t.Fatal("invalid document accepted")
	}
	if _, err := idx.Delete(ctx, ""); err == nil {
		t.Fatal("empty ID accepted")
	}
	d, err := search.Encode("one", map[string]string{"title": "red"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := search.UpsertMutation(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutations := range [][]search.Mutation{nil, {m, m}, {{}}} {
		if _, err := idx.Bulk(ctx, mutations); err == nil {
			t.Fatal("invalid batch accepted")
		}
	}
	if len(backend.Calls()) != 0 {
		t.Fatal("validation called backend")
	}
}

// TestIndexOwnsDefinition prevents mutation of caller declarations and returned copies.
func TestIndexOwnsDefinition(t *testing.T) {
	d := minimalDefinition()
	idx, err := search.NewIndex(searchfake.New(), d)
	if err != nil {
		t.Fatal(err)
	}
	d.Fields[0].Name = "changed"
	owned := idx.Definition()
	owned.Fields[0].Name = "also_changed"
	if idx.Definition().Fields[0].Name != "title" {
		t.Fatal("aliased definition")
	}
}

// TestIndexTypedResultsAndErrorRedaction keeps backend errors and malformed typed values out of public responses.
func TestIndexTypedResultsAndErrorRedaction(t *testing.T) {
	backend := searchfake.New()
	idx, err := search.NewIndex(backend, minimalDefinition())
	if err != nil {
		t.Fatal(err)
	}
	backend.QueueSearch(search.Result{}, fmt.Errorf("secret-token query-content: %w", search.ErrUnauthorized))
	if _, err := idx.Search(context.Background(), search.Query{}); err != search.ErrUnauthorized {
		t.Fatal(err)
	}
	backend.QueueSearch(search.Result{Hits: []search.Hit{{Document: search.Document{ID: "one", Body: json.RawMessage(`{"title":"red"}`)}}}, Total: search.Total{Value: 1, Relation: search.TotalExact}}, nil)
	page, err := search.SearchAs[struct {
		Title string `json:"title"`
	}](context.Background(), idx, search.Query{})
	if err != nil || len(page.Hits) != 1 || page.Hits[0].Value.Title != "red" || page.Hits[0].ID != "one" {
		t.Fatal(page, err)
	}
	backend.QueueSearch(search.Result{Hits: []search.Hit{{Document: search.Document{ID: "one", Body: json.RawMessage(`{"title":"red"}`)}}}, Total: search.Total{Value: 1, Relation: search.TotalExact}}, nil)
	_, err = search.SearchAs[struct {
		Title int `json:"title"`
	}](context.Background(), idx, search.Query{})
	var decode *search.DecodeError
	if !errors.As(err, &decode) || decode.ID != "one" || !errors.Is(err, search.ErrInvalidDocument) {
		t.Fatal(err)
	}
	for _, err := range []error{errors.New("password"), fmt.Errorf("password: %w", search.ErrUnavailable)} {
		if strings.Contains(fmt.Sprintf("%+v", search.SafeError(err)), "password") {
			t.Fatal("backend prose leaked")
		}
	}
}

// TestObserverIsolation proves synchronous panics cannot alter accepted mutation outcomes.
func TestObserverIsolation(t *testing.T) {
	var events []search.Event
	observer := search.MultiObserver{
		search.ObserverFunc(func(context.Context, search.Event) { panic("private document") }),
		search.ObserverFunc(func(_ context.Context, event search.Event) { events = append(events, event) }),
	}
	idx, err := search.NewIndex(searchfake.New(), minimalDefinition(), search.WithObserver(observer))
	if err != nil {
		t.Fatal(err)
	}
	r, err := search.Upsert(context.Background(), idx, "one", map[string]string{"title": "private document"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := idx.Wait(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Phase != search.PhaseSubmission || events[1].Phase != search.PhaseTerminal || events[0].ReceiptID != events[1].ReceiptID {
		t.Fatal(events)
	}
	if strings.Contains(fmt.Sprintf("%+v", events), "private document") {
		t.Fatal("content leaked to observer")
	}
}

// TestBulkOutcomeOwnership keeps a caller's changes from corrupting retained evidence.
func TestBulkOutcomeOwnership(t *testing.T) {
	idx, err := search.NewIndex(searchfake.New(), minimalDefinition())
	if err != nil {
		t.Fatal(err)
	}
	m, err := search.DeleteMutation("one")
	if err != nil {
		t.Fatal(err)
	}
	r, err := idx.Bulk(context.Background(), []search.Mutation{m})
	if err != nil {
		t.Fatal(err)
	}
	first, err := idx.Wait(context.Background(), r.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	first.Bulk.Items[0].ID = "changed"
	second, err := idx.Wait(context.Background(), r.Receipt)
	if err != nil || second.Bulk.Items[0].ID != "one" {
		t.Fatal(second, err)
	}
}
