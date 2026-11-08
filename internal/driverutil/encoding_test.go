package driverutil

import (
	"errors"
	"testing"

	"github.com/goforj/search"
)

// TestHitRejectsCorruptStoredTypes prevents backend corruption from reaching unsafe highlight or facet assertions.
func TestHitRejectsCorruptStoredTypes(t *testing.T) {
	definition := search.Definition{Name: "records", Fields: []search.Field{
		{Name: "title", Path: "/title", Type: search.TextField, Searchable: true},
		{Name: "tags", Path: "/tags", Type: search.KeywordsField},
		{Name: "code", Path: "/code", Type: search.KeywordField},
		{Name: "active", Path: "/active", Type: search.BooleanField},
		{Name: "rank", Path: "/rank", Type: search.IntegerField, Required: true, Sortable: true},
	}}
	query := search.Query{Highlight: []search.Highlight{{Field: "title"}}, Sort: []search.Sort{{Field: "rank", Direction: search.Ascending}}}
	for _, body := range []string{`null`, `[]`, `{}`, `{"rank":0,"title":7}`, `{"rank":0,"code":7}`, `{"rank":0,"tags":"wrong"}`, `{"rank":0,"tags":[1]}`, `{"rank":0,"active":"true"}`, `{"rank":"0"}`, `{"rank":0.5}`, `{"rank":9007199254740992}`, `{"rank":-9007199254740992}`} {
		if _, err := Hit("one", body, definition, query, nil); !errors.Is(err, search.ErrUnavailable) {
			t.Fatal(body, err)
		}
	}
	for _, body := range []string{`{"rank":0}`, `{"rank":0,"title":"blue mug","tags":[],"active":true}`, `{"rank":0,"title":null,"tags":null}`} {
		if _, err := Hit("one", body, definition, query, nil); err != nil {
			t.Fatal(body, err)
		}
	}
}

// TestLedgerOwnsSubmittedOutcome keeps caller mutation from changing retained terminal evidence.
func TestLedgerOwnsSubmittedOutcome(t *testing.T) {
	ledger := &Ledger{}
	outcome := search.Outcome{State: search.OutcomeSucceeded, Bulk: &search.BulkOutcome{Precision: search.PerItem, Items: []search.ItemResult{{ID: "one", Status: search.ItemSucceeded}}}}
	receipt, err := ledger.Complete(outcome)
	if err != nil {
		t.Fatal(err)
	}
	outcome.Bulk.Items[0].ID = "changed"
	retained, err := ledger.Wait(t.Context(), receipt)
	if err != nil || retained.Bulk.Items[0].ID != "one" {
		t.Fatal(retained, err)
	}
}

// TestCorruptFacetUTF8IsBackendFailure keeps corrupt persisted terms from being classified as invalid caller input.
func TestCorruptFacetUTF8IsBackendFailure(t *testing.T) {
	if _, err := DecodeScalar("vff", search.KeywordField); !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
}
